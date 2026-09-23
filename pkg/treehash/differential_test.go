package treehash

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The differential tests compare Compute with real Git. Git is an oracle
// only; dircue itself never runs it. They are skipped when git is absent.

func gitAvailable(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed; differential oracle unavailable")
	}
	return bin
}

// gitEnv isolates the oracle from user and system configuration so it runs
// under the assumptions documented on Compute.
func gitEnv(home string) []string {
	return append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
}

var oracleConfig = []string{
	"-c", "core.autocrlf=false", "-c", "core.filemode=true", "-c", "core.symlinks=true",
	"-c", "core.ignorecase=false", "-c", "core.precomposeunicode=false", "-c", "core.safecrlf=false",
	"-c", "core.excludesFile=" + os.DevNull, "-c", "core.attributesFile=" + os.DevNull,
	"-c", "init.defaultBranch=main", "-c", "advice.addEmbeddedRepo=false",
}

func runGit(t *testing.T, git, dir, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(git, append(append([]string{}, oracleConfig...), args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv(home)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// oracleTree records the tree Git writes for dir in a fresh repository.
func oracleTree(t *testing.T, git, dir string, format Format) string {
	t.Helper()
	home := t.TempDir()
	runGit(t, git, dir, home, "init", "-q", "--object-format="+string(format))
	runGit(t, git, dir, home, "add", "-A")
	return runGit(t, git, dir, home, "write-tree")
}

func computeDir(t *testing.T, dir string, opts Options) Result {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	res, err := Compute(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

type treeGen struct {
	r    *rand.Rand
	used map[string]bool // lower-cased paths, so case-insensitive filesystems cannot collide
}

var genNames = []string{
	"a", "b", "a.b", "a-b", "a_b", "ab", "a b", "é", "x.txt", "y.md", "z.bin", "w.crlf",
	"x.log", "keep.log", "top.txt", "deep", "y.tmp", "z.o", "build", "sub", "#h", "!bang",
	"sp ", ".hidden", "run.sh", "q.json", "c.c", "trailing ", "lf.txt", "auto.txt",
}

func (g *treeGen) name(dir string) string {
	for i := 0; i < 64; i++ {
		n := genNames[g.r.Intn(len(genNames))]
		key := strings.ToLower(filepath.Join(dir, n))
		if !g.used[key] {
			g.used[key] = true
			return n
		}
	}
	n := "n" + strconv.Itoa(g.r.Int())
	g.used[strings.ToLower(filepath.Join(dir, n))] = true
	return n
}

func (g *treeGen) eol() string {
	if g.r.Intn(4) == 0 {
		return "\r\n"
	}
	return "\n"
}

func (g *treeGen) content() []byte {
	var b bytes.Buffer
	lines := g.r.Intn(6)
	for i := 0; i < lines; i++ {
		switch g.r.Intn(12) {
		case 0:
			b.WriteString("bin\x00ary")
		case 1:
			b.WriteString("lone\rcr")
		case 2:
			b.Write([]byte{0xff, 0xfe, 0x01, 0x02})
		case 3:
			b.WriteString("\t\b\x1b\x0c ctl")
		default:
			b.WriteString("line " + strconv.Itoa(g.r.Intn(100)))
		}
		switch g.r.Intn(3) {
		case 0:
			b.WriteString("\r\n")
		case 1:
			b.WriteString("\n")
		}
	}
	if g.r.Intn(10) == 0 {
		b.WriteByte(0x1a)
	}
	if g.r.Intn(8) == 0 {
		b.WriteString("\r")
	}
	return b.Bytes()
}

var genAttrPatterns = []string{"*.txt", "*.bin", "*", "sub/*.txt", "/x*", "*.md", "*.crlf", "lf.txt", "auto.txt", "\"a b\"", "*.c", "**/sub/*", "[ay]*", "sub/**", "?.txt", "*[[:alpha:]].md", "a/**/b", "\"\\#h\""}
var genAttrValues = []string{"text", "-text", "text=auto", "eol=crlf", "eol=lf", "binary", "crlf", "-crlf", "crlf=input", "text eol=crlf", "!text", "mytext", "text=input", "diff"}
var genIgnoreLines = []string{"*.log", "build/", "!keep.log", "/top.txt", "**/deep", "*.tmp", "sub/", "a*", "\\#h", "trailing\\ ", "*.o", "!a.b", "y.*", "/sub/x.txt", "*.bin", "é",
	"[ab]*", "[!a]*.txt", "[^x]?.md", "?.o", "sub/**/x.txt", "**/sub/**", "a/**", "*[[:digit:]]*", "[[:upper:]]*", "[a-c].b", "\\!bang", "!build/", "!sub/", "sub/*", "*/a", "**", "!*.txt", "q.json/**/top.txt", "[]a]*", "x.???"}

func (g *treeGen) fill(t *testing.T, root, dir string, depth int) {
	t.Helper()
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if g.r.Intn(3) == 0 {
		var lines []string
		if dir == "" && g.r.Intn(2) == 0 {
			lines = append(lines, "[attr]mytext text eol=lf")
		}
		for i := 0; i < 1+g.r.Intn(4); i++ {
			lines = append(lines, genAttrPatterns[g.r.Intn(len(genAttrPatterns))]+" "+genAttrValues[g.r.Intn(len(genAttrValues))])
		}
		write(t, filepath.Join(full, ".gitattributes"), []byte(strings.Join(lines, g.eol())+"\n"), 0o644)
		g.used[strings.ToLower(filepath.Join(dir, ".gitattributes"))] = true
	}
	if g.r.Intn(3) == 0 {
		var lines []string
		for i := 0; i < 1+g.r.Intn(4); i++ {
			lines = append(lines, genIgnoreLines[g.r.Intn(len(genIgnoreLines))])
		}
		write(t, filepath.Join(full, ".gitignore"), []byte(strings.Join(lines, g.eol())+"\n"), 0o644)
		g.used[strings.ToLower(filepath.Join(dir, ".gitignore"))] = true
	}
	for i := 0; i < 1+g.r.Intn(6); i++ {
		name := g.name(dir)
		p := filepath.Join(full, name)
		switch k := g.r.Intn(10); {
		case k < 6:
			mode := os.FileMode(0o644)
			if g.r.Intn(4) == 0 {
				mode = 0o755
			}
			write(t, p, g.content(), mode)
		case k < 7:
			targets := []string{"a", "../x", "nowhere", "sub/", "x.txt", "é"}
			if err := os.Symlink(targets[g.r.Intn(len(targets))], p); err != nil {
				t.Fatal(err)
			}
		case k < 9 && depth < 3:
			g.fill(t, root, filepath.Join(dir, name), depth+1)
		default:
			if err := os.MkdirAll(p, 0o755); err != nil { // an empty directory
				t.Fatal(err)
			}
		}
	}
}

func write(t *testing.T, p string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func iterations(def int) int {
	if v, err := strconv.Atoi(os.Getenv("DIRCUE_TREEHASH_ITERATIONS")); err == nil && v > 0 {
		return v
	}
	return def
}

func TestComputeMatchesGitOnRandomTrees(t *testing.T) {
	git := gitAvailable(t)
	for _, format := range []Format{FormatSHA1, FormatSHA256} {
		for i := 0; i < iterations(40); i++ {
			seed := int64(i*7919 + 17)
			t.Run(fmt.Sprintf("%s/seed-%d", format, seed), func(t *testing.T) {
				dir := t.TempDir()
				g := &treeGen{r: rand.New(rand.NewSource(seed)), used: map[string]bool{}}
				g.fill(t, dir, "", 0)
				got := computeDir(t, dir, Options{Format: format})
				want := oracleTree(t, git, dir, format)
				// Without an index, ignored entries leave the ID equal to a fresh
				// "git add -A" but possibly not to an original commit; that is the
				// only qualification a generated tree may carry.
				switch {
				case got.Ignored > 0 && (got.Status != StatusPartial || len(got.Reasons) != 1 || got.Reasons[0] != ReasonIgnoredWithoutIndex):
					t.Fatalf("ignored=%d status %s reasons %v", got.Ignored, got.Status, got.Reasons)
				case got.Ignored == 0 && got.Status != StatusComplete:
					t.Fatalf("status %s reasons %v", got.Status, got.Reasons)
				}
				if got.TreeID != want {
					t.Fatalf("tree mismatch: dircue %s git %s\n%s", got.TreeID, want, describeTree(t, dir))
				}
			})
		}
	}
}

func describeTree(t *testing.T, dir string) string {
	var b strings.Builder
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || strings.Contains(p, "/.git") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(&b, "%v %q", info.Mode(), rel)
		if info.Mode().IsRegular() && info.Size() < 512 {
			data, _ := os.ReadFile(p)
			fmt.Fprintf(&b, " %q", data)
		}
		b.WriteByte('\n')
		return nil
	})
	return b.String()
}

func TestNestedRepositoryBecomesGitlink(t *testing.T) {
	git := gitAvailable(t)
	dir := t.TempDir()
	home := t.TempDir()
	write(t, filepath.Join(dir, "top.txt"), []byte("top\n"), 0o644)
	nested := filepath.Join(dir, "vendor", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(nested, "lib.go"), []byte("package lib\n"), 0o644)
	runGit(t, git, nested, home, "init", "-q")
	runGit(t, git, nested, home, "add", "-A")
	runGit(t, git, nested, home, "commit", "-q", "-m", "x")
	runGit(t, git, nested, home, "pack-refs", "--all")
	got := computeDir(t, dir, Options{})
	want := oracleTree(t, git, dir, FormatSHA1)
	if got.TreeID != want || got.Status != StatusComplete || got.Gitlinks != 1 {
		t.Fatalf("got %+v want %s", got, want)
	}
}

func TestUnresolvableNestedRepositoryIsReported(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(dir, "n", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "n", ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	got := computeDir(t, dir, Options{})
	if got.Status != StatusPartial || !contains(got.Reasons, ReasonNestedUnresolved) {
		t.Fatalf("got %+v", got)
	}
}

func TestTrackedIgnoredFilesUseRepositoryIndex(t *testing.T) {
	git := gitAvailable(t)
	dir := t.TempDir()
	home := t.TempDir()
	write(t, filepath.Join(dir, ".gitignore"), []byte("*.log\nbuild/\n"), 0o644)
	write(t, filepath.Join(dir, "kept.log"), []byte("tracked despite ignore\n"), 0o644)
	write(t, filepath.Join(dir, "other.log"), []byte("untracked\n"), 0o644)
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "build", "keep.txt"), []byte("tracked\n"), 0o644)
	write(t, filepath.Join(dir, "build", "out.txt"), []byte("ignored\n"), 0o644)
	runGit(t, git, dir, home, "init", "-q")
	runGit(t, git, dir, home, "add", "-A")
	runGit(t, git, dir, home, "add", "-f", "kept.log", "build/keep.txt")
	got := computeDir(t, dir, Options{})
	runGit(t, git, dir, home, "add", "-A")
	want := runGit(t, git, dir, home, "write-tree")
	if got.TreeID != want || got.Status != StatusComplete {
		t.Fatalf("got %+v want %s", got, want)
	}
}

func TestCheckoutOfCommitMatchesItsTree(t *testing.T) {
	git := gitAvailable(t)
	src := t.TempDir()
	home := t.TempDir()
	write(t, filepath.Join(src, ".gitattributes"), []byte("*.txt text eol=crlf\n*.bat eol=crlf\n"), 0o644)
	write(t, filepath.Join(src, "a.txt"), []byte("one\ntwo\n"), 0o644)
	write(t, filepath.Join(src, "b.bat"), []byte("echo\n"), 0o644)
	write(t, filepath.Join(src, "tool"), []byte("#!/bin/sh\n"), 0o755)
	runGit(t, git, src, home, "init", "-q")
	runGit(t, git, src, home, "add", "-A")
	runGit(t, git, src, home, "commit", "-q", "-m", "x")
	want := runGit(t, git, src, home, "rev-parse", "HEAD^{tree}")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, git, filepath.Dir(clone), home, "clone", "-q", src, clone)
	// The checkout converts *.txt and *.bat to CRLF; normalization reverses it.
	if data, _ := os.ReadFile(filepath.Join(clone, "a.txt")); !bytes.Contains(data, []byte("\r\n")) {
		t.Fatalf("expected a CRLF checkout, got %q", data)
	}
	if err := os.RemoveAll(filepath.Join(clone, ".git")); err != nil {
		t.Fatal(err)
	}
	got := computeDir(t, clone, Options{})
	if got.TreeID != want || got.Status != StatusComplete {
		t.Fatalf("got %+v want %s", got, want)
	}
	raw := computeDir(t, clone, Options{Scope: ScopeRaw})
	if raw.TreeID == want {
		t.Fatalf("raw scope unexpectedly equals the normalized tree")
	}
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func TestCRLFIgnoreAndAttributeFilesMatchGit(t *testing.T) {
	git := gitAvailable(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".gitignore"), []byte("*.log\r\nkeep\r\n\xef\xbb\xbfbom.txt\r\n"), 0o644)
	write(t, filepath.Join(dir, ".gitattributes"), []byte("*.txt text\r\n*.dat -text\r\n"), 0o644)
	write(t, filepath.Join(dir, "a.log"), []byte("x"), 0o644)
	write(t, filepath.Join(dir, "keep"), []byte("x"), 0o644)
	write(t, filepath.Join(dir, "bom.txt"), []byte("a\r\nb\r\n"), 0o644)
	write(t, filepath.Join(dir, "c.dat"), []byte("a\r\n"), 0o644)
	got := computeDir(t, dir, Options{})
	if want := oracleTree(t, git, dir, FormatSHA1); got.TreeID != want {
		t.Fatalf("got %s want %s", got.TreeID, want)
	}
}

func TestSpecialFilesAreReportedNotHashed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	if err := syscallMkfifo(filepath.Join(dir, "pipe")); err != nil {
		t.Skip("mkfifo unsupported:", err)
	}
	got := computeDir(t, dir, Options{})
	if got.Status != StatusPartial || !contains(got.Reasons, ReasonSpecialFiles) || got.SpecialFiles != 1 {
		t.Fatalf("got %+v", got)
	}
	// An ignored special file is not reported as excluded content.
	write(t, filepath.Join(dir, ".gitignore"), []byte("pipe\n"), 0o644)
	if got := computeDir(t, dir, Options{}); contains(got.Reasons, ReasonSpecialFiles) || got.SpecialFiles != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLimitsMakeTheDigestUnavailable(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("f%d", i)), bytes.Repeat([]byte("x"), 100), 0o644)
	}
	if got := computeDir(t, dir, Options{MaxEntries: 3}); got.Status != StatusUnavailable || got.TreeID != "" || got.Reasons[0] != ReasonEntryLimit {
		t.Fatalf("entries: %+v", got)
	}
	if got := computeDir(t, dir, Options{MaxBytes: 250}); got.Status != StatusUnavailable || got.Reasons[0] != ReasonByteLimit {
		t.Fatalf("bytes: %+v", got)
	}
}

