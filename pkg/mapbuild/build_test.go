package mapbuild

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/deployables"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/intentmap"
	"github.com/war-and-code/dircue/pkg/mapdoc"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/scanner"
)

func TestDeclaredPythonScriptsRemainDistinctEvidenceBackedMapInterfaces(t *testing.T) {
	doc := declarations.Parse("pyproject.toml", []byte(`[project]
name = "weather"
[project.scripts]
weather = "weather.cli:main"
[project.gui-scripts]
weather-gui = "weather.ui:launch"
`))
	if doc == nil || len(doc.Project.Interfaces) != 2 {
		t.Fatalf("Python interfaces: %+v", doc)
	}
	detector := intentmap.New(intentmap.Options{})
	detector.AddDeclarations([]declarations.Project{*doc.Project})
	report, err := detector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mapDoc := mapdoc.New()
	addIntent(&mapDoc, report)
	kinds := map[string]string{}
	for _, n := range mapDoc.Nodes {
		if n.Kind == mapdoc.NodeInterface {
			kinds[n.Properties["interface_kind"]] = n.Properties["target"]
		}
	}
	if kinds["python-console-script"] != "weather.cli:main" || kinds["python-gui-script"] != "weather.ui:launch" || len(kinds) != 2 {
		t.Fatalf("Python entrypoint interfaces: %+v", kinds)
	}
}

func TestDotnetLaunchInterfacePreservesConditionalAndUnresolvedOutputTypes(t *testing.T) {
	parsed := declarations.Parse("src/App/App.csproj", []byte(`<Project>
  <PropertyGroup Condition="'$(Configuration)' == 'Release'"><OutputType>WinExe</OutputType></PropertyGroup>
  <PropertyGroup><OutputType>$(ChosenOutputType)</OutputType></PropertyGroup>
</Project>`))
	detector := intentmap.New(intentmap.Options{})
	detector.AddDeclarations([]declarations.Project{*parsed.Project})
	report, err := detector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	doc := mapdoc.New()
	addIntent(&doc, report)
	seen := map[string]mapdoc.Node{}
	for _, n := range doc.Nodes {
		if n.Properties["interface_kind"] == "dotnet-application" {
			seen[n.Properties["target"]] = n
		}
	}
	if len(seen) != 2 {
		t.Fatalf("launch interfaces: %+v", seen)
	}
	conditional := seen["WinExe"]
	if conditional.Name != "App" || conditional.Properties["condition"] == "" || conditional.Evidence[0].Span == nil || conditional.Evidence[0].Span.StartLine != 2 || conditional.Coverage.Status != mapdoc.CoveragePartial {
		t.Fatalf("conditional output type: %+v", conditional)
	}
	unresolved := seen["$(ChosenOutputType)"]
	if unresolved.Properties["state"] != "unresolved" || unresolved.Evidence[0].Span == nil || unresolved.Evidence[0].Span.StartLine != 3 {
		t.Fatalf("unresolved output type: %+v", unresolved)
	}
}

func TestDeployableLaunchFacetsRetainSafeValuesAndEvidence(t *testing.T) {
	doc := mapdoc.New()
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "Dockerfile", Coverage: "complete", DockerFinalStage: "runtime", Evidence: []deployables.Evidence{{Field: "FROM", Value: "python:3", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{
			{Kind: "docker_entrypoint", Value: "python3 -m http.server", Qualification: "declared", Stage: "runtime", Evidence: deployables.Evidence{Field: "ENTRYPOINT", Line: 2, Basis: "dockerfile-instruction-exec"}},
			{Kind: "docker_entrypoint_arguments", Qualification: "withheld_arguments", Stage: "runtime", Evidence: deployables.Evidence{Field: "ENTRYPOINT arguments withheld", Line: 2, Basis: "dockerfile-instruction-exec"}},
		}},
		{Provider: "kubernetes", Kind: "workload", Name: "nightly", Path: "cron.yaml", K8sKind: "CronJob", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "CronJob", Line: 2, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "cron_schedule", Value: "0 3 * * *", Qualification: "declared", Evidence: deployables.Evidence{Field: "schedule", Value: "0 3 * * *", Line: 6, Basis: "kubernetes-cronjob-field"}}}},
	}})
	found := map[string]mapdoc.Fact{}
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, fact := range n.Facts {
			found[n.Name+":"+fact.Name] = fact
		}
	}
	entrypoint := found["api:docker_entrypoint"]
	withheld := found["api:docker_entrypoint_arguments"]
	cron := found["nightly:cron_schedule"]
	if entrypoint.Value != "python3 -m http.server" || entrypoint.Evidence[0].Span == nil || entrypoint.Evidence[0].Span.StartLine != 2 || entrypoint.Properties["instruction_form"] != "exec" || entrypoint.Properties["stage"] != "runtime" || entrypoint.Properties["stage_is_final"] != "true" {
		t.Fatalf("Docker launch facet: %+v", entrypoint)
	}
	if withheld.State != "withheld_arguments" || withheld.Evidence[0].Span == nil {
		t.Fatalf("Docker args withholding evidence: %+v", withheld)
	}
	if cron.Value != "0 3 * * *" || cron.Evidence[0].Span == nil || cron.Evidence[0].Span.StartLine != 6 {
		t.Fatalf("CronJob schedule facet: %+v", cron)
	}
}

func TestFilenameHintDoesNotClaimValidatedBinary(t *testing.T) {
	n := fileNode("opaque.lib", "binary", "static_library", "extension", 20)
	if n.Coverage.Status != mapdoc.CoveragePartial || n.Evidence[0].Basis != mapdoc.BasisFilenameHint {
		t.Fatalf("unverified suffix claimed a complete binary: %+v", n)
	}
}

func TestMakefileDockerfilePathIsInvocationRootRelative(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/package.json"}, "npm")
	component.Name = "api"
	component.Properties = map[string]string{"root": "services/api", "ecosystem": "npm"}
	doc.Nodes = append(doc.Nodes, component)
	definition := deployables.Definition{
		Kind: "container_build", Provider: "dockerfile", Name: "api", Path: "services/api/Dockerfile", Coverage: "complete",
		Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}},
	}
	report := &deployables.Report{
		Status: "complete", Definitions: []deployables.Definition{definition},
		BuildContexts: []deployables.BuildContext{{SourcePath: "Makefile", Context: "services/api", Dockerfile: "services/api/Dockerfile",
			ContextEvidence: deployables.Evidence{Field: "context", Value: "services/api", Line: 7, Basis: "makefile-docker-build"},
			FileEvidence:    deployables.Evidence{Field: "-f", Value: "services/api/Dockerfile", Line: 7, Basis: "makefile-docker-build"}}},
	}
	addDeployables(&doc, report)
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"services/api/Dockerfile"}, "dockerfile:container_build:api").ID
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeBuilds && edge.From == dockerID && edge.To == component.ID {
			return
		}
	}
	t.Fatal("root-relative Docker -f path did not link the selected build context component")
}

func TestDockerCargoZigbuildSelectsOnlyUniqueDeclaredBinaryCrate(t *testing.T) {
	makeDoc := func(duplicate bool) mapdoc.Document {
		doc := mapdoc.New()
		root := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "Cargo.toml"}, "cargo-root")
		root.Name = "(root)"
		root.Properties = map[string]string{"root": ".", "ecosystem": "cargo"}
		python := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "pyproject.toml"}, "python-root")
		python.Name = "ruff"
		python.Properties = map[string]string{"root": ".", "ecosystem": "python-uv"}
		crate := mapdoc.NewNode(mapdoc.NodeComponent, []string{"crates/ruff", "crates/ruff/Cargo.toml"}, "cargo-ruff")
		crate.Name = "ruff"
		crate.Properties = map[string]string{"root": "crates/ruff", "ecosystem": "cargo"}
		doc.Nodes = append(doc.Nodes, root, python, crate)
		if duplicate {
			other := mapdoc.NewNode(mapdoc.NodeComponent, []string{"crates/other", "crates/other/Cargo.toml"}, "cargo-ruff-other")
			other.Name = "ruff"
			other.Properties = map[string]string{"root": "crates/other", "ecosystem": "cargo"}
			doc.Nodes = append(doc.Nodes, other)
		}
		return doc
	}
	definition := deployables.Definition{
		Kind: "container_build", Provider: "dockerfile", Name: "(root)", Path: "Dockerfile", Coverage: "complete", DockerFinalStage: "build",
		Evidence:         []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}},
		References:       []deployables.Reference{{Kind: "copy_source", Value: "crates", Qualification: "local", Stage: "build", Evidence: deployables.Evidence{Field: "COPY source", Value: "crates", Line: 27, Basis: "dockerfile-instruction"}}},
		DockerPathWrites: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cargo zigbuild --bin ruff --target $(TARGET) --release", Stage: "build", Evidence: deployables.Evidence{Field: "RUN", Line: 30, Basis: "dockerfile-instruction"}}},
	}
	intent := &intentmap.Report{Observations: []intentmap.Observation{{Kind: intentmap.KindInterface, Name: "ruff", ProjectID: "crates/ruff/Cargo.toml", Properties: map[string]string{"interface_kind": "cargo-default-run"}}}}
	for _, tc := range []struct {
		name      string
		duplicate bool
		want      string
	}{
		{name: "unique declared bin and workspace root", want: "cargo-root"},
		{name: "ambiguous package name", duplicate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := makeDoc(tc.duplicate)
			observations := slices.Clone(intent.Observations)
			if tc.duplicate {
				observations = append(observations, intentmap.Observation{Kind: intentmap.KindInterface, Name: "ruff", ProjectID: "crates/other/Cargo.toml", Properties: map[string]string{"interface_kind": "cargo-default-run"}})
			}
			addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition}}, &intentmap.Report{Observations: observations})
			dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
			rootID := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "Cargo.toml"}, "cargo-root").ID
			baselineID := mapdoc.NewEdge(mapdoc.EdgeBuilds, dockerID, rootID, "dockerfile-multi:"+rootID).ID
			for _, edge := range doc.Edges {
				if edge.Type != mapdoc.EdgeBuilds || edge.From != dockerID {
					continue
				}
				if tc.want == "" || edge.To != mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "Cargo.toml"}, "cargo-root").ID {
					t.Fatalf("zigbuild attribution was not uniquely source-backed: %+v", edge)
				}
				if edge.Coverage.Status != mapdoc.CoveragePartial {
					t.Fatalf("static build relationship must remain partial: %+v", edge.Coverage)
				}
				if edge.ID != baselineID {
					t.Fatalf("strengthened edge changed its stable ID: got %s want %s", edge.ID, baselineID)
				}
				return
			}
			if tc.want != "" {
				t.Fatal("unique declared binary crate did not receive build edge")
			}
		})
	}
}

