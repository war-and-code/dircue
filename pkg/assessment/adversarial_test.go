package assessment

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestValidateReportRejectsCounterfeitAndOverflowMutations(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*Report)
	}{
		{"inventory below candidate population", func(r *Report) { r.Inventory.Files.Count-- }},
		{"project total differs from lockfile total", func(r *Report) { r.Projects.Count++ }},
		{"unsorted candidate evidence", func(r *Report) {
			r.CandidateEvidence[0], r.CandidateEvidence[1] = r.CandidateEvidence[1], r.CandidateEvidence[0]
		}},
		{"unknown candidate evidence kind", func(r *Report) { r.CandidateEvidence[0].Kind = "not-a-classifier-kind" }},
		{"lockfile states do not partition", func(r *Report) { r.LockfilesOverall.Missing.Count-- }},
		{"unsupported ecosystem metric does not partition", func(r *Report) { r.UnsupportedEcosystemProjects.Count++ }},
		{"invalid tree/source mode pairing", func(r *Report) { r.Source.Tree = "invalid" }},
		{"directory source cannot claim a commit", func(r *Report) { r.Source.Commit = "0123456789abcdef0123456789abcdef01234567" }},
		{"complete metric carries omission reason", func(r *Report) { r.Projects.Reasons = []string{"fabricated_omission"} }},
		{"relationship evidence duplicate", func(r *Report) { r.WorkspaceEvidence[1] = r.WorkspaceEvidence[0] }},
		{"relationship evidence kind exceeds kind total", func(r *Report) { r.WorkspaceEvidence[1].Kind = r.WorkspaceEvidence[0].Kind }},
		{"relationship source project missing", func(r *Report) { r.WorkspaceEvidence[0].SourceProject = "" }},
		{"relationship source root mismatch", func(r *Report) { r.WorkspaceEvidence[0].SourceRoot = "wrong" }},
		{"relationship target missing", func(r *Report) { r.WorkspaceEvidence[0].TargetPath = "" }},
		{"relationship target root mismatch", func(r *Report) { r.WorkspaceEvidence[0].TargetRoot = "wrong" }},
		{"relationship evidence path exceeds bound", func(r *Report) { r.WorkspaceEvidence[0].TargetPath = strings.Repeat("x", MaxEvidencePathBytes+1) }},
		{"manifest file total overflow", func(r *Report) {
			r.ManifestCandidates = []CandidateCount{
				{Filename: "a", Kind: "manifest", Ecosystem: "npm", Files: math.MaxInt64},
				{Filename: "b", Kind: "manifest", Ecosystem: "npm", Files: math.MaxInt64},
				{Filename: "c", Kind: "manifest", Ecosystem: "npm", Files: 4},
			}
			r.ManifestCandidatePopulation.Count = 2
		}},
		{"filename file total overflow", func(r *Report) {
			r.FilenameCandidates = []CandidateKindCount{
				{Kind: "alpha", Files: math.MaxInt64}, {Kind: "beta", Files: math.MaxInt64}, {Kind: "gamma", Files: 4},
			}
			r.FilenameCandidatePopulation.Count = 2
			r.CandidateEvidence = nil
			r.OmittedCandidateEvidence = map[string]int64{"alpha": math.MaxInt64, "beta": math.MaxInt64, "gamma": 4}
		}},
		{"root evidence count overflow", func(r *Report) { r.OmittedProjectRootEvidence = math.MaxInt64 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			r := nativeFixture(t, false)
			mutation.edit(r)
			if err := ValidateReport(r); err == nil {
				t.Fatal("ValidateReport accepted a counterfeit report")
			}
		})
	}
}

