package projects

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func graphFixtureProject(id string, refs ...Reference) Project {
	return Project{ID: id, Root: "src", Kind: "dotnet", Evidence: []string{id}, References: refs}
}
func graphFixtureRef(source, target string) Reference {
	return Reference{Kind: "project-reference", Value: target, Target: target, Evidence: source, State: "resolved", TargetStatus: "present"}
}
func graphFixtureReport(projects ...Project) *Report {
	return &Report{Source: "directory", Status: "complete", Projects: projects}
}

func TestGraphCyclesComponentsAndDuplicateEdges(t *testing.T) {
	a, b, c, d, e, f := "src/A.csproj", "src/B.csproj", "src/C.fsproj", "src/D.vbproj", "src/E.csproj", "src/F.csproj"
	ab := graphFixtureRef(a, b)
	alternate := ab
	alternate.Value = "./B.csproj"
	conditional := graphFixtureRef(f, e)
	conditional.State = "conditional"
	conditional.Condition = "'$(OS)'=='Windows_NT'"
	input := graphFixtureReport(graphFixtureProject(f, conditional), graphFixtureProject(e), graphFixtureProject(d, graphFixtureRef(d, d)), graphFixtureProject(c, graphFixtureRef(c, a)), graphFixtureProject(b, graphFixtureRef(b, c)), graphFixtureProject(a, ab, ab, alternate))
	before, _ := json.Marshal(input)
	r := AnalyzeGraph(input)
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("graph analysis mutated its input")
	}
	if r.Status != "partial" || r.Coverage.Projects != 6 || r.Coverage.ReferenceObservations != 6 || r.Coverage.DuplicateObservations != 1 || r.Coverage.UniqueEdges != 4 || r.Coverage.IncludedObservations != 5 || r.Coverage.ConditionalEdges != 1 {
		t.Fatalf("coverage: %+v", r.Coverage)
	}
	if len(r.Components) != 4 {
		t.Fatalf("weak components: %+v", r.Components)
	}
	if len(r.Cycles) != 2 || !reflect.DeepEqual(r.Cycles[0].Nodes, []string{a, b, c}) || !reflect.DeepEqual(r.Cycles[1].Nodes, []string{d}) {
		t.Fatalf("cycles: %+v", r.Cycles)
	}
	for _, n := range r.Nodes {
		if n.ID == e || n.ID == f {
			if n.FanIn != 0 || n.FanOut != 0 || n.Cyclic {
				t.Fatalf("conditional edge affected graph: %+v", n)
			}
		} else if n.FanIn != 1 || n.FanOut != 1 || !n.Cyclic {
			t.Fatalf("duplicate inflated degree or lost cycle: %+v", n)
		}
	}
	if r.Nodes[0].Component != a || r.Nodes[2].Component != a {
		t.Fatalf("co-root projects were merged or wrong component: %+v", r.Nodes)
	}
}

func TestGraphResolutionAndCertaintyAreIndependent(t *testing.T) {
	a, b := "src/A.csproj", "src/B.csproj"
	cases := []struct{ value, target, state, targetStatus, condition, certainty, resolution string }{
		{"B.csproj", b, "resolved", "present", "", "unconditional", "in_scope"},
		{"B.csproj", b, "conditional", "present", "Debug", "conditional", "in_scope"},
		{"Absent.csproj", "src/Absent.csproj", "missing", "missing", "", "unconditional", "missing"},
		{"Absent.csproj", "src/Absent.csproj", "conditional", "missing", "Debug", "conditional", "missing"},
		{"../../outside.csproj", "", "unresolved", "unresolved", "", "unresolved", "external"},
		{`C:\private\Outside.csproj`, "", "unresolved", "unresolved", "", "unresolved", "external"},
		{"$(Generated)/B.csproj", "", "unresolved", "unresolved", "", "unresolved", "unresolved"},
		{"B.csproj", "../Outside.csproj", "resolved", "present", "", "unconditional", "external"},
		{"other.csproj", "src/other.csproj", "resolved", "present", "", "unconditional", "unrecognized_target"},
		{"B.csproj", b, "missing", "present", "", "unresolved", "in_scope"},
	}
	for _, tc := range cases {
		t.Run(tc.value+tc.state+tc.targetStatus, func(t *testing.T) {
			ref := Reference{Kind: "project-reference", Value: tc.value, Target: tc.target, State: tc.state, TargetStatus: tc.targetStatus, Condition: tc.condition, Evidence: a}
			r := AnalyzeGraph(graphFixtureReport(graphFixtureProject(a, ref), graphFixtureProject(b)))
			edge := r.Edges[0]
			if edge.Certainty != tc.certainty || edge.Resolution != tc.resolution {
				t.Fatalf("edge: %+v", edge)
			}
			wantIncluded := tc.certainty == "unconditional" && tc.resolution == "in_scope"
			if edge.Included != wantIncluded {
				t.Fatalf("inclusion: %+v", edge)
			}
			if !wantIncluded && (r.Coverage.UniqueEdges != 0 || r.Status != "partial") {
				t.Fatalf("omission hidden: %+v", r)
			}
		})
	}
}

