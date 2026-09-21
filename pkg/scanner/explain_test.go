package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dircue/pkg/explain"
)

func traceSource() explain.Source {
	return explain.Source{Mode: "directory", Consistency: "live-directory-non-atomic"}
}

func tracedJob(path, content string, attrs overrides, provenance []explain.Override) job {
	return job{
		path: path, size: int64(len(content)), attrs: attrs, traceOverrides: provenance,
		read: func(limit int64) ([]byte, int64, error) {
			data := []byte(content)
			return data[:min(int64(len(data)), limit)], int64(len(data)), nil
		},
	}
}

func TestLanguageTraceAgreesWithActualIncludedDecision(t *testing.T) {
	item := tracedJob("src/main.go", goSource, overrides{}, nil)
	trace := &explain.LanguageTrace{Source: traceSource()}
	value, err := analyzeFileWithLanguageTrace(context.Background(), nil, item, Options{}, trace)
	if err != nil {
		t.Fatal(err)
	}
	report, err := explain.LanguageReport(*trace)
	if err != nil {
		t.Fatal(err)
	}
	if value.language != "Go" || report.Decision.Status != "included" || report.Decision.ReportedLanguage != value.language || report.Decision.DetectedLanguage != "Go" || report.Decision.Strategy != "Extension" {
		t.Fatalf("trace disagrees with actual result: value=%+v report=%+v", value, report)
	}
	if report.Extent.ReadBytes != int64(len(goSource)) || !report.Extent.ContentComplete || report.Extent.ClassifiedBytes != int64(len(goSource)) {
		t.Fatalf("wrong extent: %+v", report.Extent)
	}
}

func TestLanguageTraceCoversActualExclusionReturns(t *testing.T) {
	truth, falsity := true, false
	lfs := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 123\n"
	tests := []struct {
		name, path, content, reason string
		attrs                       overrides
		options                     Options
		wantRead                    bool
	}{
		{"vendored", "vendor/main.go", goSource, "vendored", overrides{}, Options{}, false},
		{"generated", "main.go", goSource, "generated", overrides{generated: &truth}, Options{}, true},
		{"documentation", "main.go", goSource, "documentation", overrides{documentation: &truth}, Options{}, true},
		{"detectable", "main.go", goSource, "not_detectable", overrides{detectable: &falsity}, Options{}, true},
		{"lfs", "main.go", lfs, "lfs_pointer", overrides{lfsTracked: true}, Options{}, true},
		{"unknown", "artifact.unknown-extension", "plain words\n", "not_detectable", overrides{}, Options{}, true},
		{"binary", "main.go", "\x00binary", "binary", overrides{}, Options{}, true},
		{"too-large", "main.go", goSource, "file_too_large", overrides{}, Options{MaxFileBytes: 1}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			item := tracedJob(test.path, test.content, test.attrs, nil)
			original := item.read
			item.read = func(limit int64) ([]byte, int64, error) {
				reads++
				return original(limit)
			}
			trace := &explain.LanguageTrace{Source: traceSource()}
			value, err := analyzeFileWithLanguageTrace(context.Background(), nil, item, test.options, trace)
			if err != nil {
				t.Fatal(err)
			}
			report, err := explain.LanguageReport(*trace)
			if err != nil {
				t.Fatal(err)
			}
			if report.Decision.Status != "excluded" || report.Decision.Reason != test.reason || value.language != "" {
				t.Fatalf("wrong actual decision: value=%+v report=%+v", value, report.Decision)
			}
			if (reads > 0) != test.wantRead {
				t.Fatalf("reads=%d, wantRead=%t", reads, test.wantRead)
			}
		})
	}
}

func TestLanguageTraceUsesRetainedOverrideProvenance(t *testing.T) {
	truth, falsity := true, false
	provenance := []explain.Override{
		{Attribute: "linguist-generated", Value: "false", Source: "src/.gitattributes", Line: 3, Provenance: "retained"},
		{Attribute: "linguist-language", Value: "Python", Source: ".gitattributes", Line: 2, Provenance: "retained"},
	}
	item := tracedJob("src/main.custom", goSource, overrides{languageSet: true, language: "Python", generated: &falsity, detectable: &truth}, provenance)
	trace := &explain.LanguageTrace{Source: traceSource()}
	value, err := analyzeFileWithLanguageTrace(context.Background(), nil, item, Options{}, trace)
	if err != nil {
		t.Fatal(err)
	}
	report, err := explain.LanguageReport(*trace)
	if err != nil {
		t.Fatal(err)
	}
	if value.language != "Python" || report.Decision.DetectedLanguage != "" || report.Decision.ReportedLanguage != "Python" {
		t.Fatalf("language override disagrees: value=%+v decision=%+v", value, report.Decision)
	}
	want := []explain.Override{
		{Attribute: "linguist-detectable", Value: "true", Provenance: "unavailable"},
		{Attribute: "linguist-generated", Value: "false", Source: "src/.gitattributes", Line: 3, Provenance: "retained"},
		{Attribute: "linguist-language", Value: "Python", Source: ".gitattributes", Line: 2, Provenance: "retained"},
	}
	if !reflect.DeepEqual(report.Overrides, want) {
		t.Fatalf("overrides=%+v, want %+v", report.Overrides, want)
	}
}

