package componentmap

import (
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// componentKinds lists the declaration kinds that form component roots, sorted
// for binary search. Its length and the number of distinct ecosystem() values
// are pinned by TestComponentKindCounts and stated in README.md and CHANGELOG.md.
var componentKinds = []string{
	"autoconf",
	"bazel-module",
	"bazel-workspace",
	"cargo",
	"cargo-workspace",
	"clojure-deps",
	"clojure-leiningen",
	"cmake",
	"dart-pub",
	"deno",
	"dotnet",
	"elixir-mix",
	"erlang-rebar",
	"go",
	"go-workspace",
	"gradle",
	"haskell-cabal",
	"haskell-stack",
	"julia-project",
	"kbuild-kconfig",
	"maven",
	"meson",
	"npm",
	"perl-cpanfile",
	"perl-extutils",
	"php-composer",
	"python",
	"python-uv",
	"python-workspace",
	"r-package",
	"ruby-bundler",
	"ruby-gem",
	"scala-sbt",
	"solution",
	"swift-package",
	"zig-build",
}

// Build converts the declaration report into a deterministic component graph.
// It uses only retained declaration facts and performs no I/O.
func Build(report *declarations.Report) Fragment {
	f := Fragment{Components: []Component{}, Relationships: []Relationship{}, QualifiedReferences: []QualifiedReference{}}
	if report == nil {
		f.Coverage = Coverage{Status: "unknown", InputStatus: "unavailable"}
		return f
	}
	f.Coverage.InputStatus = report.Status
	byManifest := make(map[string]Component, len(report.Projects))
	byRoot := make(map[string]Component, len(report.Projects))
	gradleComponentsByRoot := make(map[string][]Component)
	ambiguousRoot := make(map[string]bool)
	gradleSettings := make([]declarations.Project, 0)
	projects := slices.Clone(report.Projects)
	slices.SortFunc(projects, func(a, b declarations.Project) int { return strings.Compare(a.ID, b.ID) })
	unnamedDotnetNames := map[string]int{}
	for _, p := range projects {
		if p.Name != "" || p.Kind != "dotnet" || strings.ToLower(path.Base(cleanRoot(p.Root))) != "src" || strings.ToLower(path.Ext(p.ID)) != ".csproj" {
			continue
		}
		stem := strings.TrimSuffix(path.Base(p.ID), path.Ext(p.ID))
		if stem != "" {
			key := cleanRoot(p.Root) + "\x00" + strings.ToLower(stem)
			unnamedDotnetNames[key]++
		}
	}
	for _, p := range projects {
		if isGradleSettingsManifest(p.ID) {
			gradleSettings = append(gradleSettings, p)
		}
		if p.ID == "" || byManifest[p.ID].Key != "" || !componentKind(p.Kind) {
			continue
		}
		coverage := "complete"
		if report.Status != "complete" {
			coverage = "partial"
		}
		name := p.Name
		if name == "" && p.Kind == "dotnet" && strings.ToLower(path.Base(cleanRoot(p.Root))) == "src" && strings.ToLower(path.Ext(p.ID)) == ".csproj" {
			stem := strings.TrimSuffix(path.Base(p.ID), path.Ext(p.ID))
			key := cleanRoot(p.Root) + "\x00" + strings.ToLower(stem)
			if unnamedDotnetNames[key] == 1 {
				name = stem
			}
		}
		c := Component{Key: p.ID, Root: cleanRoot(p.Root), Manifest: p.ID, Ecosystem: ecosystem(p.Kind), Kind: p.Kind, Name: name, Version: p.Version, Coverage: coverage, Requirements: declaredRequirements(p)}
		byManifest[p.ID] = c
		if c.Ecosystem == "gradle" {
			gradleComponentsByRoot[c.Root] = append(gradleComponentsByRoot[c.Root], c)
		}
		if _, exists := byRoot[c.Root]; exists {
			ambiguousRoot[c.Root] = true
		} else {
			byRoot[c.Root] = c
		}
		f.Components = append(f.Components, c)
	}
	syntheticGradleRoots := addGradleSettingsMembership(&f, gradleSettings, gradleComponentsByRoot)

	// Physical containment is independently true and does not imply workspace
	// membership or a build dependency. Link only the nearest containing root.
	for _, child := range f.Components {
		// A settings-only Gradle workspace is a container for qualified workspace
		// declarations, not an evaluated project that physically owns arbitrary
		// files or projects beneath its path.
		if syntheticGradleRoots[child.Key] {
			continue
		}
		parent := nearestParent(child, byRoot, ambiguousRoot)
		if parent == "" {
			continue
		}
		f.Relationships = append(f.Relationships, Relationship{Type: "contains", From: parent, To: child.Key, DeclarationKind: "root-containment", Evidence: child.Manifest, State: "inferred", Coverage: "complete"})
	}

	seen := map[string]bool{}
	for _, p := range projects {
		if _, ok := byManifest[p.ID]; !ok {
			continue
		}
		for _, ref := range p.References {
			// Gradle settings references are joined below against exact-root
			// Gradle components. Do not treat a settings configuration as a
			// component merely because a legacy/test report labels it "gradle".
			if ref.Kind == "gradle-module" && isGradleSettingsManifest(p.ID) {
				continue
			}
			typ, reverse, relevant := relationshipKind(ref.Kind)
			if !relevant {
				continue
			}
			target, targetOK := byManifest[ref.Target]
			if !targetOK && !ambiguousRoot[cleanRoot(ref.Target)] {
				target, targetOK = byRoot[cleanRoot(ref.Target)]
			}
			if ref.Target == "" || !targetOK || ref.TargetStatus == "missing" || ref.TargetStatus == "unresolved" || ref.TargetStatus == "external" {
				f.QualifiedReferences = append(f.QualifiedReferences, qualified(p.ID, ref, targetOK))
				continue
			}
			from, to := p.ID, target.Key
			if reverse {
				from, to = to, from
			}
			if from == to {
				continue
			}
			coverage := "complete"
			if ref.State == "conditional" || ref.State == "unresolved" || ref.Condition != "" {
				coverage = "partial"
			}
			r := Relationship{Type: typ, From: from, To: to, DeclarationKind: ref.Kind, Evidence: ref.Evidence, State: ref.State, Condition: ref.Condition, Coverage: coverage}
			key := relationshipKey(r)
			if !seen[key] {
				seen[key] = true
				f.Relationships = append(f.Relationships, r)
			}
		}
	}

	// Maven reactor sibling dependency resolution (O-27).
	// A <dependency> whose groupId:artifactId exactly matches the declared
	// coordinates of another Maven component in the same scan may be a local
	// project relationship. A known version mismatch rules the match out;
	// missing or unresolved versions leave the attribution partial.
	// Ambiguity (two components with the same coordinates) yields no edge.
	// Profile and scope conditions are preserved from the requirement.
	mavenByCoords := buildMavenCoordIndex(projects, byManifest)
	mavenVersions := make(map[string]string, len(mavenByCoords))
	for _, p := range projects {
		if p.Kind == "maven" {
			mavenVersions[p.ID] = mavenProjectVersion(p)
		}
	}
	for _, p := range projects {
		if _, ok := byManifest[p.ID]; !ok {
			continue
		}
		for _, req := range p.Requirements {
			if req.Kind != "maven-dependency" {
				continue
			}
			coords := mavenDepGA(req.Value)
			if coords == "" {
				continue
			}
			targetKey, ok := mavenByCoords[coords]
			if !ok || targetKey == "" {
				// No matching sibling or ambiguous coordinates.
				continue
			}
			from, to := p.ID, targetKey
			if from == to {
				continue
			}
			depVersion := mavenDepVersion(req.Value)
			targetVersion := mavenVersions[targetKey]
			if depVersion != "" && targetVersion != "" && depVersion != targetVersion {
				continue
			}
			coverage := "complete"
			if req.State != "declared" || req.Condition != "" || depVersion == "" || targetVersion == "" {
				coverage = "partial"
			}
			r := Relationship{Type: "depends_on_local", From: from, To: to, DeclarationKind: "maven-sibling-dependency", Evidence: req.Evidence, State: req.State, Condition: req.Condition, Coverage: coverage}
			k := relationshipKey(r)
			if !seen[k] {
				seen[k] = true
				f.Relationships = append(f.Relationships, r)
			}
		}
	}
	sortFragment(&f)
	f.Coverage.Components = len(f.Components)
	f.Coverage.Relationships = len(f.Relationships)
	f.Coverage.QualifiedReferences = len(f.QualifiedReferences)
	f.Coverage.Status = "complete"
	if report.Status != "complete" || len(f.QualifiedReferences) > 0 {
		f.Coverage.Status = "partial"
	}
	return f
}

func declaredRequirements(p declarations.Project) []DeclaredRequirement {
	var out []DeclaredRequirement
	add := func(kind, value, state, evidence, condition string) {
		if !packageRequirementKind(kind) || value == "" {
			return
		}
		if evidence == "" {
			evidence = p.ID
		}
		out = append(out, DeclaredRequirement{Kind: kind, Value: value, State: state, Evidence: evidence, Condition: condition})
	}
	for _, r := range p.Requirements {
		add(r.Kind, r.Value, r.State, r.Evidence, r.Condition)
	}
	for _, r := range p.References {
		add(r.Kind, r.Value, r.State, r.Evidence, r.Condition)
	}
	slices.SortFunc(out, func(a, b DeclaredRequirement) int {
		return strings.Compare(a.Kind+"\x00"+a.Value+"\x00"+a.Evidence, b.Kind+"\x00"+b.Value+"\x00"+b.Evidence)
	})
	return out
}

func packageRequirementKind(kind string) bool {
	switch kind {
	case "npm-dependency", "go-require", "cargo-dependency", "cargo-workspace-dependency", "python-dependency", "python-build-requirement", "package-reference", "maven-dependency", "gradle-dependency":
		return true
	default:
		return false
	}
}

func componentKind(kind string) bool {
	_, found := slices.BinarySearch(componentKinds, kind)
	return found
}

func isGradleSettingsManifest(id string) bool {
	base := path.Base(id)
	return base == "settings.gradle" || base == "settings.gradle.kts"
}

// addGradleSettingsMembership materializes the passive settings-file references
// retained by declarations as map relationships. It never evaluates Gradle or
// associates a target by proximity or name: both ends must be uniquely
// identified by their selected manifest roots.
func addGradleSettingsMembership(f *Fragment, settings []declarations.Project, gradleByRoot map[string][]Component) map[string]bool {
	synthetic := make(map[string]bool)
	if len(settings) == 0 {
		return synthetic
	}

	settingsByRoot := make(map[string]int, len(settings))
	for _, p := range settings {
		settingsByRoot[cleanRoot(p.Root)]++
	}

	seenSynthetic := make(map[string]bool)
	seenEdges := make(map[string]bool)
	for _, setting := range settings {
		root := cleanRoot(setting.Root)
		rootComponents := gradleByRoot[root]
		workspaceKey := ""
		sourceReason := ""
		if len(rootComponents) == 1 {
			workspaceKey = rootComponents[0].Key
		} else {
			workspaceKey = setting.ID
			if len(rootComponents) > 1 {
				sourceReason = "ambiguous_gradle_root_component"
			}
		}

		if len(rootComponents) != 1 {
			if !seenSynthetic[setting.ID] {
				f.Components = append(f.Components, Component{
					Key:                setting.ID,
					Root:               root,
					Manifest:           setting.ID,
					Ecosystem:          "gradle",
					Kind:               "gradle",
					Name:               setting.Name,
					Coverage:           "partial",
					gradleSettingsRoot: true,
				})
				seenSynthetic[setting.ID] = true
				synthetic[setting.ID] = true
			}
		}

		for _, ref := range setting.References {
			if ref.Kind != "gradle-module" {
				continue
			}
			if sourceReason != "" {
				f.QualifiedReferences = append(f.QualifiedReferences, gradleQualified(workspaceKey, ref, sourceReason))
				continue
			}
			if settingsByRoot[root] != 1 {
				f.QualifiedReferences = append(f.QualifiedReferences, gradleQualified(workspaceKey, ref, "ambiguous_gradle_settings_root"))
				continue
			}
			if ref.Target == "" || ref.State == "unresolved" || ref.TargetStatus == "unresolved" {
				q := qualified(workspaceKey, ref, false)
				q.gradleSettingsEvidence = true
				f.QualifiedReferences = append(f.QualifiedReferences, q)
				continue
			}
			if ref.TargetStatus != "present" {
				q := qualified(workspaceKey, ref, false)
				q.gradleSettingsEvidence = true
				f.QualifiedReferences = append(f.QualifiedReferences, q)
				continue
			}

			targets := gradleByRoot[cleanRoot(ref.Target)]
			if len(targets) != 1 {
				reason := "target_not_a_retained_gradle_component"
				if len(targets) > 1 {
					reason = "ambiguous_gradle_target_root"
				}
				f.QualifiedReferences = append(f.QualifiedReferences, gradleQualified(workspaceKey, ref, reason))
				continue
			}
			if targets[0].Key == workspaceKey {
				f.QualifiedReferences = append(f.QualifiedReferences, gradleQualified(workspaceKey, ref, "self_gradle_workspace_reference"))
				continue
			}
			relationship := Relationship{
				Type:                   "member_of",
				From:                   targets[0].Key,
				To:                     workspaceKey,
				DeclarationKind:        ref.Kind,
				Evidence:               ref.Evidence,
				State:                  ref.State,
				Condition:              ref.Condition,
				Coverage:               "complete",
				gradleSettingsEvidence: true,
			}
			if ref.State == "conditional" || ref.State == "unresolved" || ref.Condition != "" {
				relationship.Coverage = "partial"
			}
			key := relationshipKey(relationship)
			if !seenEdges[key] {
				seenEdges[key] = true
				f.Relationships = append(f.Relationships, relationship)
			}
		}
	}
	return synthetic
}

func gradleQualified(from string, ref declarations.Reference, reason string) QualifiedReference {
	return QualifiedReference{
		From:                   from,
		DeclarationKind:        ref.Kind,
		Value:                  ref.Value,
		Target:                 ref.Target,
		TargetStatus:           ref.TargetStatus,
		Evidence:               ref.Evidence,
		State:                  ref.State,
		Condition:              ref.Condition,
		Reason:                 reason,
		gradleSettingsEvidence: true,
	}
}

func relationshipKind(kind string) (typ string, reverse, relevant bool) {
	switch kind {
	case "npm-workspace-member", "go-workspace-member", "cargo-workspace-member", "cargo-workspace-path-member", "uv-workspace-member", "solution-member", "module", "gradle-module":
		return "member_of", true, true
	case "npm-local-dependency", "npm-workspace-dependency", "go-local-replacement", "cargo-path-dependency", "cargo-workspace-path-dependency", "uv-local-dependency", "project-reference", "parent", "pub-path-dependency":
		return "depends_on_local", false, true
	default:
		return "", false, false
	}
}

func qualified(from string, ref declarations.Reference, targetOK bool) QualifiedReference {
	reason := "target_unknown"
	if ref.TargetStatus == "external" {
		reason = "target_external"
	} else if ref.TargetStatus == "missing" {
		reason = "target_missing"
	} else if ref.Target != "" && !targetOK {
		reason = "target_not_a_retained_component"
	} else if ref.State == "unresolved" || ref.TargetStatus == "unresolved" {
		reason = "reference_unresolved"
	}
	return QualifiedReference{From: from, DeclarationKind: ref.Kind, Value: ref.Value, Target: ref.Target, TargetStatus: ref.TargetStatus, Evidence: ref.Evidence, State: ref.State, Condition: ref.Condition, Reason: reason}
}

func ecosystem(kind string) string {
	switch {
	case kind == "npm":
		return "npm"
	case kind == "go" || kind == "go-workspace":
		return "go"
	case strings.HasPrefix(kind, "cargo"):
		return "cargo"
	case kind == "python":
		return "python"
	case kind == "python-uv" || kind == "python-workspace":
		return "python-uv"
	case kind == "kbuild-kconfig":
		return "kbuild-kconfig"
	case kind == "maven":
		return "maven"
	case kind == "gradle":
		return "gradle"
	case kind == "solution" || kind == "dotnet":
		return "dotnet"
	case strings.HasPrefix(kind, "ruby-"):
		return "ruby"
	case kind == "php-composer":
		return "php"
	case kind == "swift-package":
		return "swift"
	case kind == "dart-pub":
		return "dart"
	case kind == "elixir-mix":
		return "elixir"
	case kind == "erlang-rebar":
		return "erlang"
	case kind == "scala-sbt":
		return "scala"
	case strings.HasPrefix(kind, "haskell-"):
		return "haskell"
	case kind == "cmake":
		return "cmake"
	case kind == "meson":
		return "meson"
	case kind == "autoconf":
		return "autoconf"
	case kind == "deno":
		return "deno"
	case strings.HasPrefix(kind, "bazel-"):
		return "bazel"
	case kind == "zig-build":
		return "zig"
	case kind == "julia-project":
		return "julia"
	case kind == "r-package":
		return "r"
	case strings.HasPrefix(kind, "clojure-"):
		return "clojure"
	case strings.HasPrefix(kind, "perl-"):
		return "perl"
	default:
		return "unknown"
	}
}

func cleanRoot(root string) string {
	if root == "" || root == "/" {
		return "."
	}
	return path.Clean(root)
}

func nearestParent(child Component, byRoot map[string]Component, ambiguousRoot map[string]bool) string {
	for root := path.Dir(child.Root); ; root = path.Dir(root) {
		if !ambiguousRoot[root] {
			if parent, ok := byRoot[root]; ok && parent.Key != child.Key {
				return parent.Key
			}
		}
		if root == "." {
			break
		}
	}
	return ""
}

func relationshipKey(r Relationship) string {
	return strings.Join([]string{r.Type, r.From, r.To, r.DeclarationKind, r.Evidence, r.State, r.Condition}, "\x00")
}

func sortFragment(f *Fragment) {
	slices.SortFunc(f.Components, func(a, b Component) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(f.Relationships, func(a, b Relationship) int { return strings.Compare(relationshipKey(a), relationshipKey(b)) })
	slices.SortFunc(f.QualifiedReferences, func(a, b QualifiedReference) int {
		ak := strings.Join([]string{a.From, a.DeclarationKind, a.Target, a.Value, a.Evidence, a.State, a.Condition, a.Reason}, "\x00")
		bk := strings.Join([]string{b.From, b.DeclarationKind, b.Target, b.Value, b.Evidence, b.State, b.Condition, b.Reason}, "\x00")
		return strings.Compare(ak, bk)
	})
}

// buildMavenCoordIndex returns a map from "groupId:artifactId" to the
// component key for Maven components in the scan. A coordinate that appears
// more than once maps to the empty string (ambiguous; no edge is emitted).
// groupId inheritance: a module without a maven-groupId requirement inherits
// the groupId from the first part of its maven-parent requirement value
// ("parentGroupId:parentArtifactId:...").
func buildMavenCoordIndex(projects []declarations.Project, byManifest map[string]Component) map[string]string {
	type entry struct {
		key string
		n   int
	}
	coordCount := map[string]*entry{}
	for _, p := range projects {
		if _, ok := byManifest[p.ID]; !ok {
			continue
		}
		if p.Kind != "maven" {
			continue
		}
		groupID := mavenRequirement(p, "maven-groupId")
		if groupID == "" {
			// Inherit from parent: first colon-delimited field of maven-parent value.
			if parent := mavenRequirement(p, "maven-parent"); parent != "" {
				if i := strings.IndexByte(parent, ':'); i > 0 {
					groupID = parent[:i]
				}
			}
		}
		artifactID := mavenRequirement(p, "maven-artifactId")
		if groupID == "" || artifactID == "" {
			continue
		}
		coords := groupID + ":" + artifactID
		if e, exists := coordCount[coords]; exists {
			e.n++
			e.key = "" // ambiguous
		} else {
			coordCount[coords] = &entry{key: p.ID, n: 1}
		}
	}
	out := make(map[string]string, len(coordCount))
	for coords, e := range coordCount {
		if e.n == 1 {
			out[coords] = e.key
		} else {
			out[coords] = "" // ambiguous
		}
	}
	return out
}

// mavenRequirement returns the first requirement value for the given kind, or "".
func mavenRequirement(p declarations.Project, kind string) string {
	for _, r := range p.Requirements {
		if r.Kind == kind {
			return r.Value
		}
	}
	return ""
}

// mavenDepGA returns the "groupId:artifactId" portion of a maven-dependency
// requirement value, stripping the optional ":version" suffix.
func mavenDepGA(value string) string {
	// Value may be "groupId:artifactId" or "groupId:artifactId:version".
	first := strings.IndexByte(value, ':')
	if first < 0 {
		return ""
	}
	second := strings.IndexByte(value[first+1:], ':')
	if second < 0 {
		return strings.ToLower(value) // already "g:a"
	}
	return strings.ToLower(value[:first+1+second])
}

func mavenDepVersion(value string) string {
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 || strings.Contains(parts[2], "${") || strings.ContainsAny(parts[2], "[](),") || parts[2] == "LATEST" || parts[2] == "RELEASE" {
		return ""
	}
	return parts[2]
}

func mavenProjectVersion(p declarations.Project) string {
	for _, r := range p.Requirements {
		if r.Kind == "maven-version" && r.Condition == "" {
			if r.State == "declared" {
				return r.Value
			}
			return ""
		}
	}
	for _, r := range p.Requirements {
		if r.Kind == "maven-parent" && r.Condition == "" && r.State == "declared" {
			parts := strings.SplitN(r.Value, ":", 3)
			if len(parts) == 3 && !strings.Contains(parts[2], "${") {
				return parts[2]
			}
		}
	}
	return ""
}
