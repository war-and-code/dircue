package deployables

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/war-and-code/dircue/pkg/profile"
)

const contextTestProgram = "var builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Api>(\"api\");\n"

func TestAppHostProjectUsingAliasesAreScopedAndCommentSafe(t *testing.T) {
	for _, tc := range []struct {
		name, project string
		want          string
	}{
		{"distributed-application alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake.DistributedApplication" Alias="DistributedApplication" /></ItemGroup></Project>`, "aspire_global_alias"},
		{"projects alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake.Projects" Alias="Projects" /></ItemGroup></Project>`, "aspire_global_alias"},
		{"child distributed-application alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake"><Alias>DistributedApplication</Alias></Using></ItemGroup></Project>`, "aspire_global_alias"},
		{"child projects alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake"><Alias> Projects </Alias></Using></ItemGroup></Project>`, "aspire_global_alias"},
		{"unrelated child alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake"><Alias>Unrelated</Alias></Using></ItemGroup></Project>`, ""},
		{"unresolved alias attribute", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake" Alias="$(BindingName)" /></ItemGroup></Project>`, "aspire_alias_context_incomplete"},
		{"unresolved alias child", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake"><Alias>$(BindingName)</Alias></Using></ItemGroup></Project>`, "aspire_alias_context_incomplete"},
		{"unrelated alias", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><Using Include="Fake" Alias="Other" /></Project>`, ""},
		{"comment lookalike", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><!-- <Using Include="Fake" Alias="Projects" /> --></Project>`, ""},
		{"foreign lookalike", `<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><x:Using xmlns:x="urn:fake" Alias="Projects" /></ItemGroup></Project>`, ""},
		{"wrong SDK", `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><Using Include="Fake" Alias="Projects" /></ItemGroup></Project>`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := []Candidate{
				{Path: "src/AppHost/Launcher.csproj", Size: int64(len(tc.project)), Read: func(context.Context, int64) ([]byte, int64, error) {
					return []byte(tc.project), int64(len(tc.project)), nil
				}},
				{Path: "src/AppHost/Program.cs", Size: int64(len(contextTestProgram)), Read: func(context.Context, int64) ([]byte, int64, error) {
					return []byte(contextTestProgram), int64(len(contextTestProgram)), nil
				}},
			}
			issue, err := inspectAspireGlobalAliases(context.Background(), "src/AppHost", "src/AppHost/Program.cs", files, map[string]bool{"src/AppHost": true})
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if issue != nil {
				got = issue.code
			}
			if got != tc.want {
				t.Fatalf("issue code = %q, want %q (issue=%+v)", got, tc.want, issue)
			}
		})
	}
}

func TestAspireAliasSourceEncodingFailsClosedBeforeKeywordPrefilter(t *testing.T) {
	encoded := []byte{0xff, 0xfe}
	for _, r := range utf16.Encode([]rune("global using DistributedApplication = Fake;")) {
		encoded = append(encoded, byte(r), byte(r>>8))
	}
	for _, data := range [][]byte{encoded, append([]byte("global using DistributedApplication = Fake;"), 0)} {
		if _, err := inspectAspireAliasSource(aspireAliasSource{data: data, size: int64(len(data))}); err == nil {
			t.Fatalf("unsupported byte encoding passed prefilter: %q", data)
		}
	}
	if _, err := projectUsingAlias(encoded); err == nil {
		t.Fatal("UTF-16 project file was accepted as complete")
	}
}

func TestAspireBindingRangeParserHandlesLargeUsingContext(t *testing.T) {
	source := "var builder = DistributedApplication.CreateBuilder(args);\n" + strings.Repeat("using System;\n", 6000) + "builder.AddProject<Projects.Api>(\"api\");\n"
	defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
	if err != nil || !found || len(defs) != 1 || len(defs[0].References) != 1 {
		t.Fatalf("large ordinary using context changed binding: found=%t defs=%+v err=%v", found, defs, err)
	}
}

func FuzzAspireAliasContextIsDeterministicAndBounded(f *testing.F) {
	for _, seed := range []string{
		"global using Projects = Fake;",
		"// global using Projects = Fake;\nconst string s = \"global using Projects = Fake;\";",
		"using DistributedApplication = Fake;",
		"<Project Sdk=\"Aspire.AppHost.Sdk/9.0.0\"><ItemGroup><Using Include=\"Fake\" Alias=\"Projects\" /></ItemGroup></Project>",
		"<Project Sdk=\"Microsoft.NET.Sdk\"><ItemGroup><Using Alias=\"Projects\" /></ItemGroup></Project>",
		"<Project Sdk=\"Aspire.AppHost.Sdk/9.0.0\"><!-- <Using Alias=\"Projects\" /> --></Project>",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 16<<10 {
			t.Skip()
		}
		data := []byte(input)
		first, firstErr := inspectAspireAliasSource(aspireAliasSource{data: data, size: int64(len(data))})
		second, secondErr := inspectAspireAliasSource(aspireAliasSource{data: data, size: int64(len(data))})
		if first != second || (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("C# alias inspection is nondeterministic")
		}
		projectFirst, projectFirstErr := projectUsingAlias(data)
		projectSecond, projectSecondErr := projectUsingAlias(data)
		if projectFirst != projectSecond || (projectFirstErr == nil) != (projectSecondErr == nil) {
			t.Fatalf("project alias inspection is nondeterministic")
		}
	})
}

