package deployables

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
)

func TestAspireAdversarialCSharpLexingIsConservative(t *testing.T) {
	base := "var builder = DistributedApplication.CreateBuilder(args);\n"
	call := "builder.AddProject<Projects.Service>(\"service\");\n"
	cases := []struct {
		name     string
		source   string
		wantRefs int
		wantErr  bool
	}{
		{
			name:     "character brace literals do not alter nesting",
			source:   "var left = '{'; var right = '}';\n" + base + call,
			wantRefs: 1,
		},
		{
			name:     "escaped keyword is not treated as a using directive",
			source:   "var @using = nameof(Projects.Service);\n" + base + call,
			wantRefs: 1,
		},
		{
			name:   "unqualified generic AddProject use fails closed in linear scan",
			source: base + call + "static class Hostile { void M() { AddProject<AddProject<AddProject<T>>>(x); } }\n",
		},
		{
			name:     "UTF-8 BOM does not hide top-level declaration",
			source:   "\uFEFF" + base + call,
			wantRefs: 1,
		},
		{
			name:    "malformed directive prefix cannot close a conditional",
			source:  "#if FEATURE\nvar ignored = true;\n#endifx\n" + base + call,
			wantErr: true,
		},
		{
			name:    "malformed directive prefix is not treated as if",
			source:  "#ifx FEATURE\n" + base + call + "#endif\n",
			wantErr: true,
		},
		{
			name:   "DistributedApplication alias is not assumed to be Aspire",
			source: "using DistributedApplication = Custom.Fake;\n" + base + call,
		},
		{
			name:   "custom Projects alias is not assumed to be generated projects",
			source: "using Projects = Custom.Fake;\n" + base + call,
		},
		{
			name: "verbatim DistributedApplication type cannot shadow Aspire",
			source: base + call + `
static class @DistributedApplication { public static FakeBuilder CreateBuilder(string[] args) => new(); }
class FakeBuilder { public FakeBuilder AddProject<T>(string name) => this; }
`,
		},
		{
			name:   "verbatim Projects type cannot shadow generated project identities",
			source: base + call + "class @Projects { public class Service {} }\n",
		},
		{
			name:   "verbatim DistributedApplication alias cannot bind creation",
			source: "using @DistributedApplication = Custom.Fake;\n" + base + call,
		},
		{
			name:   "verbatim Projects alias cannot bind generated project identities",
			source: "using @Projects = Custom.Fake;\n" + base + call,
		},
		{
			name:   "verbatim DistributedApplication local cannot shadow type lookup",
			source: "var @DistributedApplication = Custom.Fake;\n" + base + call,
		},
		{
			name: "deconstruction local cannot shadow DistributedApplication type lookup",
			source: `var (DistributedApplication, ignored) = (new Fake(), 0);
var builder = DistributedApplication.CreateBuilder(args);
builder.AddProject<Projects.Service>("service");
`,
		},
		{
			name: "typed deconstruction local cannot shadow DistributedApplication type lookup",
			source: `(Fake DistributedApplication, int ignored) = (new Fake(), 0);
var builder = DistributedApplication.CreateBuilder(args);
builder.AddProject<Projects.Service>("service");
`,
		},
		{
			name:     "verbatim keyword identifier is not a using directive",
			source:   "var @using = nameof(Projects.Service);\n" + base + call,
			wantRefs: 1,
		},
		{
			name:    "Unicode escaped DistributedApplication local cannot shadow type lookup",
			source:  "var Distri\\u0062utedApplication = Custom.Fake;\n" + base + call,
			wantErr: true,
		},
		{
			name: "visible custom generic AddProject extension makes binding ambiguous",
			source: base + call + `
static class FakeExtensions { public static object AddProject<T>(this IDistributedApplicationBuilder builder, string name) => new(); }
`,
		},
		{
			name:     "call inside raw interpolated string is not observed",
			source:   "var ignored = $$\"\"\"{{ builder.AddProject<Projects.Fake>(\"fake\") }}\"\"\";\n" + base + call,
			wantRefs: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(tc.source))
			if tc.wantErr {
				if err == nil || found || len(defs) != 0 {
					t.Fatalf("malformed input was attributed: found=%t defs=%+v err=%v", found, defs, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := 0
			if found && len(defs) > 0 {
				got = len(defs[0].References)
			}
			if got != tc.wantRefs {
				t.Fatalf("got %d refs, want %d; found=%t defs=%+v", got, tc.wantRefs, found, defs)
			}
		})
	}
}

func TestCollectorAspireGlobalAliasGuardUsesSelectedProjectScope(t *testing.T) {
	program := []byte("using Aspire.Hosting;\nvar builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Api>(\"api\");\n")
	project := []byte(`<Project Sdk="Aspire.AppHost.Sdk/9.0.0" />`)
	apiProject := []byte(`<Project Sdk="Microsoft.NET.Sdk" />`)
	alias := []byte("global using DistributedApplication = Fake.DistributedApplication;\n")
	nestedAlias := []byte("global using DistributedApplication = Fake.DistributedApplication;\n")
	ordinary := []byte("// global using DistributedApplication = Fake;\nconst string x = \"global using Projects = Fake;\";\n")
	for name, paths := range map[string][]string{
		"same-project alias suppresses with truthful omission": {
			"src/AppHost/GlobalUsings.cs", "src/AppHost/AppHost.csproj", "src/AppHost/Program.cs", "src/Api/Api.csproj",
		},
		"nested project alias is included by apphost compile glob": {
			"src/AppHost/AppHost.csproj", "src/AppHost/Program.cs", "src/AppHost/tests/Tests.csproj", "src/AppHost/tests/GlobalUsings.cs",
		},
		"helper Program.cs alias is compilation-wide": {
			"src/AppHost/AppHost.csproj", "src/AppHost/Program.cs", "src/AppHost/Helpers/Program.cs",
		},
		"comment and literal lookalikes do not suppress": {
			"src/AppHost/AppHost.csproj", "src/AppHost/Program.cs", "src/AppHost/GlobalUsings.cs",
		},
		"incomplete context suppresses rather than claiming": {
			"src/AppHost/AppHost.csproj", "src/AppHost/Program.cs", "src/AppHost/GlobalUsings.cs",
		},
		"AppHost SDK project alias suppresses declaration": {
			"src/AppHost/AppHost.csproj", "src/AppHost/Program.cs",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewCollector(Options{})
			for _, filePath := range paths {
				content := []byte(nil)
				size := int64(0)
				switch filePath {
				case "src/AppHost/Program.cs":
					content = program
				case "src/AppHost/Helpers/Program.cs":
					content = alias
				case "src/AppHost/AppHost.csproj":
					content = project
					if name == "AppHost SDK project alias suppresses declaration" {
						content = []byte(`<Project Sdk="Aspire.AppHost.Sdk/9.0.0"><ItemGroup><Using Include="Fake" Alias="DistributedApplication" /></ItemGroup></Project>`)
					}
				case "src/Api/Api.csproj", "src/AppHost/tests/Tests.csproj":
					content = apiProject
				case "src/AppHost/GlobalUsings.cs":
					if name == "comment and literal lookalikes do not suppress" {
						content = ordinary
					} else if name == "incomplete context suppresses rather than claiming" {
						content = alias[:len(alias)-1]
						size = int64(len(alias)) // scanner supplied a truncated prefix
					} else {
						content = alias
					}
				case "src/AppHost/tests/GlobalUsings.cs":
					content = nestedAlias
				}
				if size == 0 {
					size = int64(len(content))
				}
				if _, err := c.Detect(context.Background(), profile.File{Path: filePath, Size: size, Content: content}); err != nil {
					t.Fatal(err)
				}
			}
			report := c.Finish()
			wantSuppressed := name == "same-project alias suppresses with truthful omission" || name == "nested project alias is included by apphost compile glob" || name == "helper Program.cs alias is compilation-wide" || name == "incomplete context suppresses rather than claiming" || name == "AppHost SDK project alias suppresses declaration"
			if got := len(report.Definitions) == 0; got != wantSuppressed {
				t.Fatalf("suppression=%t want=%t report=%+v", got, wantSuppressed, report)
			}
			if wantSuppressed {
				wantCode := "aspire_global_alias"
				if name == "incomplete context suppresses rather than claiming" {
					wantCode = "aspire_alias_context_incomplete"
				}
				if report.Omissions[wantCode] != 1 || report.Status != "partial" {
					t.Fatalf("suppression lacks bounded omission %q: %+v", wantCode, report)
				}
			}
		})
	}
}