func TestGraphOnlyMeasuresDefinedRelationType(t *testing.T) {
	a, b := "src/A.csproj", "src/B.csproj"
	imported := graphFixtureRef(b, a)
	imported.Kind = "import"
	solution := Project{ID: "App.sln", Kind: "solution", References: []Reference{{Kind: "solution-member", Target: a}}}
	maven := Project{ID: "pom.xml", Kind: "maven", References: []Reference{{Kind: "module", Target: "child/pom.xml"}}}
	r := AnalyzeGraph(graphFixtureReport(graphFixtureProject(a, graphFixtureRef(a, b)), graphFixtureProject(b, imported), solution, maven))
	if len(r.Cycles) != 0 || r.Coverage.UniqueEdges != 1 || r.Coverage.ExcludedReferenceKinds["import"] != 1 || r.Coverage.ExcludedProjectKinds["solution"] != 1 || r.Coverage.ExcludedProjectKinds["maven"] != 1 || r.Status != "complete" {
		t.Fatalf("unrelated edges changed graph semantics: %+v", r)
	}
	input := graphFixtureReport(graphFixtureProject(a), graphFixtureProject(b))
	input.Configurations = []Configuration{{Path: "Directory.Build.props", References: []Reference{graphFixtureRef("Directory.Build.props", b)}}}
	r = AnalyzeGraph(input)
	if r.Status != "partial" || len(r.Edges) != 0 || r.Coverage.ExcludedReferenceKinds["configuration:project-reference"] != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Path != "Directory.Build.props" {
		t.Fatalf("shared configuration was guessed or silently lost: %+v", r)
	}
}

func TestGraphCoverageAbsenceAndPartialInventory(t *testing.T) {
	skipped := graphFixtureReport()
	skipped.Status = "skipped"
	if r := AnalyzeGraph(skipped); r.Status != "skipped" || r.Reason != "project_inventory_skipped" {
		t.Fatalf("skipped input: %+v", r)
	}
	if r := AnalyzeGraph(nil); r.Status != "skipped" || r.Reason != "project_inventory_unavailable" {
		t.Fatalf("nil input: %+v", r)
	}
	if r := AnalyzeGraph(graphFixtureReport()); r.Status != "not_applicable" || r.Reason != "no_dotnet_projects" {
		t.Fatalf("empty scope: %+v", r)
	}
	if r := AnalyzeGraph(graphFixtureReport(Project{ID: "pom.xml", Kind: "maven"})); r.Status != "not_applicable" || r.Coverage.ExcludedProjectKinds["maven"] != 1 {
		t.Fatalf("unsupported scope: %+v", r)
	}
	input := graphFixtureReport(graphFixtureProject("A.csproj"))
	input.Source = "git"
	input.Tree = "0123456789012345678901234567890123456789"
	input.Status = "partial"
	input.OmittedFiles = 4
	input.Diagnostics = []Diagnostic{{Path: "bad.csproj", Code: "invalid-xml", Message: "Malformed XML"}}
	r := AnalyzeGraph(input)
	if r.Status != "partial" || r.InputStatus != "partial" || r.Tree != input.Tree || r.Source != "git" || r.Coverage.InputOmittedFiles != 4 || len(r.Diagnostics) != 1 {
		t.Fatalf("inventory provenance/coverage lost: %+v", r)
	}
}