func TestObserveAliasRescanRequiresOriginalCandidateSize(t *testing.T) {
	program := []byte(contextTestProgram)
	project := []byte(`<Project Sdk="Aspire.AppHost.Sdk/9.0.0" />`)
	alias := []byte("global using DistributedApplication = Fake;\n")
	changed := append(append([]byte(nil), alias...), []byte("// appeared after selection\n")...)
	reads := 0
	files := []Candidate{
		{Path: "src/AppHost/AppHost.csproj", Size: int64(len(project)), Read: func(context.Context, int64) ([]byte, int64, error) { return project, int64(len(project)), nil }},
		{Path: "src/AppHost/GlobalUsings.cs", Size: int64(len(alias)), Read: func(context.Context, int64) ([]byte, int64, error) {
			reads++
			return changed, int64(len(changed)), nil
		}},
		{Path: "src/AppHost/Program.cs", Size: int64(len(program)), Read: func(context.Context, int64) ([]byte, int64, error) { return program, int64(len(program)), nil }},
	}
	report, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Definitions) != 0 || report.Omissions["aspire_alias_context_incomplete"] != 1 {
		t.Fatalf("changed supplementary candidate was trusted: %+v", report)
	}
	if report.Coverage.RetainedReferences != 0 {
		t.Fatalf("withheld references remain counted as retained: %d", report.Coverage.RetainedReferences)
	}
	if reads != 1 {
		t.Fatalf("expected one supplementary source read, got %d", reads)
	}
}

func TestObserveProjectAliasRescanRequiresCompleteReturnedBytes(t *testing.T) {
	program := []byte(contextTestProgram)
	project := []byte(`<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake" Alias="Projects" /></ItemGroup></Project>`)
	files := []Candidate{
		{Path: "src/AppHost/Launcher.csproj", Size: int64(len(project)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return project[:len(project)-1], int64(len(project)), nil
		}},
		{Path: "src/AppHost/Program.cs", Size: int64(len(program)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return program, int64(len(program)), nil
		}},
	}
	report, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Definitions) != 0 || report.Omissions["aspire_alias_context_incomplete"] != 1 {
		t.Fatalf("short project XML was trusted: %+v", report)
	}
}

func TestCollectorOwnProgramDoesNotConsumeAliasContextBudget(t *testing.T) {
	c := NewCollector(Options{})
	files := []struct {
		path string
		data []byte
	}{
		{"src/AppHost/Program.cs", []byte(contextTestProgram)},
		{"src/AppHost/AppHost.csproj", []byte(`<Project Sdk="Aspire.AppHost.Sdk/9.0.0" />`)},
	}
	for i := 0; i < maxAspireAliasFiles-1; i++ {
		files = append(files, struct {
			path string
			data []byte
		}{fmt.Sprintf("src/AppHost/Helper%03d.cs", i), []byte("class Helper {}")})
	}
	for _, file := range files {
		if _, err := c.Detect(context.Background(), profile.File{Path: file.path, Size: int64(len(file.data)), Content: file.data}); err != nil {
			t.Fatal(err)
		}
	}
	report := c.Finish()
	if len(report.Definitions) != 1 || report.Omissions["aspire_alias_context_limit"] != 0 {
		t.Fatalf("host Program.cs consumed helper context budget: defs=%d omissions=%v", len(report.Definitions), report.Omissions)
	}
}

func TestCollectorAliasDiagnosticsAreIndependentOfHostArrivalOrder(t *testing.T) {
	const hosts = maxAspireAliasFiles + 1
	program := []byte(contextTestProgram)
	alias := []byte("global using Projects = Fake;\n")
	run := func(reverse bool) []string {
		c := NewCollector(Options{})
		for offset := 0; offset < hosts; offset++ {
			i := offset
			if reverse {
				i = hosts - offset - 1
			}
			root := fmt.Sprintf("src/host%03d.apphost", i)
			for _, item := range []struct {
				path string
				data []byte
			}{{root + "/Program.cs", program}, {root + "/GlobalUsings.cs", alias}} {
				if _, err := c.Detect(context.Background(), profile.File{Path: item.path, Size: int64(len(item.data)), Content: item.data}); err != nil {
					t.Fatal(err)
				}
			}
		}
		report := c.Finish()
		if len(report.Definitions) != 0 || report.Omissions["aspire_global_alias"] != hosts {
			t.Fatalf("alias issue inventory was truncated: defs=%d omissions=%v", len(report.Definitions), report.Omissions)
		}
		result := make([]string, 0, len(report.Diagnostics))
		for _, diagnostic := range report.Diagnostics {
			result = append(result, diagnostic.Path+"\x00"+diagnostic.Code)
		}
		return result
	}
	if forward, reverse := run(false), run(true); strings.Join(forward, "\n") != strings.Join(reverse, "\n") {
		t.Fatalf("diagnostics depend on source arrival order:\nforward=%v\nreverse=%v", forward, reverse)
	}
}
