package mapbuild

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
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

// dockerCargoComponent narrows root-level co-location attribution only when
// a copied crate tree and a literal Cargo build command occur in the same
// Docker build stage. Zigbuild targets additionally need an exact declared
// default-run interface on one unique Cargo component.
func dockerCargoComponent(def deployables.Definition, owners []string, d *mapdoc.Document, intents ...*intentmap.Report) (string, []mapdoc.Evidence, bool) {
	if def.DockerContextUnknown || def.Path != "" && path.Dir(def.Path) != "." {
		return "", nil, false
	}
	type copiedCrate struct {
		root     string
		line     int
		evidence mapdoc.Evidence
	}
	copyEvidenceByStage := map[string][]copiedCrate{}
	for _, ref := range def.References {
		if ref.Kind != "copy_source" {
			continue
		}
		source := strings.TrimPrefix(path.Clean(ref.Value), "./")
		if source == "crates" || strings.HasPrefix(source, "crates/") {
			copyEvidenceByStage[ref.Stage] = append(copyEvidenceByStage[ref.Stage], copiedCrate{root: source, line: ref.Evidence.Line, evidence: deployableEvidence(def.Path, ref.Evidence)})
		}
	}
	for _, ref := range def.DockerPathWrites {
		command := strings.TrimSpace(strings.TrimPrefix(ref.Value, "RUN "))
		for _, segment := range strings.Split(command, "&&") {
			fields := strings.Fields(strings.TrimSpace(segment))
			if len(fields) >= 2 && fields[0] == "cargo" && (fields[1] == "build" || fields[1] == "zigbuild") {
				// A builder-stage command does not prove that this Cargo output is
				// shipped in the final image. Narrow co-location only when the build
				// itself runs in the final stage; cross-stage artifact tracing is
				// handled separately when its source and destination are explicit.
				if ref.Stage != def.DockerFinalStage {
					continue
				}
				if cargoTargetOverridden(fields) {
					continue
				}
				copiedCrates := copyEvidenceByStage[ref.Stage]
				if len(copiedCrates) == 0 {
					continue
				}
				copyEvidence := make([]mapdoc.Evidence, 0, len(copiedCrates))
				orderedCopies := make([]copiedCrate, 0, len(copiedCrates))
				for _, copied := range copiedCrates {
					if copied.line > 0 && ref.Evidence.Line > 0 && copied.line >= ref.Evidence.Line {
						continue
					}
					orderedCopies = append(orderedCopies, copied)
					copyEvidence = append(copyEvidence, copied.evidence)
				}
				if len(orderedCopies) == 0 {
					continue
				}
				buildEvidence := deployableEvidence(def.Path, ref.Evidence)
				if fields[1] == "zigbuild" {
					binary, valid := literalCargoBin(fields)
					if !valid {
						continue
					}
					matches := []string{}
					var manifest string
					for _, n := range d.Nodes {
						if n.Kind != mapdoc.NodeComponent || n.Properties["ecosystem"] != "cargo" || n.Name != binary {
							continue
						}
						root := n.Properties["root"]
						if !strings.HasPrefix(root, "crates/") {
							continue
						}
						copied := false
						for _, source := range orderedCopies {
							if source.root == "crates" || root == source.root || strings.HasPrefix(root, source.root+"/") {
								copied = true
								break
							}
						}
						if !copied {
							continue
						}
						projectPath := ""
						for _, candidate := range n.Paths {
							if path.Base(candidate) == "Cargo.toml" {
								projectPath = candidate
								break
							}
						}
						if projectPath == "" || !intentDeclaresCargoBin(intents, projectPath, binary) {
							continue
						}
						matches = append(matches, n.ID)
						manifest = projectPath
					}
					if len(matches) != 1 {
						continue
					}
					evidence := append(append([]mapdoc.Evidence{}, copyEvidence...), buildEvidence,
						mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: manifest, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/project-declaration", Version: "1.0.0"}})
					var workspaceCargo []string
					for _, id := range owners {
						for _, n := range d.Nodes {
							if n.ID == id && n.Properties["ecosystem"] == "cargo" {
								workspaceCargo = append(workspaceCargo, id)
							}
						}
					}
					if len(workspaceCargo) != 1 {
						continue
					}
					return workspaceCargo[0], append(evidence, mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: path.Join(path.Dir(def.Path), "Cargo.toml"), SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "dircue/project-declaration", Version: "1.0.0"}}), true
				}
				var cargoOwner string
				for _, id := range owners {
					for _, n := range d.Nodes {
						if n.ID == id && n.Properties["ecosystem"] == "cargo" {
							if cargoOwner != "" {
								return "", nil, false
							}
							cargoOwner = id
						}
					}
				}
				if cargoOwner != "" {
					return cargoOwner, append(copyEvidence, buildEvidence), true
				}
			}
		}
	}
	return "", nil, false
}

func literalCargoBin(fields []string) (string, bool) {
	var binary string
	for i := 2; i < len(fields); i++ {
		if fields[i] != "--bin" {
			continue
		}
		if binary != "" || i+1 >= len(fields) || strings.ContainsAny(fields[i+1], "$*?{}") {
			return "", false
		}
		binary = fields[i+1]
		i++
	}
	return binary, binary != ""
}

func cargoTargetOverridden(fields []string) bool {
	for _, field := range fields {
		if field == "--manifest-path" || strings.HasPrefix(field, "--manifest-path=") || field == "--package" || strings.HasPrefix(field, "--package=") || field == "-p" || strings.HasPrefix(field, "-p") && len(field) > 2 || field == "--workspace" || field == "--all" || field == "--all-targets" {
			return true
		}
	}
	return false
}

func intentDeclaresCargoBin(reports []*intentmap.Report, manifest, binary string) bool {
	for _, report := range reports {
		if report == nil {
			continue
		}
		for _, observation := range report.Observations {
			if observation.Kind == intentmap.KindInterface && observation.Name == binary && observation.ProjectID == manifest && observation.Properties["interface_kind"] == "cargo-default-run" {
				return true
			}
		}
	}
	return false
}

func dockerHasCargoZigbuild(def deployables.Definition) bool {
	for _, ref := range def.DockerPathWrites {
		command := strings.TrimSpace(strings.TrimPrefix(ref.Value, "RUN "))
		for _, segment := range strings.Split(command, "&&") {
			fields := strings.Fields(strings.TrimSpace(segment))
			if len(fields) >= 2 && fields[0] == "cargo" && fields[1] == "zigbuild" {
				return true
			}
		}
	}
	return false
}

