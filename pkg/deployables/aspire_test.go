package deployables

import (
	"strings"
	"testing"
)

func TestAspireLexerIgnoresCSharpCommentsAndStringForms(t *testing.T) {
	source := `var builder = DistributedApplication.CreateBuilder(args);
// builder.AddProject<Projects.Comment>("fake");
var a = "builder.AddProject<Projects.Regular>(\"fake\")";
var b = @"builder.AddProject<Projects.Verbatim>(""fake"")";
var c = $"builder.AddProject<Projects.Interpolated>(\"{value}\")";
var d = """builder.AddProject<Projects.Raw>(""fake"")""";
var e = $$"""fake builder.AddProject<Projects.InterpolatedRaw>("x")""";
builder.AddProject<Projects.Real>("real");
builder.AddProject<Projects.Second>("second");
`
	defs, found, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(source))
	if err != nil || !found {
		tokens, lexErr := lexAspireCSharp(source)
		t.Fatalf("parse: found=%v err=%v lex=%v tokens=%#v", found, err, lexErr, tokens)
	}
	if len(defs) != 1 || len(defs[0].References) != 2 {
		t.Fatalf("unexpected definitions: %#v", defs)
	}
	if defs[0].References[0].Value != "Real" || defs[0].References[0].Evidence.Line != 8 || defs[0].References[1].Value != "Second" || defs[0].References[1].Evidence.Line != 9 {
		t.Fatalf("references lack precise identities/lines: %#v", defs[0].References)
	}
}

func TestAspireOnlyAcceptsUnconditionalTopLevelDirectCalls(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"nested", `var builder = DistributedApplication.CreateBuilder(args);\nvoid Helper() { builder.AddProject<Projects.Nested>("n"); }\n`, 0},
		{"conditional", `var builder = DistributedApplication.CreateBuilder(args);\n#if FEATURE\nbuilder.AddProject<Projects.Conditional>("c");\n#endif\n`, 0},
		{"lookalike", `var builder = DistributedApplication.CreateBuilder(args);\nother.AddProject<Projects.Other>("x");\n`, 0},
		{"helper", `var builder = DistributedApplication.CreateBuilder(args);\nRegister(builder, Projects.Helper);\n`, 0},
		{"shadow", `var builder = DistributedApplication.CreateBuilder(args);\nbuilder = Other.CreateBuilder();\nbuilder.AddProject<Projects.AfterShadow>("x");\n`, 0},
		{"control-flow", `var builder = DistributedApplication.CreateBuilder(args);\nif (enabled) builder.AddProject<Projects.Conditional>("x");\n`, 0},
		{"lambda-body", `var builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Direct>("x", configure: x => { x.WithAnnotation("a"); });\n`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defs, _, err := parseAspireAppHost("src/AppHost/Program.cs", []byte(strings.ReplaceAll(tc.body, `\n`, "\n")))
			if err != nil {
				t.Fatal(err)
			}
			got := 0
			if len(defs) > 0 {
				got = len(defs[0].References)
			}
			if got != tc.want {
				t.Fatalf("got %d refs, want %d (%#v)", got, tc.want, defs)
			}
		})
	}
}
