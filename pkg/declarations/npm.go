package declarations

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type npmDeclarationData struct {
	missingName bool
	valid       bool
	workspace   bool
	patterns    []string
	exclusions  []string
	usable      bool
}

// ParseNPM reads package declarations and executable names. Command bodies,
// repository metadata, registry configuration and dependency URLs are omitted.
func ParseNPM(name string, content []byte) *Document {
	if path.Base(name) != "package.json" || npmInstalledPath(name) {
		return nil
	}
	d := NewDocument(name, "npm")
	data := &npmDeclarationData{usable: true}
	d.Data = data
	object, err := ValidateJSON(content)
	if err != nil {
		d.Parsed = false
		AddDiagnostic(d, "invalid-npm-manifest", "package.json must be a bounded JSON object without duplicate keys.")
		return d
	}
	data.valid = true
	rawName, nameExists := object["name"]
	nameText, nameIsString := rawName.(string)
	data.missingName = !nameExists || nameIsString && nameText == ""
	npmRequirement(d, "declaration-format", "npm-v11-static-subset", "")
	if value, ok := npmStringField(d, object, "name"); ok && value != "" {
		if npmPackageName(value) && npmRequirement(d, "npm-package", value, "") {
			d.Project.Name = value
		} else {
			AddDiagnostic(d, "unsupported-npm-name", "Package identity is outside the supported name syntax.")
		}
	}
	if value, ok := npmStringField(d, object, "version"); ok {
		if npmRange(value) && npmRequirement(d, "npm-version", value, "") {
			d.Project.Version = value
		} else {
			AddDiagnostic(d, "unsupported-npm-version", "Package version is outside the supported declaration syntax.")
		}
	}
	if value, present := object["private"]; present {
		if value, ok := value.(bool); ok {
			npmRequirement(d, "npm-private", strconv.FormatBool(value), "")
		} else {
			AddDiagnostic(d, "invalid-npm-field", "The private declaration must be boolean.")
		}
	}
	if value, ok := npmStringField(d, object, "packageManager"); ok {
		manager, version, valid := strings.Cut(value, "@")
		if valid && npmIdentifier(manager) && npmRange(version) {
			npmRequirement(d, "package-manager", value, "")
			AddInterface(d, Interface{Kind: "prerequisite", Name: manager, Target: version, State: "declared", Evidence: name})
			if manager != "npm" {
				AddDiagnostic(d, "unsupported-package-manager-semantics", "Package-manager identity is retained; npm declarations do not establish other package-manager workspace or resolution semantics.")
			}
		} else {
			AddDiagnostic(d, "unsupported-package-manager", "Package-manager declaration is not a supported name and version; its raw value was omitted.")
		}
	}
	if engines, ok := npmObjectField(d, object, "engines"); ok {
		for _, name := range npmKeys(engines) {
			value, ok := engines[name].(string)
			if !ok || !npmIdentifier(name) || !npmRange(value) {
				AddDiagnostic(d, "unsupported-npm-engine", "An engine declaration is outside the supported name and version-range syntax.")
				continue
			}
			npmRequirement(d, "npm-engine", name+" "+value, "")
			AddInterface(d, Interface{Kind: "prerequisite", Name: name, Target: value, State: "declared", Evidence: d.Project.ID, Condition: "engine range declaration"})
		}
	}
	npmWorkspacePatterns(d, data, object)
	for _, field := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		if dependencies, ok := npmObjectField(d, object, field); ok {
			for _, dependency := range npmKeys(dependencies) {
				value, ok := dependencies[dependency].(string)
				if !ok || !npmPackageName(dependency) {
					AddDiagnostic(d, "invalid-npm-dependency", "A dependency name or declaration has unsupported syntax.")
					continue
				}
				npmDependency(d, dependency, value, field)
			}
		}
	}
	npmInterfaces(d, object)
	for _, field := range []string{"pnpm", "resolutions", "installConfig", "devEngines", "overrides", "bundleDependencies", "bundledDependencies"} {
		if _, ok := object[field]; ok {
			AddDiagnostic(d, "unsupported-npm-field", "The "+field+" declaration is not evaluated by this npm adapter.")
		}
	}
	return d
}

func npmRequirement(d *Document, kind, value, condition string) bool {
	return AddRequirement(d, Requirement{Kind: kind, Value: value, State: "declared", Evidence: d.Project.ID, Condition: condition})
}

func npmKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func npmStringField(d *Document, object map[string]any, field string) (string, bool) {
	v, present := object[field]
	if !present {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		AddDiagnostic(d, "invalid-npm-field", "The "+field+" declaration must be a string.")
	}
	return s, ok
}

func npmObjectField(d *Document, object map[string]any, field string) (map[string]any, bool) {
	v, present := object[field]
	if !present {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		AddDiagnostic(d, "invalid-npm-field", "The "+field+" declaration must be an object.")
	}
	return m, ok
}

func npmIdentifier(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.", r)) {
			return false
		}
	}
	return true
}

func npmPackageName(value string) bool {
	if len(value) > 214 {
		return false
	}
	if strings.HasPrefix(value, "@") {
		scope, name, ok := strings.Cut(value[1:], "/")
		return ok && npmIdentifier(scope) && npmIdentifier(name)
	}
	return npmIdentifier(value)
}

// This accepts declaration text, not semver validity. URL and protocol syntax
// is deliberately absent so credentials cannot enter a version observation.
func npmRange(value string) bool {
	if len(value) > 1024 || value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".-_+*^~<>=| ", r)) {
			return false
		}
	}
	return true
}

func npmDependency(d *Document, name, value, section string) {
	ref := Reference{Kind: "npm-dependency", Value: name, State: "declared", TargetStatus: "external", Evidence: d.Project.ID, Condition: section}
	if strings.HasPrefix(value, "file:") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		raw := strings.TrimPrefix(value, "file:")
		ref.Kind = "npm-local-dependency"
		if target, ok := LocalTarget(d.Project.ID, raw, "package.json"); ok && !strings.HasSuffix(strings.ToLower(raw), ".tgz") && !strings.HasSuffix(strings.ToLower(raw), ".tar.gz") {
			ref.Value += " file:" + raw
			ref.Target = target
			ref.TargetStatus = ""
		} else {
			ref.State = "unresolved"
			AddDiagnostic(d, "external-npm-dependency", "A local dependency is outside the selected inventory or refers to an unsupported archive.")
		}
	} else if value == "workspace:*" || value == "workspace:^" || value == "workspace:~" {
		ref.Kind = "npm-workspace-dependency"
		ref.State = "unresolved"
		ref.TargetStatus = "unresolved"
	} else if strings.HasPrefix(value, "workspace:") {
		ref.State = "unresolved"
		AddDiagnostic(d, "unsupported-npm-workspace-dependency", "A workspace dependency uses an unsupported path, alias, or version constraint; its raw value was omitted.")
	} else if npmRange(value) {
		ref.Value += "@" + value
	} else {
		ref.State = "unresolved"
		AddDiagnostic(d, "unsupported-npm-dependency", "A dependency uses URL, alias, link, portal, catalog, or other unsupported resolution syntax; its raw value was omitted.")
	}
	AddReference(d, ref)
}

func npmWorkspacePatterns(d *Document, data *npmDeclarationData, object map[string]any) {
	v, found := object["workspaces"]
	if !found {
		return
	}
	data.workspace = true
	npmRequirement(d, "npm-workspace-root", "declared", "")
	if obj, ok := v.(map[string]any); ok {
		v = obj["packages"]
		for key := range obj {
			if key != "packages" {
				AddDiagnostic(d, "unsupported-npm-workspace-field", "Workspace object fields other than packages are not interpreted.")
			}
		}
	}
	patterns, ok := v.([]any)
	if !ok || len(patterns) > MaxPatterns {
		data.usable = false
		AddDiagnostic(d, "unsupported-npm-workspaces", "Workspaces must be a bounded array of path patterns or an object containing that array in packages.")
		return
	}
	for _, p := range patterns {
		pattern, ok := p.(string)
		if !ok || strings.Contains(pattern, ":") || strings.IndexFunc(pattern, unicode.IsControl) >= 0 {
			data.usable = false
			AddDiagnostic(d, "unsupported-npm-workspace-pattern", "Workspace patterns must use the supported bounded path glob syntax.")
			continue
		}
		negations := len(pattern) - len(strings.TrimLeft(pattern, "!"))
		pattern = strings.TrimPrefix(pattern[negations:], "./")
		if _, err := MatchPattern(pattern, "candidate"); err != nil {
			data.usable = false
			AddDiagnostic(d, "unsupported-npm-workspace-pattern", "Workspace patterns must use the supported bounded path glob syntax.")
			continue
		}
		if negations%2 == 1 {
			npmRequirement(d, "npm-workspace-exclusion", pattern, "")
			data.exclusions = append(data.exclusions, pattern)
			continue
		}
		npmRequirement(d, "npm-workspace-pattern", pattern, "")
		// npm discards earlier exclusions matched by a later positive pattern.
		// Matching the pattern text here follows npm/map-workspaces; it is not
		// equivalent to simply applying exclusions after every inclusion.
		kept := data.exclusions[:0]
		for _, excluded := range data.exclusions {
			if matched, _ := npmMatch(excluded, pattern); !matched {
				kept = append(kept, excluded)
			}
		}
		data.exclusions = kept
		data.patterns = append(data.patterns, pattern)
	}
	kept := data.patterns[:0]
	for _, pattern := range data.patterns {
		excluded := false
		for _, exclusion := range data.exclusions {
			if matched, _ := npmMatch(exclusion, pattern); matched {
				excluded = true
				break
			}
		}
		if !excluded {
			kept = append(kept, pattern)
		}
	}
	data.patterns = kept
}

