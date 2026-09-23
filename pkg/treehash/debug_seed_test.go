package treehash

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestDebugSeed prints a per-path comparison for one generator seed. It runs
// only when DIRCUE_TREEHASH_DEBUG_SEED is set.
func TestDebugSeed(t *testing.T) {
	v := os.Getenv("DIRCUE_TREEHASH_DEBUG_SEED")
	if v == "" {
		t.Skip("set DIRCUE_TREEHASH_DEBUG_SEED to compare one generated tree")
	}
	seed, _ := strconv.ParseInt(v, 10, 64)
	git := gitAvailable(t)
	dir := t.TempDir()
	g := &treeGen{r: rand.New(rand.NewSource(seed)), used: map[string]bool{}}
	g.fill(t, dir, "", 0)
	got := computeDir(t, dir, Options{KeepBlobs: true})
	home := t.TempDir()
	runGit(t, git, dir, home, "init", "-q")
	runGit(t, git, dir, home, "add", "-A")
	listed := runGit(t, git, dir, home, "ls-files", "-s")
	gitBlobs := map[string]string{}
	for _, line := range strings.Split(listed, "\n") {
		meta, name, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		gitBlobs[name] = f[0] + " " + f[1]
	}
	for p, id := range got.Blobs {
		if g, ok := gitBlobs[p]; !ok || !strings.HasSuffix(g, id) {
			t.Logf("dircue %s %s | git %q", p, id, g)
		}
	}
	for p, g := range gitBlobs {
		if _, ok := got.Blobs[p]; !ok {
			t.Logf("git-only %s %s", p, g)
		}
	}
	t.Logf("tree dircue=%s", got.TreeID)
	for _, name := range []string{".gitignore", ".gitattributes"} {
		out := runGit(t, git, dir, home, "ls-files", "-o", "-c", "--", "*"+name, name)
		for _, p := range strings.Split(out, "\n") {
			if p == "" {
				continue
			}
			data, _ := os.ReadFile(dir + "/" + p)
			t.Logf("%s: %q", p, data)
		}
	}
	t.Log(runGit(t, git, dir, home, "check-ignore", "-v", "--no-index", ".hidden/keep.log/a.b/build", ".hidden/keep.log/a.b", ".hidden/keep.log"))
	t.Log(describeTree(t, dir))
}