func TestDockerCargoZigbuildRejectsUnmatchedStageAndPseudocommands(t *testing.T) {
	doc := mapdoc.New()
	crate := mapdoc.NewNode(mapdoc.NodeComponent, []string{"crates/ruff", "crates/ruff/Cargo.toml"}, "cargo-ruff")
	crate.Name = "ruff"
	crate.Properties = map[string]string{"root": "crates/ruff", "ecosystem": "cargo"}
	doc.Nodes = append(doc.Nodes, crate)
	intent := &intentmap.Report{Observations: []intentmap.Observation{{Kind: intentmap.KindInterface, Name: "ruff", ProjectID: "crates/ruff/Cargo.toml", Properties: map[string]string{"interface_kind": "cargo-default-run"}}}}
	base := deployables.Definition{
		Kind: "container_build", Provider: "dockerfile", Name: "(root)", Path: "Dockerfile", Coverage: "complete", DockerFinalStage: "build-stage",
		References:       []deployables.Reference{{Kind: "copy_source", Value: "crates", Qualification: "local", Stage: "copy-stage", Evidence: deployables.Evidence{Line: 1}}},
		DockerPathWrites: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cargo zigbuild --bin ruff", Stage: "build-stage", Evidence: deployables.Evidence{Line: 2}}},
	}
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("crate copy from a different stage was combined with the zigbuild command")
	}
	base.References[0].Stage = "build-stage"
	base.References[0].Value = "crates/other"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("copying a different crate subtree was treated as copying the selected bin crate")
	}
	base.References[0].Value = "crates"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); !ok {
		t.Fatal("root Dockerfile with known context did not retain the Cargo component link")
	}
	base.Path = "services/api/Dockerfile"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("nested Dockerfile was treated as building the repository-root crates tree")
	}
	base.Path = "Dockerfile"
	base.DockerContextUnknown = true
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("unknown Docker context was treated as the repository-root crates tree")
	}
	base.DockerContextUnknown = false
	base.DockerPathWrites[0].Value = "RUN pseudocargo zigbuild --bin ruff"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok || dockerHasCargoZigbuild(base) {
		t.Fatal("pseudocargo command was accepted as a literal Cargo zigbuild")
	}
	base.DockerPathWrites[0].Value = "RUN cargo zigbuild --bin $(BIN)"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("dynamic --bin name was treated as a static crate selection")
	}
	for _, command := range []string{
		"RUN cargo zigbuild --bin ruff --manifest-path ../external/Cargo.toml",
		"RUN cargo zigbuild --bin ruff -p another-package",
		"RUN cargo zigbuild --bin ruff -panother-package",
	} {
		base.DockerPathWrites[0].Value = command
		if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
			t.Fatalf("overridden workspace selection was attributed: %s", command)
		}
	}
	for _, command := range []string{
		"RUN cargo build --manifest-path ../external/Cargo.toml",
		"RUN cargo build --package another-package",
		"RUN cargo build -panother-package",
	} {
		base.DockerPathWrites[0].Value = command
		if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
			t.Fatalf("overridden cargo build was attributed to the local workspace: %s", command)
		}
	}
	base.DockerPathWrites[0].Value = "RUN cargo build --release"
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); !ok {
		t.Fatal("literal cargo build --release stopped being accepted")
	}
	base.References[0].Evidence.Line = 3
	if _, _, ok := dockerCargoComponent(base, []string{crate.ID}, &doc, intent); ok {
		t.Fatal("COPY occurring after cargo build was treated as its source")
	}
}

func TestTerraformLiteralLocalModuleSourcesLinkModuleDeployables(t *testing.T) {
	root := deployables.Definition{Kind: "infrastructure", Provider: "terraform", Name: "infra", Path: "infra/main.tf", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "module", Value: "child", Line: 1, Basis: "terraform-literal-block"}}, References: []deployables.Reference{{Kind: "module_source", Value: "../modules/network", Qualification: "local", Evidence: deployables.Evidence{Field: "source", Value: "../modules/network", Line: 2, Basis: "terraform-module-source"}}}}
	child := deployables.Definition{Kind: "infrastructure", Provider: "terraform", Name: "network", Path: "modules/network/main.tf", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "resource", Value: "aws_vpc.main", Line: 1, Basis: "terraform-literal-block"}}}
	doc := mapdoc.New()
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{root, child}})
	from := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"infra"}, "terraform:infrastructure:infra").ID
	to := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"modules/network"}, "terraform:infrastructure:network").ID
	for _, e := range doc.Edges {
		if e.From == from && e.To == to && e.Type == mapdoc.EdgeDependsOnLocal && e.Coverage.Status == mapdoc.CoveragePartial {
			return
		}
	}
	t.Fatalf("missing partial local-module dependency edge: %+v", doc.Edges)
}

func TestComposeContextLinksReferencedDockerfileToComponent(t *testing.T) {
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/package.json"}, "npm")
	component.Name = "api"
	component.Properties = map[string]string{"root": "services/api", "ecosystem": "npm"}
	doc := mapdoc.New()
	doc.Nodes = append(doc.Nodes, component)
	compose := deployables.Definition{Kind: "service", Provider: "compose", Name: "api", Path: "deploy/compose.yaml", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "service", Value: "api", Line: 1, Basis: "compose-services-map"}}, References: []deployables.Reference{{Kind: "build_context", Value: "../services/api", Qualification: "local", Evidence: deployables.Evidence{Field: "build_context", Value: "../services/api", Line: 4, Basis: "compose-field"}}, {Kind: "dockerfile", Value: "Dockerfile.api", Qualification: "local", Evidence: deployables.Evidence{Field: "dockerfile", Value: "Dockerfile.api", Line: 5, Basis: "compose-field"}}}}
	docker := deployables.Definition{Kind: "container_build", Provider: "dockerfile", Name: "api", Path: "services/api/Dockerfile.api", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{compose, docker}})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"services/api/Dockerfile.api"}, "dockerfile:container_build:api").ID
	for _, e := range doc.Edges {
		if e.From == dockerID && e.To == component.ID && e.Type == mapdoc.EdgeBuilds && e.Coverage.Status == mapdoc.CoveragePartial {
			return
		}
	}
	t.Fatalf("missing partial context-derived Dockerfile edge: %+v", doc.Edges)
}

func TestMakefileStaticBuildContextLinksDockerfile(t *testing.T) {
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "Cargo.toml"}, "cargo")
	component.Name = "app"
	component.Properties = map[string]string{"root": ".", "ecosystem": "cargo"}
	doc := mapdoc.New()
	doc.Nodes = append(doc.Nodes, component)
	docker := deployables.Definition{Kind: "container_build", Provider: "dockerfile", Name: "app", Path: "cmd/app/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}
	report := &deployables.Report{Status: "complete", Definitions: []deployables.Definition{docker}, BuildContexts: []deployables.BuildContext{{SourcePath: "Makefile", Context: ".", Dockerfile: "cmd/app/Dockerfile", ContextEvidence: deployables.Evidence{Field: "context", Value: ".", Line: 7, Basis: "makefile-docker-build"}, FileEvidence: deployables.Evidence{Field: "-f", Value: "cmd/app/Dockerfile", Line: 7, Basis: "makefile-docker-build"}}}}
	addDeployables(&doc, report)
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"cmd/app/Dockerfile"}, "dockerfile:container_build:app").ID
	for _, e := range doc.Edges {
		if e.From == dockerID && e.To == component.ID && e.Type == mapdoc.EdgeBuilds && e.Coverage.Status == mapdoc.CoveragePartial {
			return
		}
	}
	t.Fatalf("missing partial Makefile-context Dockerfile edge: %+v", doc.Edges)
}

func TestGitHubContextUsesCheckoutSubdirectoryCoordinates(t *testing.T) {
	doc := mapdoc.New()
	for _, root := range []string{"services/api", "repo/services/api"} {
		c := mapdoc.NewNode(mapdoc.NodeComponent, []string{root, root + "/go.mod"}, "go")
		c.Name = root
		c.Properties = map[string]string{"root": root, "ecosystem": "go"}
		doc.Nodes = append(doc.Nodes, c)
	}
	wf := deployables.Definition{Provider: "github-actions", Kind: "workflow", Name: "build", Path: ".github/workflows/build.yml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "jobs", Value: "1", Line: 1, Basis: "github-workflow-map"}}, References: []deployables.Reference{{Kind: "build_context", Value: "repo/services/api", Qualification: "local", Checkout: "repo", Evidence: deployables.Evidence{Field: "with.context", Value: "repo/services/api", Line: 8, Basis: "github-build-push-action"}}}}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{wf}})
	wfID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{wf.Path}, "github-actions:workflow:build").ID
	for _, edge := range doc.Edges {
		if edge.From != wfID || edge.Type != mapdoc.EdgeBuilds {
			continue
		}
		for _, node := range doc.Nodes {
			if node.ID != edge.To || node.Kind != mapdoc.NodeComponent {
				continue
			}
			if node.Properties["root"] != "services/api" {
				t.Fatalf("checkout subdirectory was not stripped: attributed to %q", node.Properties["root"])
			}
			return
		}
	}
	t.Fatalf("missing checkout-aware static context edge: %+v", doc.Edges)
}

func TestGitHubBuildContextAfterForeignRootCheckoutIsNotAttributed(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{".", "go.mod"}, "go")
	component.Name = "root"
	component.Properties = map[string]string{"root": ".", "ecosystem": "go"}
	doc.Nodes = append(doc.Nodes, component)
	wf := deployables.Definition{Provider: "github-actions", Kind: "workflow", Name: "build", Path: ".github/workflows/build.yml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "jobs", Value: "1", Line: 1, Basis: "github-workflow-map"}}, References: []deployables.Reference{{Kind: "build_context", Value: ".", Qualification: "external", Checkout: ".", CheckoutNamed: true, Evidence: deployables.Evidence{Field: "with.context", Value: ".", Line: 8, Basis: "github-build-push-action"}}}}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{wf}})
	wfID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{wf.Path}, "github-actions:workflow:build").ID
	for _, edge := range doc.Edges {
		if edge.From == wfID && edge.Type == mapdoc.EdgeBuilds {
			t.Fatalf("foreign repository checkout context was attributed to local component: %+v", edge)
		}
	}
}