func TestAspireAliasContextLimitIsFiniteAndDisclosed(t *testing.T) {
	files := make([]Candidate, maxAspireAliasFiles+1)
	for i := range files {
		files[i] = Candidate{Path: fmt.Sprintf("src/AppHost/Part%03d.cs", i), Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) { return []byte("x"), 1, nil }}
	}
	issue, err := inspectAspireGlobalAliases(context.Background(), "src/AppHost", "src/AppHost/Program.cs", files, map[string]bool{"src/AppHost": true})
	if err != nil || issue == nil || issue.code != "aspire_alias_context_limit" {
		t.Fatalf("context inventory limit was not disclosed: issue=%+v err=%v", issue, err)
	}
}

func TestAspireIrrelevantProgramSkipsTokenLimit(t *testing.T) {
	source := "// ordinary .NET Program.cs with no Aspire calls\n" + strings.Repeat("var value = 1;\n", maxAspireCSharpTokens)
	defs, found, err := parseAspireAppHost("src/Service/Program.cs", []byte(source))
	if err != nil || found || len(defs) != 0 {
		t.Fatalf("irrelevant Program.cs should be skipped before tokenization: found=%t defs=%+v err=%v", found, defs, err)
	}
}

func TestAspireInterpolationNestingHasFiniteDepth(t *testing.T) {
	interpolation := "0"
	for i := 0; i <= maxAspireInterpolationDepth; i++ {
		interpolation = "$\"{" + interpolation + "}\""
	}
	source := fmt.Sprintf("var ignored = %s;\nvar builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Service>(\"service\");\n", interpolation)
	if len(source) > 256<<10 {
		t.Fatalf("test input unexpectedly exceeds declaration byte budget: %d", len(source))
	}
	defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
	if err == nil || found || len(defs) != 0 {
		t.Fatalf("over-depth interpolation was not safely rejected: found=%t defs=%+v err=%v", found, defs, err)
	}
}

