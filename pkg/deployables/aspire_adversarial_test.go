package deployables

import (
	"fmt"
	"reflect"
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
