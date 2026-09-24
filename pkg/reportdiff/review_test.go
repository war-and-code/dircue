package reportdiff_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
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
	if m.Counts.Unavailable != reportdiff.MaxChanges+5 || m.Counts.OmittedChanges == 0 {
		t.Fatalf("unavailable counts: %+v", m.Counts)
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

func TestReviewModuleOnlyDoesNotClaimLegacyRemoval(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{"main.go": "package main\nfunc main() {}\n", "go.mod": "module example.test/demo\n\ngo 1.26\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) *reportdiff.Snapshot {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--source", "directory", "--json", root)
		if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v %s", args, err, &stderr)
		}
		snapshot, err := reportdiff.Load(&out)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	full := run("analyze", "all", "--declarations")
	for _, mode := range []string{"declarations", "discovery", "registries"} {
		t.Run(mode, func(t *testing.T) {
			limited := run("analyze", mode)
			for _, pair := range [][2]*reportdiff.Snapshot{{full, limited}, {limited, full}} {
				result, err := reportdiff.Compare(pair[0], pair[1])
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"languages", "ecosystems"} {
					m := reviewModule(t, result, name)
					if m.Counts.Removed != 0 || m.Counts.Added != 0 || m.Counts.Unavailable == 0 {
						t.Fatalf("%s claimed absence for an unexecuted profiler: %+v", name, m)
					}
				}
			}
		})
	}
}

func reviewNumericReport(t testing.TB, number string) []byte {
	t.Helper()
	p := reviewProfile(t, 0)
	p.Languages = []profile.Language{{Name: "Go", Bytes: 1, FileCount: 1, Percentage: 79}}
	p.Summary.LanguageBytes = 1
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Replace(encoded, []byte(`"percentage":79`), []byte(`"percentage":`+number), 1)
}

func TestReviewNumericBoundFreshProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReviewNumericBoundChild$", "-test.timeout=10s")
	command.Env = append(os.Environ(), "DIRCUE_COMPARE_NUMERIC_CHILD=1", "GOMEMLIMIT=64MiB")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("numeric probe failed: %v\n%s", err, output)
	}
}

func TestReviewNumericBoundChild(t *testing.T) {
	if os.Getenv("DIRCUE_COMPARE_NUMERIC_CHILD") != "1" {
		t.Skip("isolated numeric probe")
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, number := range []string{"1e-10000001", "0e-10000001", "-0e-10000001", "1e10000001", "0e10000001", "1e-1025", "0e+1025", "1e-1024", "1e-325", "1e309", "1e-" + strings.Repeat("9", 100), "0." + strings.Repeat("0", 200) + "1", strings.Repeat("9", 200)} {
		for i := 0; i < 20; i++ {
			if _, err := reportdiff.Load(bytes.NewReader(reviewNumericReport(t, number))); err == nil {
				t.Fatalf("accepted unrepresentable or unbounded number %q", number)
			}
		}
	}
	for _, number := range []string{"0", "-0", "0e-1024", "0e1024", "5e-324", "1e-300", "1.25"} {
		if _, err := reportdiff.Load(bytes.NewReader(reviewNumericReport(t, number))); err != nil {
			t.Fatalf("rejected bounded representable %s: %v", number, err)
		}
	}
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > 64<<20 {
		t.Fatalf("tiny numeric cases allocated %d bytes", after.TotalAlloc-before.TotalAlloc)
	}
}

func FuzzReviewNumbers(f *testing.F) {
	for _, bits := range []uint64{0, 1, 2, math.Float64bits(1e-300), math.Float64bits(1), math.Float64bits(99.9)} {
		f.Add(bits, int32(-10000001))
	}
	f.Fuzz(func(t *testing.T, bits uint64, exponent int32) {
		number := math.Float64frombits(bits)
		if !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 && number <= 100 {
			text := strconv.FormatFloat(number, 'g', -1, 64)
			if _, err := reportdiff.Load(bytes.NewReader(reviewNumericReport(t, text))); err != nil {
				t.Fatalf("producer float %s rejected: %v", text, err)
			}
		}
		for _, coefficient := range []string{"0", "1"} {
			text := coefficient + "e" + strconv.FormatInt(int64(exponent), 10)
			_, err := reportdiff.Load(bytes.NewReader(reviewNumericReport(t, text)))
			if (exponent < -reportdiff.MaxNumberExponent || exponent > reportdiff.MaxNumberExponent) && err == nil {
				t.Fatalf("out-of-bounds exponent accepted: %s", text)
			}
		}
	})
}