func TestLanguageTracePreservesReadFailureAndCancellation(t *testing.T) {
	readErr := errors.New("fixture read failed")
	item := job{path: "main.go", size: 10, read: func(int64) ([]byte, int64, error) { return nil, 0, readErr }}
	trace := &explain.LanguageTrace{Source: traceSource()}
	if _, err := analyzeFileWithLanguageTrace(context.Background(), nil, item, Options{}, trace); !errors.Is(err, readErr) || trace.Decision.Reason != "read_error" {
		t.Fatalf("read failure trace: decision=%+v err=%v", trace.Decision, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	item = tracedJob("main.go", goSource, overrides{}, nil)
	trace = &explain.LanguageTrace{Source: traceSource()}
	if _, err := analyzeFileWithLanguageTrace(ctx, nil, item, Options{}, trace); !errors.Is(err, context.Canceled) || trace.Decision.Reason != "cancelled" {
		t.Fatalf("cancellation trace: decision=%+v err=%v", trace.Decision, err)
	}
}

func TestAttributeTraceRetainsFinalNestedAndMacroSources(t *testing.T) {
	root, warnings := parseAttributes(".gitattributes", []byte("*.go linguist-vendored=true\n[attr]generated linguist-generated=true\n"))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	child, warnings := parseAttributes("src/.gitattributes", []byte("*.go linguist-vendored=false generated\n*.go !linguist-language\n"))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	attrs, got, err := resolveAttributeRuleSetsTraceContext(context.Background(), "src/main.go", [][]attributeRule{root, child})
	if err != nil {
		t.Fatal(err)
	}
	if attrs.vendored == nil || *attrs.vendored || attrs.generated == nil || !*attrs.generated || attrs.languageSet {
		t.Fatalf("wrong resolved attributes: %+v", attrs)
	}
	want := []explain.Override{
		{Attribute: "linguist-generated", Value: "true", Source: ".gitattributes", Line: 2, Provenance: "retained"},
		{Attribute: "linguist-language", Value: "unset", Source: "src/.gitattributes", Line: 2, Provenance: "retained"},
		{Attribute: "linguist-vendored", Value: "false", Source: "src/.gitattributes", Line: 1, Provenance: "retained"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provenance=%+v, want %+v", got, want)
	}
}

func TestGitInfoAttributeTraceUsesActualOrigin(t *testing.T) {
	rules, warnings, exceeded := parseGitAttributesBoundedFrom(".gitattributes", ".git/info/attributes", []byte("*.go linguist-language=Python\n"), 10)
	if exceeded || len(warnings) != 0 {
		t.Fatalf("parse: exceeded=%t warnings=%+v", exceeded, warnings)
	}
	_, got, err := resolveAttributesTraceContext(context.Background(), "src/main.go", rules)
	if err != nil {
		t.Fatal(err)
	}
	want := []explain.Override{{Attribute: "linguist-language", Value: "Python", Source: ".git/info/attributes", Line: 1, Provenance: "retained"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("info provenance=%+v, want %+v", got, want)
	}
}

func reportLanguageForPath(reportLanguages map[string][]string, filename string) string {
	for language, files := range reportLanguages {
		for _, candidate := range files {
			if candidate == filename {
				return language
			}
		}
	}
	return ""
}

func TestFreshLanguageExplanationConformsToOrdinaryScan(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{
		".gitattributes":       "generated.go linguist-generated\ngenerated-kept.go -linguist-generated\nvendor/kept.go -linguist-vendored\noverride.data linguist-language=Python\nbinary-override.data linguist-language=Python\n[attr]asruby linguist-language=Ruby\nmacro.data asruby\n",
		"main.go":              goSource,
		"generated.go":         goSource,
		"generated-kept.go":    "// Code generated by fixture. DO NOT EDIT.\n" + goSource,
		"vendor/vendor.go":     goSource,
		"vendor/kept.go":       goSource,
		"binary.go":            "\x00binary",
		"binary-override.data": "\x00binary",
		"override.data":        goSource,
		"macro.data":           goSource,
		"info.data":            goSource,
	})
	if err := os.MkdirAll(filepath.Join(root, ".git/info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git/info/attributes"), []byte("info.data linguist-language=JavaScript\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ordinary, err := Scan(context.Background(), root, Options{IncludeFiles: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		status, reason, language, overrideSource, overrideRule string
	}{
		"main.go":              {"included", "included", "Go", "", ""},
		"generated.go":         {"excluded", "generated", "", ".gitattributes", "generated"},
		"generated-kept.go":    {"included", "included", "Go", ".gitattributes", "generated"},
		"vendor/vendor.go":     {"excluded", "vendored", "", "", ""},
		"vendor/kept.go":       {"included", "included", "Go", ".gitattributes", "vendored"},
		"binary.go":            {"excluded", "binary", "", "", ""},
		"binary-override.data": {"included", "included", "Python", ".gitattributes", "language-override"},
		"override.data":        {"included", "included", "Python", ".gitattributes", "language-override"},
		"macro.data":           {"included", "included", "Ruby", ".gitattributes", "language-override"},
		"info.data":            {"included", "included", "JavaScript", ".git/info/attributes", "language-override"},
	}
	for filename, expected := range want {
		t.Run(filename, func(t *testing.T) {
			targeted, err := Scan(context.Background(), root, Options{ExplainPath: filename, Workers: 1})
			if err != nil {
				t.Fatal(err)
			}
			actual := targeted.Explanation
			if actual == nil || actual.Decision.Status != expected.status || actual.Decision.Reason != expected.reason || actual.Decision.ReportedLanguage != expected.language {
				t.Fatalf("targeted decision mismatch: %+v", actual)
			}
			if got := reportLanguageForPath(languageFiles(ordinary), filename); got != expected.language {
				t.Fatalf("ordinary scan language=%q targeted=%q", got, expected.language)
			}
			if expected.overrideSource != "" {
				found := false
				for _, override := range actual.Overrides {
					if override.Source == expected.overrideSource && override.Provenance == "retained" {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing exact override provenance: %+v", actual.Overrides)
				}
				if expected.overrideRule != "" {
					stepFound := false
					for _, step := range actual.Steps {
						if step.Rule == expected.overrideRule && step.Provider == expected.overrideSource {
							stepFound = true
						}
					}
					if !stepFound {
						t.Fatalf("override step lost source origin: %+v", actual.Steps)
					}
				}
			}
			encoded, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{`"overrides":[`, `"facts":[]`, `"steps":[`, `"omissions":[]`} {
				if !strings.Contains(string(encoded), required) {
					t.Fatalf("nullable explanation collection: %s", encoded)
				}
			}
		})
	}
}

func TestFreshLanguageExplanationUsesSelectedGitRevisionDespiteDirtyWorktree(t *testing.T) {
	root, repo, first := gitFixture(t, map[string]string{"main.data": goSource, ".gitattributes": "main.data linguist-language=Python\n"})
	commit, err := repo.CommitObject(first)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.data"), []byte("const dirty = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("main.data linguist-language=Ruby\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Revision: first.String(), IncludeFiles: true, Workers: 1}
	ordinary, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	targeted, err := Scan(context.Background(), root, Options{Revision: first.String(), ExplainPath: "main.data", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := reportLanguageForPath(languageFiles(ordinary), "main.data"); got != "Python" || targeted.Explanation.Decision.ReportedLanguage != got || targeted.Explanation.Source.Tree != tree.Hash.String() {
		t.Fatalf("revision source drifted: ordinary=%q explanation=%+v", got, targeted.Explanation)
	}
}

func TestFreshLanguageExplanationDisclosesUnavailableAndBoundedTargets(t *testing.T) {
	root := fixtures(t, map[string]string{"main.go": goSource, "extra.go": goSource})
	if err := os.Symlink("main.go", filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                 string
		opts                 Options
		path, status, reason string
	}{
		{"size-limit", Options{Source: "directory", MaxFileBytes: 1}, "main.go", "excluded", "file_too_large"},
		{"tree-limit", Options{Source: "directory", MaxTreeSize: 1}, "main.go", "unavailable", "tree_size_limit"},
		{"absent", Options{Source: "directory"}, "missing.go", "unavailable", "not_in_selected_inventory"},
		{"nonregular", Options{Source: "directory"}, "link.go", "unavailable", "non_regular_file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.opts.ExplainPath = test.path
			test.opts.Workers = 1
			report, err := Scan(context.Background(), root, test.opts)
			if err != nil {
				t.Fatal(err)
			}
			if report.Explanation == nil || report.Explanation.Decision.Status != test.status || report.Explanation.Decision.Reason != test.reason {
				t.Fatalf("unexpected unavailable decision: %+v", report.Explanation)
			}
		})
	}
}

func BenchmarkLanguageExplanationIncrementalCost(b *testing.B) {
	content := []byte(goSource)
	item := job{
		path: "src/main.go", size: int64(len(content)),
		read: func(limit int64) ([]byte, int64, error) {
			return content[:min(int64(len(content)), limit)], int64(len(content)), nil
		},
	}
	b.Run("ordinary", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := analyzeFile(context.Background(), nil, item, Options{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("targeted-trace", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			trace := &explain.LanguageTrace{Source: traceSource()}
			if _, err := analyzeFileWithLanguageTrace(context.Background(), nil, item, Options{}, trace); err != nil {
				b.Fatal(err)
			}
		}
	})
}