func addDeployables(d *mapdoc.Document, r *deployables.Report, intents ...*intentmap.Report) {
	setQuestion(d, "deployables", mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_deployable_catalog"}})
	componentsByRoot := map[string][]string{}
	componentsByName := map[string][]string{}
	mavenComponents := map[string]bool{}
	for _, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			root := n.Properties["root"]
			componentsByRoot[root] = append(componentsByRoot[root], n.ID)
			if n.Properties["ecosystem"] == "maven" || n.Discriminator == "maven" {
				mavenComponents[n.ID] = true
			}
			if n.Name != "" {
				componentsByName[n.Name] = append(componentsByName[n.Name], n.ID)
			}
		}
	}

	// Aggregate Terraform definitions: one deployable per module root directory.
	// Individual .tf files in the same directory belong to the same module.
	// See docs/MAP.md, "Terraform granularity" for the design rationale.
	definitions := aggregateTerraformDefs(r.Definitions)

	// Aggregate Kubernetes/Tekton definitions: one deployable per
	// (K8sKind, metadata.name, namespace, component-scope) tuple.
	// Files that redeclare the same object (kustomize overlays, rendered
	// release bundles, etc.) become additional evidence rather than separate
	// nodes. See docs/MAP.md, "Kubernetes granularity".
	definitions = aggregateKubernetesDefs(definitions, componentsByRoot)
	// Index statically declared Maven archive artifacts by filename. A Docker
	// COPY source can then identify the reactor module whose WAR/EAR it ships;
	// the archive's POM directory identifies its owning component.
	type mavenArchiveOwner struct {
		component string
		root      string
	}
	mavenArchives := map[string][]mavenArchiveOwner{}
	for _, def := range definitions {
		if def.Provider != "maven" || def.Kind != "archive" || def.Name == "" {
			continue
		}
		// Only the Maven component at the POM's directory can own the archive.
		// Another ecosystem's manifest at the same root (for example a
		// package.json beside pom.xml) must not make the owner ambiguous.
		for _, owner := range componentsByRoot[path.Dir(def.Path)] {
			if !mavenComponents[owner] {
				continue
			}
			mavenArchives[path.Base(def.Name)] = append(mavenArchives[path.Base(def.Name)], mavenArchiveOwner{component: owner, root: path.Dir(def.Path)})
		}
	}

	type imageOwner struct {
		component     string
		evidence      mapdoc.Evidence
		imageEvidence mapdoc.Evidence
	}
	composeImages := map[string][]imageOwner{}
	skaffoldImages := map[string][]imageOwner{}
	// composeServiceByID tracks compose service node IDs keyed by service name
	// so that depends_on references can be resolved after all nodes are added.
	composeServiceByID := map[string]string{}
	type composeDep struct {
		fromID   string
		toName   string
		evidence mapdoc.Evidence
	}
	var composeDeps []composeDep
	type terraformModuleRef struct {
		fromID, fromDir, source string
		evidence                mapdoc.Evidence
	}
	var terraformModuleRefs []terraformModuleRef
	terraformNodeByDir := map[string]string{}
	type contextDockerfile struct {
		file, component string
		evidence        mapdoc.Evidence
	}
	var contextDockerfiles []contextDockerfile
	dockerNodesByPath := map[string]string{}
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
	for _, def := range definitions {
		// Terraform module deployables use the directory as their identity path so
		// the node ID is stable regardless of which .tf files are present. Their
		// def.Path holds the primary .tf file (for evidence source attribution).
		nodePaths := []string{def.Path}
		if def.Provider == "terraform" {
			nodePaths = []string{path.Dir(def.Path)}
		}
		n := mapdoc.NewNode(mapdoc.NodeDeployable, nodePaths, def.Provider+":"+def.Kind+":"+def.Name)
		if def.Provider == "dockerfile" {
			dockerNodesByPath[def.Path] = n.ID
		}
		n.Name = def.Name
		n.Properties = map[string]string{"kind": def.Kind, "provider": def.Provider, "source_sha256": def.SourceSHA256}
		if def.Format != "" {
			n.Properties["format"] = def.Format
		}
		if def.Count > 1 {
			n.Properties["declaration_count"] = fmt.Sprintf("%d", def.Count)
		}
		if role := mapPathRole(def.Path); role != "" {
			n.Properties["role"] = role
			n.Properties["role_basis"] = "path_name"
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if def.Coverage != "complete" {
			coverageReason := "static_declaration_not_evaluated"
			if def.Provider == "helm" {
				coverageReason = "helm_templates_not_rendered"
			}
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{coverageReason}}
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
		if def.Provider == "compose" && def.Kind == "service" {
			composeServiceByID[def.Name] = n.ID
		}
		for _, ref := range def.References {
			factIndex := len(n.Facts)
			fact := mapdoc.Fact{Kind: "deployable_reference", Name: ref.Kind, Value: ref.Value, State: ref.Qualification, Coverage: referenceCoverage(ref.Qualification), Evidence: []mapdoc.Evidence{deployableEvidence(def.Path, ref.Evidence)}}
			if strings.HasPrefix(ref.Kind, "docker_entrypoint") || strings.HasPrefix(ref.Kind, "docker_cmd") {
				fact.Properties = map[string]string{"arguments": "withheld", "instruction_form": strings.TrimPrefix(ref.Evidence.Basis, "dockerfile-instruction-")}
				if ref.Stage != "" {
					fact.Properties["stage"] = ref.Stage
					fact.Properties["stage_is_final"] = fmt.Sprint(ref.Stage == def.DockerFinalStage)
				}
			}
			n.Facts = append(n.Facts, fact)
			if ref.Kind == "service_dependency" && ref.Qualification == "local" {
				composeDeps = append(composeDeps, composeDep{fromID: n.ID, toName: ref.Value, evidence: deployableEvidence(def.Path, ref.Evidence)})
			}
			if (ref.Kind == "build_context" || ref.Kind == "code_uri" || ref.Kind == "working_directory") && ref.Qualification == "local" {
				// A workflow working-directory resolves from the repository root;
				// other references resolve from the declaring file's directory.
				// CodeUri may name a build artifact (e.g. target/app.jar), so it
				// and working-directory fall back to the nearest ancestor root.
				resolved := path.Clean(path.Join(path.Dir(def.Path), ref.Value))
				if ref.Kind == "working_directory" {
					resolved = path.Clean(ref.Value)
				}
				if def.Provider == "github-actions" && ref.Kind == "build_context" {
					resolved = path.Clean(ref.Value)
					if ref.Checkout != "" {
						if resolved == ref.Checkout {
							resolved = "."
						} else if strings.HasPrefix(resolved, ref.Checkout+"/") {
							resolved = strings.TrimPrefix(resolved, ref.Checkout+"/")
						}
					}
				}
				owner, reason := localPathOwner(componentsByRoot, resolved, ref.Kind != "build_context")
				if ref.Kind == "working_directory" {
					owner, reason = workingDirectoryOwner(d, r, ref, componentsByRoot, resolved)
				}
				if reason != "" {
					n.Facts[factIndex].Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{reason}}
				}
				// Workflow edges are added by addWorkflowComponentEdges.
				if owner != "" && ref.Kind != "working_directory" {
					localComponent = owner
					localEvidence = deployableEvidence(def.Path, ref.Evidence)
					if def.Provider != "skaffold" {
						addRelationship(mapdoc.EdgeBuilds, n.ID, owner, ref.Kind+":"+resolved, "declared_context_matches_component_root", localEvidence)
					}
					if def.Provider == "compose" && def.Kind == "service" {
						if def.Provider == "compose" {
							addRelationship(mapdoc.EdgeRuns, n.ID, owner, "compose-service:"+resolved, "service_declares_build_context", localEvidence)
						}
						var dockerfile string
						for _, candidate := range def.References {
							if candidate.Kind == "dockerfile" && candidate.Qualification == "local" {
								dockerfile = path.Clean(path.Join(resolved, candidate.Value))
								break
							}
						}
						if dockerfile != "" {
							contextDockerfiles = append(contextDockerfiles, contextDockerfile{file: dockerfile, component: owner, evidence: localEvidence})
						}
					}
				}
			}
			if ref.Kind == "image" && ref.Qualification != "unresolved" {
				declaredImages = append(declaredImages, struct {
					name     string
					evidence mapdoc.Evidence
				}{ref.Value, deployableEvidence(def.Path, ref.Evidence)})
			}
			// Aspire AppHost: AddProject<Projects.X>() → runs edge to project component.
			if ref.Kind == "aspire_project" && ref.Qualification == "local" {
				owners := aspireProjectOwners(componentsByRoot, componentsByName, ref.Value)
				if len(owners) == 1 {
					addRelationship(mapdoc.EdgeRuns, n.ID, owners[0], "aspire-project:"+ref.Value, "aspire_addproject_declares_run", deployableEvidence(def.Path, ref.Evidence))
				}
			}
		}
		// Maven WAR/EAR: the pom.xml declares the packaging and the deployable
		// artifact. Link it to the co-located Maven component with a builds edge.
		if def.Provider == "maven" && def.Kind == "archive" && len(def.Evidence) > 0 {
			owners := componentsByRoot[path.Dir(def.Path)]
			if len(owners) == 1 {
				addRelationship(mapdoc.EdgeBuilds, n.ID, owners[0], "maven-war:"+def.Path, "maven_pom_declares_war_packaging", deployableEvidence(def.Path, def.Evidence[0]))
			}
		}
		if def.Provider == "dockerfile" && def.Kind == "container_build" {
			owners := componentsByRoot[path.Dir(def.Path)]
			pathEvidence := append(append([]deployables.Reference(nil), def.DockerPathCopies...), def.DockerPathWrites...)
			// References are sorted by kind and value, not by line, so the final
			// stage is the one whose FROM instruction appears last in the file.
			finalStage := ""
			finalStageLine := 0
			lastInstruction := 0
			for _, item := range def.References {
				if item.Kind == "base_image_or_stage" && item.Evidence.Line > finalStageLine {
					finalStage, finalStageLine = item.Stage, item.Evidence.Line
				}
				if item.Evidence.Line > lastInstruction {
					lastInstruction = item.Evidence.Line
				}
			}
			for _, item := range def.DockerPathWrites {
				if item.Evidence.Line > lastInstruction {
					lastInstruction = item.Evidence.Line
				}
			}
			var artifactOwners []string
			artifactOwnerSet := map[string]bool{}
			var artifactEvidence mapdoc.Evidence
			for _, ref := range def.References {
				if (ref.Kind != "copy_source" && ref.Kind != "copy_source_stage") || ref.Qualification != "local" {
					continue
				}
				matches := mavenArchives[path.Base(ref.Value)]
				var matched mavenArchiveOwner
				matchedOnce := false
				for _, candidate := range matches {
					cleanSource := strings.TrimPrefix(path.Clean(ref.Value), "/")
					root := strings.TrimPrefix(path.Clean(candidate.root), "./")
					pathIdentifiesModule := !def.DockerContextUnknown && ref.Kind == "copy_source" && ref.Stage != "" && ref.Stage == finalStage && ref.TargetPath != "" && cleanSource == path.Join(root, "target", path.Base(ref.Value))
					if root == "." {
						// A bare target/<artifact> source can only identify the root
						// Maven module when the Dockerfile itself is at the repository
						// root. For nested Dockerfiles the build context is not known
						// here, so this path alone is insufficient ownership evidence.
						pathIdentifiesModule = !def.DockerContextUnknown && ref.Kind == "copy_source" && ref.Stage != "" && ref.Stage == finalStage && ref.TargetPath != "" && path.Dir(def.Path) == "." && cleanSource == path.Join("target", path.Base(ref.Value))
					}
					if pathIdentifiesModule {
						pathIdentifiesModule = dockerFinalCopySurvives(def.References, pathEvidence, ref, lastInstruction+1)
					}
					stageIdentifiesArtifact := !def.DockerContextUnknown && ref.Kind == "copy_source_stage" && ref.Evidence.Field == "COPY --from source" && ref.Stage != "" && ref.Stage == finalStage && ref.TargetPath != "" && dockerFinalCopySurvives(def.References, pathEvidence, ref, lastInstruction+1) && dockerContextIncludesModule(def.References, pathEvidence, candidate.root, ref.SourceStage, ref.SourcePath, ref.Evidence.Line)
					if pathIdentifiesModule || stageIdentifiesArtifact {
						if matchedOnce {
							matchedOnce = false // duplicate artifact identities are ambiguous
							break
						}
						matched, matchedOnce = candidate, true
					}
				}
				if matchedOnce && !artifactOwnerSet[matched.component] {
					artifactOwners = append(artifactOwners, matched.component)
					artifactOwnerSet[matched.component] = true
					artifactEvidence = deployableEvidence(def.Path, ref.Evidence)
				}
			}
			if len(artifactOwners) == 1 {
				addRelationship(mapdoc.EdgeBuilds, n.ID, artifactOwners[0], "dockerfile-artifact:"+def.Path, "dockerfile_copy_source_matches_maven_archive", artifactEvidence)
			} else if owner, evidence, ok := dockerCargoComponent(def, owners, d, intents...); ok {
				if len(owners) > 1 {
					// Preserve the prior co-location edge ID for the same selected
					// owner while strengthening its evidence and narrowing away the
					// unrelated co-located owners.
					e := mapdoc.NewEdge(mapdoc.EdgeBuilds, n.ID, owner, "dockerfile-multi:"+owner)
					if !seenEdges[e.ID] {
						seenEdges[e.ID] = true
						e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"dockerfile_copy_and_build_evidence_matches_cargo_component"}}
						e.Evidence = evidence
						d.Edges = append(d.Edges, e)
					}
				} else {
					addRelationship(mapdoc.EdgeBuilds, n.ID, owner, "dockerfile:"+def.Path, "dockerfile_copy_and_build_evidence_matches_cargo_component", evidence...)
				}
			} else if dockerHasCargoZigbuild(def) {
				// An explicit but ambiguous zigbuild target must not fall through to
				// generic co-location with the repository-root Cargo manifest.
			} else if len(owners) == 1 {
				addRelationship(mapdoc.EdgeBuilds, n.ID, owners[0], "dockerfile:"+def.Path, "dockerfile_co_located_with_component", deployableEvidence(def.Path, def.Evidence[0]))
			} else if len(owners) > 1 {
				// Multiple components share the same directory as this Dockerfile
				// (e.g. a monorepo root with Ruby and Node components). Emit a
				// partial builds edge to each so the graph retains the co-location
				// signal; callers should treat these as hints, not proof. The reason
				// "dockerfile_co_located_with_multiple_components" distinguishes
				// this case from the unambiguous single-component case.
				ev := deployableEvidence(def.Path, def.Evidence[0])
				for _, ownerID := range owners {
					e := mapdoc.NewEdge(mapdoc.EdgeBuilds, n.ID, ownerID, "dockerfile-multi:"+ownerID)
					if seenEdges[e.ID] {
						continue
					}
					seenEdges[e.ID] = true
					e.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"dockerfile_co_located_with_multiple_components"}}
					e.Evidence = []mapdoc.Evidence{ev}
					d.Edges = append(d.Edges, e)
				}
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
			if def.Provider == "kubernetes" || def.Provider == "helm" {
				imageUsers = append(imageUsers, struct {
					id       string
					image    string
					evidence mapdoc.Evidence
				}{n.ID, image.name, image.evidence})
			}
		}
		if def.Provider == "terraform" {
			dir := path.Clean(path.Dir(def.Path))
			terraformNodeByDir[dir] = n.ID
			for _, ref := range def.References {
				if ref.Kind == "module_source" && ref.Qualification == "local" {
					terraformModuleRefs = append(terraformModuleRefs, terraformModuleRef{fromID: n.ID, fromDir: dir, source: ref.Value, evidence: deployableEvidence(def.Path, ref.Evidence)})
				}
			}
		}
		d.Nodes = append(d.Nodes, n)
	}
	for _, ref := range terraformModuleRefs {
		targetDir := path.Clean(path.Join(ref.fromDir, ref.source))
		if targetDir == ".." || strings.HasPrefix(targetDir, "../") || strings.HasPrefix(targetDir, "/") {
			continue
		}
		if targetID := terraformNodeByDir[targetDir]; targetID != "" {
			addRelationship(mapdoc.EdgeDependsOnLocal, ref.fromID, targetID, "terraform-module:"+targetDir, "terraform_declares_local_module_source", ref.evidence)
		}
	}
	for _, binding := range contextDockerfiles {
		if dockerID := dockerNodesByPath[binding.file]; dockerID != "" {
			addRelationship(mapdoc.EdgeBuilds, dockerID, binding.component, "declared-build-context:"+binding.file, "declared_context_matches_component_root", binding.evidence)
		}
	}
	if r != nil {
		for _, binding := range r.BuildContexts {
			// Root Makefile recipes run from the invocation directory (the
			// repository root), and Docker resolves both -f and context paths
			// from that directory. Compose differs: its Dockerfile is relative
			// to the declared build context.
			base := path.Dir(binding.SourcePath)
			resolved := path.Clean(path.Join(base, binding.Context))
			owner, _ := localPathOwner(componentsByRoot, resolved, false)
			if owner == "" {
				continue
			}
			dockerPath := path.Clean(path.Join(base, binding.Dockerfile))
			dockerID := dockerNodesByPath[dockerPath]
			if dockerID == "" {
				continue
			}
			addRelationship(mapdoc.EdgeBuilds, dockerID, owner, "makefile-context:"+dockerPath, "declared_context_matches_component_root",
				deployableEvidence(binding.SourcePath, binding.ContextEvidence), deployableEvidence(binding.SourcePath, binding.FileEvidence),
				mapdoc.Evidence{Basis: mapdoc.BasisResolvedReference, Path: dockerPath, SourceKind: mapdoc.SourceFile, Rule: &mapdoc.Producer{ID: "dircue/selected-inventory", Version: "1.0.0"}})
		}
	}
	// Emit depends_on edges for compose service dependencies declared via depends_on.
	// Both sides must be known compose service deployable nodes.
	for _, dep := range composeDeps {
		toID, ok := composeServiceByID[dep.toName]
		if !ok {
			continue
		}
		e := mapdoc.NewEdge(mapdoc.EdgeDependsOn, dep.fromID, toID, "compose-depends_on:"+dep.toName)
		if seenEdges[e.ID] {
			continue
		}
		seenEdges[e.ID] = true
		e.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		e.Evidence = []mapdoc.Evidence{dep.evidence}
		d.Edges = append(d.Edges, e)
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

func dockerFinalCopySurvives(refs, pathEvidence []deployables.Reference, copied deployables.Reference, endLine int) bool {
	for _, artifactPath := range dockerCopiedArtifactPaths(copied) {
		if !dockerNoInterveningWrites(pathEvidence, copied.Stage, artifactPath, copied.Evidence.Line, endLine) {
			return false
		}
		if !dockerNoInterveningWrites(refs, copied.Stage, artifactPath, copied.Evidence.Line, endLine) {
			return false
		}
	}
	return true
}

// dockerCopiedArtifactPaths returns where a copied archive may land. A target
// ending in "/" is a directory. A target without a slash is ambiguous when it
// does not itself name an archive: Docker writes the file there, or into it
// when it is an existing directory, so both locations must survive.
func dockerCopiedArtifactPaths(copied deployables.Reference) []string {
	target := copied.TargetPath
	if strings.HasSuffix(target, "/") {
		return []string{path.Join(target, path.Base(copied.Value))}
	}
	if dockerArchiveName(path.Base(copied.Value)) && !dockerArchiveName(path.Base(target)) {
		return []string{target, path.Join(target, path.Base(copied.Value))}
	}
	return []string{target}
}

func dockerArchiveName(name string) bool {
	return strings.HasSuffix(name, ".war") || strings.HasSuffix(name, ".ear")
}

// dockerContextIncludesModule traces a copied artifact backward through
// explicit stage and RUN cp transfers. The trace must reach the Maven module's
// target directory with the same archive basename. A broad build-context COPY
// is not ownership evidence: a later ADD or RUN can replace the artifact.
func dockerContextIncludesModule(refs, pathCopies []deployables.Reference, moduleRoot, sourceStage, artifactPath string, stageCopyLine int) bool {
	if sourceStage == "" || artifactPath == "" {
		return false
	}
	allRefs := append(append([]deployables.Reference(nil), refs...), pathCopies...)
	// Walk only backward through explicit local COPY --from relationships. This
	// handles staged builds such as OpenMRS (compile -> dev -> final) while
	// rejecting a context COPY made in a stage unrelated to the copied artifact.
	type stageAt struct {
		name         string
		artifactPath string
		beforeLine   int
	}
	queue := []stageAt{{name: sourceStage, artifactPath: path.Clean(artifactPath), beforeLine: stageCopyLine}}
	visited := map[string]int{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visitKey := current.name + "\x00" + current.artifactPath
		if prior, seen := visited[visitKey]; seen && prior >= current.beforeLine {
			continue
		}
		visited[visitKey] = current.beforeLine
		for _, ref := range allRefs {
			if ref.Stage != current.name || ref.Evidence.Line >= current.beforeLine {
				continue
			}
			if ref.Kind == "copy_source" && ref.Qualification == "local" && ref.TargetPath != "" && dockerCopyPathCarries(ref.TargetPath, current.artifactPath) {
				sourcePath := dockerCopyMappedSource(ref.Value, ref.TargetPath, current.artifactPath)
				ident := dockerArtifactPathIdentifiesModule(sourcePath, moduleRoot)
				clean := dockerNoInterveningWrites(allRefs, current.name, current.artifactPath, ref.Evidence.Line, current.beforeLine)
				if ident && clean {
					return true
				}
			}
			if ref.Kind == "copy_from" && ref.Qualification == "local" && ref.SourceStage != "" && ref.SourcePath != "" && dockerCopyPathCarries(ref.TargetPath, current.artifactPath) {
				if !dockerNoInterveningWrites(allRefs, current.name, current.artifactPath, ref.Evidence.Line, current.beforeLine) {
					continue
				}
				sourcePath := dockerCopyMappedSource(ref.SourcePath, ref.TargetPath, current.artifactPath)
				queue = append(queue, stageAt{name: ref.SourceStage, artifactPath: sourcePath, beforeLine: ref.Evidence.Line})
			}
			if ref.Kind == "run_copy" && ref.Qualification == "local" && ref.SourcePath != "" && dockerCopyPathCarries(ref.TargetPath, current.artifactPath) {
				if !dockerNoInterveningWrites(allRefs, current.name, current.artifactPath, ref.Evidence.Line, current.beforeLine) {
					continue
				}
				if !dockerRunAtLineIsSafe(allRefs, current.name, current.artifactPath, ref.Evidence.Line) {
					continue
				}
				sourcePath := dockerCopyMappedSource(ref.SourcePath, ref.TargetPath, current.artifactPath)
				queue = append(queue, stageAt{name: current.name, artifactPath: sourcePath, beforeLine: ref.Evidence.Line})
			}
		}
	}
	return false
}

func dockerNoInterveningWrites(refs []deployables.Reference, stage, artifactPath string, copyLine, artifactLine int) bool {
	for _, ref := range refs {
		if ref.Stage != stage || ref.Evidence.Line <= copyLine || ref.Evidence.Line >= artifactLine {
			continue
		}
		switch ref.Kind {
		case "add_source":
			return false
		case "run_instruction_opaque":
			return false
		case "copy_opaque":
			if ref.TargetPath == "" || dockerWriteAffects(ref.TargetPath, artifactPath) {
				return false
			}
		case "copy_source", "copy_source_stage":
			// A later COPY in the same stage can replace the artifact.
			if ref.TargetPath == "" || dockerWriteAffects(dockerCopyWrittenPath(ref), artifactPath) {
				return false
			}
		case "copy_from":
			if ref.TargetPath == "" {
				return false
			}
		case "run_instruction":
			if !dockerRunIsKnownBuildOrCopy(refs, ref, artifactPath) {
				return false
			}
			for _, write := range refs {
				if write.Kind == "run_copy" && write.Stage == stage && write.Evidence.Line == ref.Evidence.Line && dockerWriteAffects(write.TargetPath, artifactPath) {
					return false
				}
			}
		}
	}
	return true
}

// dockerCopyWrittenPath is the path a COPY source writes. A source that looks
// like a single file copied into a directory writes only <dir>/<name>; any
// other source may write anywhere below its target.
func dockerCopyWrittenPath(ref deployables.Reference) string {
	target := ref.TargetPath
	if !strings.HasSuffix(target, "/") {
		return target
	}
	// A source ending in "/", without an extension, or with a directory-like
	// suffix (conf.d, lib-1.2) may be a directory whose contents land anywhere
	// below the target. Other names are read as a single file.
	if strings.HasSuffix(ref.Value, "/") {
		return target
	}
	base := path.Base(path.Clean(ref.Value))
	ext := path.Ext(base)
	if ext == "" || ext == base || base == "." || base == ".." || strings.ContainsAny(base, "*?") || dockerDirectoryLikeExt.MatchString(ext) {
		return target
	}
	return path.Join(target, base)
}

var dockerDirectoryLikeExt = regexp.MustCompile(`^\.(d|[0-9]+)$`)

// dockerWriteAffects reports whether writing written can replace artifact:
// the same path, or an ancestor directory whose contents are written. A write
// strictly below artifact means artifact is a directory, not the archive file.
func dockerWriteAffects(written, artifact string) bool {
	written = path.Clean(written)
	artifact = path.Clean(artifact)
	return written == artifact || strings.HasPrefix(artifact, strings.TrimSuffix(written, "/")+"/")
}

func dockerRunIsKnownBuildOrCopy(refs []deployables.Reference, run deployables.Reference, artifactPath string) bool {
	command := strings.TrimSpace(run.Value)
	if strings.HasPrefix(strings.ToUpper(command), "RUN ") {
		command = strings.TrimSpace(command[4:])
	}
	command = strings.TrimPrefix(command, "&& ")
	command = strings.TrimSuffix(command, "\\")
	if strings.ContainsAny(command, "|;`<>") || strings.Contains(command, "||") || strings.Contains(command, "$(") || dockerBackgroundJob.MatchString(strings.ReplaceAll(command, "&&", " ")) {
		return false
	}
	for _, segment := range strings.Split(command, "&&") {
		segment = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(segment), "\\"))
		fields := strings.Fields(segment)
		// Leading VAR=value assignments only set the command's environment,
		// but the command can read them, so substitute them into its words.
		assigned := map[string]string{}
		for len(fields) > 0 && dockerEnvAssignment.MatchString(fields[0]) {
			name, value, _ := strings.Cut(fields[0], "=")
			assigned[name] = strings.Trim(value, `"'`)
			fields = fields[1:]
		}
		if len(assigned) > 0 {
			for i, field := range fields {
				fields[i] = dockerInlineVar.ReplaceAllStringFunc(field, func(ref string) string {
					m := dockerInlineVar.FindStringSubmatch(ref)
					name := m[1]
					if name == "" {
						name = m[2]
					}
					if value, ok := assigned[name]; ok {
						return value
					}
					return ref
				})
			}
			fields = strings.Fields(strings.Join(fields, " "))
		}
		if len(fields) == 0 {
			return false
		}
		tool := strings.ToLower(path.Base(fields[0]))
		switch tool {
		case "mvn", "mvnw", "mvn.cmd":
			if !dockerMavenBuildOnly(fields[1:]) {
				return false
			}
			continue
		case "mkdir", "chmod", "chown":
			continue
		case "apt-get", "apt", "apk", "yum", "dnf", "microdnf":
			// OS package operations write system locations, not the build
			// output being traced. Other subcommands stay unvetted.
			if !dockerPackageManagerOnly(fields[1:]) {
				return false
			}
			continue
		case "rm":
			for _, field := range fields[1:] {
				if strings.HasPrefix(field, "-") {
					continue
				}
				if !strings.HasPrefix(field, "/") {
					return false
				}
				if dockerPathsOverlap(field, artifactPath) {
					return false
				}
			}
			continue
		case "cp":
			var operands []string
			for _, field := range fields[1:] {
				if strings.HasPrefix(field, "-") {
					continue
				}
				operands = append(operands, field)
			}
			if len(operands) > 2 {
				// GNU cp places each source under the final directory. Treat
				// the command as harmless only when every concrete destination
				// is disjoint from the artifact being traced. Multi-source cp is
				// never itself path-transfer evidence.
				destination := operands[len(operands)-1]
				if !strings.HasPrefix(destination, "/") || strings.ContainsAny(destination, "*?[]{}$\\\"'") {
					return false
				}
				for _, source := range operands[:len(operands)-1] {
					if !strings.HasPrefix(source, "/") || strings.ContainsAny(source, "*?[]{}$\\\"'") || dockerPathsOverlap(path.Join(destination, path.Base(source)), artifactPath) {
						return false
					}
				}
				continue
			}
			if len(operands) != 2 {
				return false
			}
			foundCopy := false
			for _, ref := range refs {
				if ref.Kind == "run_copy" && ref.Stage == run.Stage && ref.Evidence.Line == run.Evidence.Line && ref.SourcePath == operands[0] && ref.TargetPath == operands[1] {
					foundCopy = true
					break
				}
			}
			if !foundCopy {
				return false
			}
		default:
			return false
		}
	}
	return true
}

