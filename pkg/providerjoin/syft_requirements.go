package providerjoin

import (
	"net/url"
	"sort"
	"strings"
	"unicode"

	"dircue/pkg/mapdoc"
)

type syftPackageObservation struct {
	nodeIndex int
	key       string
	owner     string
}

func reconcileSyftRequirements(out *Result, in Input, bindingState Binding, covered []string, observations []syftPackageObservation) {
	// A known mismatch cannot support cross-report attribution. Reports without
	// identity may support qualified candidate matches, but never absence claims.
	if bindingState == BindingMismatch {
		return
	}
	components := map[string]mapdoc.Node{}
	for _, node := range in.Nodes {
		if node.Kind == mapdoc.NodeComponent {
			components[node.ID] = node
		}
	}
	byKey := map[string][]syftPackageObservation{}
	for _, observation := range observations {
		if observation.key != "" {
			byKey[observation.key] = append(byKey[observation.key], observation)
		}
	}
	matched := map[int]bool{}
	for componentID, component := range components {
		eligible := componentCovered(component, covered)
		for _, requirement := range component.Facts {
			key, ok := declaredPackageKey(requirement)
			if !ok {
				continue
			}
			matches := observationsForOwner(byKey[key], componentID)
			switch len(matches) {
			case 1:
				match := matches[0]
				n := &out.Nodes[match.nodeIndex]
				state := "exact_identity"
				reasons := []string{"version_and_resolution_not_compared"}
				if bindingState != BindingVerified {
					state = "candidate_identity_unverified_binding"
					reasons = append(reasons, "provider_snapshot_binding_"+string(bindingState))
				}
				n.Facts = append(n.Facts, comparisonFact("declared_requirement_match", requirement.Name, key, state, appendEvidence(n.Evidence, requirement.Evidence), reasons))
				e := mapdoc.NewEdge(mapdoc.EdgePackagedIn, n.ID, componentID, "declared_requirement:"+key)
				e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: reasons}
				e.Evidence = appendEvidence(n.Evidence, requirement.Evidence)
				out.Edges = append(out.Edges, e)
				matched[match.nodeIndex] = true
			case 0:
				if eligible && bindingState == BindingVerified {
					out.Nodes[0].Facts = append(out.Nodes[0].Facts, comparisonFact("declared_requirement_comparison", requirement.Name, key, "no_match_in_supplied_report", appendEvidence(out.Nodes[0].Evidence, requirement.Evidence), []string{"provider_cataloger_coverage_may_be_partial", "supplied_report_not_proof_of_dependency_absence"}))
				}
			default:
				reasons := []string{"multiple_provider_artifacts_share_identity"}
				if bindingState != BindingVerified {
					reasons = append(reasons, "provider_snapshot_binding_"+string(bindingState))
				}
				out.Nodes[0].Facts = append(out.Nodes[0].Facts, comparisonFact("declared_requirement_comparison", requirement.Name, key, "ambiguous_reported_identity", appendEvidence(out.Nodes[0].Evidence, requirement.Evidence), reasons))
				for _, match := range matches {
					matched[match.nodeIndex] = true
				}
			}
		}
	}
	if bindingState != BindingVerified {
		sortSyftComparisonFacts(out)
		return
	}
	for _, observation := range observations {
		if matched[observation.nodeIndex] || observation.owner == "" || !componentCovered(components[observation.owner], covered) {
			continue
		}
		n := &out.Nodes[observation.nodeIndex]
		n.Facts = append(n.Facts, comparisonFact("declared_requirement_comparison", "", observation.key, "no_matching_retained_declaration", n.Evidence, []string{"transitive_or_incomplete_declarations_possible"}))
	}
	sortSyftComparisonFacts(out)
}

func observationsForOwner(observations []syftPackageObservation, componentID string) []syftPackageObservation {
	out := make([]syftPackageObservation, 0, len(observations))
	for _, observation := range observations {
		if observation.owner == componentID {
			out = append(out, observation)
		}
	}
	return out
}

func sortSyftComparisonFacts(out *Result) {
	for i := range out.Nodes {
		sort.Slice(out.Nodes[i].Facts, func(a, b int) bool { return factSortKey(out.Nodes[i].Facts[a]) < factSortKey(out.Nodes[i].Facts[b]) })
	}
}