func TestFilterAndEncodingAttributesQualifyTheResult(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".gitattributes"), []byte("*.bin filter=lfs\n*.u16 working-tree-encoding=UTF-16\n*.id ident\n"), 0o644)
	write(t, filepath.Join(dir, "a.bin"), []byte("x"), 0o644)
	write(t, filepath.Join(dir, "b.u16"), []byte("x"), 0o644)
	write(t, filepath.Join(dir, "c.id"), []byte("$Id$"), 0o644)
	got := computeDir(t, dir, Options{})
	for _, r := range []string{ReasonFilterDriver, ReasonWorkingTreeEncoding, ReasonIdent} {
		if !contains(got.Reasons, r) {
			t.Fatalf("missing %s in %+v", r, got)
		}
	}
	if got.Status != StatusPartial || got.TreeID == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestDeterministicAcrossWorkerCounts(t *testing.T) {
	dir := t.TempDir()
	g := &treeGen{r: rand.New(rand.NewSource(42)), used: map[string]bool{}}
	g.fill(t, dir, "", 0)
	first := computeDir(t, dir, Options{Workers: 1, KeepBlobs: true})
	for _, w := range []int{2, 8, 32} {
		got := computeDir(t, dir, Options{Workers: w, KeepBlobs: true})
		if got.TreeID != first.TreeID || len(got.Blobs) != len(first.Blobs) {
			t.Fatalf("workers=%d: %s vs %s", w, got.TreeID, first.TreeID)
		}
	}
}