var (
	dockerEnvAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	dockerInlineVar     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	dockerBackgroundJob = regexp.MustCompile(`(^|\s)&(\s|$)`)
	dockerBareVariable  = regexp.MustCompile(`^\$(\{[A-Za-z_][A-Za-z0-9_]*\}|[A-Za-z_][A-Za-z0-9_]*)$`)
	mavenPhases         = map[string]bool{"validate": true, "initialize": true, "generate-sources": true, "process-sources": true, "generate-resources": true, "process-resources": true, "compile": true, "process-classes": true, "generate-test-sources": true, "process-test-sources": true, "generate-test-resources": true, "process-test-resources": true, "test-compile": true, "process-test-classes": true, "test": true, "prepare-package": true, "package": true, "pre-integration-test": true, "integration-test": true, "post-integration-test": true, "verify": true, "install": true, "deploy": true, "pre-clean": true, "clean": true, "post-clean": true}
)

// dockerMavenBuildOnly accepts lifecycle phases, options and variables that
// have no static value. Any other word, including a plugin goal such as
// dependency:copy that can place a downloaded artifact in target/, fails
// closed.
func dockerMavenBuildOnly(args []string) bool {
	takesValue := map[string]bool{"-f": true, "--file": true, "-s": true, "--settings": true, "-gs": true, "--global-settings": true, "-pl": true, "--projects": true, "-rf": true, "--resume-from": true, "-P": true, "--activate-profiles": true, "-T": true, "--threads": true, "-t": true, "--toolchains": true, "-l": true, "--log-file": true, "-D": true, "--define": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if takesValue[arg] {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") || dockerBareVariable.MatchString(arg) {
			continue
		}
		if !mavenPhases[arg] {
			return false
		}
	}
	return true
}

