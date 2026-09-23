package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"dircue/pkg/profile"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func commitFixture(t *testing.T, root string, repo *git.Repository) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err = wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	hash, err := wt.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
func gitFixture(t *testing.T, files map[string]string) (string, *git.Repository, plumbing.Hash) {
	t.Helper()
	root := fixtures(t, files)
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, commitFixture(t, root, repo)
}
func languageFiles(report *profile.Report) map[string][]string {
	result := map[string][]string{}
	for _, language := range report.Languages {
		result[language.Name] = language.Files
	}
	return result
}
func TestGitSnapshotRevisionAndDirectorySource(t *testing.T) {
	root, repo, first := gitFixture(t, map[string]string{"src/main.go": goSource, ".gitattributes": "*.go linguist-language=Python\n"})
	if err := os.WriteFile(filepath.Join(root, "src/main.go"), []byte("const x = 42;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.go linguist-language=JavaScript\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.rb"), []byte("puts 42\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), root, Options{IncludeFiles: true, IncludeStrategies: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Root != root || !reflect.DeepEqual(languageFiles(report), map[string][]string{"Python": {"src/main.go"}}) {
		t.Fatalf("snapshot: %+v", report)
	}
	if got := report.Strategies["src/main.go"]; got != "Extension (overridden by .gitattributes)" {
		t.Fatalf("strategy: %q", got)
	}
	dir, err := Scan(context.Background(), root, Options{Source: "directory", IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(languageFiles(dir), map[string][]string{"JavaScript": {"src/main.go"}, "Ruby": {"untracked.rb"}}) {
		t.Fatalf("directory: %+v", dir.Languages)
	}
	subtree, err := Scan(context.Background(), filepath.Join(root, "src"), Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if subtree.Root != filepath.Join(root, "src") || len(subtree.Languages) != 1 || subtree.Languages[0].Name != "Go" {
		t.Fatalf("subdirectory escaped into parent snapshot: %+v", subtree)
	}
	if _, err := Scan(context.Background(), filepath.Join(root, "src"), Options{Source: "git"}); err == nil {
		t.Fatal("explicit Git subdirectory accepted")
	}
	commitFixture(t, root, repo)
	previous, err := Scan(context.Background(), root, Options{Revision: first.String(), IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Languages, report.Languages) {
		t.Fatalf("revision mismatch: %+v", previous.Languages)
	}
	inspected, err := Inspect(context.Background(), filepath.Join(root, "src/main.go"), Options{Revision: first.String()})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Language != "Python" || string(inspected.Content) != goSource {
		t.Fatalf("inspection: %+v", inspected)
	}
	if _, err := Scan(context.Background(), root, Options{Revision: "missing-revision"}); err == nil {
		t.Fatal("unknown revision accepted")
	}
	if _, err := Scan(context.Background(), root, Options{Revision: "HEAD:src/main.go"}); err == nil || !strings.Contains(err.Error(), "rev:path") {
		t.Fatalf("rev:path was not rejected: %v", err)
	}
	commit, err := repo.CommitObject(first)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	byTree, err := Scan(context.Background(), root, Options{Tree: tree.Hash.String(), IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byTree.Languages, report.Languages) {
		t.Fatalf("tree mismatch: %+v", byTree.Languages)
	}
	inspectedTree, err := Inspect(context.Background(), filepath.Join(root, "src/main.go"), Options{Tree: tree.Hash.String()})
	if err != nil || inspectedTree.Language != "Python" || string(inspectedTree.Content) != goSource {
		t.Fatalf("tree inspection: inspection=%+v err=%v", inspectedTree, err)
	}
	for _, opts := range []Options{{Tree: "abc"}, {Revision: "HEAD", Tree: tree.Hash.String()}, {Source: "directory", Tree: tree.Hash.String()}} {
		if _, err := Scan(context.Background(), root, opts); err == nil {
			t.Fatalf("invalid tree selection accepted: %+v", opts)
		}
		if _, err := Inspect(context.Background(), filepath.Join(root, "src/main.go"), opts); err == nil {
			t.Fatalf("invalid inspection tree selection accepted: %+v", opts)
		}
	}
	if _, err := Scan(context.Background(), root, Options{Source: "directory", Revision: "HEAD"}); err == nil {
		t.Fatal("directory revision accepted")
	}
	if _, err := Scan(context.Background(), filepath.Join(root, "missing"), Options{}); err == nil {
		t.Fatal("missing child scanned ancestor repository")
	}
}

func TestGitTreeBoundaryNestedAttributesAndSymlinks(t *testing.T) {
	root, repo, _ := gitFixture(t, map[string]string{".gitattributes": "*.go linguist-language=Python\n", "src/.gitattributes": "*.go !linguist-language\n", "src/main.go": goSource, "main.go": goSource})
	report, err := Scan(context.Background(), root, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"Go": {"src/main.go"}, "Python": {"main.go"}}
	if !reflect.DeepEqual(languageFiles(report), want) {
		t.Fatalf("attributes: %+v", report.Languages)
	}
	limited, err := Scan(context.Background(), root, Options{MaxTreeSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Languages) != 0 || len(limited.Warnings) != 1 || limited.Warnings[0].Code != "tree_size_limit" {
		t.Fatalf("limit: %+v", limited)
	}
	if err := os.Symlink("main.go", filepath.Join(root, "link.go")); err != nil {
		t.Skip(err)
	}
	commitFixture(t, root, repo)
	report, err = Scan(context.Background(), root, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(languageFiles(report), want) || report.Summary.SkippedFiles < 1 {
		t.Fatalf("symlink: %+v", report)
	}
}

func TestGitTreeLimitPreflightDoesNotReadBlobs(t *testing.T) {
	root, repo, hash := gitFixture(t, map[string]string{"a.go": goSource, "b.go": goSource})
	commit, err := repo.CommitObject(hash)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := tree.FindEntry("a.go")
	if err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(root, ".git", "objects", entry.Hash.String()[:2], entry.Hash.String()[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), root, Options{MaxTreeSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Languages) != 0 || len(report.Warnings) != 1 || report.Warnings[0].Code != "tree_size_limit" {
		t.Fatalf("tree limit read a blob or returned partial data: %+v", report)
	}
	if report, err := Scan(context.Background(), root, Options{MaxTreeSize: 3}); err == nil || report != nil {
		t.Fatalf("under-limit corrupt repository was accepted: report=%+v err=%v", report, err)
	}
}

func TestSourceSelectionUnbornAndInvalid(t *testing.T) {
	root := fixtures(t, map[string]string{"main.go": goSource})
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Languages) != 1 {
		t.Fatalf("unborn fallback: %+v", report)
	}
	if _, err := Scan(context.Background(), root, Options{Source: "git"}); err == nil {
		t.Fatal("explicit unborn Git accepted")
	}
	for _, opts := range []Options{{Source: "unknown"}, {MaxTreeSize: -1}, {Source: "git"}, {Revision: "HEAD"}} {
		if _, err := Scan(context.Background(), t.TempDir(), opts); err == nil {
			t.Fatalf("invalid options accepted %+v", opts)
		}
	}
}

func TestLinguistPoliciesAndBoundedLargeFiles(t *testing.T) {
	big := goSource + strings.Repeat("// source\n", 150000)
	files := map[string]string{"large.go": big, "empty.go": "", "docs/tutorial.go": goSource, ".hidden.go": goSource, "index.tsx": "export default function App() { return <div>Hello</div>; }\n", "manual/override.go": goSource, ".gitattributes": "manual/** -linguist-documentation\n"}
	root := fixtures(t, files)
	var observed int
	detector := detectorFunc(func(_ context.Context, file profile.File) ([]profile.Finding, error) {
		if file.Path == "large.go" {
			observed = len(file.Content)
			if file.Size != int64(len(big)) {
				return nil, errors.New("size truncated")
			}
		}
		return nil, nil
	})
	report, err := Scan(context.Background(), root, Options{Workers: 1, IncludeFiles: true, Detectors: []profile.Detector{detector}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings: %+v", report.Warnings)
	}
	if observed != int(maxAttributesBytes) {
		t.Fatalf("detector input unbounded: %d", observed)
	}
	for _, language := range report.Languages {
		if language.Name == "Go" && language.Bytes != int64(len(big)+2*len(goSource)) {
			t.Fatalf("full size accounting: %+v", language)
		}
	}
	want := map[string][]string{"Go": {".hidden.go", "large.go", "manual/override.go"}, "TypeScript": {"index.tsx"}}
	if !reflect.DeepEqual(languageFiles(report), want) {
		t.Fatalf("language grouping/policies: %+v", report.Languages)
	}
}

func TestGitInfoAttributesAndQuotedReferenceBehavior(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{".gitattributes": "*.go linguist-language=Python\n\"with space.go\" linguist-generated\n", "main.go": goSource, "with space.go": goSource})
	if err := os.MkdirAll(filepath.Join(root, ".git/info"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git/info/attributes"), []byte("main.go linguist-language=Ruby\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), root, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(languageFiles(report), map[string][]string{"Ruby": {"main.go"}, "Python": {"with space.go"}}) {
		t.Fatalf("info attrs: %+v", report.Languages)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "unsupported_gitattributes" {
		t.Fatalf("quoted warning: %+v", report.Warnings)
	}
	inspected, err := Inspect(context.Background(), filepath.Join(root, "main.go"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Language != "Ruby" {
		t.Fatalf("info inspection: %+v", inspected)
	}
}

func TestUnicodeBOMAndLFSPointer(t *testing.T) {
	utf16 := []byte{0xff, 0xfe}
	for _, c := range []byte("print('hello')\n") {
		utf16 = append(utf16, c, 0)
	}
	utf32 := []byte{0, 0, 0xfe, 0xff}
	for _, c := range []byte("print('hello')\n") {
		utf32 = append(utf32, 0, 0, 0, c)
	}
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:abcdef\nsize 123\n"
	root := fixtures(t, map[string]string{"utf16.py": string(utf16), "utf32.py": string(utf32), "lfs.py": pointer, "normal.py": pointer, ".gitattributes": "lfs.py filter=lfs\n"})
	report, err := Scan(context.Background(), root, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(languageFiles(report), map[string][]string{"Python": {"normal.py", "utf16.py", "utf32.py"}}) {
		t.Fatalf("encoding/lfs: %+v", report.Languages)
	}
	if report.Languages[0].Bytes != int64(len(utf16)+len(utf32)+len(pointer)) {
		t.Fatal("encoded byte accounting changed")
	}
}

// Loose-object fixtures do not exercise go-git's mutable pack index cache.
// Use native Git only to construct delta-compressed test data; production scans
// never invoke the Git executable.
func TestPackedDeltaRepositoryConcurrentWorkers(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed to construct packed test fixture")
	}
	files := map[string]string{}
	for i := 0; i < 128; i++ {
		files[fmt.Sprintf("src/file%03d.py", i)] = fmt.Sprintf("# revision %d\n", i) + strings.Repeat("print('shared delta base')\n", 400)
	}
	root, repo, _ := gitFixture(t, files)
	for filename, content := range files {
		if err := os.WriteFile(filepath.Join(root, filename), []byte(content+"print('new revision')\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commitFixture(t, root, repo)
	for _, args := range [][]string{{"repack", "-ad", "--depth=50", "--window=50"}, {"prune-packed"}} {
		cmd := exec.Command(gitBin, append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pack fixture: %v: %s", err, output)
		}
	}
	packs, err := filepath.Glob(filepath.Join(root, ".git/objects/pack/*.pack"))
	if err != nil || len(packs) == 0 {
		t.Fatal("fixture has no pack file")
	}
	var expected []profile.Language
	for _, workers := range []int{1, 16, 8, 16} {
		report, err := Scan(context.Background(), root, Options{Workers: workers, IncludeFiles: true})
		if err != nil {
			t.Fatal(err)
		}
		if expected == nil {
			expected = report.Languages
		} else if !reflect.DeepEqual(expected, report.Languages) {
			t.Fatalf("packed workers %d mismatch", workers)
		}
	}
}

func TestGitSnapshotAllowsControlCharactersInStoredNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows filesystems cannot construct every Git-valid control-character filename")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed to construct control-character tree fixture")
	}
	root := t.TempDir()
	names := []string{"tab\tname.py", "line\nbreak.py", "control-\x1b[31m.py"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("print('hello')\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "--all"}, {"commit", "-qm", "fixture"}} {
		cmd := exec.Command(gitBin, append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	report, err := Scan(context.Background(), root, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(names)
	slices.Sort(want)
	if got := languageFiles(report)["Python"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("control-character paths: %#v", got)
	}
	for _, name := range names {
		inspected, err := Inspect(context.Background(), filepath.Join(root, name), Options{})
		if err != nil {
			t.Fatalf("inspect %q: %v", name, err)
		}
		if inspected.Path != name || inspected.Language != "Python" {
			t.Fatalf("inspect %q: %+v", name, inspected)
		}
	}
}

func TestGitTreeComponentValidationKeepsStructuralBoundary(t *testing.T) {
	for _, name := range []string{"tab\tname.py", "line\nbreak.py", "control-\x1b.py", `literal\backslash.py`} {
		if err := validateGitTreeComponent(name); err != nil {
			t.Fatalf("valid Git name %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", ".git", "GIT~1", "a/b", "a\\..\\b", "nul\x00name", string([]byte{0xff})} {
		if err := validateGitTreeComponent(name); err == nil {
			t.Fatalf("unsafe Git name %q accepted", name)
		}
	}
}

func TestLinkedWorktreeCommonAttributesAndInspect(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed to construct linked worktree fixture")
	}
	root, _, hash := gitFixture(t, map[string]string{"main.go": goSource})
	if err := os.MkdirAll(filepath.Join(root, ".git/info"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git/info/attributes"), []byte("*.go linguist-language=Ruby\n"), 0644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	cmd := exec.Command(gitBin, "-C", root, "worktree", "add", "--detach", linked, hash.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree fixture: %v: %s", err, output)
	}
	report, err := Scan(context.Background(), linked, Options{IncludeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Root != linked || !reflect.DeepEqual(languageFiles(report), map[string][]string{"Ruby": {"main.go"}}) {
		t.Fatalf("linked: %+v", report)
	}
	inspected, err := Inspect(context.Background(), filepath.Join(linked, "main.go"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Language != "Ruby" || string(inspected.Content) != goSource {
		t.Fatalf("linked inspection: %+v", inspected)
	}
}

func TestGitInfoAttributesCannotEscapeMetadataRoot(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{"main.go": goSource})
	outside := fixtures(t, map[string]string{"attributes": "*.go linguist-language=Ruby\n"})
	if err := os.RemoveAll(filepath.Join(root, ".git/info")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".git/info")); err != nil {
		t.Skip(err)
	}
	if _, err := Scan(context.Background(), root, Options{}); err == nil {
		t.Fatal("external attribute directory followed")
	}
}

func TestInspectFileSizeLimit(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{"main.go": goSource})
	for _, source := range []string{"auto", "directory"} {
		for _, limit := range []int64{1, -1, 1<<63 - 1} {
			if _, err := Inspect(context.Background(), filepath.Join(root, "main.go"), Options{Source: source, MaxFileBytes: limit}); err == nil {
				t.Fatalf("source %s accepted limit %d", source, limit)
			}
		}
		inspected, err := Inspect(context.Background(), filepath.Join(root, "main.go"), Options{Source: source, MaxFileBytes: int64(len(goSource))})
		if err != nil {
			t.Fatal(err)
		}
		if inspected.Language != "Go" {
			t.Fatalf("boundary: %+v", inspected)
		}
	}
}

func TestGitAttributeRuleLimit(t *testing.T) {
	root, _, _ := gitFixture(t, map[string]string{
		".gitattributes": strings.Repeat("*.go linguist-generated\n", maxAttributeRules+1),
		"main.go":        goSource,
	})
	report, err := Scan(context.Background(), root, Options{})
	if err != nil || len(report.Warnings) != 1 || report.Warnings[0].Code != "unsupported_gitattributes" || len(report.Languages) != 1 {
		t.Fatalf("attribute rule limit was not disclosed: report=%+v err=%v", report, err)
	}
}

func TestGitMissingBlobPolicyAndCorruptAutoDoNotSilentlyDowngrade(t *testing.T) {
	root, repo, hash := gitFixture(t, map[string]string{"main.go": goSource, "safe.py": "print('safe')\n"})
	commit, err := repo.CommitObject(hash)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := tree.FindEntry("main.go")
	if err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(root, ".git", "objects", entry.Hash.String()[:2], entry.Hash.String()[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}
	if report, err := Scan(context.Background(), root, Options{}); err == nil || report != nil {
		t.Fatalf("missing object silently downgraded: report=%+v err=%v", report, err)
	}
	report, err := Scan(context.Background(), root, Options{Source: "git", ErrorPolicy: ErrorPolicyContinue, IncludeFiles: true, Discovery: true, Projects: true, Declarations: true, Metrics: &MetricsOptions{IncludeFiles: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != "missing_git_object" || !reflect.DeepEqual(languageFiles(report), map[string][]string{"Python": {"safe.py"}}) {
		t.Fatalf("continued report: %+v", report)
	}
	if report.Discovery == nil || report.Discovery.Status != "partial" || report.Discovery.Omissions["missing_git_object"] != 1 || report.Projects == nil || report.Projects.Status != "partial" || report.Projects.OmittedFiles != 1 || report.Declarations == nil || report.Declarations.Status != "partial" || report.Declarations.Coverage.OmittedFiles != 1 || report.Metrics == nil || report.Metrics.Status != "partial" {
		t.Fatalf("continued module coverage was not qualified: %+v", report)
	}
	metricOmission := false
	for _, skipped := range report.Metrics.Skipped {
		if skipped.Reason == "missing_git_object" && skipped.Files != nil && *skipped.Files == 1 {
			metricOmission = true
		}
	}
	if !metricOmission {
		t.Fatalf("missing metric omission: %+v", report.Metrics.Skipped)
	}
}

func TestAutoSourceRejectsInvalidHEADWithoutBranchRefs(t *testing.T) {
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	// A genuinely unborn repository points HEAD at a branch. A missing
	// non-branch target is corruption and must not downgrade to directory mode.
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/tags/missing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if report, err := Scan(context.Background(), root, Options{}); err == nil || report != nil {
		t.Fatalf("invalid HEAD silently downgraded: report=%+v err=%v", report, err)
	}
}

func TestLocalGitRevisionResolutionAcrossPackedRefsAndAncestry(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git needed for revision fixture")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(gitBinary, append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q")
	commits := make([]string, 0, 3)
	for i, file := range []string{"first.py", "second.go", "third.java"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("// fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", file)
		run("commit", "-q", "-m", fmt.Sprintf("commit %d", i+1))
		commits = append(commits, run("rev-parse", "HEAD"))
	}
	run("branch", "packed-branch", commits[1])
	run("tag", "packed-lightweight", commits[0])
	run("tag", "-a", "packed-annotated", commits[1], "-m", "fixture tag")
	run("pack-refs", "--all", "--prune")

	cases := map[string]string{
		"HEAD":               commits[2],
		"packed-branch":      commits[1],
		"packed-lightweight": commits[0],
		"packed-annotated":   commits[1],
		commits[1]:           commits[1],
		commits[2][:12]:      commits[2],
		"HEAD^":              commits[1],
		"HEAD~2":             commits[0],
	}
	for revision, want := range cases {
		t.Run(revision, func(t *testing.T) {
			report, err := Scan(t.Context(), root, Options{Source: "git", Revision: revision, Discovery: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Discovery == nil || report.Discovery.Source.Commit != want {
				t.Fatalf("resolved commit=%q, want %q", report.Discovery.Source.Commit, want)
			}
		})
	}
	bare := filepath.Join(t.TempDir(), "fixture.git")
	clone := exec.Command(gitBinary, "clone", "-q", "--bare", root, bare)
	if output, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone bare fixture: %v: %s", err, output)
	}
	bareReport, err := Scan(t.Context(), bare, Options{Source: "git", Discovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if bareReport.Discovery.Source.Commit != commits[2] {
		t.Fatalf("bare resolved commit=%q, want %q", bareReport.Discovery.Source.Commit, commits[2])
	}
}
