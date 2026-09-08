package scanner

import (
	"context"
	"errors"
	"testing"
)

func TestGlobScopeAndPrecedence(t *testing.T) {
	rules, warnings := parseAttributes("src/.gitattributes", []byte("*.go linguist-vendored\nlib/** linguist-vendored=false\nlib/*.go !linguist-vendored\n/root.py linguist-language=Go\n**/special?.[ch] linguist-generated\n"))
	if len(warnings) != 0 {
		t.Fatalf("parse warnings: %+v", warnings)
	}
	for _, tc := range []struct {
		path      string
		vendor    *bool
		language  string
		generated bool
	}{
		{"elsewhere/a.go", nil, "", false},
		{"src/a.go", boolPointer(true), "", false},
		{"src/deep/a.go", boolPointer(true), "", false},
		{"src/lib/deep/a.go", boolPointer(false), "", false},
		{"src/lib/a.go", nil, "", false},
		{"src/root.py", nil, "Go", false},
		{"src/nested/root.py", nil, "", false},
		{"src/special1.c", nil, "", true},
		{"src/deep/special2.h", nil, "", true},
		{"src/deep/special23.h", nil, "", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got := resolveAttributes(tc.path, rules)
			if (got.vendored == nil) != (tc.vendor == nil) || (got.vendored != nil && *got.vendored != *tc.vendor) || got.language != tc.language || overrideBool(got.generated, false) != tc.generated {
				t.Fatalf("overrides: %+v", got)
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestUnsupportedAttributesWarn(t *testing.T) {
	_, warnings := parseAttributes(".gitattributes", []byte("# harmless comment\n*.txt text\n!negative linguist-vendored\n\"with space\" linguist-generated\n[attr]custom linguist-vendored\nfolder/ linguist-vendored\n*.go linguist-language=NoSuchLanguage\n*.go linguist-detectable=perhaps\n*.go linguist-unknown\na**b linguist-generated\n[broken linguist-generated\n"))
	if len(warnings) != 4 {
		t.Fatalf("got %d warnings: %+v", len(warnings), warnings)
	}
	for _, warning := range warnings {
		if warning.Path != ".gitattributes" || warning.Code != "unsupported_gitattributes" {
			t.Fatalf("warning: %+v", warning)
		}
	}
}

func TestAttributeBooleanAndLanguageResets(t *testing.T) {
	rules, warnings := parseAttributes(".gitattributes", []byte("*.go linguist-generated=true linguist-vendored=true linguist-detectable=false linguist-language=Python\n*.go -linguist-generated linguist-vendored=false linguist-detectable=true\nreset.go !linguist-generated !linguist-vendored !linguist-detectable !linguist-language\n"))
	if len(warnings) != 0 {
		t.Fatalf("warnings: %+v", warnings)
	}
	got := resolveAttributes("main.go", rules)
	if got.generated == nil || *got.generated || got.vendored == nil || *got.vendored || got.detectable == nil || !*got.detectable || got.language != "Python" {
		t.Fatalf("override: %+v", got)
	}
	got = resolveAttributes("reset.go", rules)
	if got.generated != nil || got.vendored != nil || got.detectable != nil || got.language != "" {
		t.Fatalf("reset: %+v", got)
	}
}

func TestQuotedMacrosDocumentationAndTruthyValues(t *testing.T) {
	rules, warnings := parseAttributes(".gitattributes", []byte("[attr]private linguist-vendored linguist-generated\n\"with space.go\" private\ntruth.go linguist-documentation=perhaps\n*.json linguist-detectable=true\n\"caf\\303\\251.go\" linguist-language=Python\n"))
	if len(warnings) != 0 {
		t.Fatalf("warnings: %+v", warnings)
	}
	private := resolveAttributes("with space.go", rules)
	if !overrideBool(private.vendored, false) || !overrideBool(private.generated, false) {
		t.Fatalf("macro: %+v", private)
	}
	if !overrideBool(resolveAttributes("truth.go", rules).documentation, false) {
		t.Fatal("Linguist truthy string not honored")
	}
	if got := resolveAttributes("café.go", rules).language; got != "Python" {
		t.Fatalf("octal quote: %q", got)
	}
}

func TestMacroTokenPrecedenceAndCycles(t *testing.T) {
	rules, warnings := parseAttributes(".gitattributes", []byte("[attr]generated linguist-generated\n[attr]loop loop\na.py generated -linguist-generated\nb.py -linguist-generated generated\nc.py loop\n"))
	if len(warnings) != 0 {
		t.Fatalf("warnings: %+v", warnings)
	}
	if overrideBool(resolveAttributes("a.py", rules).generated, true) {
		t.Fatal("later explicit token must override macro")
	}
	if !overrideBool(resolveAttributes("b.py", rules).generated, false) {
		t.Fatal("later macro must override explicit token")
	}
	resolveAttributes("c.py", rules)
}

func TestEmptyAttributeValueDiffersFromReset(t *testing.T) {
	rules, _ := parseAttributes(".gitattributes", []byte("a.json linguist-detectable=\na.go linguist-language=\nreset.json !linguist-detectable\n"))
	if !overrideBool(resolveAttributes("a.json", rules).detectable, false) {
		t.Fatal("empty string attribute should be truthy")
	}
	language := resolveAttributes("a.go", rules)
	if !language.languageSet || language.language != "" {
		t.Fatalf("empty language alias should suppress inference: %+v", language)
	}
	if resolveAttributes("reset.json", rules).detectable != nil {
		t.Fatal("unspecified attribute became empty string")
	}
}

func TestAttributeRuleLimitAndCancellation(t *testing.T) {
	rules, warnings, exceeded := parseAttributesBounded(".gitattributes", []byte("a.go linguist-generated\nb.go linguist-generated\nc.go linguist-generated\n"), 2)
	if !exceeded || len(rules) != 2 || len(warnings) != 0 {
		t.Fatalf("bounded parse: rules=%d warnings=%+v exceeded=%v", len(rules), warnings, exceeded)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolveAttributesContext(ctx, "a.go", rules); !errors.Is(err, context.Canceled) {
		t.Fatalf("attribute resolution ignored cancellation: %v", err)
	}
}

func TestAttributeRuleSetsPreserveAncestorOrderAndMacros(t *testing.T) {
	root, warnings := parseAttributes(".gitattributes", []byte("[attr]python linguist-language=Python\n*.txt linguist-generated\n"))
	if len(warnings) != 0 {
		t.Fatalf("root warnings: %+v", warnings)
	}
	child, warnings := parseAttributes("src/.gitattributes", []byte("*.txt -linguist-generated python\n"))
	if len(warnings) != 0 {
		t.Fatalf("child warnings: %+v", warnings)
	}
	got, err := resolveAttributeRuleSetsContext(context.Background(), "src/readme.txt", [][]attributeRule{root, child})
	if err != nil {
		t.Fatal(err)
	}
	if got.language != "Python" || overrideBool(got.generated, true) {
		t.Fatalf("ancestor rule-set behavior changed: %+v", got)
	}
}