func dockerPackageManagerOnly(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "update", "upgrade", "install", "add", "clean", "autoremove", "remove", "del", "purge", "makecache":
			return true
		}
		return false
	}
	return false
}

func dockerRunAtLineIsSafe(refs []deployables.Reference, stage, artifactPath string, line int) bool {
	found := false
	for _, ref := range refs {
		if ref.Kind == "run_instruction_opaque" && ref.Stage == stage && ref.Evidence.Line == line {
			return false
		}
		if ref.Kind == "run_instruction" && ref.Stage == stage && ref.Evidence.Line == line {
			found = dockerRunIsKnownBuildOrCopy(refs, ref, artifactPath)
		}
	}
	return found
}

func dockerPathsOverlap(a, b string) bool {
	a = path.Clean(a)
	b = path.Clean(b)
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/") || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/")
}

func dockerArtifactPathIdentifiesModule(artifactPath, moduleRoot string) bool {
	artifact := strings.Trim(strings.TrimPrefix(path.Clean(artifactPath), "./"), "/")
	root := strings.Trim(strings.TrimPrefix(path.Clean(moduleRoot), "./"), "/")
	if root == "." {
		root = ""
	}
	if artifact == "" {
		return false
	}
	suffix := "target/" + path.Base(artifact)
	if root != "" {
		suffix = root + "/" + suffix
	}
	return artifact == suffix
}