func TestGraphAmbiguousAndInvalidProjectIDs(t *testing.T) {
	a, b := "src/A.csproj", "src/B.csproj"
	input := graphFixtureReport(graphFixtureProject(a, graphFixtureRef(a, b)), graphFixtureProject(b), graphFixtureProject(b, graphFixtureRef(b, a)), graphFixtureProject("../outside.csproj"))
	r := AnalyzeGraph(input)
	if r.Status != "partial" || len(r.Nodes) != 2 || !r.Nodes[1].Ambiguous || r.Coverage.UniqueEdges != 0 || r.Coverage.AmbiguousTargets != 1 || r.Coverage.ExcludedProjectKinds["invalid_manifest_path"] != 1 {
		t.Fatalf("ambiguous identities created definite edges: %+v", r)
	}
}

func TestGraphDeterministicUnderInventoryPermutation(t *testing.T) {
	input := graphChain(150, true)
	input.Projects = append(input.Projects, Project{ID: "other/pom.xml", Kind: "maven"})
	expected, _ := json.Marshal(AnalyzeGraph(input))
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 12; i++ {
		rng.Shuffle(len(input.Projects), func(i, j int) { input.Projects[i], input.Projects[j] = input.Projects[j], input.Projects[i] })
		for j := range input.Projects {
			rng.Shuffle(len(input.Projects[j].References), func(a, b int) {
				input.Projects[j].References[a], input.Projects[j].References[b] = input.Projects[j].References[b], input.Projects[j].References[a]
			})
		}
		got, _ := json.Marshal(AnalyzeGraph(input))
		if string(got) != string(expected) {
			t.Fatalf("iteration %d changed deterministic output", i)
		}
	}
}

func graphChain(size int, cycle bool) *Report {
	input := graphFixtureReport()
	for i := 0; i < size; i++ {
		id := fmt.Sprintf("src/p%05d/Project.csproj", i)
		p := graphFixtureProject(id)
		if i+1 < size {
			p.References = append(p.References, graphFixtureRef(id, fmt.Sprintf("src/p%05d/Project.csproj", i+1)))
		}
		input.Projects = append(input.Projects, p)
	}
	if cycle && size > 0 {
		p := &input.Projects[size-1]
		p.References = append(p.References, graphFixtureRef(p.ID, input.Projects[0].ID))
	}
	return input
}

func TestGraphLargeDeepChains(t *testing.T) {
	for _, size := range []int{2048, 20000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r := AnalyzeGraph(graphChain(size, false))
			if r.Status != "complete" || len(r.Nodes) != size || len(r.Components) != 1 || len(r.Cycles) != 0 || r.Coverage.UniqueEdges != size-1 {
				t.Fatalf("deep chain: nodes=%d components=%d cycles=%d edges=%d", len(r.Nodes), len(r.Components), len(r.Cycles), r.Coverage.UniqueEdges)
			}
		})
	}
	r := AnalyzeGraph(graphChain(2048, true))
	if len(r.Cycles) != 1 || len(r.Cycles[0].Nodes) != 2048 || r.Components[0].Edges != 2048 {
		t.Fatalf("large SCC lost: %+v", r.Coverage)
	}
}

