package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
	git "github.com/war-and-code/dircue/third_party/go-git"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/object"
)

func TestGitObjectCacheBudgetPreservesReport(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{
		"go.mod":  "module example.test/cache\n\ngo 1.24\n",
		"main.go": "package main\nfunc main() {}\n",
	})
	baseline, err := Scan(context.Background(), root, Options{Source: "git", Workers: 2, Discovery: true, Declarations: true})
	if err != nil {
		t.Fatal(err)
	}
	budgeted, err := Scan(context.Background(), root, Options{Source: "git", Workers: 2, Discovery: true, Declarations: true, GitObjectCacheBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(budgeted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("retained-object cache budget changed Git scan answer\nbase=%s\nbudgeted=%s", a, b)
	}
}

func packedCacheFixture(t *testing.T) (string, []string, map[string]string, func(...string) string) {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed for packed cache fixture")
	}
	files := map[string]string{}
	names := []string{}
	for i := 0; i < maxGitPackDescriptorsPerConcurrentLane+3; i++ {
		name := fmt.Sprintf("src/file%02d.py", i)
		names = append(names, name)
		files[name] = fmt.Sprintf("# fixture %d\n", i) + strings.Repeat("print('bounded cached pack')\n", 6000)
	}
	root, _, head := gitFixture(t, files)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(root, ".git")
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		t.Fatalf("fixture Git directory unavailable: %v", err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"--git-dir=" + gitDir, "--work-tree=" + root}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	if top := run("rev-parse", "--show-toplevel"); filepath.Clean(filepath.FromSlash(top)) != filepath.Clean(resolved) {
		t.Fatalf("fixture preflight top=%q want=%q", top, resolved)
	}
	if got := run("rev-parse", "HEAD"); got != head.String() {
		t.Fatalf("fixture preflight HEAD=%q want=%q", got, head)
	}
	for _, name := range names {
		hash := run("rev-parse", "HEAD:"+name)
		cmd := exec.Command(binary, "--git-dir="+gitDir, "--work-tree="+root, "pack-objects", "--window=0", filepath.Join(root, ".git", "objects", "pack", "pack"))
		cmd.Stdin = strings.NewReader(hash + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pack one object: %v: %s", err, out)
		}
	}
	run("prune-packed")
	packs, err := filepath.Glob(filepath.Join(root, ".git", "objects", "pack", "*.pack"))
	if err != nil || len(packs) <= maxGitPackDescriptorsPerConcurrentLane {
		t.Fatalf("want more than cache capacity packs: %d, %v", len(packs), err)
	}
	return root, names, files, run
}