func TestManifestGroupsBoundAllRecognizedDotnetFamilies(t *testing.T) {
	c := New("directory", "")
	const n = 140
	for i := 0; i < n; i++ {
		c.Add(discovery.File{Path: fmt.Sprintf("solutions/App%d.sln", i), Size: 1})
		c.Add(discovery.File{Path: fmt.Sprintf("projects/App%d.CSPROJ", i), Size: 1})
		c.Add(discovery.File{Path: fmt.Sprintf("d%d\\global.json", i), Size: 1})
	}
	for _, filename := range []string{"global.json", "GLOBAL.JSON", "directory.build.props", "Directory.Build.Props", "nuget.config", "NuGet.Config"} {
		c.Add(discovery.File{Path: "config/" + filename, Size: 1})
	}
	manifests := int64(3*n + 6)
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: manifests, ParsedManifests: int(manifests)}}
	r, err := c.Finish(decls, nil)
	if err != nil {
		t.Fatalf("normalized manifest groups rejected variable .NET names: %v", err)
	}
	if len(r.ManifestCandidates) > 5 {
		t.Fatalf("expected finite groups for solution, project, and conventional configuration names: %+v", r.ManifestCandidates)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestValidateReportRejectsNULInRetainedPaths(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*Report)
	}{
		{"candidate", func(r *Report) { r.CandidateEvidence[0].Path = "app/\x00package.json" }},
		{"root", func(r *Report) { r.ProjectRootEvidence[0] = "app/\x00member" }},
		{"relationship", func(r *Report) { r.WorkspaceEvidence[0].TargetPath = "app/\x00member/package.json" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			r := nativeFixture(t, false)
			mutation.edit(r)
			if err := ValidateReport(r); err == nil {
				t.Fatal("ValidateReport accepted NUL in a selected path")
			}
		})
	}
}

func TestGeneratedParserRelationshipsUseExactSupportedSemantics(t *testing.T) {
	t.Run("npm workspace dependency is local", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"package.json":                 `{"name":"root","workspaces":["packages/*"]}`,
			"packages/app/package.json":    `{"name":"app","dependencies":{"member":"workspace:*"}}`,
			"packages/member/package.json": `{"name":"member"}`,
		})
		r := finishParsedAssessment(t, decls, records, files)
		if r.WorkspaceMembership.Count != 2 || r.LocalDependencies.Count != 1 {
			t.Fatalf("npm workspace relations were not separated: workspace=%+v local=%+v", r.WorkspaceMembership, r.LocalDependencies)
		}
		if r.LocalDependencyByKind[0].Kind != "npm-workspace-dependency" {
			t.Fatalf("wrong npm local edge kinds: %+v", r.LocalDependencyByKind)
		}
	})

	t.Run("Go local replacement is local", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"workspace/go.work":    "go 1.24.0\nuse ./app\n",
			"workspace/app/go.mod": "module example.org/app\ngo 1.24.0\nreplace example.org/lib => ../lib\n",
			"workspace/lib/go.mod": "module example.org/lib\ngo 1.24.0\n",
		})
		r := finishParsedAssessment(t, decls, records, files)
		if r.WorkspaceMembership.Count != 1 || r.WorkspaceByKind[0].Kind != "go-workspace-member" || r.LocalDependencies.Count != 1 || r.LocalDependencyByKind[0].Kind != "go-local-replacement" {
			t.Fatalf("Go workspace/local relationships missing: workspace=%+v local=%+v", r.WorkspaceByKind, r.LocalDependencyByKind)
		}
	})

	t.Run("Cargo workspace and path dependency are separated", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"Cargo.toml":                "[workspace]\nmembers = [\"crates/app\", \"crates/common\"]\nexclude = [\"crates/ignored\"]\ndefault-members = [\"crates/app\"]\n",
			"crates/app/Cargo.toml":     "[package]\nname = \"app\"\nversion = \"1.0.0\"\n[dependencies]\ncommon = { path = \"../common\" }\n",
			"crates/common/Cargo.toml":  "[package]\nname = \"common\"\nversion = \"1.0.0\"\n",
			"crates/ignored/Cargo.toml": "[package]\nname = \"ignored\"\nversion = \"1.0.0\"\n",
		})
		r := finishParsedAssessment(t, decls, records, files)
		if r.WorkspaceMembership.Count != 2 || r.LocalDependencies.Count != 1 || r.LocalDependencyByKind[0].Kind != "cargo-path-dependency" {
			t.Fatalf("Cargo relationships missing or conflated: workspace=%+v local=%+v", r.WorkspaceByKind, r.LocalDependencyByKind)
		}
		for _, row := range r.WorkspaceByKind {
			if row.Kind == "cargo-default-member" || row.Kind == "cargo-workspace-exclude" {
				t.Fatalf("selection/exclusion was counted as membership: %+v", row)
			}
		}
	})

	t.Run("Maven reactor and Gradle modules are workspace relations", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"maven/pom.xml":               "<project><groupId>org.example</groupId><artifactId>root</artifactId><version>1</version><modules><module>child</module></modules></project>",
			"maven/child/pom.xml":         "<project><groupId>org.example</groupId><artifactId>child</artifactId><version>1</version></project>",
			"gradle/settings.gradle":      "rootProject.name = 'root'\ninclude(':service')\n",
			"gradle/service/build.gradle": "plugins { id 'java' }\n",
			"dotnet/App.sln":              "Microsoft Visual Studio Solution File, Format Version 12.00\nProject(\"{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}\") = \"App\", \"src/App.csproj\", \"{A0000000-0000-0000-0000-000000000000}\"\nEndProject\n",
			"dotnet/src/App.csproj":       "<Project />",
		})
		r := finishParsedAssessment(t, decls, records, files)
		seen := map[string]bool{}
		for _, row := range r.WorkspaceByKind {
			seen[row.Kind] = true
		}
		if r.WorkspaceMembership.Count != 3 || !seen["module"] || !seen["gradle-module"] || !seen["solution-member"] {
			t.Fatalf("JVM module relations were not counted: %+v", r.WorkspaceByKind)
		}
	})

	t.Run("Dart path dependency is local", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"app/pubspec.yaml":   "name: app\ndependencies:\n  local:\n    path: ../local\n",
			"local/pubspec.yaml": "name: local\n",
		})
		r := finishParsedAssessment(t, decls, records, files)
		if r.LocalDependencies.Count != 1 || r.LocalDependencyByKind[0].Kind != "pub-path-dependency" {
			t.Fatalf("Dart path dependency missing: %+v", r.LocalDependencyByKind)
		}
	})

	t.Run(".NET project reference is local", func(t *testing.T) {
		decls, records, files := parseAssessmentDeclarations(t, map[string]string{
			"app/App.csproj": `<Project><ItemGroup><ProjectReference Include="../lib/Lib.csproj" /></ItemGroup></Project>`,
			"lib/Lib.csproj": `<Project />`,
		})
		r := finishParsedAssessment(t, decls, records, files)
		if r.LocalDependencies.Count != 1 || r.LocalDependencyByKind[0].Kind != "project-reference" {
			t.Fatalf(".NET project reference missing: %+v", r.LocalDependencyByKind)
		}
	})
}