func npmInterfaces(d *Document, object map[string]any) {
	if scripts, ok := npmObjectField(d, object, "scripts"); ok {
		// Only conventional entry-point scripts (start, serve) are modeled as
		// interfaces. Developer tasks (build, test, lint, format, …) are not
		// application entry points; emitting them inflates interface counts
		// with noise. The raw command text is never retained regardless.
		entryPointScripts := map[string]bool{"start": true, "serve": true}
		for _, name := range npmKeys(scripts) {
			_, isString := scripts[name].(string)
			if !isString || name == "" || len(name) > 256 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				AddDiagnostic(d, "invalid-npm-script", "A script must have a bounded name and a string command; command text is never retained.")
				continue
			}
			if entryPointScripts[name] {
				AddInterface(d, Interface{Kind: "script", Name: name, State: "declared", Evidence: d.Project.ID})
			}
		}
	}
	if bins, ok := object["bin"]; ok {
		switch bins := bins.(type) {
		case string:
			if d.Project.Name == "" {
				AddDiagnostic(d, "unnamed-npm-bin", "A string bin declaration has no supported package name for its executable name.")
			} else {
				npmBin(d, path.Base(d.Project.Name), bins)
			}
		case map[string]any:
			for _, name := range npmKeys(bins) {
				value, ok := bins[name].(string)
				if !ok {
					AddDiagnostic(d, "invalid-npm-bin", "Executable targets must be strings.")
					continue
				}
				npmBin(d, name, value)
			}
		default:
			AddDiagnostic(d, "invalid-npm-bin", "The bin declaration must be a string or an object.")
		}
	}
	if directories, ok := object["directories"].(map[string]any); ok {
		if _, found := directories["bin"]; found {
			AddDiagnostic(d, "unsupported-npm-bin-directory", "Directory-based executable discovery is not performed.")
		}
	}
}

func npmBin(d *Document, name, raw string) {
	if !npmIdentifier(name) {
		AddDiagnostic(d, "invalid-npm-bin", "An executable name has unsupported syntax.")
		return
	}
	value := Interface{Kind: "entrypoint", Name: name, State: "declared", Evidence: d.Project.ID}
	if target, ok := LocalTarget(d.Project.ID, raw, ""); ok {
		value.Target = target
	} else {
		value.State = "unresolved"
		AddDiagnostic(d, "external-npm-bin", "An executable target is outside the selected inventory or has unsupported path syntax.")
	}
	AddInterface(d, value)
}

func npmInstalledPath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "node_modules" {
			return true
		}
	}
	return false
}