func TestAspireRawInterpolationBraceArityDoesNotPromoteEmbeddedCalls(t *testing.T) {
	source := strings.Join([]string{
		"var builder = DistributedApplication.CreateBuilder(args);",
		`var ignored = $$"""{{ ("""nested""", builder.AddProject<Projects.Fake>("fake")) }}""";`,
		`builder.AddProject<Projects.Real>("real");`,
	}, "\n")
	defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
	if err != nil || !found || len(defs) != 1 || len(defs[0].References) != 1 || defs[0].References[0].Value != "Real" {
		t.Fatalf("raw interpolation leaked or obscured references: found=%t defs=%+v err=%v", found, defs, err)
	}
}

func TestAspireMalformedStatementDoesNotPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed C# input panicked: %v", recovered)
		}
	}()
	source := "var builder = DistributedApplication.CreateBuilder(args);\nbuilder $\"x\" = 1;\nbuilder.AddProject<Projects.Real>(\"real\");\n"
	defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if found || len(defs) != 0 {
		t.Fatalf("call after malformed builder use was attributed: found=%t defs=%+v", found, defs)
	}
}

func FuzzAspireAppHostParser(f *testing.F) {
	for _, seed := range []string{
		`builder.AddProject<Projects.Api>("api");`,
		`// builder.AddProject<Projects.Comment>("fake");`,
		`var text = "builder.AddProject<Projects.String>(\"fake\")";`,
		`var text = $$"""{{ builder.AddProject<Projects.Raw>("fake") }}""";`,
		`var ignored = enabled ? builder.AddProject<Projects.Conditional>("fake") : null;`,
		`var (DistributedApplication, ignored) = (new Fake(), 0); builder.AddProject<Projects.Fake>("fake");`,
		`Replace(ref builder); builder.AddProject<Projects.AfterRef>("fake");`,
		`builder $"x" = 1; builder.AddProject<Projects.AfterMalformed>("fake");`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, tail string) {
		if int64(len(tail)) > DefaultFileBytes {
			t.Skip()
		}
		source := "var builder = DistributedApplication.CreateBuilder(args);\n" + tail + "\n"
		first, firstFound, firstErr := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
		second, secondFound, secondErr := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
		if (firstErr == nil) != (secondErr == nil) || firstFound != secondFound || !reflect.DeepEqual(first, second) {
			t.Fatalf("Aspire parser is nondeterministic: found=%t/%t err=%v/%v", firstFound, secondFound, firstErr, secondErr)
		}
		if firstErr != nil {
			if firstFound || len(first) != 0 {
				t.Fatalf("failed parse retained declarations: found=%t defs=%+v err=%v", firstFound, first, firstErr)
			}
			return
		}
		lines := 1 + strings.Count(source, "\n")
		for _, def := range first {
			if len(def.References) > maxAspireSourceReferences {
				t.Fatalf("Aspire parser exceeded reference cap: %d", len(def.References))
			}
			for _, ref := range def.References {
				if len(ref.Value) > DefaultStringBytes {
					t.Fatalf("Aspire reference value exceeded string cap: %d", len(ref.Value))
				}
				assertFuzzEvidenceLine(t, ref.Evidence, lines)
			}
		}
	})
}