func TestGeneratedAndSyntheticNonRelationshipsAreNotMisclassified(t *testing.T) {
	decls, records, files := parseAssessmentDeclarations(t, map[string]string{
		"py/pyproject.toml":   "[build-system]\nrequires=[]\nbuild-backend=\"backend\"\nbackend-path=[\"backend\"]\n",
		"py/requirements.txt": "-r nested.txt\n",
		"py/nested.txt":       "requests==2.0\n",
	})
	for i := range records {
		for _, ref := range records[i].Project.References {
			if ref.Kind == "python-backend-path" || ref.Kind == "python-requirements-include" {
				if ref.Target == "" {
					t.Fatalf("fixture did not exercise a confined negative relationship: %+v", ref)
				}
			}
		}
	}
	projectIndex := -1
	for i := range decls.Projects {
		if decls.Projects[i].ID == "py/pyproject.toml" {
			projectIndex = i
			break
		}
	}
	if projectIndex < 0 {
		t.Fatal("fixture Python project was not parsed")
	}
	project := decls.Projects[projectIndex]
	project.References = append(project.References, []declarations.Reference{
		{Kind: "cargo-default-member", Target: "other/member/Cargo.toml", TargetStatus: "present", State: "declared"},
		{Kind: "cargo-workspace-exclude", Target: "other/excluded/Cargo.toml", TargetStatus: "present", State: "declared"},
		{Kind: "python-backend-path", Target: "other/backend", TargetStatus: "present", State: "declared"},
		{Kind: "python-requirements-include", Target: "other/requirements.txt", TargetStatus: "present", State: "declared"},
		{Kind: "local-artifact", Target: "other/lib/local.jar", TargetStatus: "present", State: "declared"},
		{Kind: "invented-local-dependency", Target: "other/member/package.json", TargetStatus: "present", State: "declared"},
	}...)
	decls.Projects[projectIndex] = project
	for i := range records {
		if records[i].Project.ID == project.ID {
			records[i].Project = project
		}
	}
	// The parsed Python documents and the explicit negative references above
	// must never contribute to the two relationship metrics.
	r := finishParsedAssessment(t, decls, records, files)
	if r.WorkspaceMembership.Count != 0 || r.LocalDependencies.Count != 0 {
		t.Fatalf("non-relationships were counted: workspace=%+v local=%+v", r.WorkspaceMembership, r.LocalDependencies)
	}
}