func dockerCopyMappedSource(source, destination, artifact string) string {
	source = path.Clean(source)
	destinationIsDir := strings.HasSuffix(destination, "/")
	destination = path.Clean(destination)
	artifact = path.Clean(artifact)
	if artifact == destination {
		return source
	}
	if destinationIsDir && path.Base(source) == path.Base(artifact) && (strings.HasSuffix(path.Base(source), ".war") || strings.HasSuffix(path.Base(source), ".ear")) {
		return source
	}
	relative := strings.TrimPrefix(artifact, destination+"/")
	return path.Join(source, relative)
}

func dockerCopyPathCarries(destination, artifact string) bool {
	if destination == "" || artifact == "" {
		return false
	}
	destination = path.Clean(destination)
	artifact = path.Clean(artifact)
	return artifact == destination || strings.HasPrefix(artifact, strings.TrimSuffix(destination, "/")+"/")
}

// aggregateTerraformDefs collapses multiple per-file Terraform definitions
// for the same module directory into one representative definition. The merged
// definition holds all evidence items and references from the constituent files.
// Path is set to the first .tf file (for evidence source attribution); callers
// that build mapdoc nodes should use path.Dir(def.Path) for the node's identity
// paths so the node ID remains stable across changes in which .tf files exist.
// Non-Terraform definitions are returned unchanged.
func aggregateTerraformDefs(defs []deployables.Definition) []deployables.Definition {
	type dirState struct {
		firstPath string
		evidence  []deployables.Evidence
		refs      []deployables.Reference
		coverage  string
	}
	tfByDir := map[string]*dirState{}
	tfOrder := []string{}
	var out []deployables.Definition
	for _, def := range defs {
		if def.Provider != "terraform" {
			out = append(out, def)
			continue
		}
		dir := path.Dir(def.Path)
		if dir == "." {
			dir = "."
		}
		st := tfByDir[dir]
		if st == nil {
			st = &dirState{firstPath: def.Path, coverage: def.Coverage}
			tfByDir[dir] = st
			tfOrder = append(tfOrder, dir)
		}
		st.evidence = append(st.evidence, def.Evidence...)
		st.refs = append(st.refs, def.References...)
		// If any file is partial, the whole module is partial.
		if def.Coverage != "complete" {
			st.coverage = "qualified"
		}
	}
	for _, dir := range tfOrder {
		st := tfByDir[dir]
		// Use "(root)" for the repository root module so that it displays as
		// "(root) [infrastructure]" rather than the misleading "main.tf".
		// Non-root modules use their directory's base name (e.g. "modules/vpc"
		// → "vpc"), which is the conventional Terraform module identity.
		moduleName := path.Base(dir)
		if moduleName == "." || moduleName == "" {
			moduleName = "(root)"
		}
		// Path is set to the primary .tf file so that evidence items reference
		// a real source file (required by the quality gate's evidence-path filter
		// and by schema validation). The mapdoc node uses path.Dir(def.Path) for
		// its identity paths, making the node ID stable even as .tf files are
		// added or removed from the module.
		d := deployables.Definition{
			Kind:       "infrastructure",
			Provider:   "terraform",
			Name:       moduleName,
			Path:       st.firstPath,
			Coverage:   st.coverage,
			Evidence:   st.evidence,
			References: st.refs,
		}
		d.ID = d.Kind + ":" + d.Provider + ":" + dir + "#" + d.Name
		out = append(out, d)
	}
	return out
}

