package deployables

import (
	"fmt"
	"strings"
	"testing"
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