func TestVirtualWorkspaceRecordsAreInProjectPopulation(t *testing.T) {
	decls, records, files := parseAssessmentDeclarations(t, map[string]string{
		"Cargo.toml":            "[workspace]\nmembers = []\n",
		"python/pyproject.toml": "[tool.uv]\nmanaged = true\n[tool.uv.workspace]\nmembers = []\n",
	})
	r := finishParsedAssessment(t, decls, records, files)
	if r.Projects.Count != 2 || r.ProjectRoots.Count != 2 || r.LockfilesOverall.Projects.Count != 2 || r.LockfilesOverall.Unsupported.Count != 2 {
		t.Fatalf("virtual workspace project scope is inconsistent: projects=%+v roots=%+v locks=%+v", r.Projects, r.ProjectRoots, r.LockfilesOverall)
	}
	if !strings.Contains(r.Projects.Scope, "virtual-workspace") {
		t.Fatalf("project metric does not disclose workspace records: %q", r.Projects.Scope)
	}
}

func TestValidateReportRejectsHostileSerializedMetric(t *testing.T) {
	r := nativeFixture(t, false)
	r.Inventory.Files.Scope = strings.Repeat("x", MaxAssessmentJSONBytes+1)
	if err := ValidateReport(r); err == nil {
		t.Fatal("ValidateReport accepted a report larger than the serialized limit")
	}
}

func TestFinishQualifiesRepresentableFileSizeAggregateOverflow(t *testing.T) {
	c := New("directory", "")
	c.Add(discovery.File{Path: "npm/package.json", Size: math.MaxInt64})
	c.Add(discovery.File{Path: "dotnet/App.csproj", Size: math.MaxInt64})
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 2, ParsedManifests: 2}}
	r, err := c.Finish(decls, nil)
	if err != nil {
		t.Fatalf("Finish rejected selected file metadata with a qualified aggregate overflow: %v", err)
	}
	if r.Inventory.Bytes.Count != math.MaxInt64 || r.Inventory.Bytes.Completeness != "lower_bound" {
		t.Fatalf("inventory byte overflow was not qualified: %+v", r.Inventory.Bytes)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestFinishQualifiesDiagnosticsAndTruncatedProjectRecords(t *testing.T) {
	for _, tc := range []struct {
		name     string
		diagnose bool
		complete bool
	}{
		{name: "diagnostic", diagnose: true, complete: true},
		{name: "truncated document", complete: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := declarations.Project{ID: "workspace/package.json", Root: "workspace", Kind: "npm", References: []declarations.Reference{{Kind: "npm-workspace-member", Target: "workspace/member/package.json", TargetStatus: "present", State: "declared"}}}
			target := declarations.Project{ID: "workspace/member/package.json", Root: "workspace/member", Kind: "npm"}
			projects := []declarations.Project{p, target}
			records := projectRecords(projects)
			records[0].Complete = tc.complete
			decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 2, ParsedManifests: 2}, Projects: projects}
			if tc.diagnose {
				decls.Diagnostics = []declarations.Diagnostic{{Path: p.ID, Code: "partial", Message: "bounded parser observation"}}
			}
			collector := New("directory", "")
			collector.Add(discovery.File{Path: p.ID, Size: 1})
			collector.Add(discovery.File{Path: target.ID, Size: 1})
			r, err := collector.Finish(decls, nil, records)
			if err != nil {
				t.Fatal(err)
			}
			if r.Projects.Count != 2 || r.ProjectRoots.Count != 2 {
				t.Fatalf("parsed project totals changed: %+v", r.Projects)
			}
			if r.WorkspaceMembership.Completeness != "lower_bound" {
				t.Fatalf("workspace relationship was not qualified: %+v", r.WorkspaceMembership)
			}
			if err := ValidateReport(r); err != nil {
				t.Fatalf("ValidateReport: %v", err)
			}
		})
	}
}