func TestDeclaredLocalArtifactsLinkSelectedContentInventory(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{
		"app/pom.xml":    []byte(`<project><artifactId>app</artifactId><build><plugins><plugin><artifactId>generator</artifactId><dependencies><dependency><groupId>local</groupId><artifactId>helper</artifactId><scope>system</scope><systemPath>${basedir}/../lib/helper.jar</systemPath></dependency></dependencies></plugin></plugins></build></project>`),
		"app/App.csproj": []byte(`<Project><ItemGroup><Reference Include="Helper"><HintPath>../lib/Helper.dll</HintPath></Reference><Reference Include="Dynamic" HintPath="$(LibraryDir)\Dynamic.dll" /></ItemGroup></Project>`),
		"lib/helper.jar": []byte("not opened by the project parser"),
		"lib/Helper.dll": []byte("also not opened by the project parser"),
	}
	for name, data := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := scanner.Scan(context.Background(), root, scanner.Options{Source: "directory", Discovery: true, Declarations: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Declarations == nil || report.Declarations.Status != "partial" {
		t.Fatalf("declarations: %+v", report.Declarations)
	}
	// Discovery candidate retention is deliberately sparse. Removing these
	// examples proves exact target lookup comes from the selected inventory.
	report.Discovery.Candidates = nil
	report.Formats = nil
	doc, err := Build(report, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"maven->lib/helper.jar": false, "dotnet->lib/Helper.dll": false}
	unresolvedAttributeFound := false
	for _, node := range doc.Nodes {
		if node.Kind != mapdoc.NodeComponent || node.Properties["ecosystem"] != "dotnet" {
			continue
		}
		for _, fact := range node.Facts {
			if fact.Kind == "artifact_reference" && fact.State == "unresolved" {
				unresolvedAttributeFound = true
			}
		}
	}
	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeReferencesArtifact {
			continue
		}
		from, to := "", ""
		for _, node := range doc.Nodes {
			if node.ID == edge.From {
				from = node.Properties["ecosystem"]
			}
			if node.ID == edge.To {
				to = node.Paths[0]
			}
		}
		key := from + "->" + to
		if _, ok := wanted[key]; !ok || wanted[key] || len(edge.Evidence) != 2 {
			t.Fatalf("unexpected or duplicate artifact edge: %+v (%s)", edge, key)
		}
		wanted[key] = true
	}
	for key, found := range wanted {
		if !found {
			t.Errorf("missing artifact edge %s", key)
		}
	}
	if !unresolvedAttributeFound {
		t.Fatal("dynamic HintPath attribute did not survive as an unresolved artifact reference")
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "${basedir}") || strings.Contains(string(encoded), "../lib/") || strings.Contains(string(encoded), root) {
		t.Fatalf("artifact map leaked raw declaration or host path: %s", encoded)
	}
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeReferencesArtifact && edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Fatalf("selected path suffix was overstated as validated artifact evidence: %+v", edge)
		}
	}
}

func TestDeclaredBuildAndRunLinksHaveEvidence(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "services/api"}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "declaration", Line: 1, Basis: "static-field"}
	definitions := []deployables.Definition{
		{Provider: "compose", Kind: "service", Name: "api", Path: "compose.yml", Coverage: "complete", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{
			{Kind: "build_context", Value: "services/api", Qualification: "local", Evidence: evidence},
			{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence},
		}},
		// Name "api" reflects dockerfileDisplayName("services/api/Dockerfile") = path.Base("services/api") = "api".
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "services/api/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{evidence}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "image", Value: "example/api:v1", Qualification: "external", Evidence: evidence}}},
		{Provider: "cloudformation", Kind: "infrastructure", Name: "function", Path: "template.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "code_uri", Value: "services/api", Qualification: "local", Evidence: evidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	builds, runs := 0, 0
	for _, edge := range doc.Edges {
		if edge.To != component.ID {
			t.Fatalf("edge points to wrong component: %+v", edge)
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) == 0 {
			t.Fatalf("declared link lacks qualification or evidence: %+v", edge)
		}
		switch edge.Type {
		case mapdoc.EdgeBuilds:
			builds++
		case mapdoc.EdgeRuns:
			runs++
		}
	}
	if builds != 3 || runs != 2 {
		t.Fatalf("want Dockerfile, Compose and SAM builds plus Compose and Kubernetes runs; got builds=%d runs=%d", builds, runs)
	}
	for _, question := range doc.Coverage {
		if question.Question == "deployables" && question.Status != mapdoc.CoveragePartial {
			t.Fatalf("bounded parser claimed complete deployable catalog: %+v", question)
		}
	}
}

func TestCodeUriAmbiguousNearestRootDoesNotFallBack(t *testing.T) {
	doc := mapdoc.New()
	for _, source := range [][]string{{"go.mod"}, {"services/api/go.mod"}, {"services/api/package.json"}} {
		component := mapdoc.NewNode(mapdoc.NodeComponent, source, "test")
		root := "."
		if strings.HasPrefix(source[0], "services/") {
			root = "services/api"
		}
		component.Properties = map[string]string{"root": root}
		doc.Nodes = append(doc.Nodes, component)
	}
	evidence := deployables.Evidence{Field: "CodeUri", Value: "services/api/target/app.jar", Line: 4, Basis: "static-field"}
	definition := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "function", Path: "template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{evidence},
		References: []deployables.Reference{{Kind: "code_uri", Value: evidence.Value, Qualification: "local", Evidence: evidence}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition}})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeBuilds {
			t.Fatalf("ambiguous nearest CodeUri root incorrectly fell back to repository component: %+v", edge)
		}
	}
}

func TestCodeUriAncestorLookupIncludesRepositoryRoot(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	component.Properties = map[string]string{"root": "."}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "CodeUri", Value: "build/app.jar", Line: 4, Basis: "static-field"}
	definition := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "function", Path: "template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{evidence},
		References: []deployables.Reference{{Kind: "code_uri", Value: evidence.Value, Qualification: "local", Evidence: evidence}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition}})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeBuilds && edge.To == component.ID {
			return
		}
	}
	t.Fatalf("CodeUri below repository root did not associate to its unique component: %+v", doc.Edges)
}

func TestKubernetesImageDoesNotInferRunFromDockerfileAndBasename(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/emailservice", "src/emailservice/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/emailservice"}
	doc.Nodes = append(doc.Nodes, component)
	workloadEvidence := deployables.Evidence{Field: "image", Value: "registry.example/demo/emailservice:v1", Line: 22, Basis: "kubernetes-container-field"}
	dockerEvidence := deployables.Evidence{Field: "FROM", Value: "python:3.13", Line: 1, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "emailservice", Path: "k8s/emailservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 2, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: workloadEvidence.Value, Qualification: "external", Evidence: workloadEvidence}}},
		// Name "emailservice" reflects dockerfileDisplayName("src/emailservice/Dockerfile").
		{Provider: "dockerfile", Kind: "container_build", Name: "emailservice", Path: "src/emailservice/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: dockerEvidence.Value, Qualification: "external", Evidence: dockerEvidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("Dockerfile co-location and matching basename are insufficient to infer a run link: %+v", edge)
		}
	}
}

func TestKubernetesImageDoesNotChooseAmongDuplicateComponentRoots(t *testing.T) {
	doc := mapdoc.New()
	for _, root := range []string{"one/api", "two/api"} {
		component := mapdoc.NewNode(mapdoc.NodeComponent, []string{root}, "go")
		component.Properties = map[string]string{"root": root}
		doc.Nodes = append(doc.Nodes, component)
	}
	evidence := deployables.Evidence{Field: "image", Value: "registry.example/api:v1", Line: 1, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: evidence.Value, Qualification: "external", Evidence: evidence}}},
		// Names reflect dockerfileDisplayName: path.Base(path.Dir(…)) = "api" for both.
		// The paths differ so the node IDs are distinct.
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "one/api/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: "alpine", Qualification: "external", Evidence: deployables.Evidence{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}},
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "two/api/Dockerfile", Coverage: "complete", References: []deployables.Reference{{Kind: "base_image_or_stage", Value: "alpine", Qualification: "external", Evidence: deployables.Evidence{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("ambiguous component match emitted a run link: %+v", edge)
		}
	}
}

func TestKubernetesImageDoesNotClaimAncestorComponentFromNestedDockerfile(t *testing.T) {
	doc := mapdoc.New()
	outer := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice"}, "dotnet")
	outer.Properties = map[string]string{"root": "src/cartservice"}
	doc.Nodes = append(doc.Nodes, outer)
	evidence := deployables.Evidence{Field: "image", Value: "cartservice:v1", Line: 1, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "kubernetes", Kind: "workload", Name: "cartservice", Path: "k8s/cartservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{{Kind: "image", Value: evidence.Value, Qualification: "external", Evidence: evidence}}},
		// Name "src" reflects dockerfileDisplayName("src/cartservice/src/Dockerfile") = path.Base("src/cartservice/src") = "src".
		{Provider: "dockerfile", Kind: "container_build", Name: "src", Path: "src/cartservice/src/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("nested Dockerfile alone linked an ancestor component: %+v", edge)
		}
	}
}

func TestSkaffoldArtifactSelectsNestedComponentByFullNormalizedImage(t *testing.T) {
	doc := mapdoc.New()
	outer := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice"}, "dotnet")
	outer.Properties = map[string]string{"root": "src/cartservice"}
	nested := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/cartservice/src"}, "dotnet")
	nested.Properties = map[string]string{"root": "src/cartservice/src"}
	doc.Nodes = append(doc.Nodes, outer, nested)
	artifactImage := deployables.Evidence{Field: "image", Value: "registry.example/team/cartservice:build", Line: 10, Basis: "skaffold-artifact"}
	artifactContext := deployables.Evidence{Field: "context", Value: "src/cartservice/src", Line: 11, Basis: "skaffold-artifact"}
	kubeImage := deployables.Evidence{Field: "image", Value: "registry.example/team/cartservice:v1@sha256:1234", Line: 22, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "skaffold", Kind: "container_build", Name: "cartservice", Path: "skaffold.yaml", Coverage: "complete", Evidence: []deployables.Evidence{artifactImage, artifactContext}, References: []deployables.Reference{{Kind: "image", Value: artifactImage.Value, Qualification: "local", Evidence: artifactImage}, {Kind: "build_context", Value: artifactContext.Value, Qualification: "local", Evidence: artifactContext}}},
		{Provider: "kubernetes", Kind: "workload", Name: "cartservice", Path: "k8s/cartservice.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 2, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: kubeImage.Value, Qualification: "external", Evidence: kubeImage}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeRuns {
			continue
		}
		if edge.To != nested.ID || edge.To == outer.ID || len(edge.Evidence) != 3 || edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Fatalf("Skaffold mapping did not select and qualify the nested component: %+v", edge)
		}
		if edge.Evidence[0].Path != "k8s/cartservice.yaml" || edge.Evidence[1].Path != "skaffold.yaml" || edge.Evidence[2].Path != "skaffold.yaml" {
			t.Fatalf("run link lacks Kubernetes/Skaffold evidence: %+v", edge.Evidence)
		}
		return
	}
	t.Fatal("expected a Kubernetes run edge through the exact Skaffold image repository and context")
}

func TestKubernetesImageDoesNotMatchSkaffoldByBasenameAcrossRegistries(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/api", "src/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/api"}
	doc.Nodes = append(doc.Nodes, component)
	artifact := deployables.Evidence{Field: "image", Value: "registry.example/team-a/api:v1", Line: 4, Basis: "skaffold-artifact"}
	context := deployables.Evidence{Field: "context", Value: "src/api", Line: 5, Basis: "skaffold-artifact"}
	workload := deployables.Evidence{Field: "image", Value: "other.example/team-b/api:v1", Line: 10, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "skaffold", Kind: "container_build", Name: "api", Path: "skaffold.yaml", Coverage: "complete", References: []deployables.Reference{
			{Kind: "image", Value: artifact.Value, Qualification: "declared", Evidence: artifact},
			{Kind: "build_context", Value: context.Value, Qualification: "local", Evidence: context},
		}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: workload.Value, Qualification: "external", Evidence: workload}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			t.Fatalf("same basename from unrelated registries produced a run link: %+v", edge)
		}
	}
}

