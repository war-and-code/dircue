package declarations

import (
	"path"
	"slices"
	"strings"
)

// Cargo's selected-inventory resolver has a shared work budget across all roots.
// It bounds repeated workspace-pattern and dependency visits, independently of
// the number of observations that survive the per-manifest output budget.
const cargoMaxResolutionWork = 1 << 20

type cargoWork struct{ remaining int }

func (w *cargoWork) use(d *Document) bool {
	if w.remaining <= 0 {
		AddDiagnostic(d, "cargo-resolution-limit", "Cargo workspace resolution reached its inventory work limit.")
		return false
	}
	w.remaining--
	return true
}

// ResolveCargo links Cargo declarations only to manifests in the selected source.
// Workspace membership does not assert feature resolution or successful builds.
func ResolveCargo(docs []*Document, files map[string]bool) {
	index := map[string]*Document{}
	roots := []*Document{}
	packages := []*Document{}
	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		data, ok := d.Data.(*cargoData)
		if !ok {
			continue
		}
		index[d.Project.ID] = d
		if data.workspace {
			roots = append(roots, d)
		}
		if data.packagePresent {
			packages = append(packages, d)
		}
	}
	slices.SortFunc(roots, func(a, b *Document) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	slices.SortFunc(packages, func(a, b *Document) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	work := &cargoWork{remaining: cargoMaxResolutionWork}
	// Each manifest can be claimed by more than one root. Such claims remain
	// ambiguous and cannot supply inherited declarations.
	claims := map[string][]*Document{}
	for _, root := range roots {
		data := root.Data.(*cargoData)
		if !data.usable {
			AddDiagnostic(root, "cargo-workspace-selection-unresolved", "Unsupported or capped workspace selection prevents membership and inheritance resolution.")
			continue
		}
		if data.incomplete {
			AddDiagnostic(root, "cargo-workspace-selection-unresolved", "Some workspace member declarations are unsupported; valid sibling members were retained but inheritance remains unresolved.")
		}
		members := map[string]bool{}
		add := func(member *Document, kind, value string) bool {
			if member == nil {
				return false
			}
			md := member.Data.(*cargoData)
			if !md.packagePresent || member.Project.Name == "" {
				AddDiagnostic(root, "cargo-member-not-package", "A Cargo workspace member points to a virtual workspace or invalid package.")
				return false
			}
			if member != root && md.workspace {
				AddDiagnostic(root, "cargo-nested-workspace", "A declared Cargo workspace member defines another workspace root.")
				return false
			}
			if md.workspacePathSet {
				target, ok := LocalTarget(member.Project.ID, md.workspacePath, "Cargo.toml")
				if !ok || target != root.Project.ID {
					AddDiagnostic(root, "cargo-workspace-conflict", "A declared member points to a different or unavailable workspace root.")
					return false
				}
			}
			if members[member.Project.ID] {
				return false
			}
			members[member.Project.ID] = true
			claims[member.Project.ID] = append(claims[member.Project.ID], root)
			AddReference(root, Reference{Kind: kind, Value: value, Target: member.Project.ID, TargetStatus: "present", State: "declared", Evidence: root.Project.ID})
			return true
		}
		if data.packagePresent {
			add(root, "cargo-workspace-member", ".")
		}
		for _, pattern := range data.excludes {
			cargoPatternReferences(root, pattern, "cargo-workspace-exclude", packages, files, work, nil)
		}
		for _, pattern := range data.members {
			cargoPatternReferences(root, pattern, "cargo-workspace-member", packages, files, work, func(member *Document) bool {
				if cargoExcluded(root, member, data.excludes, work) {
					return false
				}
				return add(member, "cargo-workspace-member", pattern)
			})
		}
		queue := make([]string, 0, len(members))
		for id := range members {
			queue = append(queue, id)
		}
		slices.Sort(queue)
		for pos := 0; pos < len(queue); pos++ {
			if !work.use(root) {
				break
			}
			member := index[queue[pos]]
			md := member.Data.(*cargoData)
			for _, raw := range md.dependencies {
				if !work.use(root) {
					break
				}
				dep, evidence, ok := cargoEffectiveDependency(raw, root)
				if !ok || dep.localPath == "" {
					continue
				}
				target, ok := LocalTarget(evidence, dep.localPath, "Cargo.toml")
				if !ok {
					continue
				}
				candidate := index[target]
				if candidate == nil || !cargoWithin(root.Project.Root, candidate.Project.Root) || cargoExcluded(root, candidate, data.excludes, work) {
					continue
				}
				if add(candidate, "cargo-workspace-path-member", cargoSafeRelative(root.Project.Root, candidate.Project.Root)) {
					queue = append(queue, target)
				}
			}
		}
		for _, alias := range cargoDependencyKeys(data.workspaceDependencies) {
			dep := data.workspaceDependencies[alias]
			if !work.use(root) {
				break
			}
			if !dep.valid {
				continue
			}
			value := dep.alias
			if dep.name != dep.alias {
				value += " (package=" + dep.name + ")"
			}
			if dep.version != "" {
				value += " " + dep.version
			}
			if !AddRequirement(root, Requirement{Kind: "cargo-workspace-dependency", Value: value, State: "declared", Evidence: root.Project.ID, Condition: cargoDependencyCondition(dep) + "; source=" + dep.source}) {
				break
			}
			if dep.localPath != "" {
				ref := cargoLocalReference(root.Project.ID, "cargo-workspace-path-dependency", dep.localPath, "Cargo.toml", files)
				ref.Condition = cargoDependencyCondition(dep)
				AddReference(root, ref)
			}
		}
		if data.defaultsSet {
			for _, pattern := range data.defaults {
				cargoPatternReferences(root, pattern, "cargo-default-member", packages, files, work, func(member *Document) bool {
					if !members[member.Project.ID] {
						AddDiagnostic(root, "cargo-default-not-member", "A default-member declaration does not identify a supported workspace member.")
						return false
					}
					AddReference(root, Reference{Kind: "cargo-default-member", Value: pattern, Target: member.Project.ID, TargetStatus: "present", State: "declared", Evidence: root.Project.ID})
					return true
				})
			}
		} else {
			value := "workspace-members"
			if data.packagePresent {
				value = "root-package"
			}
			AddRequirement(root, Requirement{Kind: "cargo-default-selection", Value: value, State: "declared", Evidence: root.Project.ID})
		}
	}
	for _, d := range packages {
		if !work.use(d) {
			continue
		}
		data := d.Data.(*cargoData)
		owners := claims[d.Project.ID]
		var owner *Document
		if len(owners) == 1 {
			candidate := owners[0]
			if candidate.Data.(*cargoData).incomplete {
				AddDiagnostic(d, "cargo-inheritance-unresolved", "Workspace membership is observed, but incomplete member selection prevents confident inheritance.")
			} else {
				owner = candidate
			}
		} else if len(owners) > 1 {
			AddDiagnostic(d, "cargo-ambiguous-workspace", "Multiple workspace roots claim this package; inherited declarations remain unresolved.")
		}
		if data.workspacePathSet {
			ref := cargoLocalReference(d.Project.ID, "cargo-workspace-root", data.workspacePath, "Cargo.toml", files)
			AddReference(d, ref)
			if owner == nil && ref.Target != "" {
				AddDiagnostic(d, "cargo-workspace-membership-unresolved", "The package workspace pointer does not establish membership in a selected workspace.")
			}
		} else if owner == nil && !data.workspace {
			// Cargo searches parents, but a nearby workspace alone does not make this
			// package a member. Preserve this incompatibility rather than inheriting.
			for dir := path.Dir(d.Project.Root); d.Project.Root != "."; dir = path.Dir(dir) {
				root := index[path.Join(dir, "Cargo.toml")]
				if root != nil && root.Data.(*cargoData).workspace && root.Data.(*cargoData).usable && !root.Data.(*cargoData).incomplete {
					if !cargoExcluded(root, d, root.Data.(*cargoData).excludes, work) {
						AddDiagnostic(d, "cargo-unlisted-nested-package", "A package beneath a workspace is not among its supported selected members.")
					}
					break
				}
				if dir == "." {
					break
				}
			}
		}
		for _, key := range []string{"version", "edition", "rust-version"} {
			if !data.inherited[key] {
				continue
			}
			value := "workspace"
			state := "unresolved"
			evidence := d.Project.ID
			if owner != nil {
				if inherited, ok := owner.Data.(*cargoData).workspaceValues[key]; ok {
					value = inherited
					state = "declared"
					evidence = owner.Project.ID
					if key == "version" {
						d.Project.Version = value
					}
				}
			}
			if state == "unresolved" {
				AddDiagnostic(d, "cargo-inheritance-unresolved", "A Cargo package field cannot be inherited from a unique selected workspace definition.")
			}
			AddRequirement(d, Requirement{Kind: "cargo-" + key, Value: value, State: state, Evidence: evidence, Condition: "workspace inheritance declared in " + d.Project.ID})
		}
		for _, raw := range data.dependencies {
			if !work.use(d) {
				break
			}
			dep, evidence, ok := cargoEffectiveDependency(raw, owner)
			if !ok {
				if raw.valid {
					AddRequirement(d, Requirement{Kind: "cargo-dependency", Value: raw.alias, State: "unresolved", Evidence: d.Project.ID, Condition: raw.scope})
					AddDiagnostic(d, "cargo-dependency-inheritance-unresolved", "A dependency cannot be inherited from a unique selected workspace definition.")
				}
				continue
			}
			condition := cargoDependencyCondition(dep)
			state := "declared"
			if dep.optional || dep.selector != "" || dep.scope != "dependencies" {
				state = "conditional"
			}
			value := dep.alias
			if dep.name != dep.alias {
				value += " (package=" + dep.name + ")"
			}
			if dep.version != "" {
				value += " " + dep.version
			}
			if !AddRequirement(d, Requirement{Kind: "cargo-dependency", Value: value, State: state, Evidence: evidence, Condition: condition + "; source=" + dep.source}) {
				break
			}
			if dep.localPath != "" {
				ref := cargoLocalReference(evidence, "cargo-path-dependency", dep.localPath, "Cargo.toml", files)
				ref.Condition = condition
				if state == "conditional" && ref.State != "unresolved" {
					ref.State = "conditional"
				}
				AddReference(d, ref)
				if target := index[ref.Target]; target != nil && target.Project.Name != "" && target.Project.Name != dep.name {
					AddDiagnostic(d, "cargo-dependency-name-mismatch", "A local dependency target declares a different package name.")
				}
			}
		}
		for i := range d.Project.Interfaces {
			iface := &d.Project.Interfaces[i]
			if iface.Target != "" && !files[iface.Target] {
				iface.State = "unresolved"
				AddDiagnostic(d, "cargo-target-unavailable", "A declared Cargo target path is absent from the selected inventory.")
			}
		}
		cargoBuildInterface(d, data, files)
	}
	// Avoid presenting ambiguous claims as definite memberships.
	for _, root := range roots {
		for i := range root.Project.References {
			ref := &root.Project.References[i]
			if strings.HasPrefix(ref.Kind, "cargo-workspace-") && len(claims[ref.Target]) > 1 {
				ref.State = "unresolved"
				AddDiagnostic(root, "cargo-ambiguous-workspace", "A selected package is claimed by multiple workspace roots.")
			}
		}
	}
}

func cargoPatternReferences(root *Document, pattern, kind string, packages []*Document, files map[string]bool, work *cargoWork, accept func(*Document) bool) {
	matched := false
	for _, member := range packages {
		if !work.use(root) {
			return
		}
		if !cargoPatternMatches(root.Project.ID, pattern, member.Project.ID) {
			continue
		}
		matched = true
		if accept != nil {
			accept(member)
			continue
		}
		if !AddReference(root, Reference{Kind: kind, Value: pattern, Target: member.Project.ID, State: "declared", TargetStatus: "present", Evidence: root.Project.ID}) {
			return
		}
	}
	if !matched {
		if strings.ContainsAny(pattern, "*?[") {
			AddReference(root, Reference{Kind: kind, Value: pattern, State: "unresolved", TargetStatus: "unresolved", Evidence: root.Project.ID})
			if kind != "cargo-workspace-exclude" {
				AddDiagnostic(root, "cargo-workspace-pattern-unmatched", "A Cargo workspace pattern matched no selected package manifest.")
			}
		} else {
			AddReference(root, cargoLocalReference(root.Project.ID, kind, pattern, "Cargo.toml", files))
			if kind != "cargo-workspace-exclude" {
				AddDiagnostic(root, "cargo-workspace-member-unavailable", "A Cargo workspace declaration has no selected parsed package target.")
			}
		}
	}
}
func cargoPatternMatches(manifest, pattern, candidate string) bool {
	target, ok := LocalTarget(manifest, pattern, "Cargo.toml")
	if !ok {
		return false
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return target == candidate
	}
	pattern = path.Join(path.Dir(manifest), pattern)
	matched, err := MatchPattern(pattern, path.Dir(candidate))
	return err == nil && matched
}
func cargoExcluded(root, member *Document, patterns []string, work *cargoWork) bool {
	for _, pattern := range patterns {
		if !work.use(root) {
			return true
		}
		if cargoPatternMatches(root.Project.ID, pattern, member.Project.ID) {
			return true
		}
	}
	return false
}
func cargoWithin(root, candidate string) bool {
	return root == "." || candidate == root || strings.HasPrefix(candidate, root+"/")
}
func cargoSafeRelative(root, candidate string) string {
	if root == candidate {
		return "."
	}
	if root == "." {
		return candidate
	}
	return strings.TrimPrefix(candidate, root+"/")
}
func cargoLocalReference(manifest, kind, raw, basename string, files map[string]bool) Reference {
	ref := Reference{Kind: kind, Value: raw, State: "declared", TargetStatus: "missing", Evidence: manifest}
	target, ok := LocalTarget(manifest, raw, basename)
	if !ok {
		ref.Value = "[outside-selected-root]"
		ref.State = "unresolved"
		ref.TargetStatus = "external"
		return ref
	}
	ref.Target = target
	if files[target] {
		ref.TargetStatus = "present"
	}
	return ref
}
func cargoEffectiveDependency(raw cargoDependency, owner *Document) (cargoDependency, string, bool) {
	if !raw.valid {
		return raw, "", false
	}
	if !raw.inherited {
		return raw, raw.manifest, true
	}
	if owner == nil {
		return raw, "", false
	}
	inherited, ok := owner.Data.(*cargoData).workspaceDependencies[raw.alias]
	if !ok || !inherited.valid {
		return raw, "", false
	}
	inherited.scope = raw.scope
	inherited.selector = raw.selector
	inherited.optional = raw.optional
	inherited.features = append(append([]string{}, inherited.features...), raw.features...)
	return inherited, owner.Project.ID, true
}
func cargoDependencyCondition(dep cargoDependency) string {
	parts := []string{dep.scope}
	if dep.selector != "" {
		parts = append(parts, "target="+dep.selector)
	}
	if dep.optional {
		parts = append(parts, "optional=true")
	}
	if dep.defaultFeatures != nil {
		value := "false"
		if *dep.defaultFeatures {
			value = "true"
		}
		parts = append(parts, "default-features="+value)
	}
	if len(dep.features) > 0 {
		parts = append(parts, "features="+strings.Join(dep.features, ","))
	}
	return strings.Join(parts, "; ")
}
func cargoBuildInterface(d *Document, data *cargoData, files map[string]bool) {
	if enabled, ok := data.build.(bool); ok && !enabled {
		AddRequirement(d, Requirement{Kind: "cargo-build-script", Value: "disabled", State: "declared", Evidence: d.Project.ID})
		return
	}
	raw, explicit := data.build.(string)
	if !explicit {
		raw = "build.rs"
	}
	target, ok := LocalTarget(d.Project.ID, raw, "")
	if !ok {
		AddInterface(d, Interface{Kind: "cargo-build-script", Name: "build", State: "unresolved", Evidence: d.Project.ID})
		AddDiagnostic(d, "cargo-external-build-script", "A Cargo build script path lies outside the selected inventory.")
		return
	}
	if explicit || files[target] {
		state := "candidate"
		if explicit {
			state = "declared"
		}
		if !files[target] {
			state = "unresolved"
			AddDiagnostic(d, "cargo-build-script-unavailable", "A declared Cargo build script is absent from the selected inventory.")
		}
		AddInterface(d, Interface{Kind: "cargo-build-script", Name: "build", Target: target, State: state, Evidence: d.Project.ID})
	}
}

func cargoDependencyKeys(deps map[string]cargoDependency) []string {
	keys := make([]string, 0, len(deps))
	for key := range deps {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