func TestFinishAcceptsMatchingGitSourceAndConfinedReferences(t *testing.T) {
	const tree = "0123456789abcdef0123456789abcdef01234567"
	project := declarations.Project{ID: "repo/package.json", Root: "repo", Kind: "npm", References: []declarations.Reference{{Kind: "npm-workspace-member", Target: "repo/member/package.json", TargetStatus: "present", State: "declared"}}}
	member := declarations.Project{ID: "repo/member/package.json", Root: "repo/member", Kind: "npm"}
	projects := []declarations.Project{project, member}
	collector := New("git", tree)
	collector.Add(discovery.File{Path: project.ID, Size: 2})
	collector.Add(discovery.File{Path: member.ID, Size: 3})
	decls := &declarations.Report{Source: "git", Tree: tree, Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: 2, ParsedManifests: 2}, Projects: projects}
	locks := &lockfiles.Report{Source: "git", Tree: tree, Status: "complete", Contexts: []lockfiles.Context{{ProjectID: project.ID, Ecosystem: "npm", AssociationState: "observed"}, {ProjectID: member.ID, Ecosystem: "npm", AssociationState: "missing"}}}
	r, err := collector.Finish(decls, locks, projectRecords(projects))
	if err != nil {
		t.Fatal(err)
	}
	if r.Source.Mode != "git" || r.Source.Tree != tree || r.WorkspaceMembership.Count != 1 {
		t.Fatalf("Git source/reference facts were lost: %+v", r)
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func TestManifestGroupsNormalizeVariableFilenameFamilies(t *testing.T) {
	c := New("directory", "")
	paths := []string{"dotnet/One.csproj", "dotnet/Two.fsproj", "dotnet/Three.vbproj", "hs/first.cabal", "ruby/first.gemspec"}
	for _, p := range paths {
		c.Add(discovery.File{Path: p, Size: 1})
	}
	decls := &declarations.Report{Source: "directory", Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: int64(len(paths)), ParsedManifests: len(paths)}}
	r, err := c.Finish(decls, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ManifestCandidates) != len(paths) {
		t.Fatalf("unexpected normalized groups: %+v", r.ManifestCandidates)
	}
	for _, row := range r.ManifestCandidates {
		if !strings.HasPrefix(row.Filename, "*.") {
			t.Fatalf("variable filename was not normalized: %+v", row)
		}
	}
	if err := ValidateReport(r); err != nil {
		t.Fatalf("ValidateReport: %v", err)
	}
}

func FuzzFinishProducesValidNativeReports(f *testing.F) {
	f.Add([]byte{0, 0, 0})
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add([]byte{255, 0, 255, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		mode, tree := "directory", ""
		if len(data) > 0 && data[0]&1 != 0 {
			mode, tree = "git", "0123456789abcdef0123456789abcdef01234567"
		}
		count := 1
		if len(data) > 1 {
			count += int(data[1] % 12)
		}
		projects := make([]declarations.Project, 0, count)
		records := make([]declarations.ProjectRecord, 0, count)
		collector := New(mode, tree)
		for i := 0; i < count; i++ {
			kind, filename := "npm", "package.json"
			if len(data) > i+2 {
				switch data[i+2] % 3 {
				case 1:
					kind, filename = "python", "pyproject.toml"
				case 2:
					kind, filename = "dotnet", fmt.Sprintf("p%d.csproj", i)
				}
			}
			root := fmt.Sprintf("p%d", i)
			p := declarations.Project{ID: root + "/" + filename, Root: root, Kind: kind}
			projects = append(projects, p)
			records = append(records, declarations.ProjectRecord{Project: p, Parsed: true, Complete: len(data) <= i+14 || data[i+14]&1 == 0})
			size := int64(i + 1)
			if len(data) > i+20 && data[i+20] == 255 {
				size = math.MaxInt64
			} else if len(data) > i+20 {
				size += int64(data[i+20])
			}
			collector.Add(discovery.File{Path: p.ID, Size: size})
		}
		coverage := declarations.Coverage{ManifestCandidates: int64(count), ParsedManifests: count}
		if len(data) > 1 && data[1]&0x80 != 0 {
			coverage.ManifestCandidates++
			collector.Add(discovery.File{Path: "unparsed/package.json", Size: 1})
		}
		decls := &declarations.Report{Source: mode, Tree: tree, Status: "complete", Coverage: coverage, Projects: projects}
		if len(data) > 0 && data[0]&0x40 != 0 {
			decls.Diagnostics = []declarations.Diagnostic{{Path: ".", Code: "bounded", Message: "synthetic bounded parse evidence"}}
		}
		locks := &lockfiles.Report{Source: mode, Tree: tree, Status: "complete", Contexts: []lockfiles.Context{}}
		r, err := collector.Finish(decls, locks, records)
		if err != nil {
			t.Fatalf("valid generated inputs failed Finish: %v", err)
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("Finish emitted invalid report: %v", err)
		}
	})
}

