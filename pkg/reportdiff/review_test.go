package reportdiff_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"dircue/internal/cli"
	"dircue/pkg/declarations"
	"dircue/pkg/discovery"
	"dircue/pkg/profile"
	"dircue/pkg/reportdiff"
)

func reviewProfile(t testing.TB, n int) profile.Report {
	t.Helper()
	p := profile.Report{SchemaVersion: "1.4.0", Root: "fixture", Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
	var err error
	p.Declarations, err = declarations.New("directory", "", 0).Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%03d/package.json", i)
		p.Declarations.Projects = append(p.Declarations.Projects, declarations.Project{ID: name, Root: fmt.Sprintf("p%03d", i), Kind: "npm", Name: fmt.Sprintf("p%03d", i), Version: "1.0.0", Requirements: []declarations.Requirement{{Kind: "npm-engine", Value: "node >=22", State: "declared", Evidence: name}}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{{Kind: "script", Name: "test", State: "declared", Evidence: name}}})
	}
	p.Declarations.Coverage.SelectedFiles = int64(n)
	p.Declarations.Coverage.ManifestCandidates = int64(n)
	p.Declarations.Coverage.ParsedManifests = n
	p.Declarations.Coverage.RetainedObservations = 2 * n
	return p
}
func reviewLoad(t testing.TB, p profile.Report) *reportdiff.Snapshot {
	t.Helper()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	s, e := reportdiff.Load(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func reviewCompare(t testing.TB, a, b profile.Report) *reportdiff.Report {
	t.Helper()
	r, e := reportdiff.Compare(reviewLoad(t, a), reviewLoad(t, b))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func reviewModule(t testing.TB, r *reportdiff.Report, name string) reportdiff.Module {
	t.Helper()
	for _, m := range r.Modules {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("module %s missing", name)
	return reportdiff.Module{}
}

func TestReviewEmptySupportedEcosystemsPreventsAbsenceClaims(t *testing.T) {
	a, b := reviewProfile(t, 1), reviewProfile(t, 0)
	a.Declarations.SupportedEcosystems = []string{}
	b.Declarations.SupportedEcosystems = []string{}
	m := reviewModule(t, reviewCompare(t, a, b), "declarations")
	if m.Compatibility != "observed_only" || m.Counts.Removed != 0 || m.Counts.Unavailable != 1 {
		t.Fatalf("absence claimed without ecosystem scope: %+v", m)
	}
}
func TestReviewDiscoveryEmptyVersionAndProviderPolicy(t *testing.T) {
	a, b := reviewProfile(t, 0), reviewProfile(t, 0)
	c := discovery.New("directory", "", 0)
	c.Add(discovery.File{Path: "package.json", Size: 2})
	a.Discovery, b.Discovery = c.Finish(), discovery.New("directory", "", 0).Finish()
	a.Discovery.RuleVersion = ""
	b.Discovery.RuleVersion = ""
	m := reviewModule(t, reviewCompare(t, a, b), "discovery")
	if m.Counts.Removed != 0 || m.Compatibility != "observed_only" {
		t.Fatalf("missing provenance removal: %+v", m)
	}
	b.Discovery.RuleVersion = "2.0.0"
	m = reviewModule(t, reviewCompare(t, a, b), "discovery")
	if m.Compatibility != "incomparable" || len(m.Changes) != 0 {
		t.Fatalf("policy mismatch compared as content: %+v", m)
	}
}

func TestReviewLongIDsValuesAndOutputCounts(t *testing.T) {
	a, b := reviewProfile(t, 0), reviewProfile(t, 0)
	a.Root = strings.Repeat("\x01", reportdiff.MaxStringBytes)
	b.Root = strings.Repeat("\x02", reportdiff.MaxStringBytes)
	for i := 0; i < reportdiff.MaxChanges+5; i++ {
		b.Languages = append(b.Languages, profile.Language{Name: fmt.Sprintf("%05d-%s", i, strings.Repeat("l", 1900)), Bytes: 1, Percentage: 0, FileCount: 1})
	}
	b.Summary.LanguageBytes = int64(len(b.Languages))
	r := reviewCompare(t, a, b)
	encoded, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if len(encoded) > reportdiff.MaxOutputBytes || r.Status != "partial" {
		t.Fatalf("output bound/status: %d %s", len(encoded), r.Status)
	}
	for _, m := range r.Modules {
		total := m.Counts.Added + m.Counts.Removed + m.Counts.Changed + m.Counts.Unavailable
		if total != len(m.Changes)+m.Counts.OmittedChanges {
			t.Fatalf("counts lost by byte cap: %s %+v retained=%d", m.Name, m.Counts, len(m.Changes))
		}
	}
	m := reviewModule(t, r, "languages")
	if m.Counts.Added != reportdiff.MaxChanges+5 || m.Counts.OmittedChanges == 0 {
		t.Fatalf("added counts: %+v", m.Counts)
	}
}

func TestReviewTextOutputEscapesControlIdentifiers(t *testing.T) {
	a, b := reviewProfile(t, 0), reviewProfile(t, 0)
	b.Languages = []profile.Language{{Name: "unsafe\x1b[2J\nname", Bytes: 1, Percentage: 100, FileCount: 1}}
	b.Summary.LanguageBytes = 1
	root := t.TempDir()
	for name, p := range map[string]profile.Report{"base.json": a, "head.json": b} {
		data, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(root, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	var out, errOut bytes.Buffer
	if e := cli.Execute(context.Background(), []string{"compare", filepath.Join(root, "base.json"), filepath.Join(root, "head.json")}, &out, &errOut); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out.Bytes(), []byte{0x1b}) || !strings.Contains(out.String(), `\x1b[2J\nname`) {
		t.Fatalf("unsafe control display: %q", out.String())
	}
}

func FuzzReviewStructuredComparison(f *testing.F) {
	for _, seed := range [][]byte{{0, 0, 0}, {1, 1, 0}, {5, 2, 1}, {7, 3, 0}, {9, 4, 0}, {9, 5, 1}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 {
			return
		}
		n := int(data[0]%12) + 1
		mode := data[1] % 6
		partial := data[2]%2 == 1
		a, b := reviewProfile(t, n), reviewProfile(t, n)
		switch mode {
		case 0:
			slices.Reverse(b.Declarations.Projects)
			slices.Reverse(b.Declarations.SupportedEcosystems)
		case 1:
			b.Declarations.Projects = b.Declarations.Projects[:n-1]
			b.Declarations.Coverage.SelectedFiles--
			b.Declarations.Coverage.ManifestCandidates--
			b.Declarations.Coverage.ParsedManifests--
			b.Declarations.Coverage.RetainedObservations -= 2
		case 2:
			b.Declarations.Projects[n-1].Version = "2.0.0"
		case 3:
			b.Declarations.Limits.ManifestBytes--
		case 4:
			b.Declarations.Projects = append(b.Declarations.Projects, b.Declarations.Projects[0])
		case 5:
			b.Declarations.SupportedEcosystems = []string{}
			a.Declarations.SupportedEcosystems = []string{}
		}
		if partial {
			b.Declarations.Status = "partial"
			b.Declarations.Coverage.OmittedFiles = 1
		}
		beforeA, _ := json.Marshal(a)
		beforeB, _ := json.Marshal(b)
		x, y := reviewLoad(t, a), reviewLoad(t, b)
		r, e := reportdiff.Compare(x, y)
		if e != nil {
			t.Fatal(e)
		}
		same, e := reportdiff.Compare(x, y)
		if e != nil {
			t.Fatal(e)
		}
		rj, _ := json.Marshal(r)
		sj, _ := json.Marshal(same)
		if !bytes.Equal(rj, sj) {
			t.Fatal("comparison was nondeterministic")
		}
		afterA, _ := json.Marshal(a)
		afterB, _ := json.Marshal(b)
		if !bytes.Equal(beforeA, afterA) || !bytes.Equal(beforeB, afterB) {
			t.Fatal("caller profile mutated")
		}
		m := reviewModule(t, r, "declarations")
		switch mode {
		case 0, 5:
			if m.Counts.Unchanged != n || len(m.Changes) != 0 {
				t.Fatalf("equivalent observations changed: %+v", m)
			}
		case 1:
			if partial {
				if m.Counts.Removed != 0 || m.Counts.Unavailable != 1 {
					t.Fatalf("partial absence: %+v", m)
				}
			} else if m.Counts.Removed != 1 {
				t.Fatalf("complete removal: %+v", m)
			}
		case 2:
			if m.Counts.Changed != 1 || m.Counts.Unchanged != n-1 {
				t.Fatalf("field delta: %+v", m)
			}
		case 3, 4:
			if m.Status != "incomparable" || len(m.Changes) != 0 {
				t.Fatalf("policy or duplicate identity accepted: %+v", m)
			}
		}
		reverse, e := reportdiff.Compare(y, x)
		if e != nil {
			t.Fatal(e)
		}
		rm := reviewModule(t, reverse, "declarations")
		if m.Counts.Added != rm.Counts.Removed || m.Counts.Removed != rm.Counts.Added || m.Counts.Changed != rm.Counts.Changed || m.Counts.Unavailable != rm.Counts.Unavailable {
			t.Fatalf("direction symmetry broken: %+v %+v", m.Counts, rm.Counts)
		}
	})
}

func TestReviewMetricsIdentityIncludesGrammar(t *testing.T) {
	build := func(first, second int64) profile.Report {
		p := reviewProfile(t, 0)
		counts := func(n int64) profile.Counts { return profile.Counts{Files: 1, Bytes: n, Lines: n, Code: n} }
		p.Metrics = &profile.MetricsReport{Engine: "scc", EngineVersion: "4.1.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: 1 << 20, Totals: profile.Counts{Files: 2, Bytes: 3, Lines: 3, Code: 3}, Languages: []profile.LanguageMetrics{{Language: "Example", Grammar: "grammar-a", Counts: counts(first)}, {Language: "Example", Grammar: "grammar-b", Counts: counts(second)}}, Directories: []profile.DirectoryMetrics{}, Skipped: []profile.MetricSkip{}}
		return p
	}
	a := build(1, 2)
	m := reviewModule(t, reviewCompare(t, a, a), "metrics")
	if m.Status != "unchanged" || m.Counts.Unchanged != 3 {
		t.Fatalf("distinct grammar groups collided: %+v", m)
	}
	m = reviewModule(t, reviewCompare(t, a, build(2, 1)), "metrics")
	if m.Counts.Changed != 2 || len(m.Changes) != 2 || m.Changes[0].ID == m.Changes[1].ID {
		t.Fatalf("grammar-specific deltas lost: %+v", m)
	}
}
