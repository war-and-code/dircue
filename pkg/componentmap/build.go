package componentmap

import (
	"path"
	"slices"
	"strings"

	"dircue/pkg/declarations"
)

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
	ambiguousRoot := make(map[string]bool)
	projects := slices.Clone(report.Projects)
	slices.SortFunc(projects, func(a, b declarations.Project) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range projects {
		if p.ID == "" || byManifest[p.ID].Key != "" || !componentKind(p.Kind) {
			continue
		}
		coverage := "complete"
		if report.Status != "complete" {
			coverage = "partial"
		}
		c := Component{Key: p.ID, Root: cleanRoot(p.Root), Manifest: p.ID, Ecosystem: ecosystem(p.Kind), Kind: p.Kind, Name: p.Name, Version: p.Version, Coverage: coverage, Requirements: declaredRequirements(p)}
		byManifest[p.ID] = c
		if _, exists := byRoot[c.Root]; exists {
			ambiguousRoot[c.Root] = true
		} else {
			byRoot[c.Root] = c
		}
		f.Components = append(f.Components, c)
	}

	// Physical containment is independently true and does not imply workspace
	// membership or a build dependency. Link only the nearest containing root.
	for _, child := range f.Components {
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
	switch kind {
	case "npm", "go", "go-workspace", "cargo", "cargo-workspace", "python", "python-workspace", "maven", "gradle", "solution", "dotnet":
		return true
	default:
		return false
	}
}

func relationshipKind(kind string) (typ string, reverse, relevant bool) {
	switch kind {
	case "npm-workspace-member", "go-workspace-member", "cargo-workspace-member", "cargo-workspace-path-member", "uv-workspace-member", "solution-member", "module", "gradle-module":
		return "member_of", true, true
	case "npm-local-dependency", "npm-workspace-dependency", "go-local-replacement", "cargo-path-dependency", "cargo-workspace-path-dependency", "uv-local-dependency", "project-reference", "parent":
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
	case kind == "python" || kind == "python-workspace":
		return "python-uv"
	case kind == "maven":
		return "maven"
	case kind == "gradle":
		return "gradle"
	case kind == "solution" || kind == "dotnet":
		return "dotnet"
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