func TestComposeAndKubernetesImagesMatchNormalizedFullRepository(t *testing.T) {
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"src/api", "src/api/go.mod"}, "go")
	component.Properties = map[string]string{"root": "src/api"}
	doc.Nodes = append(doc.Nodes, component)
	evidence := deployables.Evidence{Field: "image", Value: "registry.example/team/api:build", Line: 4, Basis: "compose-field"}
	context := deployables.Evidence{Field: "build", Value: "src/api", Line: 5, Basis: "compose-build-field"}
	kubeEvidence := deployables.Evidence{Field: "image", Value: "registry.example/team/api:release@sha256:1234", Line: 10, Basis: "kubernetes-container-field"}
	definitions := []deployables.Definition{
		{Provider: "compose", Kind: "service", Name: "api", Path: "compose.yml", Coverage: "complete", Evidence: []deployables.Evidence{evidence}, References: []deployables.Reference{
			{Kind: "build_context", Value: context.Value, Qualification: "local", Evidence: context},
			{Kind: "image", Value: evidence.Value, Qualification: "declared", Evidence: evidence},
		}},
		{Provider: "kubernetes", Kind: "workload", Name: "api", Path: "k8s/api.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "kind", Value: "Deployment", Line: 1, Basis: "kubernetes-field"}}, References: []deployables.Reference{{Kind: "image", Value: kubeEvidence.Value, Qualification: "external", Evidence: kubeEvidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	runs := 0
	for _, edge := range doc.Edges {
		if edge.Type == mapdoc.EdgeRuns {
			runs++
			if edge.To != component.ID || edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Evidence) == 0 {
				t.Fatalf("normalized exact image link lost its target or evidence: %+v", edge)
			}
		}
	}
	if runs != 2 {
		t.Fatalf("want Compose declaration plus matching Kubernetes declaration, got %d run links", runs)
	}
}

func TestDuplicateObserverIdentityDoesNotDiscardWholeMap(t *testing.T) {
	evidence := deployables.Evidence{Field: "kind", Value: "Deployment", Line: 1, Basis: "static-field"}
	definition := deployables.Definition{Provider: "kubernetes", Kind: "workload", Name: "my-deploy", Path: "deploy/deployment.yaml", Coverage: "qualified", Evidence: []deployables.Evidence{evidence}}
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{Deployables: &deployables.Report{Status: "complete", Definitions: []deployables.Definition{definition, definition}}})
	if err != nil {
		t.Fatalf("duplicate observer identity discarded the map: %v", err)
	}
	count := 0
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeDeployable {
			count++
			if n.Coverage.Status != mapdoc.CoveragePartial {
				t.Fatalf("duplicate was not qualified: %+v", n)
			}
		}
	}
	if count != 1 {
		t.Fatalf("retained deployable count=%d", count)
	}
	for _, q := range doc.Coverage {
		if q.Question == "deployables" && q.Status == mapdoc.CoveragePartial && len(q.Reasons) == 2 {
			return
		}
	}
	t.Fatal("duplicate omission was not disclosed in deployable coverage")
}

func TestContentPopulationsHaveUserMeaningfulStableIdentity(t *testing.T) {
	makeReport := func(first, second string) *profile.Report {
		return &profile.Report{Discovery: &discovery.Report{
			Categories: []discovery.Group{
				{Name: "source_candidate", Basis: first, Counts: discovery.Counts{Files: 2, Bytes: 20}},
				{Name: "source_candidate", Basis: second, Counts: discovery.Counts{Files: 3, Bytes: 30}},
				{Name: "documentation_candidate", Basis: first, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
			Roles: []discovery.Group{
				{Name: "documentation", Basis: second, Counts: discovery.Counts{Files: 1, Bytes: 10}},
			},
		}}
	}
	first := contentNodes(makeReport("enry_extension", "enry_filename"))
	second := contentNodes(makeReport("enry_path", "gitattributes"))
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected one source and one documentation population: %d, %d", len(first), len(second))
	}
	byRole := func(nodes []mapdoc.Node) map[string]mapdoc.Node {
		out := map[string]mapdoc.Node{}
		for _, node := range nodes {
			out[node.Properties["role"]] = node
		}
		return out
	}
	a, b := byRole(first), byRole(second)
	if a["source"].ID != b["source"].ID || a["documentation"].ID != b["documentation"].ID {
		t.Fatalf("Enry mechanism changed population identities: %+v vs %+v", a, b)
	}
	if a["source"].Properties["files"] != "5" || a["source"].Properties["bytes"] != "50" {
		t.Fatalf("source groups were not combined: %+v", a["source"].Properties)
	}
	if a["documentation"].Properties["files"] != "1" {
		t.Fatalf("documentation category and role were double-counted: %+v", a["documentation"].Properties)
	}
}

func TestSingleMapDoesNotClaimMaterialChangeQuestion(t *testing.T) {
	report := &profile.Report{Discovery: &discovery.Report{Status: "complete", Source: discovery.Source{Mode: "directory"}}}
	doc, err := Build(report, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, question := range doc.Coverage {
		if question.Question == "material_change" {
			t.Fatal("a single directory map cannot answer a comparison question")
		}
	}
}

func TestAuxiliaryPathRolesAreHintsNotDeletedFacts(t *testing.T) {
	for _, test := range []struct{ filename, role string }{
		// No role — caller assigns primary
		{"services/api/App.csproj", ""},
		// Fixture
		{"tests/map_corpus/fixtures/app/package.json", "fixture"},
		{"crates/ruff_linter/resources/test/fixtures/isort/pyproject.toml", "fixture"},
		{"testdata/myapp/pyproject.toml", "fixture"},
		// Fixture — evaluation truth data (new path segments)
		{"crates/ty_completion_eval/truth/scope-ok/pyproject.toml", "fixture"},
		{"tests/cases/mycase/pyproject.toml", "fixture"},
		{"tests/snapshots/snap_foo/pyproject.toml", "fixture"},
		{"tests/corpus/sample/pyproject.toml", "fixture"},
		{"tests/golden/out.json", "fixture"},
		// Test
		{"src/Tests/Compiler.Tests.csproj", "test"},
		{"crates/ruff_mdtest/Cargo.toml", ""}, // "mdtest" suffix is not "_test"; no role assigned
		{"crates/ty_test/Cargo.toml", "test"},
		{"tests/App.UnitTests/App.UnitTests.csproj", "test"},
		{"tests/App.FunctionalTests/App.FunctionalTests.csproj", "test"},
		// Example
		{"examples/demo/Dockerfile", "example"},
		{"samples/web/package.json", "example"},
		{"demo/app/go.mod", "example"},
		// Vendored
		{"vendor/lib/Cargo.toml", "vendored"},
		{"node_modules/react/package.json", "vendored"},
		{"operator/.bingo/go.mod", "vendored"},
		// Docs
		{"docs/website/package.json", "docs"},
		{"RELEASING/pyproject.toml", "docs"},
		{"translations/setup.cfg", "docs"},
		// Tooling (new: benchmarks segment)
		{"scripts/benchmark/pyproject.toml", "tooling"},
		{"scripts/benchmarks/Cargo.toml", "tooling"},
		{"scripts/bench/pyproject.toml", "tooling"},
		// JVM package paths below a source set are not project layout.
		{"src/main/java/org/springframework/samples/petclinic/PetClinicApplication.java", ""},
		{"src/test/java/org/example/app/AppTest.java", "test"},
		{"examples/web/src/main/kotlin/demo/App.kt", "example"},
		// Only .NET project files use the name-suffix test rule.
		{"cmd/latest/latest.go", ""},
	} {
		if got := mapPathRole(test.filename); got != test.role {
			t.Errorf("%s: role %q, want %q", test.filename, got, test.role)
		}
	}
}

func TestPlaceholderNameRole(t *testing.T) {
	for _, test := range []struct{ name, want string }{
		{"test", "test"},
		{"Test", "test"},
		{"TEST", "test"},
		{"tests", "test"},
		{"example", "example"},
		{"examples", "example"},
		{"sample", "example"},
		{"samples", "example"},
		{"demo", "fixture"},
		{"fixture", "fixture"},
		{"fixtures", "fixture"},
		{"dummy", "fixture"},
		{"foo", "fixture"},
		{"my-project", "fixture"},
		{"my_project", "fixture"},
		// real names — not placeholders
		{"ruff", ""},
		{"myapp", ""},
		{"api-gateway", ""},
		{"TestFramework", ""},
	} {
		if got := placeholderNameRole(test.name); got != test.want {
			t.Errorf("placeholderNameRole(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

// ── P0: Attribution honesty (ACC-F01 regression) ─────────────────────────────

func TestContainmentAttributionProducesPartialEdge(t *testing.T) {
	// Regression for P0/ACC-F01: an edge from a component to a capability whose
	// projectID was assigned by directory-containment must be partial with reason
	// "attributed_by_directory_containment", not complete.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/go.mod"}, "go")
	component.Properties = map[string]string{"root": "app"}
	doc.Nodes = append(doc.Nodes, component)

	obs := intentmap.Observation{
		Kind:               intentmap.KindCapability,
		Name:               "cache:redis",
		ProjectID:          "app/go.mod",
		ProjectAttribution: "directory_containment",
		State:              "observed",
		Basis:              "declared_config",
		Path:               "app/.env.production.sample",
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: []intentmap.Observation{obs},
	}
	addIntent(&doc, report)

	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeUsesCapability {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Errorf("containment-attributed edge coverage = %q, want partial", edge.Coverage.Status)
		}
		found := false
		for _, reason := range edge.Coverage.Reasons {
			if reason == "attributed_by_directory_containment" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("edge coverage reasons = %v, want attributed_by_directory_containment", edge.Coverage.Reasons)
		}
		return
	}
	t.Fatal("no uses_capability edge was created")
}

func TestExplicitlyDeclaredCapabilityEdgeIsNotDowngraded(t *testing.T) {
	// A capability declared explicitly in a manifest (ProjectAttribution empty)
	// must produce a complete edge — not partial.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/go.mod"}, "go")
	component.Properties = map[string]string{"root": "app"}
	doc.Nodes = append(doc.Nodes, component)

	obs := intentmap.Observation{
		Kind:      intentmap.KindCapability,
		Name:      "cache:redis",
		ProjectID: "app/go.mod",
		// ProjectAttribution is intentionally empty: declared by manifest
		State: "declared",
		Basis: "declared_dependency",
		Path:  "app/go.mod",
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: []intentmap.Observation{obs},
	}
	addIntent(&doc, report)

	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeUsesCapability {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoverageComplete {
			t.Errorf("explicitly declared capability edge coverage = %q, want complete; reasons: %v", edge.Coverage.Status, edge.Coverage.Reasons)
		}
		for _, reason := range edge.Coverage.Reasons {
			if strings.Contains(reason, "containment") {
				t.Errorf("explicit edge carries containment reason unexpectedly: %v", edge.Coverage.Reasons)
			}
		}
		return
	}
	t.Fatal("no uses_capability edge was created")
}

// ── F-12: Capability granularity ─────────────────────────────────────────────

func TestCapabilityGranularityOneNodePerComponent(t *testing.T) {
	// Regression for F-12: multiple capability observations for the same
	// (name, projectID) must produce exactly one capability node with aggregated
	// evidence, not one node per file path.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"svc", "svc/go.mod"}, "go")
	component.Properties = map[string]string{"root": "svc"}
	doc.Nodes = append(doc.Nodes, component)

	// 5 observations for the same capability from 5 different source files.
	var obs []intentmap.Observation
	for i := 0; i < 5; i++ {
		obs = append(obs, intentmap.Observation{
			Kind:      intentmap.KindCapability,
			Name:      "datastore:postgresql",
			ProjectID: "svc/go.mod",
			State:     "observed",
			Basis:     "imported",
			Path:      "svc/internal/" + string(rune('a'+i)) + ".go",
		})
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: obs,
	}
	addIntent(&doc, report)

	capNodes := 0
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeCapability && n.Name == "datastore:postgresql" {
			capNodes++
			count := n.Properties["evidence_path_count"]
			if count != "5" {
				t.Errorf("evidence_path_count = %q, want 5", count)
			}
			if len(n.Evidence) != 5 {
				t.Errorf("evidence entries = %d, want 5", len(n.Evidence))
			}
		}
	}
	if capNodes != 1 {
		t.Errorf("capability nodes = %d, want 1 (one per component, not one per file)", capNodes)
	}
}

func TestCapabilityGranularityEvidencePathsAreCapped(t *testing.T) {
	// Ensure that when total observations exceed maxCapabilityEvidencePaths,
	// the node still stores only maxCapabilityEvidencePaths evidence entries
	// but evidence_path_count reflects the real total.
	doc := mapdoc.New()
	component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"svc", "svc/go.mod"}, "go")
	component.Properties = map[string]string{"root": "svc"}
	doc.Nodes = append(doc.Nodes, component)

	total := maxCapabilityEvidencePaths + 10
	var obs []intentmap.Observation
	for i := 0; i < total; i++ {
		obs = append(obs, intentmap.Observation{
			Kind:      intentmap.KindCapability,
			Name:      "cache:redis",
			ProjectID: "svc/go.mod",
			State:     "observed",
			Basis:     "imported",
			Path:      "svc/pkg/f" + string(rune('a'+i%26)) + ".go",
		})
	}
	report := &intentmap.Report{
		Coverage:     intentmap.Coverage{Status: "complete"},
		Observations: obs,
	}
	addIntent(&doc, report)

	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeCapability && n.Name == "cache:redis" {
			if len(n.Evidence) > maxCapabilityEvidencePaths {
				t.Errorf("evidence entries = %d, must not exceed maxCapabilityEvidencePaths=%d", len(n.Evidence), maxCapabilityEvidencePaths)
			}
			return
		}
	}
	t.Fatal("no cache:redis capability node created")
}

func TestDockerfileMultipleCoLocatedComponentsEmitsPartialBuildsToEach(t *testing.T) {
	// A root Dockerfile co-located with two components (e.g. mastodon's Ruby +
	// Node apps in the same root directory) should produce partial builds edges
	// to EACH component with reason dockerfile_co_located_with_multiple_components,
	// rather than being silently dropped because "exactly one co-located component"
	// doesn't hold.
	doc := mapdoc.New()
	ruby := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "ruby")
	ruby.Name = "Mastodon"
	ruby.Properties = map[string]string{"root": "."}
	npm := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "npm")
	npm.Name = "@mastodon/mastodon"
	npm.Properties = map[string]string{"root": "."}
	doc.Nodes = append(doc.Nodes, ruby, npm)

	ev := deployables.Evidence{Field: "FROM", Value: "ruby:3.3", Line: 1, Basis: "dockerfile-instruction"}
	// Name "(root)" reflects dockerfileDisplayName("Dockerfile") for the root Dockerfile.
	definitions := []deployables.Definition{
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})

	buildsEdges := 0
	for _, edge := range doc.Edges {
		if edge.Type != mapdoc.EdgeBuilds {
			continue
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial {
			t.Fatalf("multi-component builds edge should be partial: %+v", edge)
		}
		found := false
		for _, r := range edge.Coverage.Reasons {
			if r == "dockerfile_co_located_with_multiple_components" {
				found = true
			}
		}
		if !found {
			t.Fatalf("multi-component builds edge missing expected reason: %+v", edge)
		}
		buildsEdges++
	}
	if buildsEdges != 2 {
		t.Fatalf("want 2 partial builds edges (one per co-located component), got %d", buildsEdges)
	}
}