// Git keeps committed CRLF content under text=auto (has_crlf_in_index). A
// clean checkout must hash to the commit's tree, and a modified tracked file
// whose indexed blob is not inspectable must qualify the result.
func TestTextAutoWithCommittedCRLF(t *testing.T) {
	git := gitAvailable(t)
	src := t.TempDir()
	home := t.TempDir()
	write(t, filepath.Join(src, "legacy.js"), []byte("a\r\nb\r\n"), 0o644)
	runGit(t, git, src, home, "init", "-q")
	runGit(t, git, src, home, "add", "-A")
	runGit(t, git, src, home, "commit", "-q", "-m", "crlf")
	write(t, filepath.Join(src, ".gitattributes"), []byte("*.js text=auto eol=lf\n"), 0o644)
	write(t, filepath.Join(src, "new.js"), []byte("c\r\nd\r\n"), 0o644)
	runGit(t, git, src, home, "add", ".gitattributes", "new.js")
	runGit(t, git, src, home, "commit", "-q", "-m", "attrs")
	want := runGit(t, git, src, home, "rev-parse", "HEAD^{tree}")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, git, filepath.Dir(clone), home, "clone", "-q", src, clone)
	if got := computeDir(t, clone, Options{}); got.TreeID != want || got.Status != StatusComplete {
		t.Fatalf("clean checkout: got %+v want %s", got, want)
	}
	write(t, filepath.Join(clone, "legacy.js"), []byte("a\r\nb\r\nchanged\r\n"), 0o644)
	got := computeDir(t, clone, Options{})
	if got.Status != StatusPartial || !contains(got.Reasons, ReasonTextAutoIndexAssumed) {
		t.Fatalf("modified tracked text=auto file must be qualified: %+v", got)
	}
}
