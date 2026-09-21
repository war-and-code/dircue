package explain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func completeLanguageTrace() LanguageTrace {
	return LanguageTrace{
		Source:              Source{Mode: "git", Tree: strings.Repeat("a", 40), Consistency: "selected_git_tree"},
		Path:                "src/main.go",
		Scope:               Scope{Module: "languages", Engine: "go-enry/Linguist 9.7.0", Evidence: "fresh", Inventory: "selected-source-metadata-walk", Content: "target-file-bounded-prefix"},
		Extent:              Extent{FileBytes: 12, ReadBytes: 12, ClassifiedBytes: 12, ContentComplete: true},
		ClassificationLimit: 128 << 10,
		Overrides: []Override{
			{Attribute: "linguist-language", Value: "Go", Source: ".gitattributes", Line: 2, Provenance: "retained"},
		},
		Steps:    []Step{{Rule: "language-detection", Provider: "go-enry", Outcome: "Go", Evidence: "Extension"}},
		Decision: Decision{Status: "included", Reason: "language_included", DetectedLanguage: "Go", ReportedLanguage: "Go", Strategy: "Extension"},
	}
}

func TestLanguageReportDeterministicStructuredAndTextOutput(t *testing.T) {
	first, err := LanguageReport(completeLanguageTrace())
	if err != nil {
		t.Fatal(err)
	}
	second, err := LanguageReport(completeLanguageTrace())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first.ObservationID) != 64 {
		t.Fatalf("report is not deterministic: first=%+v second=%+v", first, second)
	}
	encoded, err := json.MarshalIndent(first, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/language.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(encoded), bytes.TrimSpace(golden)) {
		t.Fatalf("structured golden mismatch\nwant:\n%s\ngot:\n%s", golden, encoded)
	}
	if first.Overrides == nil || first.Facts == nil || first.Steps == nil || first.Omissions == nil {
		t.Fatalf("JSON arrays must be non-null: %s", encoded)
	}
	text, err := Text(first)
	if err != nil {
		t.Fatal(err)
	}
	want := "src/main.go: included (language_included)\n" +
		"language: Go via Extension\n" +
		"source: git tree " + strings.Repeat("a", 40) + " (selected_git_tree)\n" +
		"extent: 12/12 bytes read; 12 classified; complete=true\n" +
		"override: linguist-language=Go at .gitattributes:2\n" +
		"1. language-detection [go-enry]: Go — Extension\n"
	if text != want {
		t.Fatalf("text mismatch\nwant:\n%s\ngot:\n%s", want, text)
	}
}

func TestLanguageReportRejectsInventedOrMalformedProvenance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LanguageTrace)
	}{
		{"unknown-source-without-digest", func(v *LanguageTrace) {
			v.Source = Source{Mode: "unknown", Consistency: "not_retained"}
			v.Scope.Evidence = "retained"
		}},
		{"uppercase-digest", func(v *LanguageTrace) { v.Source.ReportSHA256 = strings.Repeat("A", 64) }},
		{"retained-without-line", func(v *LanguageTrace) { v.Overrides[0].Line = 0 }},
		{"unavailable-with-source", func(v *LanguageTrace) { v.Overrides[0].Provenance = "unavailable" }},
		{"escape-path", func(v *LanguageTrace) { v.Path = "../main.go" }},
		{"included-without-steps", func(v *LanguageTrace) { v.Steps = nil }},
		{"included-without-language", func(v *LanguageTrace) { v.Decision.ReportedLanguage = "" }},
		{"wrong-module", func(v *LanguageTrace) { v.Scope.Module = "project-evidence" }},
		{"wrong-evidence-mode", func(v *LanguageTrace) { v.Scope.Evidence = "historical" }},
		{"directory-with-tree", func(v *LanguageTrace) { v.Source.Mode = "directory" }},
		{"fresh-with-report-digest", func(v *LanguageTrace) { v.Source.ReportSHA256 = strings.Repeat("b", 64) }},
		{"malformed-tree", func(v *LanguageTrace) { v.Source.Tree = "HEAD" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace := completeLanguageTrace()
			test.mutate(&trace)
			if _, err := LanguageReport(trace); err == nil {
				t.Fatal("accepted malformed explanation")
			}
		})
	}
}

func TestLanguageReportBoundsFactsBeforeAllocationResult(t *testing.T) {
	trace := completeLanguageTrace()
	for i := 0; i < MaxFacts+5; i++ {
		trace.Facts = append(trace.Facts, Fact{Kind: "boundary", State: "unknown", Target: "src/main.go", Evidence: "src/main.go", Detail: fmt.Sprintf("detail-%03d", i)})
	}
	report, err := LanguageReport(trace)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Facts) > MaxFacts || len(report.Omissions) == 0 {
		t.Fatalf("unbounded facts: facts=%d omissions=%+v", len(report.Facts), report.Omissions)
	}
}

func TestTextEscapesUntrustedControlsWithoutChangingStructuredEvidence(t *testing.T) {
	trace := completeLanguageTrace()
	trace.Path = "src/\x1b[31m.go"
	trace.Decision.ReportedLanguage = "Go\nspoof"
	trace.Overrides[0].Value = "Go\nspoof"
	trace.Steps[0].Evidence = "Extension\nspoof"
	trace.Facts = []Fact{{Kind: "boundary", State: "unknown", Evidence: "src/main.go", Detail: "detail\x1b[2J"}}
	report, err := LanguageReport(trace)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision.ReportedLanguage != "Go\nspoof" || report.Facts[0].Detail != "detail\x1b[2J" {
		t.Fatal("structured evidence changed")
	}
	text, err := Text(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "\x1b") || strings.Contains(text, "Go\nspoof") || !strings.Contains(text, `"Go\nspoof"`) || !strings.Contains(text, `"src/\x1b[31m.go"`) {
		t.Fatalf("controls were not safely rendered: %q", text)
	}
}
