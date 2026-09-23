package forest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/forest"
)

// gitAvailable returns true when git is in PATH.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// initGitRepo creates a minimal git repo at dir (must already exist).
// It does not rely on git being configured with identity; it sets env vars.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@t.t",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@t.t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	run("git", "init", "-b", "main", dir)
	run("git", "-C", dir, "commit", "--allow-empty", "-m", "init")
}

func TestDiscoverRoots_SingleRepo(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	initGitRepo(t, dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	result := forest.DiscoverRoots(root, forest.DiscoverOptions{IncludeRemotes: true})
	if len(result.Roots) != 0 {
		// The root itself is not a nested root (it is the input root).
		// DiscoverRoots finds nested roots only.
		t.Errorf("want 0 nested roots for single repo, got %d", len(result.Roots))
	}
}

func TestDiscoverRoots_NestedRepo(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()
	initGitRepo(t, outer)
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, inner)

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{IncludeRemotes: false})
	if len(result.Roots) != 1 {
		t.Fatalf("want 1 nested root, got %d: %+v", len(result.Roots), result.Roots)
	}
	if result.Roots[0].Path != "inner" {
		t.Errorf("want path=inner, got %q", result.Roots[0].Path)
	}
	if result.Roots[0].Kind != forest.RootGitWorktree {
		t.Errorf("want kind=git_worktree, got %q", result.Roots[0].Kind)
	}
	if result.Roots[0].IdentityStatus != forest.IdentityResolved {
		t.Errorf("want resolved identity, got %q: %q", result.Roots[0].IdentityStatus, result.Roots[0].IdentityReason)
	}
}

func TestDiscoverRoots_BareRepo(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()
	bareDir := filepath.Join(outer, "bare.git")
	cmd := exec.Command("git", "init", "--bare", bareDir)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %s", out)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{})
	if len(result.Roots) != 1 {
		t.Fatalf("want 1 bare root, got %d: %+v", len(result.Roots), result.Roots)
	}
	if result.Roots[0].Kind != forest.RootGitBare {
		t.Errorf("want kind=git_bare, got %q", result.Roots[0].Kind)
	}
}

func TestDiscoverRoots_DotGitFile_InsideRoot(t *testing.T) {
	// Test that a .git file pointing inside the root IS discovered.
	// We synthesize this by manually writing a .git file pointer that
	// resolves inside the same root.
	outer := t.TempDir()
	// Create the actual git dir inside the root.
	gitDirPath := filepath.Join(outer, ".git_worktrees", "wt1")
	if err := os.MkdirAll(gitDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDirPath, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create the linked worktree with a .git file.
	wtDir := filepath.Join(outer, "linked")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// .git file pointing to ../.git_worktrees/wt1 (inside root).
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: ../.git_worktrees/wt1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{})
	if len(result.Roots) == 0 {
		t.Fatal("want at least 1 root with .git file pointer inside root, got 0")
	}
	found := false
	for _, r := range result.Roots {
		if r.Path == "linked" {
			found = true
		}
	}
	if !found {
		t.Errorf("linked worktree not found; roots: %+v", result.Roots)
	}
}

func TestDiscoverRoots_DotGitFile_OutsideRoot(t *testing.T) {
	// A .git file whose gitdir points outside the root must NOT be discovered.
	outer := t.TempDir()
	wtDir := filepath.Join(outer, "linked")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// .git file pointing outside (absolute path or escaping).
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: /tmp/outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{})
	for _, r := range result.Roots {
		if r.Path == "linked" {
			t.Errorf("root with .git pointing outside should not be discovered: %+v", r)
		}
	}
}

func TestDiscoverRoots_RootCap(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()
	// Create 3 nested repos.
	for _, name := range []string{"a", "b", "c"} {
		d := filepath.Join(outer, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, d)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{RootCap: 2})
	if !result.Partial {
		t.Error("want partial=true when cap is hit")
	}
	if len(result.Roots) > 2 {
		t.Errorf("want at most 2 roots, got %d", len(result.Roots))
	}
}