// aggregateKubernetesDefs collapses multiple per-file Kubernetes and Tekton
// definitions that declare the same logical object into a single representative
// definition. The grouping key is (Provider, K8sKind, Name, Namespace,
// componentScope): definitions that differ in any of those fields are kept
// separate. All declaring files become evidence items, capped at 20; the actual
// count is stored in Count so addDeployables can expose it as declaration_count.
//
// The component scope is the nearest ancestor directory that is the root of an
// existing component. Objects from different component scopes (different
// services in a monorepo) are never merged, ensuring that a "frontend" workload
// in service A is not conflated with "frontend" in service B.
//
// Non-Kubernetes/Tekton definitions are returned unchanged.
func aggregateKubernetesDefs(defs []deployables.Definition, componentsByRoot map[string][]string) []deployables.Definition {
	type groupKey struct {
		provider  string
		k8sKind   string
		name      string
		namespace string
		scope     string
	}
	type groupState struct {
		primary  deployables.Definition
		allPaths []string // all declaring file paths
		evidence []deployables.Evidence
		refs     []deployables.Reference
		count    int
		coverage string
	}
	groups := map[groupKey]*groupState{}
	order := []groupKey{}
	var out []deployables.Definition

	for _, def := range defs {
		if def.Provider != "kubernetes" && def.Provider != "tekton" {
			out = append(out, def)
			continue
		}
		if def.K8sKind == "" {
			// Safety: no k8s kind recorded; pass through unchanged.
			out = append(out, def)
			continue
		}
		scope := k8sComponentScope(componentsByRoot, path.Dir(def.Path))
		key := groupKey{
			provider:  def.Provider,
			k8sKind:   def.K8sKind,
			name:      def.Name,
			namespace: def.Namespace,
			scope:     scope,
		}
		st := groups[key]
		if st == nil {
			st = &groupState{
				primary:  def,
				coverage: def.Coverage,
			}
			groups[key] = st
			order = append(order, key)
		}
		st.allPaths = append(st.allPaths, def.Path)
		st.evidence = append(st.evidence, def.Evidence...)
		st.refs = append(st.refs, def.References...)
		st.count++
		if def.Coverage != "complete" {
			st.coverage = "qualified"
		}
	}

	for _, key := range order {
		st := groups[key]
		d := st.primary
		// Cap evidence at 20 items; the full count is in Count.
		evidence := st.evidence
		if len(evidence) > 20 {
			evidence = evidence[:20]
		}
		// Deduplicate references (same image may appear across many files).
		seen := map[string]bool{}
		var refs []deployables.Reference
		for _, r := range st.refs {
			k := r.Kind + "\x00" + r.Value
			if !seen[k] {
				seen[k] = true
				refs = append(refs, r)
			}
		}
		d.Evidence = evidence
		d.References = refs
		d.Coverage = st.coverage
		d.Count = st.count
		out = append(out, d)
	}
	return out
}

// k8sComponentScope returns the component ID (or directory path as fallback)
// for the deepest component root that is an ancestor of dir. Returns "" when
// no component owns the path.
func k8sComponentScope(componentsByRoot map[string][]string, dir string) string {
	p := dir
	for {
		if ids := componentsByRoot[p]; len(ids) == 1 {
			return ids[0]
		}
		if len(componentsByRoot[p]) > 1 {
			// Multiple components at this level: use the directory as the scope
			// to prevent cross-component merging without a clear owner.
			return p
		}
		parent := path.Dir(p)
		if parent == p {
			break
		}
		p = parent
		if p == "." {
			if ids := componentsByRoot["."]; len(ids) == 1 {
				return ids[0]
			}
			break
		}
	}
	return ""
}

// Reasons recorded on a local path reference that is not attributed.
const (
	reasonPathOutsideRepository  = "path_outside_repository"
	reasonAmbiguousComponentRoot = "ambiguous_component_root"
	reasonNamedRepository        = "named_repository_checkout"
	reasonPathNotInRepository    = "path_not_in_repository"
)

// workingDirectoryOwner attributes a workflow working directory, which
// resolves from the workspace root. Below an actions/checkout `path:` of this
// repository in the same job it names that path inside the repository. Below a
// checkout that names a repository it is not attributed: the map cannot tell
// offline whether that repository is this one. Any other directory the committed
// tree lacks is created at run time and is not attributed to the project that
// encloses it; absence is only asserted when the content inventory saw every
// file.
func workingDirectoryOwner(d *mapdoc.Document, r *deployables.Report, ref deployables.Reference, componentsByRoot map[string][]string, dir string) (string, string) {
	if dir == ".." || strings.HasPrefix(dir, "../") {
		return localPathOwner(componentsByRoot, dir, true)
	}
	if ref.Checkout != "" {
		if ref.CheckoutNamed {
			return "", reasonNamedRepository
		}
		dir = strings.TrimPrefix(strings.TrimPrefix(dir, ref.Checkout), "/")
		if dir == "" {
			dir = "."
		}
	} else if dir != "." && r.Directories != nil && !r.Directories[dir] && contentComplete(d) {
		return "", reasonPathNotInRepository
	}
	return localPathOwner(componentsByRoot, dir, true)
}

// contentComplete reports whether the content inventory saw every committed
// file, so every committed directory is known. Incomplete format observation
// does not affect the file set; any other content reason does.
func contentComplete(d *mapdoc.Document) bool {
	for _, q := range d.Coverage {
		if q.Question != "content" {
			continue
		}
		if q.Coverage.Status == mapdoc.CoverageComplete {
			return true
		}
		for _, reason := range q.Coverage.Reasons {
			if reason != "format_observations_incomplete" {
				return false
			}
		}
		return q.Coverage.Status == mapdoc.CoveragePartial && len(q.Coverage.Reasons) > 0
	}
	return false
}