func comparisonFact(kind, name, value, state string, evidence []mapdoc.Evidence, reasons []string) mapdoc.Fact {
	return mapdoc.Fact{Kind: kind, Name: name, Value: value, State: state, Coverage: mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: reasons}, Evidence: evidence}
}
func appendEvidence(a, b []mapdoc.Evidence) []mapdoc.Evidence {
	out := append([]mapdoc.Evidence{}, a...)
	return append(out, b...)
}
func factSortKey(f mapdoc.Fact) string {
	return f.Kind + "\x00" + f.Name + "\x00" + f.Value + "\x00" + f.State
}

func componentCovered(component mapdoc.Node, covered []string) bool {
	root := component.Properties["root"]
	if root == "" && len(component.Paths) > 0 {
		root = component.Paths[0]
	}
	if root == "" {
		return false
	}
	for _, filename := range covered {
		if root == "." || filename == root || strings.HasPrefix(filename, root+"/") {
			return true
		}
	}
	return false
}

func declaredPackageKey(f mapdoc.Fact) (string, bool) {
	if f.Kind != "declared_requirement" || f.State == "unresolved" || f.Value == "" {
		return "", false
	}
	kind, value := f.Name, strings.TrimSpace(f.Value)
	if value == "" {
		return "", false
	}
	switch kind {
	case "npm-dependency":
		name := value
		if i := strings.LastIndex(name, "@"); i > 0 {
			name = name[:i]
		}
		return "npm:" + strings.ToLower(name), true
	case "go-require":
		return "golang:" + strings.Fields(value)[0], true
	case "cargo-dependency", "cargo-workspace-dependency":
		name := strings.Fields(value)[0]
		if start := strings.Index(value, "(package="); start >= 0 {
			rest := value[start+9:]
			if end := strings.Index(rest, ")"); end >= 0 {
				name = rest[:end]
			}
		}
		return "cargo:" + strings.ToLower(name), true
	case "python-dependency", "python-build-requirement":
		name := strings.TrimLeftFunc(value, unicode.IsSpace)
		end := strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("[<>=!~;@", r) })
		if end >= 0 {
			name = name[:end]
		}
		if name == "" {
			return "", false
		}
		return "pypi:" + pythonPackageName(name), true
	case "package-reference":
		name := value
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i]
		}
		return "nuget:" + strings.ToLower(name), true
	case "maven-dependency", "gradle-dependency":
		parts := strings.Split(value, ":")
		if len(parts) < 2 {
			return "", false
		}
		return "maven:" + parts[0] + ":" + parts[1], true
	default:
		return "", false
	}
}

func syftPackageKey(packageType, purl, name string) string {
	if strings.HasPrefix(purl, "pkg:") {
		value := strings.TrimPrefix(purl, "pkg:")
		if i := strings.IndexAny(value, "?#"); i >= 0 {
			value = value[:i]
		}
		typ, rest, ok := strings.Cut(value, "/")
		if ok {
			if i := strings.LastIndex(rest, "@"); i >= 0 {
				rest = rest[:i]
			}
			if decoded, err := url.PathUnescape(rest); err == nil {
				rest = decoded
			}
			switch typ {
			case "npm":
				return "npm:" + strings.ToLower(rest)
			case "golang":
				return "golang:" + rest
			case "cargo":
				return "cargo:" + strings.ToLower(pathTail(rest))
			case "pypi":
				return "pypi:" + pythonPackageName(pathTail(rest))
			case "nuget":
				return "nuget:" + strings.ToLower(pathTail(rest))
			case "maven":
				parts := strings.Split(rest, "/")
				if len(parts) >= 2 {
					return "maven:" + strings.Join(parts[:len(parts)-1], ".") + ":" + parts[len(parts)-1]
				}
			}
		}
	}
	switch strings.ToLower(packageType) {
	case "npm":
		return "npm:" + strings.ToLower(name)
	case "go-module":
		return "golang:" + name
	case "rust-crate":
		return "cargo:" + strings.ToLower(name)
	case "python":
		return "pypi:" + pythonPackageName(name)
	case "dotnet":
		return "nuget:" + strings.ToLower(name)
	case "java-archive", "maven", "gradle":
		if strings.Count(name, ":") >= 1 {
			parts := strings.Split(name, ":")
			return "maven:" + parts[0] + ":" + parts[1]
		}
	}
	return ""
}
func pathTail(v string) string {
	if i := strings.LastIndex(v, "/"); i >= 0 {
		return v[i+1:]
	}
	return v
}
func pythonPackageName(v string) string {
	v = strings.ToLower(v)
	v = strings.Map(func(r rune) rune {
		if r == '_' || r == '.' {
			return '-'
		}
		return r
	}, v)
	for strings.Contains(v, "--") {
		v = strings.ReplaceAll(v, "--", "-")
	}
	return v
}
