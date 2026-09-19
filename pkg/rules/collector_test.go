package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func collectorTest(t testing.TB, p *Program, opts Options) *Collector {
	t.Helper()
	c, err := New(p, Source{Kind: "directory"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func finishTest(t testing.TB, c *Collector) *Report {
	t.Helper()
	r, err := c.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestCollectorContentCoverage(t *testing.T) {
	c := collectorTest(t, contentConfig(t), Options{})
	files := []File{{"match.xml", 9}, {"negative.xml", 0}, {"too-big.xml", MaxContentBytes + 1}, {"budget.xml", 9}, {"invalid.xml", 1}, {"changed.xml", 9}, {"forgotten.xml", 9}, {"irrelevant.go", 100}}
	for _, file := range files {
		eligible, err := c.ObserveMetadata(file)
		if err != nil {
			t.Fatal(err)
		}
		want := file.Path != "too-big.xml" && file.Path != "irrelevant.go"
		if eligible != want {
			t.Fatalf("eligibility %s: %v", file.Path, eligible)
		}
	}
	if err := c.ObserveContent(files[0], []byte("<project>")); err != nil {
		t.Fatal(err)
	}
	if err := c.ObserveContent(files[1], nil); err != nil {
		t.Fatal(err)
	}
	if err := c.OmitContent(files[3], ContentBudget); err != nil {
		t.Fatal(err)
	}
	if err := c.ObserveContent(files[4], []byte{0xff}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatal(err)
	}
	if err := c.OmitContent(files[4], InvalidUTF8); err != nil {
		t.Fatal(err)
	}
	if err := c.ObserveContent(files[5], []byte("x")); !errors.Is(err, ErrIncomplete) {
		t.Fatal(err)
	}
	if err := c.OmitContent(files[5], Incomplete); err != nil {
		t.Fatal(err)
	}
	r := finishTest(t, c)
	if r.InventoryFiles != 8 || r.CandidateFiles != 7 || r.ContentCandidateFiles != 7 || r.ContentEligibleFiles != 6 || r.ContentAdmittedFiles != 4 || r.ContentEvaluatedFiles != 2 || r.ContentOmittedFiles != 5 || r.TotalMatches != 8 || r.OmittedMatches != 0 || r.Status != "partial" {
		t.Fatalf("wrong counts: %+v", r)
	}
	want := map[OmissionReason]uint64{FileTooLarge: 1, ContentBudget: 1, InvalidUTF8: 1, Incomplete: 1, NotEvaluated: 1}
	if !reflect.DeepEqual(r.Omissions, want) {
		t.Fatalf("omissions %+v", r.Omissions)
	}
	if r.Rules[0] != (RuleSummary{ID: "config", CandidateFiles: 7, EvaluatedFiles: 7, MatchedFiles: 7}) || r.Rules[1] != (RuleSummary{ID: "marker", RequiresContent: true, CandidateFiles: 7, EvaluatedFiles: 2, MatchedFiles: 1, ContentOmittedFiles: 5}) {
		t.Fatalf("rule counts %+v", r.Rules)
	}
}
func TestCollectorCallerLimitPreservesMetadata(t *testing.T) {
	c := collectorTest(t, contentConfig(t), Options{MaxFileBytes: 8})
	eligible, err := c.ObserveMetadata(File{"p.xml", 9})
	if err != nil || eligible {
		t.Fatalf("%v %v", eligible, err)
	}
	r := finishTest(t, c)
	if r.TotalMatches != 1 || r.ContentEligibleFiles != 0 || r.Omissions[CallerFileLimit] != 1 || r.Limits.ContentBytes != 8 || r.Limits.CallerMaxFileBytes != 8 {
		t.Fatalf("%+v", r)
	}
}
func TestCollectorDeterministicBoundedRetention(t *testing.T) {
	p := compileTest(t, `{"schema_version":"1.0.0","rules":[{"id":"z","match":{"extensions":[".cs"]}},{"id":"a","match":{"extensions":[".cs"]}}]}`)
	var reference []byte
	for seed := int64(0); seed < 8; seed++ {
		c := collectorTest(t, p, Options{})
		for _, i := range rand.New(rand.NewSource(seed)).Perm(1800) {
			if _, err := c.ObserveMetadata(File{fmt.Sprintf("src/%04d.cs", i), 10}); err != nil {
				t.Fatal(err)
			}
		}
		if len(c.evidence) != MaxObservations || len(c.admitted) != 0 {
			t.Fatal("unbounded collector retention")
		}
		r := finishTest(t, c)
		if r.TotalMatches != 3600 || len(r.Observations) != 1024 || r.OmittedMatches != 2576 || r.Status != "partial" {
			t.Fatalf("wrong cap %+v", r)
		}
		if r.Observations[0].Path != "src/0000.cs" || r.Observations[0].RuleID != "a" || r.Observations[1023].Path != "src/0511.cs" || r.Observations[1023].RuleID != "z" {
			t.Fatal("not lexical prefix")
		}
		output, _ := json.Marshal(r)
		if seed == 0 {
			reference = output
		} else if !bytes.Equal(reference, output) {
			t.Fatal("aggregation order changed report")
		}
	}
}
func TestCollectorAdmissionBoundAndNoMutationOnErrors(t *testing.T) {
	c := collectorTest(t, contentConfig(t), Options{})
	for i := 0; i < MaxContentFiles+1; i++ {
		_, err := c.ObserveMetadata(File{fmt.Sprintf("%03d.xml", i), 0})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < MaxContentFiles; i++ {
		if err := c.ObserveContent(File{fmt.Sprintf("%03d.xml", i), 0}, nil); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := json.Marshal(c.report)
	if err := c.ObserveContent(File{"256.xml", 0}, nil); !errors.Is(err, ErrSequence) {
		t.Fatal("257th content admitted", err)
	}
	after, _ := json.Marshal(c.report)
	if !bytes.Equal(before, after) {
		t.Fatal("failed admission changed report")
	}
	if err := c.OmitContent(File{"256.xml", 0}, ContentBudget); err != nil {
		t.Fatal(err)
	}
	if err := c.ObserveContent(File{"000.xml", 0}, nil); !errors.Is(err, ErrSequence) {
		t.Fatal("duplicate admission accepted", err)
	}
	r := finishTest(t, c)
	if r.ContentAdmittedFiles != 256 || r.ContentEvaluatedFiles != 256 || r.ContentOmittedFiles != 1 || len(c.admitted) != 256 {
		t.Fatal("wrong cap accounting")
	}
}
func TestCollectorSnapshotIsolationAndSeal(t *testing.T) {
	c := collectorTest(t, contentConfig(t), Options{})
	_, _ = c.ObserveMetadata(File{"a.xml", 0})
	_ = c.OmitContent(File{"a.xml", 0}, ContentBudget)
	one := finishTest(t, c)
	want, _ := json.Marshal(one)
	one.Rules[0].ID = "changed"
	one.Observations[0].RuleID = "changed"
	one.Omissions[ContentBudget] = 10
	two := finishTest(t, c)
	got, _ := json.Marshal(two)
	if !bytes.Equal(want, got) {
		t.Fatal("snapshot aliases collector")
	}
	if _, err := c.ObserveMetadata(File{"b.xml", 0}); !errors.Is(err, ErrSequence) {
		t.Fatal(err)
	}
	if err := c.Omit(TreeSizeLimit, 1); !errors.Is(err, ErrSequence) {
		t.Fatal(err)
	}
}
func TestCollectorScopeStatusAndContext(t *testing.T) {
	p := compileTest(t, simpleConfig)
	for _, source := range []Source{{Kind: "directory", Tree: "a"}, {Kind: "git"}, {Kind: "git", Tree: "ABCDEF0123456789012345678901234567890123"}, {Kind: "other"}} {
		if _, err := New(p, source, Options{}); !errors.Is(err, ErrContext) {
			t.Fatalf("accepted source %+v", source)
		}
	}
	if _, err := New(p, Source{Kind: "git", Tree: strings.Repeat("a", 1<<20)}, Options{}); !errors.Is(err, ErrContext) {
		t.Fatal("unbounded tree context accepted")
	}
	if _, err := New(nil, Source{Kind: "directory"}, Options{}); !errors.Is(err, ErrContext) {
		t.Fatal(err)
	}
	if _, err := New(p, Source{Kind: "directory"}, Options{MaxFileBytes: -1}); !errors.Is(err, ErrContext) {
		t.Fatal(err)
	}
	git, err := New(p, Source{Kind: "git", Tree: "0123456789012345678901234567890123456789"}, Options{})
	if err != nil || finishTest(t, git).SourceConsistency != "selected_git_tree" {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		reason    OmissionReason
		inventory bool
		status    string
	}{{NonRegularFile, false, "complete"}, {UnsupportedPath, false, "partial"}, {TreeSizeLimit, false, "skipped"}, {TreeSizeLimit, true, "partial"}} {
		c := collectorTest(t, p, Options{})
		if tt.inventory {
			_, _ = c.ObserveMetadata(File{"README", 0})
		}
		if err := c.Omit(tt.reason, 1); err != nil {
			t.Fatal(err)
		}
		if r := finishTest(t, c); r.Status != tt.status {
			t.Fatalf("%+v status %s", tt, r.Status)
		}
	}
	if r := finishTest(t, collectorTest(t, p, Options{})); r.Status != "complete" || r.InventoryFiles != 0 {
		t.Fatal("empty successful inventory not complete")
	}
}
func TestCollectorRejectsInvalidSequences(t *testing.T) {
	p := contentConfig(t)
	c := collectorTest(t, p, Options{})
	if err := c.ObserveContent(File{"a.xml", 0}, nil); !errors.Is(err, ErrSequence) {
		t.Fatal("content without metadata accepted", err)
	}
	if err := c.OmitContent(File{"a.xml", 0}, ContentBudget); !errors.Is(err, ErrSequence) {
		t.Fatal("omission without metadata accepted", err)
	}
	if err := c.Omit(ContentBudget, 1); !errors.Is(err, ErrContext) {
		t.Fatal("unassociated content omission accepted")
	}
	_, _ = c.ObserveMetadata(File{"a.xml", 0})
	if err := c.OmitContent(File{"a.xml", 0}, FileTooLarge); !errors.Is(err, ErrContext) {
		t.Fatal("invalid size omission accepted")
	}
	if err := c.OmitContent(File{"a.xml", 0}, InvalidUTF8); err != nil {
		t.Fatal(err)
	}
	if err := c.OmitContent(File{"a.xml", 0}, InvalidUTF8); !errors.Is(err, ErrSequence) {
		t.Fatal("duplicate read omission accepted")
	}
}
func TestCollectorOverflowIsTransactional(t *testing.T) {
	p := contentConfig(t)
	c := collectorTest(t, p, Options{})
	c.report.InventoryFiles = math.MaxUint64
	before, _ := json.Marshal(c.report)
	if _, err := c.ObserveMetadata(File{"a.xml", 0}); !errors.Is(err, ErrCounter) {
		t.Fatal(err)
	}
	after, _ := json.Marshal(c.report)
	if !bytes.Equal(before, after) || len(c.evidence) != 0 {
		t.Fatal("overflow mutated collector")
	}
	c = collectorTest(t, p, Options{})
	c.report.TotalMatches = math.MaxUint64
	if _, err := c.ObserveMetadata(File{"a.xml", 0}); !errors.Is(err, ErrCounter) || c.report.InventoryFiles != 0 {
		t.Fatal("total overflow mutated inventory")
	}
	c = collectorTest(t, p, Options{})
	c.report.Rules[1].CandidateFiles = math.MaxUint64
	if _, err := c.ObserveMetadata(File{"a.xml", 0}); !errors.Is(err, ErrCounter) || c.report.InventoryFiles != 0 {
		t.Fatal("per-rule overflow mutated inventory")
	}
	c = collectorTest(t, p, Options{})
	_ = c.Omit(TreeSizeLimit, math.MaxUint64)
	if err := c.Omit(TreeSizeLimit, 1); !errors.Is(err, ErrCounter) || c.report.Omissions[TreeSizeLimit] != math.MaxUint64 {
		t.Fatal("omission overflow")
	}
	c = collectorTest(t, p, Options{})
	_, _ = c.ObserveMetadata(File{"a.xml", 9})
	c.report.TotalMatches = math.MaxUint64
	if err := c.ObserveContent(File{"a.xml", 9}, []byte("<project>")); !errors.Is(err, ErrCounter) || c.report.ContentAdmittedFiles != 0 {
		t.Fatal("content overflow mutated admission")
	}
}
func BenchmarkCollectorUnmatched(b *testing.B) {
	p := compileTest(b, simpleConfig)
	c := collectorTest(b, p, Options{})
	file := File{"main.go", 1024}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.ObserveMetadata(file)
	}
}

// The scanner may discover paths in any order and evict an earlier candidate
// when a lexically smaller path arrives. Content eligibility must be independent
// of arrival order, and omitted candidates must retain per-rule coverage counts.
func TestStreamingLexicalAdmission(t *testing.T) {
	p := compileTest(t, `{"schema_version":"1.0.0","rules":[{"id":"all","match":{"extensions":[".xml"]}},{"id":"a-content","match":{"extensions":[".xml"],"path_prefixes":["a/"]},"content":{"contains_utf8":"yes"}},{"id":"both-content","match":{"extensions":[".xml"]},"content":{"contains_utf8":"yes"}}]}`)
	var expected []byte
	for seed := int64(0); seed < 5; seed++ {
		c := collectorTest(t, p, Options{})
		selected := []File{}
		for _, index := range rand.New(rand.NewSource(seed)).Perm(600) {
			prefix := "a"
			if index%2 != 0 {
				prefix = "b"
			}
			file := File{Path: fmt.Sprintf("%s/%04d.xml", prefix, index), Size: 3}
			if index%17 == 0 {
				file.Size = MaxContentBytes + 1
			}
			eligible, err := c.ObserveMetadata(file)
			if err != nil {
				t.Fatal(err)
			}
			if !eligible {
				continue
			}
			selected = append(selected, file)
			sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
			if len(selected) > MaxContentFiles {
				evicted := selected[len(selected)-1]
				selected = selected[:len(selected)-1]
				if err := c.OmitContent(evicted, ContentBudget); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, file := range selected {
			if err := c.ObserveContent(file, []byte("yes")); err != nil {
				t.Fatal(err)
			}
		}
		report := finishTest(t, c)
		if report.ContentAdmittedFiles != 256 || report.ContentEvaluatedFiles != 256 || report.ContentCandidateFiles != 600 || report.ContentOmittedFiles != 344 || report.Omissions[NotEvaluated] != 0 {
			t.Fatalf("bad coverage %+v", report)
		}
		for _, r := range report.Rules {
			if r.RequiresContent && r.CandidateFiles != r.EvaluatedFiles+r.ContentOmittedFiles {
				t.Fatalf("broken per-rule conservation %+v", r)
			}
		}
		actual, _ := json.Marshal(report)
		if seed == 0 {
			expected = actual
		} else if !bytes.Equal(expected, actual) {
			t.Fatal("streaming lexical admission changed final report")
		}
	}
}