func TestGitPackCacheEvictionRetainsLazyReaderAndReport(t *testing.T) {
	root, names, files, run := packedCacheFixture(t)
	snapshot, _, err := openGitSnapshot(context.Background(), root, Options{Source: "git", MaxTreeSize: 100000}, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	first, err := snapshot.findEntry(names[0])
	if err != nil {
		t.Fatal(err)
	}
	lazy, err := object.GetBlob(snapshot.storage, first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names[1:] {
		entry, err := snapshot.findEntry(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = object.GetBlob(snapshot.storage, entry.Hash); err != nil {
			t.Fatal(err)
		}
	}
	got, size, err := blobRead(lazy, names[0], ClassificationBytes)
	if err != nil || size != int64(len(files[names[0]])) || !bytes.Equal(got, []byte(files[names[0]])[:ClassificationBytes]) {
		t.Fatalf("lazy reader after eviction: bytes=%d size=%d error=%v", len(got), size, err)
	}
	if err := snapshot.close(); err != nil {
		t.Fatal(err)
	}
	expected, err := Scan(context.Background(), root, Options{Source: "directory", Workers: 1, IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		t.Helper()
		for _, workers := range []int{1, 8, 16} {
			actual, err := Scan(context.Background(), path, Options{Source: "git", Workers: workers, IncludeFiles: true})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual.Languages, expected.Languages) {
				t.Fatalf("packed language mismatch at workers=%d", workers)
			}
		}
	}
	check(root)
	linked := filepath.Join(t.TempDir(), "linked")
	run("worktree", "add", "--detach", linked, "HEAD")
	check(linked)
	alternate := filepath.Join(t.TempDir(), "alternate")
	alternateRepo, err := git.PlainInit(alternate, false)
	if err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(alternate, ".git", "objects", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	// The maintained go-git alternate policy confines its filesystem root.
	// Exercise a supported in-bound alternate; do not expand that policy here.
	if err := os.CopyFS(filepath.Join(alternate, ".git", "alt", "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "alternates"), []byte("alt/objects\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := alternateRepo.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash(run("rev-parse", "HEAD")))); err != nil {
		t.Fatal(err)
	}
	if ref, err := alternateRepo.Head(); err != nil {
		t.Fatal("alternate HEAD:", err)
	} else if _, err := alternateRepo.CommitObject(ref.Hash()); err != nil {
		t.Fatal("alternate commit:", err)
	}
	// Alternate object decoding is supported, but this pinned go-git version's
	// EncodedObjectSize does not search alternates. Preserve that limitation.
	alternateSnapshot, _, err := openGitSnapshot(context.Background(), alternate, Options{Source: "git"}, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer alternateSnapshot.close()
	entry, err := alternateSnapshot.findEntry(names[0])
	if err != nil {
		t.Fatal(err)
	}
	alternateBlob, err := object.GetBlob(alternateSnapshot.storage, entry.Hash)
	if err != nil {
		t.Fatal(err)
	}
	alternateContent, _, err := blobRead(alternateBlob, names[0], ClassificationBytes)
	if err != nil || !bytes.Equal(alternateContent, []byte(files[names[0]])[:ClassificationBytes]) {
		t.Fatalf("alternate reader: %v", err)
	}
	if _, err := Scan(context.Background(), alternate, Options{Source: "git"}); !errors.Is(err, plumbing.ErrObjectNotFound) {
		t.Fatalf("alternate size limitation changed: %v", err)
	}
}

func TestGitReadMetricsOptInPreservesReport(t *testing.T) {
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed for packed metrics fixture")
	}
	root, _, _ := gitFixture(t, map[string]string{
		"src/main.py": strings.Repeat("print('instrumented packed object')\n", 12000),
	})
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-C", root, "rev-parse", "--show-toplevel")
	top, err := cmd.Output()
	if err != nil || filepath.Clean(filepath.FromSlash(strings.TrimSpace(string(top)))) != filepath.Clean(resolved) {
		t.Fatalf("fixture preflight top=%q error=%v", strings.TrimSpace(string(top)), err)
	}
	cmd = exec.Command(binary, "--git-dir="+filepath.Join(root, ".git"), "--work-tree="+root, "repack", "-a", "-d")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pack fixture: %v: %s", err, output)
	}

	plain, err := Scan(context.Background(), root, Options{Source: "git", Workers: 2, IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	metrics := &GitReadMetrics{}
	measured, err := Scan(context.Background(), root, Options{Source: "git", Workers: 2, IncludeFiles: true, GitReadMetrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(measured, plain) {
		t.Fatal("enabling Git read metrics changed the report")
	}
	snapshot := metrics.Snapshot()
	if snapshot.PackBytesRead == 0 || snapshot.IndexBytesRead == 0 || snapshot.InflatersStarted == 0 {
		t.Fatalf("missing packed read counters: %+v", snapshot)
	}
	if snapshot.ActiveDeltaReaders != 0 {
		t.Fatalf("scan returned with active delta readers: %+v", snapshot)
	}
	if !metrics.Reset() || metrics.Snapshot() != (GitReadMetricsSnapshot{}) {
		t.Fatalf("reset did not clear completed scan: %+v", metrics.Snapshot())
	}
}

func countOpenDescriptors(t *testing.T) int {
	t.Helper()
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		if stream, err := os.Open(dir); err == nil {
			entries, readErr := stream.Readdirnames(-1)
			_ = stream.Close()
			if readErr == nil {
				return len(entries)
			}
		}
	}
	t.Skip("descriptor enumeration unavailable on this platform")
	return 0
}

func TestGitSnapshotClosesLanesWhenAttributeRootFails(t *testing.T) {
	root, _, _, _ := packedCacheFixture(t)
	sentinel := errors.New("attribute root fault")
	before := countOpenDescriptors(t)
	for i := 0; i < 12; i++ {
		snapshot, _, err := openGitSnapshotWithAttributeRoot(context.Background(), root, Options{Source: "git"}, false, 1, func(string) (*os.Root, error) {
			return nil, sentinel
		})
		if snapshot != nil || !errors.Is(err, sentinel) {
			t.Fatalf("run %d snapshot=%v error=%v", i, snapshot, err)
		}
	}
	after := countOpenDescriptors(t)
	if after > before+3 {
		t.Fatalf("failed snapshot retained descriptors: before=%d after=%d", before, after)
	}
}

type cancelCacheDetector struct{ cancel context.CancelFunc }

func (cancelCacheDetector) Name() string { return "cancel-cache-test" }
func (d cancelCacheDetector) Detect(ctx context.Context, _ profile.File) ([]profile.Finding, error) {
	d.cancel()
	return nil, ctx.Err()
}

func TestGitPackCacheClosesAcrossSuccessErrorsAndCancellation(t *testing.T) {
	root, names, _, run := packedCacheFixture(t)
	// Warm runtime and classifier state before counting the process's descriptors.
	if _, err := Scan(context.Background(), root, Options{Source: "git", Workers: 8}); err != nil {
		t.Fatal(err)
	}
	firstHash := plumbing.NewHash(run("rev-parse", "HEAD:"+names[0]))
	cases := []struct {
		name      string
		run       func() error
		wantError bool
	}{
		{"scan", func() error {
			_, err := Scan(context.Background(), root, Options{Source: "git", Workers: 8})
			return err
		}, false},
		{"discovery", func() error {
			_, err := Scan(context.Background(), root, Options{Source: "git", Discovery: true, DiscoveryOnly: true})
			return err
		}, false},
		{"explain", func() error {
			_, err := Scan(context.Background(), root, Options{Source: "git", ExplainPath: names[0]})
			return err
		}, false},
		{"availability", func() error {
			_, err := Scan(context.Background(), root, Options{Source: "git", Availability: true})
			return err
		}, false},
		{"inspect", func() error {
			_, err := Inspect(context.Background(), filepath.Join(root, names[0]), Options{Source: "git"})
			return err
		}, false},
		{"inspect-size-error", func() error {
			_, err := Inspect(context.Background(), filepath.Join(root, names[0]), Options{Source: "git", MaxFileBytes: 1})
			return err
		}, true},
		{"snapshot-wrong-object-type", func() error {
			snapshot, _, err := openGitSnapshot(context.Background(), root, Options{Source: "git", Tree: firstHash.String()}, false, 1)
			if snapshot != nil {
				defer snapshot.close()
			}
			return err
		}, true},
		{"cancel-after-read", func() error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := Scan(ctx, root, Options{Source: "git", Workers: 8, Detectors: []profile.Detector{cancelCacheDetector{cancel}}})
			return err
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := countOpenDescriptors(t)
			for i := 0; i < 12; i++ {
				err := tc.run()
				if (err != nil) != tc.wantError {
					t.Fatalf("run %d error=%v", i, err)
				}
			}
			after := countOpenDescriptors(t)
			if after > before+3 {
				t.Fatalf("retained descriptors: before=%d after=%d", before, after)
			}
		})
	}
}

type cacheCloseRecorder struct {
	calls int
	err   error
}

func (c *cacheCloseRecorder) Close() error { c.calls++; return c.err }
func TestGitSnapshotCloseTransfersOwnershipOnce(t *testing.T) {
	firstError := errors.New("first close failed")
	secondError := errors.New("second close failed")
	first := &cacheCloseRecorder{err: firstError}
	second := &cacheCloseRecorder{err: secondError}
	snapshot := &gitSnapshot{storages: []io.Closer{first, second}}
	if err := snapshot.close(); !errors.Is(err, firstError) || !errors.Is(err, secondError) {
		t.Fatalf("close error=%v", err)
	}
	if err := snapshot.close(); err != nil {
		t.Fatal(err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("close calls=(%d,%d)", first.calls, second.calls)
	}
	var absent *gitSnapshot
	if err := absent.close(); err != nil {
		t.Fatal(err)
	}
}
