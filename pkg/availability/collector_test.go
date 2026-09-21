package availability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func file(path, content, attribute string, reads *int) File {
	return File{Path: path, Size: int64(len(content)), LFSAttribute: attribute, Read: func(_ context.Context, limit int64) ([]byte, int64, error) {
		if reads != nil {
			*reads++
		}
		data := []byte(content)
		return slices.Clone(data[:min(int64(len(data)), limit)]), int64(len(data)), nil
	}}
}

func TestCollectorSeparatesBoundariesAndQualifiesReferences(t *testing.T) {
	pointer := "version " + currentLFSVersion + "\noid sha256:" + testOID + "\nsize 5000\n"
	c := New(Options{Source: Source{Mode: "git", Tree: strings.Repeat("1", 40)}})
	c.AddFile(file("assets/model.bin", pointer, "tracked", nil))
	c.AddFile(file("assets/hydrated.bin", strings.Repeat("z", 2048), "tracked", nil))
	c.AddFile(file("notes/pointer.txt", "version "+currentLFSVersion+"\noid no\nsize nope\n", "untracked", nil))
	c.AddGitlink(Gitlink{Path: "vendor/lib", Commit: strings.Repeat("a", 40)})
	c.AddSubmoduleDeclaration(SubmoduleDeclaration{Path: "vendor/lib", Evidence: ".gitmodules"})
	c.AddSparseIndication(SparseIndication{Kind: "skip_worktree", Path: "cold", Evidence: ".git/index", Supported: true})
	c.AddMissingReference(MissingReference{Project: "app/app.csproj", Kind: "project-reference", Target: "assets/model.bin", Evidence: "app/app.csproj"})
	c.AddMissingReference(MissingReference{Project: "app/app.csproj", Kind: "project-reference", Target: "vendor/lib/src/lib.csproj", Evidence: "app/app.csproj"})
	c.AddMissingReference(MissingReference{Project: "app/app.csproj", Kind: "project-reference", Target: "cold/tool/tool.csproj", Evidence: "app/app.csproj"})
	c.AddMissingReference(MissingReference{Project: "app/app.csproj", Kind: "project-reference", Target: "generated/missing.csproj", Evidence: "app/app.csproj"})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || report.Counts.ValidPointers != 1 || report.Counts.PointerLikeFiles != 1 || report.Counts.LFSAttributedNonPointerFiles != 1 {
		t.Fatalf("counts/status: %+v %+v", report.Counts, report.Coverage)
	}
	if len(report.LFS) != 2 || report.LFS[0].OIDDigest != testOID || report.LFS[0].SourceFileBytes == report.LFS[0].DeclaredObjectBytes {
		t.Fatalf("LFS evidence/source binding: %+v", report.LFS)
	}
	if !report.Gitlinks[0].Declared || !report.Submodules[0].HasGitlink || report.Counts.MatchedSubmodules != 1 {
		t.Fatalf("submodule correlation: %+v %+v", report.Gitlinks, report.Submodules)
	}
	want := map[string]string{"assets/model.bin": "lfs_pointer", "vendor/lib/src/lib.csproj": "gitlink", "cold/tool/tool.csproj": "skip_worktree", "generated/missing.csproj": ""}
	for _, ref := range report.References {
		if ref.BoundaryKind != want[ref.Target] {
			t.Errorf("%s boundary %q, want %q", ref.Target, ref.BoundaryKind, want[ref.Target])
		}
		if ref.Target == "generated/missing.csproj" && ref.Qualification != "unqualified" {
			t.Errorf("speculative qualification: %+v", ref)
		}
	}
}

func TestCollectorLexicalInspectionBudgetIsArrivalIndependent(t *testing.T) {
	build := func(order []string) (*Report, map[string]int) {
		reads := map[string]int{}
		c := New(Options{ContentBytes: 2 * PointerSizeCutoff, EvidenceLimit: 8})
		for _, name := range order {
			counter := 0
			reads[name] = counter
			content := "version " + currentLFSVersion + "\noid sha256:" + testOID + "\nsize 1\n"
			f := file(name, content, "unknown", nil)
			f.Read = func(_ context.Context, limit int64) ([]byte, int64, error) {
				reads[name]++
				data := []byte(content)
				return data[:min(int64(len(data)), limit)], int64(len(data)), nil
			}
			c.AddFile(f)
		}
		report, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return report, reads
	}
	a, readsA := build([]string{"z.bin", "a.bin", "m.bin"})
	b, readsB := build([]string{"m.bin", "z.bin", "a.bin"})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("arrival-dependent reports:\n%s\n%s", ja, jb)
	}
	for _, reads := range []map[string]int{readsA, readsB} {
		if reads["a.bin"] != 1 || reads["m.bin"] != 1 || reads["z.bin"] != 0 {
			t.Fatalf("reads: %v", reads)
		}
	}
	if a.Coverage.OmittedPointerFiles != 1 || a.Status != "partial" || a.Coverage.ContentBytesRead > a.Bounds.ContentBytes {
		t.Fatalf("budget coverage: %+v", a.Coverage)
	}
}