func TestGraphComponentsAgainstReachabilityOracle(t *testing.T) {
	// Independent small-graph transitive closure checks the SCC and weak-component
	// implementation across graphs with isolates, self edges, and overlapping paths.
	rng := rand.New(rand.NewSource(13))
	for iteration := 0; iteration < 80; iteration++ {
		const n = 12
		input := graphFixtureReport()
		reach := make([][]bool, n)
		weak := make([][]bool, n)
		direct := make([][]bool, n)
		for i := 0; i < n; i++ {
			reach[i] = make([]bool, n)
			weak[i] = make([]bool, n)
			direct[i] = make([]bool, n)
			reach[i][i] = true
			weak[i][i] = true
			input.Projects = append(input.Projects, graphFixtureProject(fmt.Sprintf("P%02d.csproj", i)))
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if rng.Intn(8) == 0 {
					direct[i][j] = true
					reach[i][j] = true
					weak[i][j] = true
					weak[j][i] = true
					input.Projects[i].References = append(input.Projects[i].References, graphFixtureRef(input.Projects[i].ID, input.Projects[j].ID))
				}
			}
		}
		for k := 0; k < n; k++ {
			for i := 0; i < n; i++ {
				for j := 0; j < n; j++ {
					reach[i][j] = reach[i][j] || reach[i][k] && reach[k][j]
					weak[i][j] = weak[i][j] || weak[i][k] && weak[k][j]
				}
			}
		}
		got := AnalyzeGraph(input)
		for i := 0; i < n; i++ {
			cyclic := direct[i][i]
			in, out := 0, 0
			for j := 0; j < n; j++ {
				if direct[i][j] {
					out++
				}
				if direct[j][i] {
					in++
				}
				if i != j && reach[i][j] && reach[j][i] {
					cyclic = true
				}
				sameCycle := false
				for _, c := range got.Cycles {
					if slices.Contains(c.Nodes, input.Projects[i].ID) && slices.Contains(c.Nodes, input.Projects[j].ID) {
						sameCycle = true
					}
				}
				if i != j && sameCycle != (reach[i][j] && reach[j][i]) {
					t.Fatalf("SCC oracle mismatch iteration%d %d,%d", iteration, i, j)
				}
				if (got.Nodes[i].Component == got.Nodes[j].Component) != weak[i][j] {
					t.Fatal("weak component oracle mismatch")
				}
			}
			if got.Nodes[i].Cyclic != cyclic || got.Nodes[i].FanIn != in || got.Nodes[i].FanOut != out {
				t.Fatalf("node oracle mismatch: %+v", got.Nodes[i])
			}
		}
	}
}

func BenchmarkGraphLarge(b *testing.B) {
	for _, size := range []int{2048, 20000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			input := graphChain(size, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				AnalyzeGraph(input)
			}
		})
	}
}

func TestGraphFromParsedDotnetInventory(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "projects", "graph", "dotnet")
	collector := New("directory", "")
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		collector.Add(name, int64(len(content)), "configuration", Parse(name, content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := AnalyzeGraph(collector.Finish())
	if r.Status != "partial" || len(r.Nodes) != 4 || r.Coverage.UniqueEdges != 3 || r.Coverage.ConditionalEdges != 1 || r.Coverage.MissingTargets != 1 || len(r.Components) != 3 || len(r.Cycles) != 2 || r.Coverage.ExcludedProjectKinds["solution"] != 1 {
		t.Fatalf("parsed fixture: %+v", r)
	}
	if !reflect.DeepEqual(r.Cycles[0].Nodes, []string{"src/App/App.csproj", "src/Core/Core.csproj"}) || !reflect.DeepEqual(r.Cycles[1].Nodes, []string{"src/Optional/Optional.csproj"}) {
		t.Fatalf("fixture cycles: %+v", r.Cycles)
	}
}

func TestGraph2048ParsedProjectCycle(t *testing.T) {
	collector := New("directory", "")
	for i := 0; i < 2048; i++ {
		name := fmt.Sprintf("src/p%04d/P.csproj", i)
		content := []byte(fmt.Sprintf(`<Project><ItemGroup><ProjectReference Include="../p%04d/P.csproj"/></ItemGroup></Project>`, (i+1)%2048))
		collector.Add(name, int64(len(content)), "configuration", ParseDotnet(name, content))
	}
	r := AnalyzeGraph(collector.Finish())
	if r.Status != "complete" || r.Coverage.UniqueEdges != 2048 || len(r.Cycles) != 1 || len(r.Cycles[0].Nodes) != 2048 || len(r.Components) != 1 {
		t.Fatalf("parsed large cycle: %+v", r.Coverage)
	}
}