func TestRootDockerfileCopyingMavenArchiveLinksArchiveModule(t *testing.T) {
	doc := mapdoc.New()
	aggregator := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	aggregator.Name = "openmrs"
	aggregator.Properties = map[string]string{"root": "."}
	webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	webapp.Name = "openmrs-webapp"
	webapp.Properties = map[string]string{"root": "webapp"}
	doc.Nodes = append(doc.Nodes, aggregator, webapp)
	archiveEvidence := deployables.Evidence{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}
	copyEvidence := deployables.Evidence{Field: "COPY --from source", Value: "/openmrs/distribution/openmrs_core/openmrs.war", Line: 168, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "openmrs.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{archiveEvidence}},
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "base_image_or_stage", Stage: "final", Evidence: deployables.Evidence{Line: 125}}, {Kind: "copy_source", Value: "webapp/pom.xml", Qualification: "local", Evidence: deployables.Evidence{Field: "COPY source", Value: "webapp/pom.xml", Line: 38, Basis: "dockerfile-instruction"}, Stage: "compile", TargetPath: "/openmrs_core/pom.xml"}, {Kind: "copy_source", Value: ".", Qualification: "local", Evidence: deployables.Evidence{Field: "COPY source", Value: ".", Line: 55, Basis: "dockerfile-instruction"}, Stage: "compile", TargetPath: "/openmrs_core/"}, {Kind: "copy_from", Value: "compile", Qualification: "local", Evidence: deployables.Evidence{Field: "COPY --from", Value: "compile", Line: 105, Basis: "dockerfile-instruction"}, Stage: "dev", SourceStage: "compile", SourcePath: "/openmrs_core", TargetPath: "/openmrs_core/"}, {Kind: "copy_source_stage", Value: copyEvidence.Value, Qualification: "local", Evidence: copyEvidence, Stage: "final", SourceStage: "dev", SourcePath: copyEvidence.Value, TargetPath: "/openmrs/distribution/openmrs_core/openmrs.war"}}, DockerPathCopies: []deployables.Reference{{Kind: "run_copy", Value: "/openmrs_core/webapp/target/openmrs.war", Qualification: "local", Evidence: deployables.Evidence{Field: "RUN cp source", Value: "/openmrs_core/webapp/target/openmrs.war", Line: 108, Basis: "dockerfile-instruction"}, Stage: "dev", SourcePath: "/openmrs_core/webapp/target/openmrs.war", TargetPath: "/openmrs/distribution/openmrs_core/openmrs.war"}}, DockerPathWrites: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cp -a /openmrs_core/webapp/target/openmrs.war /openmrs/distribution/openmrs_core/openmrs.war", Stage: "dev", Evidence: deployables.Evidence{Line: 108}}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From != dockerID || edge.Type != mapdoc.EdgeBuilds {
			continue
		}
		if edge.To != webapp.ID {
			t.Fatalf("root Dockerfile attributed to %q; want WAR module %q", edge.To, webapp.ID)
		}
		if edge.Coverage.Status != mapdoc.CoveragePartial || len(edge.Coverage.Reasons) != 1 || edge.Coverage.Reasons[0] != "dockerfile_copy_source_matches_maven_archive" {
			t.Fatalf("artifact attribution must remain qualified: %+v", edge)
		}
		if len(edge.Evidence) != 1 || edge.Evidence[0].Path != "Dockerfile" || edge.Evidence[0].Span == nil || edge.Evidence[0].Span.StartLine != 168 {
			t.Fatalf("edge must cite the COPY source: %+v", edge.Evidence)
		}
		return
	}
	t.Fatal("no root Dockerfile builds edge emitted")
}

func TestRootDockerfileTargetCopyCanIdentifyRootMavenArchive(t *testing.T) {
	doc := mapdoc.New()
	root := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	root.Properties = map[string]string{"root": "."}
	doc.Nodes = append(doc.Nodes, root)
	archive := deployables.Evidence{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}
	copy := deployables.Evidence{Field: "COPY source", Value: "target/ROOT.war", Line: 8, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "ROOT.war", Path: "pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{archive}},
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "base_image_or_stage", Stage: "0", Evidence: deployables.Evidence{Line: 1}}, {Kind: "copy_source", Value: copy.Value, Qualification: "local", Stage: "0", TargetPath: "/app/ROOT.war", Evidence: copy}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds {
			if edge.To != root.ID || len(edge.Coverage.Reasons) != 1 || edge.Coverage.Reasons[0] != "dockerfile_copy_source_matches_maven_archive" {
				t.Fatalf("root target copy did not identify root Maven archive: %+v", edge)
			}
			return
		}
	}
	t.Fatal("no root Dockerfile builds edge emitted")
}

