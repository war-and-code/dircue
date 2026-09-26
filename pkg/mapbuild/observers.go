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

func addDeployables(d *mapdoc.Document, r *deployables.Report) {
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
			n.Facts = append(n.Facts, mapdoc.Fact{Kind: "deployable_reference", Name: ref.Kind, Value: ref.Value, State: ref.Qualification, Coverage: referenceCoverage(ref.Qualification), Evidence: []mapdoc.Evidence{deployableEvidence(def.Path, ref.Evidence)}})
			if ref.Kind == "service_dependency" && ref.Qualification == "local" {
				composeDeps = append(composeDeps, composeDep{fromID: n.ID, toName: ref.Value, evidence: deployableEvidence(def.Path, ref.Evidence)})
			}
			if (ref.Kind == "build_context" || ref.Kind == "code_uri") && ref.Qualification == "local" {
				resolved := path.Clean(path.Join(path.Dir(def.Path), ref.Value))
				owners := componentsByRoot[resolved]
				if len(owners) == 0 && ref.Kind == "code_uri" {
					// CodeUri may point to a build artifact (e.g. target/app.jar).
					// Walk up to find the nearest ancestor that is a component root.
					owners = componentAncestorOwners(componentsByRoot, resolved)
				}
				if len(owners) == 1 {
					localComponent = owners[0]
					localEvidence = deployableEvidence(def.Path, ref.Evidence)
					if def.Provider != "skaffold" {
						addRelationship(mapdoc.EdgeBuilds, n.ID, owners[0], ref.Kind+":"+resolved, "declared_context_matches_component_root", localEvidence)
					}
					if def.Provider == "compose" && def.Kind == "service" {
						addRelationship(mapdoc.EdgeRuns, n.ID, owners[0], "compose-service:"+resolved, "service_declares_build_context", localEvidence)
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
		d.Nodes = append(d.Nodes, n)
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
	base := path.Base(path.Clean(ref.Value))
	if ext := path.Ext(base); ext != "" && ext != base && base != "." && base != ".." && !strings.ContainsAny(base, "*?") {
		return path.Join(target, base)
	}
	return target
}

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
	if strings.ContainsAny(command, "|;`<>") || strings.Contains(command, "||") || strings.Contains(command, "$(") || strings.Contains(strings.ReplaceAll(command, "&&", ""), "&") {
		return false
	}
	for _, segment := range strings.Split(command, "&&") {
		segment = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(segment), "\\"))
		fields := strings.Fields(segment)
		// Leading VAR=value assignments only set the command's environment.
		for len(fields) > 0 && dockerEnvAssignment.MatchString(fields[0]) {
			fields = fields[1:]
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

var dockerEnvAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// dockerMavenBuildOnly accepts lifecycle phases, options and unresolved
// variables. An explicit plugin goal (group:plugin:goal or prefix:goal), such
// as dependency:copy, can place a downloaded artifact in target/ and fails
// closed.
func dockerMavenBuildOnly(args []string) bool {
	takesValue := map[string]bool{"-f": true, "--file": true, "-s": true, "--settings": true, "-gs": true, "--global-settings": true, "-pl": true, "--projects": true, "-rf": true, "--resume-from": true, "-P": true, "--activate-profiles": true, "-T": true, "--threads": true, "-t": true, "--toolchains": true, "-l": true, "--log-file": true, "-D": true, "--define": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if takesValue[arg] {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "$") {
			continue
		}
		if strings.Contains(arg, ":") {
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

// componentAncestorOwners walks up the directory tree from resolvedPath and
// returns the IDs of components whose root is an ancestor. Returns non-empty
// only when exactly one unique component root matches at the closest level.
// This is used for CodeUri references that point to build artifacts rather
// than source directories (e.g. target/app.jar).
func componentAncestorOwners(componentsByRoot map[string][]string, resolvedPath string) []string {
	p := resolvedPath
	for {
		parent := path.Dir(p)
		if parent == p {
			break
		}
		p = parent
		if ids := componentsByRoot[p]; len(ids) == 1 {
			return ids
		}
		if p == "." {
			break
		}
	}
	return nil
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
	name        string
	projectID   string
	attribution string
	state       string
	basis       string
	paths       []string
	evidence    []mapdoc.Evidence
	properties  map[string]string
	total       int // total distinct (path, name) observations before capping
}

func (g *capGroup) worstState(state string) {
	// partial < conditional < declared < observed (worst = partial)
	if state == "partial" || state == "unresolved" {
		g.state = "partial"
	} else if g.state == "conditional" && (state == "declared" || state == "observed") {
		// An unconditional declaration supersedes a conditional one.
		g.state = state
	}
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
				state:       o.State,
				basis:       o.Basis,
				properties:  cloneProps(o.Properties),
			}
			capGroups[gk] = g
			capGroupOrder = append(capGroupOrder, gk)
		}
		g.total++
		g.worstState(o.State)
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
		for k, v := range g.properties {
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