func TestDiscoverRoots_DeterministicOrder(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		d := filepath.Join(outer, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, d)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	var paths [][]string
	for range 3 {
		result := forest.DiscoverRoots(root, forest.DiscoverOptions{})
		var ps []string
		for _, r := range result.Roots {
			ps = append(ps, r.Path)
		}
		paths = append(paths, ps)
	}
	for i := 1; i < len(paths); i++ {
		if strings.Join(paths[0], ",") != strings.Join(paths[i], ",") {
			t.Errorf("non-deterministic order: run 0: %v, run %d: %v", paths[0], i, paths[i])
		}
	}
}

func TestDiscoverRoots_SkipsEnvTrees(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()

	// Create a real nested git repo.
	nested := filepath.Join(outer, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, nested)

	// Create a node_modules directory that should NOT be walked for git roots.
	nm := filepath.Join(outer, "node_modules", "fake-repo")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, nm)

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{SummarizeTrees: true})
	// Only "nested" should be a root; the repo inside node_modules must not appear.
	for _, r := range result.Roots {
		if strings.HasPrefix(r.Path, "node_modules") {
			t.Errorf("root inside node_modules should not be discovered: %q", r.Path)
		}
	}
	if len(result.EnvTrees) == 0 {
		t.Error("want at least 1 env tree summary (node_modules)")
	}
}

func TestDiscoverRoots_UnbornHead(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not in PATH")
	}
	outer := t.TempDir()
	unborn := filepath.Join(outer, "unborn")
	if err := os.MkdirAll(unborn, 0o755); err != nil {
		t.Fatal(err)
	}
	// git init without a commit leaves an unborn HEAD.
	cmd := exec.Command("git", "init", "-b", "main", unborn)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{})
	if len(result.Roots) != 1 {
		t.Fatalf("want 1 root, got %d", len(result.Roots))
	}
	r := result.Roots[0]
	// An unborn HEAD has a ref: pointer but no commit.
	if r.IdentityStatus != forest.IdentityUnknown {
		t.Errorf("want identity_status=unknown for unborn HEAD, got %q", r.IdentityStatus)
	}
	if r.IdentityReason == "" {
		t.Error("want non-empty identity_reason")
	}
}

func TestParseGitConfigRemotes(t *testing.T) {
	config := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = https://user:canary@github.com/org/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = git@github.com:upstream/repo.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
`
	outer := t.TempDir()
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write a HEAD file so discovery finds the root.
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(outer)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	result := forest.DiscoverRoots(root, forest.DiscoverOptions{IncludeRemotes: true})
	if len(result.Roots) != 1 {
		t.Fatalf("want 1 root, got %d", len(result.Roots))
	}
	r := result.Roots[0]
	if len(r.Remotes) != 2 {
		t.Fatalf("want 2 remotes, got %d: %+v", len(r.Remotes), r.Remotes)
	}
	// The canary must not appear in any remote URL.
	for _, rem := range r.Remotes {
		if strings.Contains(rem.URL, "canary") {
			t.Errorf("canary token found in remote URL: %q", rem.URL)
		}
		if strings.Contains(rem.URL, "user:") {
			t.Errorf("credentials found in remote URL: %q", rem.URL)
		}
	}
	// origin should have been stripped.
	for _, rem := range r.Remotes {
		if rem.Name == "origin" {
			if rem.URL != "https://github.com/org/repo.git" {
				t.Errorf("origin URL: want %q, got %q", "https://github.com/org/repo.git", rem.URL)
			}
		}
		if rem.Name == "upstream" {
			if rem.URL != "github.com:upstream/repo.git" {
				t.Errorf("upstream URL: want %q, got %q", "github.com:upstream/repo.git", rem.URL)
			}
		}
	}
}