func parseAssessmentDeclarations(t *testing.T, contents map[string]string) (*declarations.Report, []declarations.ProjectRecord, []discovery.File) {
	t.Helper()
	paths := make([]string, 0, len(contents))
	for filename := range contents {
		paths = append(paths, filename)
	}
	slices.Sort(paths)
	collector := declarations.New("directory", "", 0)
	collector.EnableProjectRecords()
	selected := make([]discovery.File, 0, len(paths))
	for _, filename := range paths {
		content := []byte(contents[filename])
		copyForReader := slices.Clone(content)
		candidate := &declarations.Candidate{Path: filename, Size: int64(len(content)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
			return slices.Clone(copyForReader), int64(len(copyForReader)), nil
		}}
		if !declarations.IsManifest(filename) {
			candidate = nil
		}
		collector.Add(filename, candidate)
		selected = append(selected, discovery.File{Path: filename, Size: int64(len(content))})
	}
	parsed, err := collector.Finish(context.Background())
	if err != nil {
		t.Fatalf("declaration collector: %v", err)
	}
	return parsed, collector.ProjectRecords(), selected
}

func finishParsedAssessment(t *testing.T, decls *declarations.Report, records []declarations.ProjectRecord, files []discovery.File) *Report {
	t.Helper()
	collector := New(decls.Source, decls.Tree)
	for _, file := range files {
		collector.Add(file)
	}
	report, err := collector.Finish(decls, nil, records)
	if err != nil {
		t.Fatalf("assessment collector: %v", err)
	}
	if err := ValidateReport(report); err != nil {
		t.Fatalf("assessment output failed validation: %v", err)
	}
	return report
}

func FuzzValidateReportRejectsMutations(f *testing.F) {
	f.Add(uint8(0), uint8(1))
	f.Add(uint8(5), uint8(3))
	f.Add(uint8(9), uint8(2))
	f.Fuzz(func(t *testing.T, operation, amount uint8) {
		r := nativeFixture(t, false)
		delta := int64(amount%5) + 1
		switch operation % 8 {
		case 0:
			r.Inventory.Files.Count -= delta
		case 1:
			r.Projects.Count += delta
		case 2:
			r.CandidateEvidence[0].Kind = "unknown"
		case 3:
			r.LockfilesOverall.Unknown.Count += delta
		case 4:
			r.WorkspaceEvidence[0].TargetPath = "../escape/package.json"
		case 5:
			r.Inventory.Bytes.Completeness = "complete"
			r.Inventory.Bytes.Reasons = []string{"fake"}
		case 6:
			r.Source.Consistency = "wrong"
		case 7:
			r.OmittedWorkspaceEvidence = math.MaxInt64
		}
		if err := ValidateReport(r); err == nil {
			t.Fatalf("mutation %d with delta %d was accepted", operation%8, delta)
		}
	})
}

func nativeFixture(t testing.TB, git bool) *Report {
	t.Helper()
	mode, tree := "directory", ""
	if git {
		mode, tree = "git", "0123456789abcdef0123456789abcdef01234567"
	}
	projects := []declarations.Project{
		{ID: "app/package.json", Root: "app", Kind: "npm", References: []declarations.Reference{{Kind: "npm-workspace-member", Target: "app/member/package.json", TargetStatus: "present", State: "declared"}, {Kind: "module", Target: "app/member/package.json", TargetStatus: "present", State: "declared"}}},
		{ID: "app/member/package.json", Root: "app/member", Kind: "npm"},
		{ID: "lib/pyproject.toml", Root: "lib", Kind: "python"},
	}
	collector := New(mode, tree)
	for _, p := range projects {
		collector.Add(discovery.File{Path: p.ID, Size: 4})
	}
	decls := &declarations.Report{Source: mode, Tree: tree, Status: "complete", Coverage: declarations.Coverage{ManifestCandidates: int64(len(projects)), ParsedManifests: len(projects)}, Projects: projects}
	locks := &lockfiles.Report{Source: mode, Tree: tree, Status: "complete", Contexts: []lockfiles.Context{{ProjectID: projects[0].ID, Ecosystem: "npm", AssociationState: "observed"}, {ProjectID: projects[1].ID, Ecosystem: "npm", AssociationState: "missing"}}}
	r, err := collector.Finish(decls, locks, projectRecords(projects))
	if err != nil {
		t.Fatalf("native fixture: %v", err)
	}
	return r
}