func TestUnusedDockerStageCannotClaimMavenArchive(t *testing.T) {
	for _, test := range []struct {
		name, copyStage string
		source          string
		stageCopy       bool
		writes          []deployables.Reference
	}{
		{name: "unused build stage", copyStage: "build"},
		{name: "unused transferred stage", copyStage: "build", stageCopy: true},
		{name: "downloaded archive within module directory", copyStage: "final", source: "webapp/download/ROOT.war"},
		{name: "overwritten final stage", copyStage: "final", writes: []deployables.Reference{{Kind: "run_instruction", Value: "RUN curl -fsSL https://example.invalid/ROOT.war -o /app/ROOT.war", Stage: "final", Evidence: deployables.Evidence{Line: 8}}}},
		{name: "copied-over final stage", copyStage: "final", writes: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cp /tmp/foreign/ROOT.war /app/ROOT.war", Stage: "final", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", SourcePath: "/tmp/foreign/ROOT.war", TargetPath: "/app/ROOT.war", Stage: "final", Evidence: deployables.Evidence{Line: 8}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			copyLine := 2
			if test.copyStage == "final" {
				copyLine = 5
			}
			source := test.source
			if source == "" {
				source = "webapp/target/ROOT.war"
			}
			copyRef := deployables.Reference{Kind: "copy_source", Value: source, Qualification: "local", Stage: test.copyStage, TargetPath: "/app/ROOT.war", Evidence: deployables.Evidence{Field: "COPY source", Line: copyLine}}
			refs := []deployables.Reference{{Kind: "base_image_or_stage", Stage: "build", Evidence: deployables.Evidence{Line: 1}}}
			if test.copyStage == "build" {
				refs = append(refs, copyRef)
			}
			if test.stageCopy {
				refs = append(refs,
					deployables.Reference{Kind: "base_image_or_stage", Stage: "intermediate", Evidence: deployables.Evidence{Line: 3}},
					deployables.Reference{Kind: "copy_from", Qualification: "local", Stage: "intermediate", SourceStage: "build", SourcePath: "/app/ROOT.war", TargetPath: "/app/ROOT.war", Evidence: deployables.Evidence{Line: 4}},
					deployables.Reference{Kind: "copy_source_stage", Value: "/app/ROOT.war", Qualification: "local", Stage: "intermediate", SourceStage: "build", SourcePath: "/app/ROOT.war", TargetPath: "/app/ROOT.war", Evidence: deployables.Evidence{Field: "COPY --from source", Line: 4}},
				)
			}
			finalLine := 4
			if test.stageCopy {
				finalLine = 5
			}
			refs = append(refs, deployables.Reference{Kind: "base_image_or_stage", Stage: "final", Evidence: deployables.Evidence{Line: finalLine}})
			if test.copyStage == "final" {
				refs = append(refs, copyRef)
			}
			doc := mapdoc.New()
			root := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
			root.Name = "root"
			root.Properties = map[string]string{"root": "."}
			webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
			webapp.Name = "webapp"
			webapp.Properties = map[string]string{"root": "webapp"}
			doc.Nodes = append(doc.Nodes, root, webapp)
			defs := []deployables.Definition{
				{Provider: "maven", Kind: "archive", Format: "war", Name: "ROOT.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "packaging", Value: "war", Line: 1}}},
				{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1}}, DockerPathWrites: test.writes, References: refs},
			}
			addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: defs})
			dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
			for _, edge := range doc.Edges {
				if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds && edge.To == webapp.ID {
					t.Fatal("Dockerfile attributed a Maven archive not present in the final image")
				}
			}
		})
	}
}

func TestNestedDockerfileTargetCopyDoesNotIdentifyRootMavenArchive(t *testing.T) {
	doc := mapdoc.New()
	root := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	root.Properties = map[string]string{"root": "."}
	service := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api", "services/api/go.mod"}, "go")
	service.Properties = map[string]string{"root": "services/api"}
	doc.Nodes = append(doc.Nodes, root, service)
	archive := deployables.Evidence{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}
	copy := deployables.Evidence{Field: "COPY source", Value: "target/ROOT.war", Line: 8, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "ROOT.war", Path: "pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{archive}},
		{Provider: "dockerfile", Kind: "container_build", Name: "api", Path: "services/api/Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "copy_source", Value: copy.Value, Qualification: "local", Evidence: copy}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"services/api/Dockerfile"}, "dockerfile:container_build:api").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds && edge.To == root.ID {
			t.Fatalf("nested Dockerfile target copy was falsely attributed to root Maven module: %+v", edge)
		}
	}
}

func TestNestedDockerfileContextSourceDoesNotIdentifyRepositoryModule(t *testing.T) {
	doc := mapdoc.New()
	root := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	root.Properties = map[string]string{"root": "webapp"}
	service := mapdoc.NewNode(mapdoc.NodeComponent, []string{"service", "service/go.mod"}, "go")
	service.Properties = map[string]string{"root": "service"}
	doc.Nodes = append(doc.Nodes, root, service)
	copy := deployables.Evidence{Field: "COPY source", Value: "webapp/target/ROOT.war", Line: 2, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "ROOT.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}}},
		{Provider: "dockerfile", Kind: "container_build", Name: "service", Path: "service/Dockerfile", Coverage: "complete", DockerContextUnknown: true, Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "copy_source", Value: copy.Value, Qualification: "local", Evidence: copy}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"service/Dockerfile"}, "dockerfile:container_build:service").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds && edge.To == root.ID {
			t.Fatalf("nested COPY source was mistaken for a repository-root path: %+v", edge)
		}
	}
}

func TestDockerfileUnrelatedCopyBasenameDoesNotSelectMavenModule(t *testing.T) {
	doc := mapdoc.New()
	aggregator := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	aggregator.Properties = map[string]string{"root": "."}
	webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	webapp.Properties = map[string]string{"root": "webapp"}
	doc.Nodes = append(doc.Nodes, aggregator, webapp)
	evidence := deployables.Evidence{Field: "COPY source", Value: "/tmp/openmrs.war", Line: 2, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "openmrs.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}}},
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "copy_source", Value: evidence.Value, Qualification: "local", Evidence: evidence}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds {
			if edge.To != aggregator.ID {
				t.Fatalf("unrelated same-basename source selected %q; want fallback to aggregator %q", edge.To, aggregator.ID)
			}
			return
		}
	}
	t.Fatal("no root Dockerfile builds edge emitted")
}

func TestDockerfileStageArchiveRequiresModuleBuildContextCopy(t *testing.T) {
	doc := mapdoc.New()
	root := mapdoc.NewNode(mapdoc.NodeComponent, []string{"."}, "maven")
	root.Properties = map[string]string{"root": "."}
	webapp := mapdoc.NewNode(mapdoc.NodeComponent, []string{"webapp", "webapp/pom.xml"}, "maven")
	webapp.Properties = map[string]string{"root": "webapp"}
	doc.Nodes = append(doc.Nodes, root, webapp)
	evidence := deployables.Evidence{Field: "COPY --from source", Value: "/ROOT.war", Line: 8, Basis: "dockerfile-instruction"}
	definitions := []deployables.Definition{
		{Provider: "maven", Kind: "archive", Format: "war", Name: "ROOT.war", Path: "webapp/pom.xml", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "packaging", Value: "war", Line: 5, Basis: "maven-pom-field"}}},
		{Provider: "dockerfile", Kind: "container_build", Name: "(root)", Path: "Dockerfile", Coverage: "complete", Evidence: []deployables.Evidence{{Field: "FROM", Line: 1, Basis: "dockerfile-instruction"}}, References: []deployables.Reference{{Kind: "copy_source", Value: ".", Qualification: "local", Evidence: deployables.Evidence{Field: "COPY source", Value: ".", Line: 2, Basis: "dockerfile-instruction"}, Stage: "build"}, {Kind: "copy_source_stage", Value: evidence.Value, Qualification: "local", Evidence: evidence, Stage: "final", SourceStage: "build", SourcePath: evidence.Value, TargetPath: "/usr/local/tomcat/webapps/ROOT.war"}}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: definitions})
	dockerID := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"Dockerfile"}, "dockerfile:container_build:(root)").ID
	for _, edge := range doc.Edges {
		if edge.From == dockerID && edge.Type == mapdoc.EdgeBuilds {
			if edge.To != root.ID {
				t.Fatalf("downloaded same-basename archive attributed to %q; want Dockerfile root fallback %q", edge.To, root.ID)
			}
			return
		}
	}
	t.Fatal("no root Dockerfile builds edge emitted")
}