// localPathOwner returns the unique component whose root is resolved or, when
// walkAncestors is set, its nearest ancestor root. When attribution is refused
// it returns a coverage reason instead: the path leaves the repository, or the
// nearest root with components holds more than one. Both empty means no
// component root matched.
func localPathOwner(componentsByRoot map[string][]string, resolved string, walkAncestors bool) (string, string) {
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", reasonPathOutsideRepository
	}
	owners := componentsByRoot[resolved]
	ambiguous := len(owners) > 1
	if len(owners) == 0 && walkAncestors {
		owners, ambiguous = componentAncestorOwners(componentsByRoot, resolved)
	}
	if ambiguous {
		return "", reasonAmbiguousComponentRoot
	}
	if len(owners) == 1 {
		return owners[0], ""
	}
	return "", ""
}

// componentAncestorOwners walks up the directory tree from resolvedPath and
// returns the IDs of components whose root is an ancestor. Returns non-empty
// only when exactly one unique component root matches at the closest level.
// The second return value is true when the walk was stopped by an ambiguous
// (>1 component) nearest root, distinguishing "ambiguous" from "no match".
// This is used for CodeUri references that point to build artifacts rather
// than source directories (e.g. target/app.jar).
//
// Paths that resolve outside the repository (resolvedPath == ".." or starting
// with "../") are never attributed and return (nil, false).
func componentAncestorOwners(componentsByRoot map[string][]string, resolvedPath string) ([]string, bool) {
	if resolvedPath == ".." || strings.HasPrefix(resolvedPath, "../") {
		return nil, false
	}
	p := resolvedPath
	for {
		if ids := componentsByRoot[p]; len(ids) > 0 {
			// A nearer root is authoritative even when its ownership is
			// ambiguous. Falling through would incorrectly attribute the path
			// to a broader component.
			if len(ids) == 1 {
				return ids, false
			}
			return nil, true // ambiguous nearest root
		}
		parent := path.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return nil, false
}

// aspireProjectOwners resolves an Aspire Projects.X identifier to a component.
// It tries exact match, then replaces underscores with dots (the Aspire
// convention maps "Identity_API" to the project named "Identity.API").
func aspireProjectOwners(componentsByRoot, componentsByName map[string][]string, projectIdent string) []string {
	// Exact match by project identifier (e.g. Projects.OrderProcessor → "OrderProcessor").
	if ids := componentsByName[projectIdent]; len(ids) == 1 {
		return ids
	}
	// Convert underscores to dots: Projects.Basket_API → "Basket.API"
	normalized := strings.ReplaceAll(projectIdent, "_", ".")
	if normalized != projectIdent {
		if ids := componentsByName[normalized]; len(ids) == 1 {
			return ids
		}
	}
	// Try root suffix match: find a component whose root ends with the identifier.
	var candidates []string
	for root, ids := range componentsByRoot {
		base := path.Base(root)
		if strings.EqualFold(base, projectIdent) || strings.EqualFold(base, normalized) {
			candidates = append(candidates, ids...)
		}
	}
	if len(candidates) == 1 {
		return candidates
	}
	return nil
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

// maxCapabilityEvidencePaths caps the number of evidence paths per capability
// node. The actual count is stored in the node's "evidence_path_count"
// property so consumers can tell whether paths were omitted.
const maxCapabilityEvidencePaths = 20

// capGroup accumulates observations for a single (capability, component) pair.
type capGroup struct {
	name                          string
	projectID                     string
	attribution                   string
	state                         string
	basis                         string
	paths                         []string
	evidence                      []mapdoc.Evidence
	properties                    map[string]string
	total                         int // total distinct (path, name) observations before capping
	testEvidence, nonTestEvidence bool
	typeOnlyImport, nonTypeImport bool
	otherEvidence                 bool
	runtimeImportEvidence         bool
}

func (g *capGroup) worstState(o intentmap.Observation) {
	// partial < conditional < declared < observed (worst = partial)
	if o.State == "partial" || o.State == "unresolved" {
		g.state = "partial"
		return
	}
	if g.state == "partial" {
		return
	}
	if o.State == "conditional" {
		if !g.runtimeImportEvidence && (g.state == "" || g.state == "observed") {
			g.state = "conditional"
		}
		return
	}
	if o.State == "declared" {
		if g.state == "" || g.state == "conditional" {
			g.state = "declared"
		}
		return
	}
	if o.State == "observed" {
		runtimeEvidence := o.Basis != "imported" || (o.Properties["import_qualifier"] != "type_only" && o.Properties["evidence_scope"] != "test_path_convention")
		if runtimeEvidence {
			g.runtimeImportEvidence = true
			g.state = "observed"
		} else if g.state == "" {
			// Without a conditional declaration the source evidence remains a
			// useful observation, while its qualifier stays available to filters.
			g.state = "observed"
		}
	}
}

func addIntent(d *mapdoc.Document, r *intentmap.Report) {
	coverage := mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"bounded_interface_and_capability_catalog"}}
	if r.Coverage.Status != "complete" {
		coverage.Reasons = append(coverage.Reasons, "interface_or_capability_observations_incomplete")
	}
	setQuestion(d, "interfaces", coverage)
	capabilityCoverage := coverage
	capabilityCoverage.Reasons = slices.Clone(coverage.Reasons)
	if r.Coverage.Omissions["import_token_limit"] > 0 {
		capabilityCoverage.Reasons = append(capabilityCoverage.Reasons, "import_token_limit_reached")
	}
	setQuestion(d, "capabilities", capabilityCoverage)
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

	// ── Capability aggregation ─────────────────────────────────────────────
	// One capability node per (capability name, component). All evidence paths
	// are attached to that single node; the path count is stored as a property.
	capGroups := map[string]*capGroup{} // key: name + "\x00" + projectID
	capGroupOrder := []string{}         // insertion order for determinism
	for _, o := range r.Observations {
		if o.Kind != intentmap.KindCapability {
			continue
		}
		gk := o.Name + "\x00" + o.ProjectID
		g := capGroups[gk]
		if g == nil {
			g = &capGroup{
				name:        o.Name,
				projectID:   o.ProjectID,
				attribution: o.ProjectAttribution,
				basis:       o.Basis,
				properties:  cloneProps(o.Properties),
			}
			capGroups[gk] = g
			capGroupOrder = append(capGroupOrder, gk)
		}
		g.total++
		if o.Basis != "imported" && o.Basis != "code_syntax" {
			g.otherEvidence = true
		}
		if o.Properties["import_qualifier"] == "type_only" {
			g.typeOnlyImport = true
		} else if o.Basis == "imported" {
			g.nonTypeImport = true
		}
		if o.Properties["evidence_scope"] == "test_path_convention" {
			g.testEvidence = true
		} else if o.Properties["evidence_scope"] == "non_test_path_convention" {
			g.nonTestEvidence = true
		}
		g.worstState(o)
		// Multiple bases → prefer declared_config > declared_dependency > others
		if g.basis == "" || (o.Basis == "declared_config" && g.basis != "declared_config") {
			g.basis = o.Basis
		}
		if len(g.paths) < maxCapabilityEvidencePaths {
			g.paths = append(g.paths, o.Path)
			g.evidence = append(g.evidence, intentEvidence(o))
		}
	}
	slices.Sort(capGroupOrder)
	for _, gk := range capGroupOrder {
		g := capGroups[gk]
		nodePaths := slices.Clone(g.paths)
		n := mapdoc.NewNode(mapdoc.NodeCapability, nodePaths, "capability:"+g.name+":"+g.projectID)
		n.Name = g.name
		n.Properties = map[string]string{
			"observation_kind":    "capability",
			"state":               g.state,
			"basis":               g.basis,
			"evidence_path_count": strconv.Itoa(g.total),
		}
		if role := capPathRole(g.paths...); role != "" {
			n.Properties["role"] = role
			n.Properties["role_basis"] = "path_name"
		}
		if g.testEvidence {
			n.Properties["test_path_evidence"] = "true"
		}
		if g.nonTestEvidence {
			n.Properties["non_test_path_evidence"] = "true"
		}
		if g.testEvidence && !g.nonTestEvidence && !g.otherEvidence {
			n.Properties["test_only_evidence"] = "true"
			n.Properties["test_evidence_scope_basis"] = "path_name_convention"
		}
		if g.typeOnlyImport {
			n.Properties["type_only_import_evidence"] = "true"
		}
		if g.nonTypeImport {
			n.Properties["non_type_only_import_evidence"] = "true"
		}
		for k, v := range g.properties {
			if k == "evidence_scope" || k == "import_qualifier" {
				continue
			}
			if _, exists := n.Properties[k]; !exists {
				n.Properties[k] = v
			}
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if g.state == "partial" || g.state == "unresolved" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"observation_" + g.state}}
		} else if g.state == "conditional" {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"optional_dependency"}}
		}
		n.Evidence = slices.Clone(g.evidence)
		if g.projectID != "" {
			if owner := componentsByManifest[g.projectID]; owner != "" {
				n.Properties["owning_component"] = owner
				edgeCoverage := n.Coverage
				edgeReasons := slices.Clone(edgeCoverage.Reasons)
				if g.attribution == "directory_containment" {
					edgeReasons = append(edgeReasons, "attributed_by_directory_containment")
					slices.Sort(edgeReasons)
					edgeCoverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: edgeReasons}
				}
				e := mapdoc.NewEdge(mapdoc.EdgeUsesCapability, owner, n.ID, "capability:"+g.name+":"+g.projectID)
				e.Coverage = edgeCoverage
				e.Evidence = slices.Clone(n.Evidence)
				d.Edges = append(d.Edges, e)
			}
		}
		d.Nodes = append(d.Nodes, n)
	}

	// ── Prerequisite / build-backend / build-script recording ─────────────
	// These are toolchain declarations, not interface endpoints. Record them
	// as structured properties on the owning component node rather than
	// emitting them as separate interface nodes.
	//
	// Aggregate by (kind, projectID) so multiple engines (node + yarn) for the
	// same component are joined as a semicolon-separated runtime_requirements
	// string on the component's Properties.
	componentNodeIdx := map[string]int{} // component ID → index in d.Nodes
	for i, n := range d.Nodes {
		if n.Kind == mapdoc.NodeComponent {
			componentNodeIdx[n.ID] = i
		}
	}
	runtimeReqs := map[string][]string{} // ownerID → ["node >= 18", "yarn ^4.0.0", ...]
	for _, o := range r.Observations {
		if o.Kind != intentmap.KindInterface {
			continue
		}
		ikind := o.Properties["interface_kind"]
		if ikind != "prerequisite" && ikind != "python-build-backend" && ikind != "cargo-build-script" {
			continue
		}
		ownerID := componentsByManifest[o.ProjectID]
		if ownerID == "" {
			continue
		}
		switch ikind {
		case "prerequisite":
			entry := o.Name
			// The target is the declared range as written (">= 18",
			// "^4.0.0"); it is recorded verbatim after the tool name.
			if t := o.Properties["target"]; t != "" {
				entry += " " + t
			}
			runtimeReqs[ownerID] = append(runtimeReqs[ownerID], entry)
		case "python-build-backend":
			if idx, ok := componentNodeIdx[ownerID]; ok {
				d.Nodes[idx].Properties["build_backend"] = o.Name
			}
		case "cargo-build-script":
			if idx, ok := componentNodeIdx[ownerID]; ok {
				d.Nodes[idx].Properties["build_script"] = o.Name
			}
		}
	}
	for ownerID, reqs := range runtimeReqs {
		if idx, ok := componentNodeIdx[ownerID]; ok {
			slices.Sort(reqs)
			d.Nodes[idx].Properties["runtime_requirements"] = strings.Join(reqs, "; ")
		}
	}

	// ── Interface observations ─────────────────────────────────────────────
	// One node per (path, interface name, project). Interfaces are inherently
	// distinct (different ports, different binaries, different proto services).
	// Toolchain kinds (prerequisite, python-build-backend, cargo-build-script)
	// are handled above as component properties and are excluded here.
	seen := map[string]bool{}
	for _, o := range r.Observations {
		if o.Kind != intentmap.KindInterface && o.Kind != intentmap.KindConfig {
			continue
		}
		if o.Kind == intentmap.KindInterface {
			ikind := o.Properties["interface_kind"]
			if ikind == "prerequisite" || ikind == "python-build-backend" || ikind == "cargo-build-script" {
				continue
			}
		}
		kind := mapdoc.NodeInterface
		key := string(o.Kind) + ":" + o.Path + ":" + o.Name + ":" + o.ProjectID
		discriminator := string(o.Kind) + ":" + o.Name + ":" + o.ProjectID
		// A .NET project can declare different OutputType values under different
		// MSBuild conditions. Keep each bounded variant separately addressable
		// so neither its condition nor an unresolved expression is discarded.
		if o.Properties["interface_kind"] == "dotnet-application" {
			key += ":" + o.Properties["target"] + ":" + o.Properties["condition"]
			discriminator += ":" + o.Properties["target"] + ":" + o.Properties["condition"]
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		n := mapdoc.NewNode(kind, []string{o.Path}, discriminator)
		n.Name = o.Name
		n.Properties = map[string]string{"observation_kind": string(o.Kind), "state": o.State, "basis": o.Basis}
		if role := mapPathRole(o.Path); role != "" {
			n.Properties["role"] = role
			n.Properties["role_basis"] = "path_name"
		} else if role, basis := interfaceKindRole(o.Properties["interface_kind"]); role != "" {
			// Path-based role was not available; fall back to the interface kind
			// so that known auxiliary targets (Cargo benchmarks, tests, examples)
			// receive a non-primary role even when their evidence path is the
			// containing Cargo.toml rather than the target source file.
			n.Properties["role"] = role
			n.Properties["role_basis"] = basis
		}
		for k, v := range o.Properties {
			n.Properties[k] = v
		}
		n.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		if o.State == "partial" || o.State == "unresolved" || (o.State == "conditional" && o.Properties["interface_kind"] == "dotnet-application") {
			n.Coverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: []string{"observation_" + o.State}}
		}
		n.Evidence = []mapdoc.Evidence{intentEvidence(o)}
		if o.ProjectID != "" {
			if owner := componentsByManifest[o.ProjectID]; owner != "" {
				n.Properties["owning_component"] = owner
				edgeKind := mapdoc.EdgeDeclares
				edgeCoverage := n.Coverage
				if o.ProjectAttribution == "directory_containment" {
					reasons := append(slices.Clone(edgeCoverage.Reasons), "attributed_by_directory_containment")
					slices.Sort(reasons)
					edgeCoverage = mapdoc.Coverage{Status: mapdoc.CoveragePartial, Reasons: reasons}
				}
				e := mapdoc.NewEdge(edgeKind, owner, n.ID, string(o.Kind)+":"+o.Path)
				e.Coverage = edgeCoverage
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

// capPathRole is like mapPathRole but applies to a slice of capability evidence
// paths. It returns the role for the primary (first) path only.
func capPathRole(paths ...string) string {
	if len(paths) == 0 {
		return ""
	}
	return mapPathRole(paths[0])
}

// interfaceKindRole returns a (role, basis) hint for interface_kind values
// that represent auxiliary targets rather than primary entry points.
// Used as a fallback when mapPathRole does not derive a role from the evidence
// path (e.g. the evidence path is the containing Cargo.toml, not the source).
//
// Cargo [[bench]] → "tooling" (benchmarks are build/perf tooling).
// Cargo [[test]]  → "test"    (integration-test targets).
// Cargo [[example]] → "example".
// All other kinds return ("", "").
func interfaceKindRole(ikind string) (role, basis string) {
	switch ikind {
	case "cargo-bench":
		return "tooling", "interface_kind"
	case "cargo-test":
		return "test", "interface_kind"
	case "cargo-example":
		return "example", "interface_kind"
	}
	return "", ""
}

func cloneProps(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