// npmMatch adds npm's default hidden-directory behavior to the shared subset.
func npmMatch(pattern, relative string) (bool, error) {
	if _, err := MatchPattern(pattern, relative); err != nil {
		return false, err
	}
	ps := strings.Split(strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/"), "/")
	rs := strings.Split(strings.TrimSuffix(relative, "/"), "/")
	previous := make([]bool, len(rs)+1)
	previous[0] = true
	for _, p := range ps {
		next := make([]bool, len(rs)+1)
		if p == "**" {
			next[0] = previous[0]
			for j := 1; j <= len(rs); j++ {
				next[j] = previous[j] || next[j-1] && !strings.HasPrefix(rs[j-1], ".")
			}
		} else {
			for j := 1; j <= len(rs); j++ {
				matched, _ := path.Match(p, rs[j-1])
				next[j] = previous[j-1] && matched && (!strings.HasPrefix(rs[j-1], ".") || strings.HasPrefix(p, "."))
			}
		}
		previous = next
	}
	return previous[len(rs)], nil
}

// ResolveNPM expands membership over selected package manifests. Dependencies
// with matching names remain external unless an explicit local path was given.
func ResolveNPM(docs []*Document, files map[string]bool) {
	packages := map[string]*Document{}
	for _, d := range docs {
		if d != nil && d.Project != nil && d.Project.Kind == "npm" {
			if data, ok := d.Data.(*npmDeclarationData); ok && data.valid {
				packages[d.Project.ID] = d
			}
		}
	}
	paths := make([]string, 0, len(files))
	for filename := range files {
		if path.Base(filename) == "package.json" && !npmInstalledPath(filename) {
			paths = append(paths, filename)
		}
	}
	slices.Sort(paths)
	ordered := append([]*Document{}, docs...)
	slices.SortFunc(ordered, func(a, b *Document) int {
		if a == nil || a.Project == nil {
			if b == nil || b.Project == nil {
				return 0
			}
			return -1
		}
		if b == nil || b.Project == nil {
			return 1
		}
		return strings.Compare(a.Project.ID, b.Project.ID)
	})
	budget := &npmResolutionBudget{remaining: npmMaxResolutionWork}
	for _, d := range ordered {
		if d == nil || d.Project == nil || d.Project.Kind != "npm" {
			continue
		}
		data, ok := d.Data.(*npmDeclarationData)
		if !ok || !data.valid {
			continue
		}
		for i := range d.Project.References {
			ref := &d.Project.References[i]
			if ref.Kind == "npm-local-dependency" && ref.Target != "" {
				npmResolveTarget(d, ref, packages, files)
			}
		}
		if data.workspace && data.usable {
			npmResolveWorkspace(d, data, packages, paths, files, budget)
		}
	}
	npmResolveWorkspaceDependencies(ordered, packages)
}

// Resolve workspace: dependencies only from observed workspace membership and
// an unambiguous declared package name. Nearby names are never inferred local.
func npmResolveWorkspaceDependencies(docs []*Document, packages map[string]*Document) {
	type workspace struct{ members map[string]bool }
	var workspaces []workspace
	for _, root := range docs {
		if root == nil || root.Project == nil {
			continue
		}
		members := map[string]bool{}
		for _, ref := range root.Project.References {
			if ref.Kind == "npm-workspace-member" && ref.State == "resolved" {
				members[ref.Target] = true
			}
		}
		if len(members) > 0 {
			workspaces = append(workspaces, workspace{members: members})
		}
	}
	for _, d := range docs {
		if d == nil || d.Project == nil {
			continue
		}
		for i := range d.Project.References {
			ref := &d.Project.References[i]
			if ref.Kind != "npm-workspace-dependency" {
				continue
			}
			candidates := map[string]bool{}
			for _, ws := range workspaces {
				if !ws.members[d.Project.ID] {
					continue
				}
				for target := range ws.members {
					member := packages[target]
					if member != nil && member.Project.Name == ref.Value {
						candidates[target] = true
					}
				}
			}
			if len(candidates) == 1 {
				for target := range candidates {
					ref.Target, ref.TargetStatus, ref.State = target, "present", "resolved"
				}
			} else {
				AddDiagnostic(d, "unresolved-npm-workspace-dependency", "A workspace dependency did not have exactly one matching declared member name in an observed containing workspace.")
			}
		}
	}
}

func npmResolveTarget(d *Document, ref *Reference, packages map[string]*Document, files map[string]bool) {
	if !files[ref.Target] {
		ref.TargetStatus, ref.State = "missing", "missing"
	} else if packages[ref.Target] == nil {
		ref.TargetStatus, ref.State = "present", "unresolved"
		AddDiagnostic(d, "unparsed-npm-target", "A local package target is present but is not a successfully parsed package manifest.")
	} else {
		ref.TargetStatus, ref.State = "present", "resolved"
	}
}

const npmMaxResolutionWork = 1 << 20

type npmResolutionBudget struct{ remaining int }

func (b *npmResolutionBudget) use(d *Document) bool {
	if b.remaining <= 0 {
		AddDiagnostic(d, "npm-resolution-limit", "npm workspace resolution reached its inventory work limit.")
		return false
	}
	b.remaining--
	return true
}

func npmResolveWorkspace(d *Document, data *npmDeclarationData, packages map[string]*Document, paths []string, files map[string]bool, budget *npmResolutionBudget) {
	matched := map[string]string{}
	excluded := map[string]bool{}
	checks := 0
	limitReached := false
	for _, pattern := range data.patterns {
		literal := !strings.ContainsAny(pattern, "*?[")
		if literal {
			if target, ok := LocalTarget(d.Project.ID, pattern, "package.json"); ok && target != d.Project.ID && !npmInstalledPath(target) {
				matched[target] = pattern
			}
		}
		for _, filename := range paths {
			checks++
			if checks > 65536 || !budget.use(d) {
				limitReached = true
				break
			}
			if filename == d.Project.ID {
				continue
			}
			relative := path.Dir(filename)
			if d.Project.Root != "." {
				prefix := d.Project.Root + "/"
				if !strings.HasPrefix(relative, prefix) {
					continue
				}
				relative = strings.TrimPrefix(relative, prefix)
			}
			if ok, _ := npmMatch(pattern, relative); ok {
				if _, exists := matched[filename]; !exists {
					matched[filename] = pattern
				}
			}
		}
		if limitReached {
			break
		}
	}
	for filename := range matched {
		relative := path.Dir(filename)
		if d.Project.Root != "." {
			relative = strings.TrimPrefix(relative, d.Project.Root+"/")
		}
		for _, pattern := range data.exclusions {
			checks++
			if checks > 65536 || !budget.use(d) {
				limitReached = true
				break
			}
			if ok, _ := npmMatch(pattern, relative); ok {
				excluded[filename] = true
				break
			}
		}
		if limitReached {
			break
		}
	}
	if limitReached {
		AddDiagnostic(d, "npm-workspace-match-limit", "Workspace matching exceeded its per-workspace or shared inventory work limit; no membership observations were retained for this workspace.")
		return
	}
	members := make([]string, 0, len(matched))
	for filename := range matched {
		members = append(members, filename)
	}
	slices.Sort(members)
	names := map[string][]string{}
	for _, filename := range members {
		kind := "npm-workspace-member"
		if excluded[filename] {
			kind = "npm-workspace-excluded"
		}
		ref := Reference{Kind: kind, Value: matched[filename], Target: filename, State: "declared", Evidence: d.Project.ID}
		npmResolveTarget(d, &ref, packages, files)
		if !AddReference(d, ref) {
			break
		}
		if member := packages[filename]; member != nil && !excluded[filename] {
			if name, ok := npmWorkspaceIdentity(member); ok {
				names[name] = append(names[name], filename)
			} else {
				// The path is present, but unsupported identity syntax prevents a
				// reliable duplicate-name check for this workspace member.
				d.Project.References[len(d.Project.References)-1].State = "unresolved"
				AddDiagnostic(d, "unsupported-npm-workspace-identity", "A selected member has no supported declared or directory-derived package identity.")
			}
		}
	}
	for _, ids := range names {
		if len(ids) > 1 {
			AddDiagnostic(d, "duplicate-npm-workspace-name", "Multiple selected members have the same declared or directory-derived package name; workspace identity is ambiguous.")
			for i := range d.Project.References {
				ref := &d.Project.References[i]
				if ref.Kind == "npm-workspace-member" && slices.Contains(ids, ref.Target) {
					ref.State = "unresolved"
				}
			}
		}
	}
}

// npm uses the containing folder (including an immediate @scope parent) when
// package.json omits its name. This identity is used only for ambiguity checks;
// it is not emitted as an explicitly declared project name.
func npmWorkspaceIdentity(d *Document) (string, bool) {
	if d.Project.Name != "" {
		return d.Project.Name, true
	}
	data, ok := d.Data.(*npmDeclarationData)
	if !ok || !data.missingName {
		return "", false
	}
	name := path.Base(d.Project.Root)
	if parent := path.Base(path.Dir(d.Project.Root)); strings.HasPrefix(parent, "@") {
		name = parent + "/" + name
	}
	return name, npmPackageName(name)
}