func TestDockerStageArtifactNeedsTraceToMavenTargetPath(t *testing.T) {
	copyArtifact := func(source string) deployables.Reference {
		return deployables.Reference{Kind: "copy_source_stage", Qualification: "local", Stage: "final", SourceStage: "build", SourcePath: source, TargetPath: "/usr/local/tomcat/webapps/ROOT.war", Evidence: deployables.Evidence{Field: "COPY --from source", Line: 20}}
	}
	contextCopy := deployables.Reference{Kind: "copy_source", Value: ".", Qualification: "local", Stage: "build", TargetPath: "/workspace", Evidence: deployables.Evidence{Field: "COPY source", Line: 2}}
	tests := []struct {
		name  string
		refs  []deployables.Reference
		paths []deployables.Reference
		stage string
		path  string
		line  int
		want  bool
	}{
		{name: "broad context then downloaded root war", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}},
		{name: "context plus curl output", refs: []deployables.Reference{contextCopy, copyArtifact("/tmp/ROOT.war")}},
		{name: "same module name under another context directory", refs: []deployables.Reference{{Kind: "copy_source", Value: "other/webapp", Qualification: "local", Stage: "build", TargetPath: "/workspace/webapp", Evidence: deployables.Evidence{Line: 2}}, copyArtifact("/workspace/webapp/target/ROOT.war")}},
		{name: "context plus multi-source cp from unrelated file", refs: []deployables.Reference{contextCopy, copyArtifact("/tmp/unrelated.war")}, paths: []deployables.Reference{{Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/tmp/unrelated.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "target-shaped path without context copy", refs: []deployables.Reference{copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "module target path mapped by context copy", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cp /workspace/webapp/target/ROOT.war /ROOT.war", Stage: "build", Evidence: deployables.Evidence{Line: 10}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}, want: true},
		{name: "opaque RUN cannot be transfer proof", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction_opaque", Value: "RUN cp /workspace/webapp/target/ROOT.war /ROOT.war", Stage: "build", Evidence: deployables.Evidence{Line: 10}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "curl overwrites context path before cp", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction", Value: "RUN curl -o /workspace/webapp/target/ROOT.war https://example.invalid/ROOT.war", Stage: "build", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "ADD overwrites context path before cp", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "add_source", Stage: "build", TargetPath: "/workspace/webapp/target/ROOT.war", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "combined cp and curl command is opaque", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction", Value: "RUN cp /x /workspace/webapp/target/ROOT.war && curl -o /workspace/webapp/target/ROOT.war https://example.invalid/ROOT.war", Stage: "build", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 8}}}},
		{name: "truncated RUN is opaque", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction_opaque", Value: "RUN mvn clean install", Stage: "build", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "rm makes intervening output uncertain", refs: []deployables.Reference{contextCopy, copyArtifact("/ROOT.war")}, paths: []deployables.Reference{{Kind: "run_instruction", Value: "RUN rm -f /workspace/webapp/target/ROOT.war", Stage: "build", Evidence: deployables.Evidence{Line: 8}}, {Kind: "run_copy", Qualification: "local", Stage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/ROOT.war", Evidence: deployables.Evidence{Line: 10}}}},
		{name: "receiving stage write blocks path transfer", refs: []deployables.Reference{{Kind: "copy_source", Value: ".", Qualification: "local", Stage: "compile", TargetPath: "/workspace", Evidence: deployables.Evidence{Line: 2}}, {Kind: "copy_from", Qualification: "local", Stage: "build", SourceStage: "compile", SourcePath: "/workspace", TargetPath: "/workspace", Evidence: deployables.Evidence{Line: 5}}, {Kind: "run_instruction", Value: "RUN curl -o /workspace/webapp/target/ROOT.war https://example.invalid/ROOT.war", Qualification: "local", Stage: "build", Evidence: deployables.Evidence{Line: 8}}, {Kind: "copy_source_stage", Qualification: "local", Stage: "final", SourceStage: "build", SourcePath: "/workspace/webapp/target/ROOT.war", TargetPath: "/usr/local/tomcat/ROOT.war", Evidence: deployables.Evidence{Field: "COPY --from source", Line: 20}}}, stage: "build", path: "/workspace/webapp/target/ROOT.war", line: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, artifact, line := tt.stage, tt.path, tt.line
			if stage == "" {
				stage, artifact, line = "build", "/ROOT.war", 21
			}
			if got := dockerContextIncludesModule(tt.refs, tt.paths, "webapp", stage, artifact, line); got != tt.want {
				t.Fatalf("dockerContextIncludesModule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDockerContextCopyMapsArchiveFileIntoDirectory(t *testing.T) {
	refs := []deployables.Reference{{Kind: "copy_source", Value: "webapp/target/ROOT.war", Qualification: "local", Stage: "build", TargetPath: "/workspace/", Evidence: deployables.Evidence{Line: 2}}}
	if !dockerContextIncludesModule(refs, nil, "webapp", "build", "/workspace/ROOT.war", 3) {
		t.Fatal("file source copied into a directory should retain its source path")
	}
}

func TestMultiSourceCopyDoesNotOverwriteUnrelatedArchive(t *testing.T) {
	run := deployables.Reference{Kind: "run_instruction", Value: "&& cp -a /openmrs_core/startup.sh /openmrs_core/wait-for-it.sh /openmrs/", Stage: "dev", Evidence: deployables.Evidence{Line: 10}}
	if !dockerRunIsKnownBuildOrCopy(nil, run, "/openmrs/distribution/openmrs_core/openmrs.war") {
		t.Fatal("unrelated multi-source cp should not erase known archive provenance")
	}
	if dockerRunIsKnownBuildOrCopy(nil, run, "/openmrs/startup.sh") {
		t.Fatal("multi-source cp that writes the artifact must block attribution")
	}
}

func TestGoModuleDisplayName(t *testing.T) {
	for _, test := range []struct{ module, want string }{
		{"github.com/grafana/loki/v3", "loki"},
		{"github.com/grafana/loki", "loki"},
		{"dircue", "dircue"},
		{"example.com/tools/v2/cmd", "cmd"},
	} {
		if got := goModuleDisplayName(test.module); got != test.want {
			t.Errorf("goModuleDisplayName(%q) = %q, want %q", test.module, got, test.want)
		}
	}
}

// TestCargoAuxTargetsGetNonPrimaryRole verifies that Cargo [[bench]], [[test]],
// and [[example]] interface observations receive a non-primary role even when
// the evidence path is the containing Cargo.toml (so mapPathRole returns "").
// These tests fail on 427c2f8 (no interfaceKindRole fallback) and pass after
// (#fix-5).
func TestCargoAuxTargetsGetNonPrimaryRole(t *testing.T) {
	for _, tc := range []struct {
		ikind    string
		wantRole string
	}{
		{"cargo-bench", "tooling"},
		{"cargo-test", "test"},
		{"cargo-example", "example"},
	} {
		doc := mapdoc.New()
		// A Rust component with a path-only Cargo.toml at the repo root.
		component := mapdoc.NewNode(mapdoc.NodeComponent, []string{"Cargo.toml"}, "rust")
		component.Properties = map[string]string{"root": "."}
		doc.Nodes = append(doc.Nodes, component)

		obs := intentmap.Observation{
			Kind:      intentmap.KindInterface,
			Name:      "my_bench",
			ProjectID: "Cargo.toml",
			State:     "declared",
			Basis:     "declared_manifest",
			// Path is the containing Cargo.toml, not a benches/* source file.
			Path: "Cargo.toml",
			Properties: map[string]string{
				"interface_kind": tc.ikind,
			},
		}
		report := &intentmap.Report{
			Coverage:     intentmap.Coverage{Status: "complete"},
			Observations: []intentmap.Observation{obs},
		}
		addIntent(&doc, report)

		var found bool
		for _, n := range doc.Nodes {
			if n.Kind != mapdoc.NodeInterface || n.Name != "my_bench" {
				continue
			}
			found = true
			role := n.Properties["role"]
			if role != tc.wantRole {
				t.Errorf("ikind=%q: node role = %q, want %q", tc.ikind, role, tc.wantRole)
			}
			basis := n.Properties["role_basis"]
			if basis != "interface_kind" {
				t.Errorf("ikind=%q: role_basis = %q, want interface_kind", tc.ikind, basis)
			}
		}
		if !found {
			t.Errorf("ikind=%q: no interface node named my_bench created", tc.ikind)
		}
	}
}

// --- P1: silent suppression tests ---

// TestBuildContextAmbiguousExactRootRecordsReason verifies that when a
// build_context resolves to a path with more than one component, the
// deployable_reference fact is marked partial/ambiguous_component_root and
// no edge is emitted.
func TestBuildContextAmbiguousExactRootRecordsReason(t *testing.T) {
	doc := mapdoc.New()
	for _, src := range [][]string{{"svc/go.mod"}, {"svc/package.json"}} {
		c := mapdoc.NewNode(mapdoc.NodeComponent, src, "test")
		c.Properties = map[string]string{"root": "svc"}
		c.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		c.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: src[0], SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
		doc.Nodes = append(doc.Nodes, c)
	}
	ev := deployables.Evidence{Field: "build", Value: "svc", Line: 2, Basis: "compose-build-field"}
	def := deployables.Definition{
		Provider: "compose", Kind: "service", Name: "svc", Path: "docker-compose.yaml",
		Coverage: "complete", Evidence: []deployables.Evidence{{Field: "image", Value: "svc:latest", Line: 1, Basis: "compose-build-field"}},
		References: []deployables.Reference{{Kind: "build_context", Value: "svc", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	// No builds edge should be emitted.
	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("unexpected builds edge for ambiguous build_context exact root: %+v", e)
		}
	}
	// The deployable_reference fact must be partial with ambiguous_component_root reason.
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "build_context" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "ambiguous_component_root" {
					t.Errorf("fact coverage reasons = %v, want [ambiguous_component_root]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Fatal("no deployable_reference fact for build_context found")
	}
}

// TestCodeUriAmbiguousExactRootRecordsReason verifies that a code_uri whose
// resolved path has multiple components records ambiguous_component_root on the
// deployable_reference fact.
func TestCodeUriAmbiguousExactRootRecordsReason(t *testing.T) {
	doc := mapdoc.New()
	for _, src := range [][]string{{"svc/go.mod"}, {"svc/package.json"}} {
		c := mapdoc.NewNode(mapdoc.NodeComponent, src, "test")
		c.Properties = map[string]string{"root": "svc"}
		c.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		c.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: src[0], SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
		doc.Nodes = append(doc.Nodes, c)
	}
	ev := deployables.Evidence{Field: "CodeUri", Value: "svc", Line: 3, Basis: "sam-function-code-uri"}
	def := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "fn", Path: "template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "Type", Value: "AWS::Serverless::Function", Line: 2, Basis: "cloudformation-resource"}},
		References: []deployables.Reference{{Kind: "code_uri", Value: "svc", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("unexpected builds edge for ambiguous code_uri exact root: %+v", e)
		}
	}
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "code_uri" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "ambiguous_component_root" {
					t.Errorf("fact coverage reasons = %v, want [ambiguous_component_root]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Fatal("no deployable_reference fact for code_uri found")
	}
}

// TestCodeUriAmbiguousNearestAncestorRecordsReason verifies that when the
// nearest ancestor of a code_uri path has multiple components, the fact is
// partial/ambiguous_component_root even though a more distant ancestor (the
// repo root) has a unique component.
func TestCodeUriAmbiguousNearestAncestorRecordsReason(t *testing.T) {
	doc := mapdoc.New()
	// Repo root: one component.
	rootC := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	rootC.Properties = map[string]string{"root": "."}
	rootC.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	rootC.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc.Nodes = append(doc.Nodes, rootC)
	// services/api: two components (ambiguous).
	for _, src := range [][]string{{"services/api/go.mod"}, {"services/api/package.json"}} {
		c := mapdoc.NewNode(mapdoc.NodeComponent, src, "test")
		c.Properties = map[string]string{"root": "services/api"}
		c.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
		c.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: src[0], SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
		doc.Nodes = append(doc.Nodes, c)
	}
	// code_uri points to an artifact nested under services/api.
	ev := deployables.Evidence{Field: "CodeUri", Value: "services/api/target/app.jar", Line: 4, Basis: "sam-function-code-uri"}
	def := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "fn", Path: "template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "Type", Value: "AWS::Serverless::Function", Line: 2, Basis: "cloudformation-resource"}},
		References: []deployables.Reference{{Kind: "code_uri", Value: "services/api/target/app.jar", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("ambiguous nearest ancestor incorrectly fell back to repo root: %+v", e)
		}
	}
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "code_uri" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "ambiguous_component_root" {
					t.Errorf("fact coverage reasons = %v, want [ambiguous_component_root]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Fatal("no deployable_reference fact for code_uri found")
	}
}

// --- P2: root-escape tests ---

// TestCodeUriRootEscapeIsNotAttributed verifies that CodeUri: ".." from a
// root-level template.yaml resolves to ".." (outside the repository) and is
// neither attributed to any component nor granted complete coverage.
func TestCodeUriRootEscapeIsNotAttributed(t *testing.T) {
	doc := mapdoc.New()
	rootC := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	rootC.Properties = map[string]string{"root": "."}
	rootC.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	rootC.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc.Nodes = append(doc.Nodes, rootC)

	// template.yaml at repo root with CodeUri: ".." resolves to ".."
	ev := deployables.Evidence{Field: "CodeUri", Value: "..", Line: 3, Basis: "sam-function-code-uri"}
	def := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "fn", Path: "template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "Type", Value: "AWS::Serverless::Function", Line: 2, Basis: "cloudformation-resource"}},
		References: []deployables.Reference{{Kind: "code_uri", Value: "..", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	// No builds edge should be emitted.
	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("out-of-repo code_uri incorrectly produced a builds edge to %v", e.To)
		}
	}
	// The fact must be partial with path_outside_repository reason.
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "code_uri" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "path_outside_repository" {
					t.Errorf("fact coverage reasons = %v, want [path_outside_repository]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Fatal("no deployable_reference fact for code_uri found")
	}
}

// TestCodeUriInfraSubdirDoesNotEscape verifies that infra/template.yaml with
// CodeUri: ".." correctly resolves to "." (the repo root) and IS attributed to
// the root component. This is the canonical valid use case.
func TestCodeUriInfraSubdirDoesNotEscape(t *testing.T) {
	doc := mapdoc.New()
	rootC := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	rootC.Properties = map[string]string{"root": "."}
	rootC.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	rootC.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc.Nodes = append(doc.Nodes, rootC)

	// infra/template.yaml with CodeUri: ".." resolves to "." (valid).
	ev := deployables.Evidence{Field: "CodeUri", Value: "..", Line: 3, Basis: "sam-function-code-uri"}
	def := deployables.Definition{
		Provider: "cloudformation", Kind: "infrastructure", Name: "fn", Path: "infra/template.yaml",
		Coverage: "qualified", Evidence: []deployables.Evidence{{Field: "Type", Value: "AWS::Serverless::Function", Line: 2, Basis: "cloudformation-resource"}},
		References: []deployables.Reference{{Kind: "code_uri", Value: "..", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	found := false
	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds && e.To == rootC.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("infra/template.yaml CodeUri:..(→.) must build the root component; edges=%+v", doc.Edges)
	}
}

// TestWorkflowAmbiguousExactRootRecordsReason verifies that a workflow
// working-directory that matches multiple component roots records
// ambiguous_component_root on the deployable_reference fact.
func TestWorkflowAmbiguousExactRootRecordsReason(t *testing.T) {
	doc, r := workflowEdgesDoc(t, "services/api")
	// Add a second component at services/api to make it ambiguous.
	dup := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/package.json"}, "npm")
	dup.Name = "api-npm"
	dup.Properties = map[string]string{"root": "services/api"}
	dup.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	dup.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "services/api/package.json", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc.Nodes = append(doc.Nodes, dup)

	addDeployables(doc, r)
	addWorkflowComponentEdges(doc, r)

	// No edge should be emitted.
	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("ambiguous exact working-directory produced a builds edge: %+v", e)
		}
	}
	// The deployable_reference fact must record ambiguous_component_root.
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "working_directory" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "ambiguous_component_root" {
					t.Errorf("fact coverage reasons = %v, want [ambiguous_component_root]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Error("no deployable_reference fact for working_directory found")
	}
}

// TestWorkflowRootEscapeIsNotAttributed verifies that a workflow
// working-directory of ".." (escaping the repository root) produces no edge
// and marks the fact partial/path_outside_repository.
func TestWorkflowRootEscapeIsNotAttributed(t *testing.T) {
	doc, r := workflowEdgesDoc(t, "..")
	addDeployables(doc, r)
	addWorkflowComponentEdges(doc, r)

	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("out-of-repo working-directory produced a builds edge: %+v", e)
		}
	}
	found := false
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "working_directory" {
				found = true
				if f.Coverage.Status != mapdoc.CoveragePartial {
					t.Errorf("fact coverage status = %v, want partial", f.Coverage.Status)
				}
				if len(f.Coverage.Reasons) == 0 || f.Coverage.Reasons[0] != "path_outside_repository" {
					t.Errorf("fact coverage reasons = %v, want [path_outside_repository]", f.Coverage.Reasons)
				}
			}
		}
	}
	if !found {
		t.Error("no deployable_reference fact for working_directory with '..' found")
	}
}

// TestBuildContextRootEscapeIsNotAttributed verifies that a build_context of
// "../other" from a root-level Compose file resolves outside the repository
// and is not attributed.
func TestBuildContextRootEscapeIsNotAttributed(t *testing.T) {
	doc := mapdoc.New()
	rootC := mapdoc.NewNode(mapdoc.NodeComponent, []string{"go.mod"}, "go")
	rootC.Properties = map[string]string{"root": "."}
	rootC.Coverage = mapdoc.Coverage{Status: mapdoc.CoverageComplete}
	rootC.Evidence = []mapdoc.Evidence{{Basis: mapdoc.BasisDeclaredConfig, Path: "go.mod", SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}}
	doc.Nodes = append(doc.Nodes, rootC)

	ev := deployables.Evidence{Field: "build", Value: "../other", Line: 2, Basis: "compose-build-field"}
	def := deployables.Definition{
		Provider: "compose", Kind: "service", Name: "other", Path: "docker-compose.yaml",
		Coverage: "complete", Evidence: []deployables.Evidence{{Field: "image", Value: "other:latest", Line: 1, Basis: "compose-build-field"}},
		References: []deployables.Reference{{Kind: "build_context", Value: "../other", Qualification: "local", Evidence: ev}},
	}
	addDeployables(&doc, &deployables.Report{Status: "complete", Definitions: []deployables.Definition{def}})

	for _, e := range doc.Edges {
		if e.Type == mapdoc.EdgeBuilds {
			t.Fatalf("out-of-repo build_context produced a builds edge: %+v", e)
		}
	}
	// Check for path_outside_repository on the fact.
	var factReason string
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeDeployable {
			continue
		}
		for _, f := range n.Facts {
			if f.Kind == "deployable_reference" && f.Name == "build_context" {
				if len(f.Coverage.Reasons) > 0 {
					factReason = f.Coverage.Reasons[0]
				}
			}
		}
	}
	if !strings.Contains(factReason, "outside_repository") {
		t.Errorf("expected path_outside_repository reason; got %q", factReason)
	}
}

func TestCapabilityEvidenceQualificationsRetainMixedBases(t *testing.T) {
	cases := []struct {
		name                                               string
		observations                                       []intentmap.Observation
		status, wantTestOnly, wantTest, wantProd, wantType string
	}{
		{"test-only imports complete", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/test/java/Test.java", Properties: map[string]string{"evidence_scope": "test_path_convention"}}}, "complete", "true", "true", "", ""},
		{"test-only Go client syntax", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "net:http-client", ProjectID: "app/go.mod", State: "observed", Basis: "code_syntax", Path: "app/mux_test.go", Properties: map[string]string{"evidence_scope": "test_path_convention"}}}, "complete", "true", "true", "", ""},
		{"mixed source imports", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/test/java/Test.java", Properties: map[string]string{"evidence_scope": "test_path_convention"}}, {Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/main/java/App.java", Properties: map[string]string{"evidence_scope": "non_test_path_convention"}}}, "complete", "", "true", "true", ""},
		{"test import and runtime declaration", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/test/java/Test.java", Properties: map[string]string{"evidence_scope": "test_path_convention"}}, {Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "declared", Basis: "declared_dependency", Path: "app/pom.xml"}}, "complete", "", "true", "", ""},
		{"test-only import but incomplete scan", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/test/java/Test.java", Properties: map[string]string{"evidence_scope": "test_path_convention"}}}, "partial", "true", "true", "", ""},
		{"type-only import", []intentmap.Observation{{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/main/ts/a.ts", Properties: map[string]string{"import_qualifier": "type_only", "evidence_scope": "non_test_path_convention"}}}, "complete", "", "", "true", "true"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			doc := mapdoc.New()
			c := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/pom.xml"}, "app")
			c.Properties = map[string]string{"root": "app"}
			doc.Nodes = append(doc.Nodes, c)
			addIntent(&doc, &intentmap.Report{Coverage: intentmap.Coverage{Status: tt.status}, Observations: tt.observations})
			for _, n := range doc.Nodes {
				if n.Kind != mapdoc.NodeCapability {
					continue
				}
				check := func(key, want string) {
					if got := n.Properties[key]; got != want {
						t.Errorf("%s=%q want %q", key, got, want)
					}
				}
				check("test_only_evidence", tt.wantTestOnly)
				check("test_path_evidence", tt.wantTest)
				check("non_test_path_evidence", tt.wantProd)
				check("type_only_import_evidence", tt.wantType)
				return
			}
			t.Fatal("missing capability node")
		})
	}
}

func TestUnrelatedFileOmissionDoesNotHideObservedTestOnlyEvidence(t *testing.T) {
	doc := mapdoc.New()
	c := mapdoc.NewNode(mapdoc.NodeComponent, []string{"app", "app/pom.xml"}, "app")
	c.Properties = map[string]string{"root": "app"}
	doc.Nodes = append(doc.Nodes, c)
	addIntent(&doc, &intentmap.Report{
		Coverage: intentmap.Coverage{Status: "partial", Omissions: map[string]int{"file_bytes": 1}},
		Observations: []intentmap.Observation{{
			Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/pom.xml", State: "observed", Basis: "imported", Path: "app/src/test/java/DbTest.java",
			Properties: map[string]string{"evidence_scope": "test_path_convention"},
		}},
	})
	for _, n := range doc.Nodes {
		if n.Kind != mapdoc.NodeCapability {
			continue
		}
		if n.Properties["test_path_evidence"] != "true" || n.Properties["test_only_evidence"] != "true" {
			t.Fatalf("unrelated file omission hid the retained test-only evidence: %+v", n.Properties)
		}
		return
	}
	t.Fatal("missing retained capability despite the unrelated omission")
}

func TestOptionalDependencyImportQualificationsDoNotPromoteRuntimeState(t *testing.T) {
	for _, tc := range []struct {
		name        string
		qualifier   string
		scope       string
		condition   string
		importFirst bool
	}{
		{name: "optional type-only first", qualifier: "type_only", scope: "non_test_path_convention", condition: "optionalDependencies", importFirst: true},
		{name: "optional test-only first", scope: "test_path_convention", condition: "optionalDependencies", importFirst: true},
		{name: "optional type-only after", qualifier: "type_only", scope: "non_test_path_convention", condition: "optionalDependencies"},
		{name: "optional test-only after", scope: "test_path_convention", condition: "optionalDependencies"},
		{name: "peer type-only", qualifier: "type_only", scope: "non_test_path_convention", condition: "peerDependencies"},
		{name: "peer test-only", scope: "test_path_convention", condition: "peerDependencies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imported := intentmap.Observation{
				Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/package.json", State: "observed", Basis: "imported", Path: "app/src/types.ts",
				Properties: map[string]string{"evidence_scope": tc.scope, "import_qualifier": tc.qualifier},
			}
			optional := intentmap.Observation{
				Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/package.json", State: "conditional", Basis: "declared_dependency", Path: "app/package.json",
				Properties: map[string]string{"condition": tc.condition},
			}
			observations := []intentmap.Observation{optional, imported}
			if tc.importFirst {
				observations = []intentmap.Observation{imported, optional}
			}
			doc := mapdoc.New()
			addIntent(&doc, &intentmap.Report{Coverage: intentmap.Coverage{Status: "complete"}, Observations: observations})
			for _, n := range doc.Nodes {
				if n.Kind == mapdoc.NodeCapability && n.Name == "datastore:postgresql" {
					if got := n.Properties["state"]; got != "conditional" {
						t.Fatalf("state=%q, want conditional; node=%+v", got, n)
					}
					return
				}
			}
			t.Fatal("missing optional PostgreSQL capability")
		})
	}
}

func TestRuntimeImportCanCorroborateOptionalDependency(t *testing.T) {
	doc := mapdoc.New()
	addIntent(&doc, &intentmap.Report{Coverage: intentmap.Coverage{Status: "complete"}, Observations: []intentmap.Observation{
		{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/package.json", State: "conditional", Basis: "declared_dependency", Path: "app/package.json", Properties: map[string]string{"condition": "optionalDependencies"}},
		{Kind: intentmap.KindCapability, Name: "datastore:postgresql", ProjectID: "app/package.json", State: "observed", Basis: "imported", Path: "app/src/db.ts", Properties: map[string]string{"evidence_scope": "non_test_path_convention"}},
	}})
	for _, n := range doc.Nodes {
		if n.Kind == mapdoc.NodeCapability && n.Name == "datastore:postgresql" {
			if got := n.Properties["state"]; got != "observed" {
				t.Fatalf("state=%q, want observed; node=%+v", got, n)
			}
			return
		}
	}
	t.Fatal("missing optional PostgreSQL capability")
}
