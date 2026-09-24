package reportdiff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/packageevidence"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/projects"
	"github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/pkg/rules"
)

func emptyProfile() profile.Report {
	return profile.Report{SchemaVersion: "1.0.0", Root: "/unopened/source", Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
}

func declarationProfile(t *testing.T, input map[string]string) profile.Report {
	t.Helper()
	p := emptyProfile()
	p.SchemaVersion = "1.4.0"
	c := declarations.New("directory", "", 0)
	for name, content := range input {
		content := content
		c.Add(name, &declarations.Candidate{Path: name, Size: int64(len(content)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
			return []byte(content), int64(len(content)), nil
		}})
	}
	var err error
	p.Declarations, err = c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func loaded(t *testing.T, p profile.Report) *Snapshot {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("load failed: %v\n%s", err, data)
	}
	return snapshot
}

func comparison(t *testing.T, a, b profile.Report) *Report {
	t.Helper()
	r, err := Compare(loaded(t, a), loaded(t, b))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func moduleNamed(t *testing.T, report *Report, name string) Module {
	t.Helper()
	for _, module := range report.Modules {
		if module.Name == name {
			return module
		}
	}
	t.Fatalf("missing module %s", name)
	return Module{}
}

func TestDeclarationsCompareIdenticalAndChangedRequirements(t *testing.T) {
	base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	same := moduleNamed(t, comparison(t, base, base), "declarations")
	if same.Status != "unchanged" || same.Compatibility != "compatible" || same.Counts.Unchanged != 1 || len(same.Changes) != 0 {
		t.Fatalf("identical: %+v", same)
	}
	head := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.25.0\n"})
	changed := moduleNamed(t, comparison(t, base, head), "declarations")
	if changed.Status != "changed" || changed.Compatibility != "compatible" || len(changed.Changes) != 1 || changed.Changes[0].ID != "go.mod" {
		t.Fatalf("changed: %+v", changed)
	}
	fields := []string{}
	for _, field := range changed.Changes[0].Fields {
		fields = append(fields, field.Field)
	}
	if !reflect.DeepEqual(fields, []string{"requirements"}) {
		t.Fatalf("changed fields: %v", fields)
	}
	if changed.Changes[0].BaseEvidence[0] != "go.mod" {
		t.Fatal("missing source evidence")
	}
}

func TestDeclarationsAddRemoveAndMoveWithoutRenameInference(t *testing.T) {
	base := declarationProfile(t, map[string]string{"old/go.mod": "module example.org/lib\ngo 1.24.0\n"})
	head := declarationProfile(t, map[string]string{"new/go.mod": "module example.org/lib\ngo 1.24.0\n"})
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Counts.Added != 1 || m.Counts.Removed != 1 || m.Counts.Changed != 0 {
		t.Fatalf("move: %+v", m)
	}
	if m.Changes[0].ID != "new/go.mod" || m.Changes[0].Status != "added" || m.Changes[1].ID != "old/go.mod" || m.Changes[1].Status != "removed" {
		t.Fatalf("identities: %+v", m.Changes)
	}
}

func TestPartialAbsenceIsUnavailableButExistingChangesRemainVisible(t *testing.T) {
	base := declarationProfile(t, map[string]string{"one/go.mod": "module example.org/one\ngo 1.24.0\n", "two/go.mod": "module example.org/two\ngo 1.24.0\n"})
	head := declarationProfile(t, map[string]string{"one/go.mod": "module example.org/one\ngo 1.25.0\n"})
	head.Declarations.Status = "partial"
	head.Declarations.Coverage.OmittedFiles = 1
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Compatibility != "observed_only" || m.Counts.Removed != 0 || m.Counts.Unavailable != 1 || m.Counts.Changed != 1 {
		t.Fatalf("partial head: %+v", m)
	}
	for _, change := range m.Changes {
		if change.ID == "two/go.mod" && (change.Status != "unavailable" || change.Reason != "absence_from_head_is_not_proven") {
			t.Fatalf("deletion claim: %+v", change)
		}
	}
	m = moduleNamed(t, comparison(t, head, base), "declarations")
	if m.Counts.Added != 0 || m.Counts.Unavailable != 1 || m.Counts.Changed != 1 {
		t.Fatalf("partial base: %+v", m)
	}
}

func TestPerModulePolicyMismatchDoesNotDiscardOtherModules(t *testing.T) {
	base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	head := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.25.0\n"})
	base.Discovery = discovery.New("directory", "", 100000).Finish()
	head.Discovery = discovery.New("directory", "", 100000).Finish()
	head.Declarations.Limits.ManifestBytes = 128
	r := comparison(t, base, head)
	m := moduleNamed(t, r, "declarations")
	if m.Compatibility != "incomparable" || len(m.Changes) != 0 || len(m.Metadata) == 0 {
		t.Fatalf("policy change: %+v", m)
	}
	if other := moduleNamed(t, r, "discovery"); other.Compatibility != "compatible" || other.Status != "unchanged" {
		t.Fatalf("unrelated module: %+v", other)
	}
}

func TestDeclarationScopeLimitsAndSourceModesAreCompatibilityGates(t *testing.T) {
	for name, mutate := range map[string]func(*declarations.Report){
		"source":     func(r *declarations.Report) { r.Source = "git"; r.Tree = strings.Repeat("a", 40) },
		"limit":      func(r *declarations.Report) { r.Limits.ManifestBytes = 100 },
		"ecosystems": func(r *declarations.Report) { r.SupportedEcosystems = []string{"go"} },
		"work":       func(r *declarations.Report) { r.Limits.ResolutionWork["npm"] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
			head := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
			mutate(head.Declarations)
			if m := moduleNamed(t, comparison(t, base, head), "declarations"); m.Status != "incomparable" {
				t.Fatalf("gate: %+v", m)
			}
		})
	}
}

func TestSelectedTreeChangesAndRootLabelsDoNotImplyMismatchedScope(t *testing.T) {
	base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	head := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.25.0\n"})
	base.Declarations.Source, head.Declarations.Source = "git", "git"
	base.Declarations.Tree, head.Declarations.Tree = strings.Repeat("a", 40), strings.Repeat("b", 40)
	base.Root, head.Root = "/base-checkout", "/other-checkout"
	r := comparison(t, base, head)
	if m := moduleNamed(t, r, "declarations"); m.Compatibility != "compatible" || m.Status != "changed" {
		t.Fatalf("tree change: %+v", m)
	}
	if r.Base.Role != "base" || r.Head.Role != "head" || r.SourcePairing == "" {
		t.Fatal("caller roles missing")
	}
}

func TestDuplicateStableIdentityIsIncomparable(t *testing.T) {
	base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	head := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	head.Declarations.Projects = append(head.Declarations.Projects, head.Declarations.Projects[0])
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Status != "incomparable" || len(m.Changes) != 0 {
		t.Fatalf("duplicate IDs: %+v", m)
	}
}

func TestMissingModuleIsUnavailableNotRemoved(t *testing.T) {
	base := declarationProfile(t, map[string]string{"go.mod": "module example.org/app\ngo 1.24.0\n"})
	head := emptyProfile()
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Status != "unavailable" || m.Counts.Removed != 0 {
		t.Fatalf("missing module: %+v", m)
	}
}

func TestLanguagePercentagesRetainTheirDenominatorAndLimits(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	base.Summary = profile.Summary{ScannedFiles: 1, AnalyzedFiles: 1, LanguageBytes: 100}
	head.Summary = profile.Summary{ScannedFiles: 2, AnalyzedFiles: 2, LanguageBytes: 200}
	base.Languages = []profile.Language{{Name: "Go", Bytes: 100, FileCount: 1, Percentage: 100}}
	head.Languages = []profile.Language{{Name: "Go", Bytes: 100, FileCount: 1, Percentage: 50}, {Name: "Python", Bytes: 100, FileCount: 1, Percentage: 50}}
	m := moduleNamed(t, comparison(t, base, head), "languages")
	if m.Compatibility != "observed_only" || m.Counts.Changed != 1 || m.Counts.Added != 1 || m.Counts.Unavailable != 0 {
		t.Fatalf("languages: %+v", m)
	}
	foundAdded := false
	for _, change := range m.Changes {
		if change.ID == "Python" {
			foundAdded = change.Status == "added" && change.Reason == ""
			continue
		}
		if change.ID != "Go" {
			continue
		}
		if change.Fields[0].Field != "denominator_language_bytes" || string(change.Fields[0].Base.Data) != "100" || string(change.Fields[0].Head.Data) != "200" {
			t.Fatalf("denominator: %+v", change)
		}
	}
	if !foundAdded {
		t.Fatalf("new language was not reported as added: %+v", m.Changes)
	}

	t.Run("warnings preserve uncertain absence", func(t *testing.T) {
		partial := head
		partial.Languages = partial.Languages[:1]
		partial.Warnings = []profile.Warning{{Path: "lost.py", Code: "file_read_error", Message: "fixture"}}
		got := moduleNamed(t, comparison(t, head, partial), "languages")
		if got.Counts.Removed != 0 || got.Counts.Unavailable != 1 {
			t.Fatalf("partial language population proved removal: %+v", got)
		}
		got = moduleNamed(t, comparison(t, partial, head), "languages")
		if got.Counts.Added != 0 || got.Counts.Unavailable != 1 {
			t.Fatalf("partial language population proved addition: %+v", got)
		}
	})

	t.Run("zero legacy population remains ambiguous", func(t *testing.T) {
		got := moduleNamed(t, comparison(t, emptyProfile(), base), "languages")
		if got.Counts.Added != 0 || got.Counts.Unavailable != 1 {
			t.Fatalf("unexecuted empty population proved addition: %+v", got)
		}
	})

	t.Run("inspected language-free population proves absence", func(t *testing.T) {
		languageFree := emptyProfile()
		languageFree.Summary = profile.Summary{ScannedFiles: 1, AnalyzedFiles: 1}
		got := moduleNamed(t, comparison(t, languageFree, base), "languages")
		if got.Counts.Added != 1 || got.Counts.Unavailable != 0 {
			t.Fatalf("complete empty language population lost addition: %+v", got)
		}
	})
}

func TestDeclaredWorkspaceRelationshipsAndInterfacesAreCompared(t *testing.T) {
	base := declarationProfile(t, map[string]string{"package.json": `{"name":"root","workspaces":["packages/a"],"scripts":{"test":"old command"}}`, "packages/a/package.json": `{"name":"a"}`, "packages/b/package.json": `{"name":"b"}`})
	head := declarationProfile(t, map[string]string{"package.json": `{"name":"root","workspaces":["packages/a","packages/b"],"scripts":{"test":"new command","build":"compile"}}`, "packages/a/package.json": `{"name":"a"}`, "packages/b/package.json": `{"name":"b"}`})
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Counts.Changed != 1 || m.Counts.Unchanged != 2 {
		t.Fatalf("workspace: %+v", m)
	}
	fields := []string{}
	for _, f := range m.Changes[0].Fields {
		fields = append(fields, f.Field)
	}
	// Developer-task scripts ("test", "build") are not modeled as interfaces,
	// so adding them does not appear as an interface change. Only references
	// (workspace membership) and requirements change.
	if !reflect.DeepEqual(fields, []string{"references", "requirements"}) {
		t.Fatalf("workspace fields: %v", fields)
	}
	data, _ := json.Marshal(m)
	if strings.Contains(string(data), "command") || strings.Contains(string(data), "compile") {
		t.Fatal("raw script content entered comparison")
	}
}

func TestLegacyProjectRelationshipsRemainObservedOnly(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.2.0", "1.2.0"
	build := func(target string) *projects.Report {
		c := projects.New("directory", "")
		name := "app/App.csproj"
		c.Add(name, 1, "configuration", projects.Parse(name, []byte(`<Project><ItemGroup><ProjectReference Include="`+target+`"/></ItemGroup></Project>`)))
		c.Add("java/pom.xml", 1, "configuration", projects.Parse("java/pom.xml", []byte(`<project><artifactId>app</artifactId></project>`)))
		return c.Finish()
	}
	base.Projects, head.Projects = build("../old/Old.csproj"), build("../new/New.csproj")
	m := moduleNamed(t, comparison(t, base, head), "projects")
	if m.Compatibility != "observed_only" || m.Counts.Changed != 1 || m.Counts.Unchanged < 1 {
		t.Fatalf("project observations: %+v", m)
	}
}

func TestRegistryConfigurationGateAndChanges(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.3.0", "1.3.0"
	build := func(origin string) *registries.Report {
		c, err := registries.New(registries.Source{Mode: "directory"}, registries.Options{})
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("registry=" + origin + "\n")
		err = c.Add(registries.Candidate{Path: ".npmrc", Size: int64(len(content)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }})
		if err != nil {
			t.Fatal(err)
		}
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	base.Registries, head.Registries = build("https://one.example"), build("https://two.example")
	m := moduleNamed(t, comparison(t, base, head), "registries")
	if m.Compatibility != "compatible" || m.Counts.Changed != 1 {
		t.Fatalf("registry: %+v", m)
	}
	head.Registries.Scope.MaxFileBytes = 128
	if m := moduleNamed(t, comparison(t, base, head), "registries"); m.Status != "incomparable" {
		t.Fatalf("version: %+v", m)
	}
}

func TestComparisonSortsSetsAndDoesNotMutateInputs(t *testing.T) {
	base := declarationProfile(t, map[string]string{"package.json": `{"name":"app","engines":{"node":">=22","npm":"11"},"scripts":{"test":"x","build":"y"}}`})
	head := declarationProfile(t, map[string]string{"package.json": `{"scripts":{"build":"y","test":"x"},"engines":{"npm":"11","node":">=22"},"name":"app"}`})
	project := &head.Declarations.Projects[0]
	for i, j := 0, len(project.Requirements)-1; i < j; i, j = i+1, j-1 {
		project.Requirements[i], project.Requirements[j] = project.Requirements[j], project.Requirements[i]
	}
	a, b := loaded(t, base), loaded(t, head)
	before, _ := json.Marshal(b.profile)
	r, err := Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if m := moduleNamed(t, r, "declarations"); m.Status != "unchanged" {
		t.Fatalf("ordering: %+v", m)
	}
	after, _ := json.Marshal(b.profile)
	if !bytes.Equal(before, after) {
		t.Fatal("comparison mutated snapshot")
	}
	second, _ := Compare(a, b)
	one, _ := json.Marshal(r)
	two, _ := json.Marshal(second)
	if !bytes.Equal(one, two) {
		t.Fatal("comparison is not deterministic")
	}
}

func TestBoundedComparisonRetainsCounts(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	for i := 0; i < MaxChanges+10; i++ {
		head.Languages = append(head.Languages, profile.Language{Name: fmt.Sprintf("language-%05d", i), Bytes: 1, Percentage: 1, FileCount: 1})
	}
	r := comparison(t, base, head)
	m := moduleNamed(t, r, "languages")
	if r.Status != "partial" || m.Counts.Unavailable != MaxChanges+10 || m.Counts.OmittedChanges != 10 || len(m.Changes) != MaxChanges {
		t.Fatalf("cap: %+v", m.Counts)
	}
	data, _ := json.Marshal(r)
	if len(data) > MaxOutputBytes {
		t.Fatalf("output exceeded cap: %d", len(data))
	}
}

func TestImportedPackageComparisonDoesNotInferRemovalFromUnknownScan(t *testing.T) {
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.3.0", "1.3.0"
	base.Projects, head.Projects = projects.New("directory", "").Finish(), projects.New("directory", "").Finish()
	base.PackageEvidence, err = packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
	if err != nil {
		t.Fatal(err)
	}
	head.PackageEvidence, err = packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
	if err != nil {
		t.Fatal(err)
	}
	head.PackageEvidence.Packages = head.PackageEvidence.Packages[1:]
	m := moduleNamed(t, comparison(t, base, head), "package_evidence")
	if m.Compatibility != "observed_only" || m.Counts.Unavailable != 1 || m.Counts.Removed != 0 {
		t.Fatalf("provider completeness: %+v", m)
	}
	head.PackageEvidence.Provider.Version = "99"
	if m := moduleNamed(t, comparison(t, base, head), "package_evidence"); m.Status != "incomparable" {
		t.Fatalf("provider version ignored: %+v", m)
	}
}

func TestPythonWorkspaceComparisonRetainsDeclaredChanges(t *testing.T) {
	base := declarationProfile(t, map[string]string{
		"pyproject.toml":              "[project]\nname='root'\nversion='1.0.0'\nrequires-python='>=3.12'\n[tool.uv.workspace]\nmembers=['packages/*']\n",
		"packages/lib/pyproject.toml": "[project]\nname='lib'\nversion='1.0.0'\n",
	})
	head := declarationProfile(t, map[string]string{
		"pyproject.toml":              "[project]\nname='root'\nversion='1.0.0'\nrequires-python='>=3.13'\n[project.scripts]\nexample='root.cli:main'\n[tool.uv.workspace]\nmembers=['packages/*']\n",
		"packages/lib/pyproject.toml": "[project]\nname='lib'\nversion='1.0.0'\n",
	})
	m := moduleNamed(t, comparison(t, base, head), "declarations")
	if m.Counts.Changed != 1 || m.Counts.Unchanged != 1 {
		t.Fatalf("Python observations: %+v", m)
	}
	if m.Changes[0].ID != "pyproject.toml" {
		t.Fatal("workspace identity changed")
	}
}

func TestEvidenceSortingAndOmissionsDoNotMutateSnapshot(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	evidence := []string{"z.txt", "a.txt"}
	for i := 0; i < MaxEvidencePaths+3; i++ {
		evidence = append(evidence, fmt.Sprintf("source-%d.txt", i))
	}
	head.Frameworks = []profile.Finding{{Kind: "framework", Name: "Example", Root: ".", Detector: "test", Evidence: evidence}}
	a, b := loaded(t, base), loaded(t, head)
	before, _ := json.Marshal(b.profile)
	r, err := Compare(a, b)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(b.profile)
	if !bytes.Equal(before, after) {
		t.Fatal("evidence sorting mutated the original report")
	}
	m := moduleNamed(t, r, "frameworks")
	if m.Changes[0].HeadEvidenceOmitted != len(evidence)-MaxEvidencePaths {
		t.Fatal("evidence truncation was not disclosed")
	}
}

func TestLargeFieldValuesRetainHashesInsteadOfPayloads(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	evidence := []string{}
	for i := 0; i < 100; i++ {
		evidence = append(evidence, fmt.Sprintf("directory/with/long/path/file-%d.txt", i))
	}
	head.Layouts = []profile.Finding{{Kind: "layout", Name: "Example", Root: ".", Detector: "test", Evidence: evidence}}
	m := moduleNamed(t, comparison(t, base, head), "layouts")
	found := false
	for _, field := range m.Changes[0].Fields {
		if field.Field == "evidence" {
			found = field.Head.Omitted && len(field.Head.Data) == 0 && field.Head.SHA256 != "" && field.Head.Bytes > MaxFieldValueBytes
		}
	}
	if !found {
		t.Fatal("large evidence array was copied into the diff")
	}
}

func TestMissingDiscoveryProvenanceWithholdsAbsenceClaims(t *testing.T) {
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.3.0", "1.3.0"
	c := discovery.New("directory", "", 100000)
	c.Add(discovery.File{Path: "go.mod", Size: 30})
	base.Discovery, head.Discovery = c.Finish(), discovery.New("directory", "", 100000).Finish()
	base.Discovery.RuleVersion, head.Discovery.RuleVersion = "", ""
	m := moduleNamed(t, comparison(t, base, head), "discovery")
	if m.Compatibility != "observed_only" || m.Counts.Removed != 0 || m.Counts.Unavailable == 0 {
		t.Fatalf("missing provenance: %+v", m)
	}
}

func metricProfile(a, b int64) profile.Report {
	p := emptyProfile()
	p.SchemaVersion = "1.1.0"
	count := func(n int64) *profile.Counts { return &profile.Counts{Files: 1, Bytes: n, Lines: n, Code: n} }
	files := []profile.FileMetrics{{Path: "one.go", Language: "Go", Grammar: "Go", Status: "counted", Counts: count(a)}, {Path: "two.go", Language: "Go", Grammar: "Go", Status: "counted", Counts: count(b)}}
	total := profile.Counts{Files: 2, Bytes: 3, Lines: 3, Code: 3}
	p.Metrics = &profile.MetricsReport{Engine: "scc", EngineVersion: "4.1.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: 16777216, Totals: total, Languages: []profile.LanguageMetrics{{Language: "Go", Grammar: "Go", Counts: total}}, Directories: []profile.DirectoryMetrics{{Path: ".", Counts: total}}, Skipped: []profile.MetricSkip{}, Files: &files}
	return p
}

func TestPerFileMetricsChangesRemainVisibleWithIdenticalAggregates(t *testing.T) {
	base, head := metricProfile(1, 2), metricProfile(2, 1)
	r := comparison(t, base, head)
	if m := moduleNamed(t, r, "metrics"); m.Status != "unchanged" || !strings.Contains(m.Scope, "metrics_files") {
		t.Fatalf("aggregate scope: %+v", m)
	}
	if m := moduleNamed(t, r, "metrics_files"); m.Counts.Changed != 2 || m.Status != "changed" {
		t.Fatalf("file metrics: %+v", m)
	}
	head.Metrics.Files = nil
	if m := moduleNamed(t, comparison(t, base, head), "metrics_files"); m.Status != "unavailable" || m.Counts.Removed != 0 {
		t.Fatalf("omitted file metrics: %+v", m)
	}
	head = metricProfile(1, 2)
	head.Metrics.EngineVersion = "4.2.0"
	if m := moduleNamed(t, comparison(t, base, head), "metrics"); m.Status != "incomparable" {
		t.Fatalf("engine version: %+v", m)
	}
}

func TestImportedFileObservationChangesAreCompared(t *testing.T) {
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.3.0", "1.3.0"
	base.Projects, head.Projects = projects.New("directory", "").Finish(), projects.New("directory", "").Finish()
	base.PackageEvidence, err = packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
	if err != nil {
		t.Fatal(err)
	}
	head.PackageEvidence, err = packageevidence.Import(context.Background(), bytes.NewReader(data), packageevidence.Options{})
	if err != nil {
		t.Fatal(err)
	}
	head.PackageEvidence.Files[0].Location.OriginalSHA256 = strings.Repeat("f", 64)
	m := moduleNamed(t, comparison(t, base, head), "package_evidence")
	if m.Counts.Changed != 1 || !strings.HasPrefix(m.Changes[0].ID, "file:") {
		t.Fatalf("imported files: %+v", m)
	}
}

func TestRulesPolicyAndOperationalStates(t *testing.T) {
	build := func(extension string) *rules.Report {
		program, err := rules.Compile([]byte(`{"schema_version":"1.0.0","rules":[{"id":"source","match":{"extensions":["` + extension + `"]}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		c, err := rules.New(program, rules.Source{Kind: "directory"}, rules.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ObserveMetadata(rules.File{Path: "main.go", Size: 20}); err != nil {
			t.Fatal(err)
		}
		r, err := c.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	base, head := emptyProfile(), emptyProfile()
	base.SchemaVersion, head.SchemaVersion = "1.3.0", "1.3.0"
	base.Rules, head.Rules = build(".go"), build(".py")
	if m := moduleNamed(t, comparison(t, base, head), "rules"); m.Status != "incomparable" {
		t.Fatalf("ruleset mismatch: %+v", m)
	}
	for _, mutate := range []func(*rules.Report){func(r *rules.Report) { r.Status = "not-a-status" }, func(r *rules.Report) { r.Source.Kind = "not-a-source" }} {
		p := emptyProfile()
		p.SchemaVersion = "1.3.0"
		p.Rules = build(".go")
		mutate(p.Rules)
		data, _ := json.Marshal(p)
		if _, err := Load(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid rules operational state accepted")
		}
	}
}