func TestIncompleteInventoryMakesUncorrelatedAbsenceUnknown(t *testing.T) {
	c := New(Options{})
	c.MarkInventoryIncomplete("tree_size_limit")
	c.AddMissingReference(MissingReference{Project: "p", Kind: "local", Target: "missing/file", Evidence: "project.json"})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.SelectedInventoryComplete || report.References[0].Qualification != "unknown" {
		t.Fatalf("report: %+v", report)
	}
}

func TestCollectorPreservesReadErrorsAndCancellation(t *testing.T) {
	c := New(Options{})
	c.AddFile(File{Path: "bad.bin", Size: 10, LFSAttribute: "unknown", Read: func(context.Context, int64) ([]byte, int64, error) { return nil, 0, errors.New("missing object") }})
	if _, err := c.Finish(context.Background()); err == nil || !strings.Contains(err.Error(), "missing object") {
		t.Fatalf("error: %v", err)
	}
	c = New(Options{})
	c.AddFile(file("ok.bin", "hello", "unknown", nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Finish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCorrelationWorkAndOutputAreBounded(t *testing.T) {
	c := New(Options{CorrelationWork: 2, OutputBytes: 1800, EvidenceLimit: 50})
	for i := 0; i < 20; i++ {
		c.AddGitlink(Gitlink{Path: fmt.Sprintf("vendor/%02d", i), Commit: strings.Repeat("a", 40)})
		c.AddMissingReference(MissingReference{Project: "project", Kind: "local", Target: fmt.Sprintf("missing/%02d", i), Evidence: "manifest.json"})
	}
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	if report.Coverage.CorrelationWork > 3 || report.Coverage.OmittedCorrelations == 0 || len(encoded) > report.Bounds.OutputBytes {
		t.Fatalf("bounds: bytes=%d coverage=%+v", len(encoded), report.Coverage)
	}
	if report.Status != "partial" || report.Omissions["output_limit"] == 0 {
		t.Fatalf("omissions: %+v", report.Omissions)
	}
}

func TestCollectorClampsBoundsAndIgnoresLateAdds(t *testing.T) {
	c := New(Options{ContentBytes: 1 << 40, EvidenceLimit: 1 << 30, BoundaryPaths: 1 << 30, CorrelationWork: 1 << 30, OutputBytes: 1 << 30, StringBytes: 1 << 30})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Bounds.ContentBytes != DefaultContentBytes || report.Bounds.EvidencePerKind != DefaultEvidenceLimit || report.Bounds.BoundaryPaths != DefaultBoundaryPaths || report.Bounds.CorrelationWork != DefaultCorrelationWork || report.Bounds.OutputBytes != DefaultOutputBytes || report.Bounds.StringBytes != DefaultStringBytes {
		t.Fatalf("unclamped bounds: %+v", report.Bounds)
	}
	c.AddGitlink(Gitlink{Path: "late", Commit: strings.Repeat("a", 40)})
	c.AddFile(file("late.bin", "late", "tracked", nil))
	again, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Counts.Gitlinks != 0 || again.Coverage.SelectedRegularFiles != 0 {
		t.Fatalf("late mutation changed report: %+v", again)
	}
}

func TestCollectorRejectsCrossPlatformAmbiguousPaths(t *testing.T) {
	c := New(Options{})
	c.AddGitlink(Gitlink{Path: `vendor\lib`, Commit: strings.Repeat("a", 40)})
	c.AddMissingReference(MissingReference{Project: "p", Kind: "local", Target: "C:target", Evidence: "manifest"})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "partial" || len(report.Gitlinks) != 0 || len(report.References) != 0 || len(report.Diagnostics) != 2 {
		t.Fatalf("ambiguous path accepted: %+v", report)
	}
}

func TestMaxFileBytesOmitsReadButPreservesInventoryCounts(t *testing.T) {
	reads := 0
	c := New(Options{MaxFileBytes: 8})
	c.AddFile(File{Path: "large.bin", Size: 9, LFSAttribute: "tracked", Read: func(context.Context, int64) ([]byte, int64, error) {
		reads++
		return nil, 9, nil
	}})
	report, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reads != 0 || report.Coverage.SelectedRegularFiles != 1 || report.Coverage.PointerCandidates != 1 || report.Coverage.OmittedPointerFiles != 1 || report.Counts.LFSTrackedFiles != 1 || report.Omissions["file_size_limit"] != 1 {
		t.Fatalf("max file coverage: reads=%d report=%+v", reads, report)
	}
}
