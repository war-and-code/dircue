package mapbuild

import (
	"path"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/mapdoc"
)

func setQuestion(d *mapdoc.Document, name string, coverage mapdoc.Coverage) {
	for i := range d.Coverage {
		if d.Coverage[i].Question == name && d.Coverage[i].Scope == "." {
			d.Coverage[i].Coverage = coverage
			return
		}
	}
	d.Coverage = append(d.Coverage, mapdoc.QuestionCoverage{Question: name, Scope: ".", Coverage: coverage})
}

func observerCoverage(value string, fallback string) mapdoc.Coverage {
	result := mapdoc.Coverage{Status: status(value)}
	if result.Status != mapdoc.CoverageComplete {
		result.Reasons = []string{fallback}
	}
	return result
}

func addDeployables(d *mapdoc.Document, r *deployables.Report) {
	setQuestion(d, "deployables", mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_deployable_catalog"}})
	componentsByRoot := map[string][]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			root := n.Properties["root"]
			componentsByRoot[root] = append(componentsByRoot[root], n.ID)
		}
	}
	type imageOwner struct {
		component     string
		evidence      mapdoc.Evidence
		imageEvidence mapdoc.Evidence
	}
	composeImages := map[string][]imageOwner{}
	skaffoldImages := map[string][]imageOwner{}
	seenDeployables := map[string]int{}
	seenEdges := map[string]bool{}
	for _, edge := range d.Edges {
		seenEdges[edge.ID] = true
	}
	imageUsers := []struct {
		id       string
		image    string
		evidence mapdoc.Evidence
	}{}
	addRelationship := func(kind mapdoc.EdgeType, from, to, source, reason string, evidence ...mapdoc.Evidence) {
		e := mapdoc.NewEdge(kind, from, to, source+":"+to)
		if seenEdges[e.ID] {
			return
		}
		seenEdges[e.ID] = true
		e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{reason}}
		e.Evidence = evidence
		d.Edges = append(d.Edges, e)
	}
	for _, def := range r.Definitions {
		n := mapdoc.NewNode(mapdoc.NodeDeployable, []string{def.Path}, def.Provider+":"+def.Kind+":"+def.Name)
		n.Name = def.Name
		n.Properties = map[string]string{"kind": def.Kind, "provider": def.Provider, "source_sha256": def.SourceSHA256}
		if role := mapPathRole(def.Path); role != "" {
			n.Properties["role"] = role
			n.Properties["role_basis"] = "path_name"
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if def.Coverage != "complete" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"static_declaration_not_evaluated"}}
		}
		for _, item := range def.Evidence {
			n.Evidence = append(n.Evidence, deployableEvidence(def.Path, item))
		}
		if len(n.Evidence) == 0 {
			continue
		}
		if original, duplicate := seenDeployables[n.ID]; duplicate {
			// Two declarations cannot share a stable identity in a valid map.
			// Retain the first observation, disclose the omission, and keep the
			// rest of the map available to callers.
			d.Nodes[original].Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"duplicate_deployable_identity"}}
			setQuestion(d, "deployables", mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_deployable_catalog", "duplicate_deployable_identity"}})
			continue
		}
		seenDeployables[n.ID] = len(d.Nodes)
		var localComponent string
		var localEvidence mapdoc.Evidence
		var declaredImages []struct {
			name     string
			evidence mapdoc.Evidence
		}
		for _, ref := range def.References {
			n.Facts = append(n.Facts, mapdoc.Fact{Kind: "deployable_reference", Name: ref.Kind, Value: ref.Value, State: ref.Qualification, Coverage: referenceCoverage(ref.Qualification), Evidence: []mapdoc.Evidence{deployableEvidence(def.Path, ref.Evidence)}})
			if (ref.Kind == "build_context" || ref.Kind == "code_uri") && ref.Qualification == "local" {
				root := path.Clean(path.Join(path.Dir(def.Path), ref.Value))
				owners := componentsByRoot[root]
				if len(owners) == 1 {
					localComponent = owners[0]
					localEvidence = deployableEvidence(def.Path, ref.Evidence)
					if def.Provider != "skaffold" {
						addRelationship(mapdoc.EdgeBuilds, n.ID, owners[0], ref.Kind+":"+root, "declared_context_matches_component_root", localEvidence)
					}
					if def.Provider == "compose" && def.Kind == "service" {
						addRelationship(mapdoc.EdgeRuns, n.ID, owners[0], "compose-service:"+root, "service_declares_build_context", localEvidence)
					}
				}
			}
			if ref.Kind == "image" && ref.Qualification != "unresolved" {
				declaredImages = append(declaredImages, struct {
					name     string
					evidence mapdoc.Evidence
				}{ref.Value, deployableEvidence(def.Path, ref.Evidence)})
			}
		}
		if def.Provider == "dockerfile" && def.Kind == "container_build" {
			if owners := componentsByRoot[path.Dir(def.Path)]; len(owners) == 1 {
				addRelationship(mapdoc.EdgeBuilds, n.ID, owners[0], "dockerfile:"+def.Path, "dockerfile_co_located_with_component", deployableEvidence(def.Path, def.Evidence[0]))
			}
		}
		for _, image := range declaredImages {
			if def.Provider == "compose" && localComponent != "" {
				name := normalizedImageRepository(image.name)
				if name != "" {
					composeImages[name] = append(composeImages[name], imageOwner{component: localComponent, evidence: localEvidence, imageEvidence: image.evidence})
				}
			}
			if def.Provider == "skaffold" && localComponent != "" {
				name := normalizedImageRepository(image.name)
				if name != "" {
					skaffoldImages[name] = append(skaffoldImages[name], imageOwner{component: localComponent, evidence: localEvidence, imageEvidence: image.evidence})
				}
			}
			if def.Provider == "kubernetes" {
				imageUsers = append(imageUsers, struct {
					id       string
					image    string
					evidence mapdoc.Evidence
				}{n.ID, image.name, image.evidence})
			}
		}
		d.Nodes = append(d.Nodes, n)
	}
	for _, use := range imageUsers {
		imageName := normalizedImageRepository(use.image)
		owners := composeImages[imageName]
		if len(owners) == 1 {
			addRelationship(mapdoc.EdgeRuns, use.id, owners[0].component, "image:"+use.image, "image_matches_compose_build_declaration", use.evidence, owners[0].evidence)
			continue
		}
		if len(owners) != 0 {
			continue
		}
		owners = skaffoldImages[imageName]
		if len(owners) == 1 {
			addRelationship(mapdoc.EdgeRuns, use.id, owners[0].component, "skaffold-image:"+imageName, "kubernetes_image_matches_skaffold_artifact_and_context", use.evidence, owners[0].imageEvidence, owners[0].evidence)
			continue
		}
		if len(owners) != 0 {
			continue
		}
	}
}

// normalizedImageRepository removes only an image tag or digest. Keeping the
// registry and namespace avoids linking unrelated images that share a basename.
func normalizedImageRepository(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	if tag := strings.LastIndexByte(image, ':'); tag > strings.LastIndexByte(image, '/') {
		image = image[:tag]
	}
	return strings.ToLower(image)
}

func referenceCoverage(qualification string) mapdoc.Coverage {
	if qualification == "local" || qualification == "declared" {
		return mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	}
	return mapdoc.Coverage{Status: mapdoc.CoverageUnknown, Reasons: []string{"reference_" + qualification}}
}

func deployableEvidence(filename string, e deployables.Evidence) mapdoc.Evidence {
	item := mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: filename, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/deployables/" + e.Basis, Version: deployables.ProviderVersion}}
	if e.Line > 0 {
		item.Span = &mapdoc.Span{StartLine: e.Line, EndLine: e.Line}
	}
	return item
}

func addIntent(d *mapdoc.Document, r *intentmap.Report) {
	coverage := mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_interface_and_capability_catalog"}}
	if r.Coverage.Status != "complete" {
		coverage.Reasons = append(coverage.Reasons, "interface_or_capability_observations_incomplete")
	}
	setQuestion(d, "interfaces", coverage)
	setQuestion(d, "capabilities", coverage)
	componentsByManifest := map[string]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			for _, p := range n.Paths {
				if p == n.Properties["root"] {
					continue
				}
				componentsByManifest[p] = n.ID
			}
		}
	}
	contentByPath := map[string]string{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeContent && len(n.Paths) == 1 && n.Paths[0] != "." {
			contentByPath[n.Paths[0]] = n.ID
		}
	}
	seen := map[string]bool{}
	for _, o := range r.Observations {
		if o.Kind == intentmap.KindImport {
			continue
		}
		kind := mapdoc.NodeInterface
		if o.Kind == intentmap.KindCapability {
			kind = mapdoc.NodeCapability
		}
		key := string(o.Kind) + ":" + o.Path + ":" + o.Name + ":" + o.ProjectID
		if seen[key] {
			continue
		}
		seen[key] = true
		n := mapdoc.NewNode(kind, []string{o.Path}, string(o.Kind)+":"+o.Name+":"+o.ProjectID)
		n.Name = o.Name
		n.Properties = map[string]string{"observation_kind": string(o.Kind), "state": o.State, "basis": o.Basis}
		if role := mapPathRole(o.Path); role != "" {
			n.Properties["role"] = role
			n.Properties["role_basis"] = "path_name"
		}
		for k, v := range o.Properties {
			n.Properties[k] = v
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if o.State == "partial" || o.State == "unresolved" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"observation_" + o.State}}
		}
		n.Evidence = []mapdoc.Evidence{intentEvidence(o)}
		if o.ProjectID != "" {
			if owner := componentsByManifest[o.ProjectID]; owner != "" {
				kind := mapdoc.EdgeDeclares
				if n.Kind == mapdoc.NodeCapability {
					kind = mapdoc.EdgeUsesCapability
				}
				e := mapdoc.NewEdge(kind, owner, n.ID, string(o.Kind)+":"+o.Path)
				e.Coverage = n.Coverage
				e.Evidence = slices.Clone(n.Evidence)
				d.Edges = append(d.Edges, e)
			}
		} else if n.Kind == mapdoc.NodeInterface && strings.HasSuffix(strings.ToLower(o.Path), ".proto") {
			owner := contentByPath[o.Path]
			if owner == "" {
				content := fileNode(o.Path, "declared_contract", "protobuf", "parsed_prefix", 0)
				content.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
				owner = content.ID
				contentByPath[o.Path] = owner
				d.Nodes = append(d.Nodes, content)
			}
			e := mapdoc.NewEdge(mapdoc.EdgeDeclares, owner, n.ID, o.Name)
			e.Coverage = n.Coverage
			e.Evidence = slices.Clone(n.Evidence)
			d.Edges = append(d.Edges, e)
		}
		d.Nodes = append(d.Nodes, n)
	}
}

func intentEvidence(o intentmap.Observation) mapdoc.Evidence {
	basis := mapdoc.BasisDeclaredConfig
	source := mapdoc.SourceConfiguration
	if o.Basis == "code_syntax" || o.Basis == "declared_contract" {
		basis, source = mapdoc.BasisCodeSyntax, mapdoc.SourceCode
	}
	if o.Basis == "imported" {
		basis, source = mapdoc.BasisRuleInferred, mapdoc.SourceCode
	}
	item := mapdoc.Evidence{Basis: basis, Path: o.Path, SourceKind: source, Rule: &mapdoc.Producer{ID: "dircue/intent/" + o.Basis, Version: intentmap.DetectorVersion}}
	if o.StartLine > 0 {
		item.Span = &mapdoc.Span{StartLine: o.StartLine, EndLine: max(o.StartLine, o.EndLine)}
	}
	return item
}
