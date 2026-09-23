package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"dircue/pkg/mapdoc"
)

func sourceBinding(t *testing.T, doc mapdoc.Document) mapdoc.QuestionCoverage {
	t.Helper()
	for _, q := range doc.Coverage {
		if q.Question == mapdoc.QuestionSourceBinding {
			return q
		}
	}
	t.Fatal("source_binding question missing")
	return mapdoc.QuestionCoverage{}
}

func mapJSON(t *testing.T, args ...string) mapdoc.Document {
	t.Helper()
	out, _, err := invoke(append([]string{"map", "--json"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	var doc mapdoc.Document
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDirectoryMapCarriesGitCompatibleDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := mapJSON(t, "--source", "directory", root)
	if doc.Source.Digest == nil || doc.Source.Digest.Algorithm != "git-sha1" || doc.Source.Digest.Scope != "gitignore_filtered+git_normalized" {
		t.Fatalf("digest = %+v", doc.Source.Digest)
	}
	if q := sourceBinding(t, doc); q.Status != mapdoc.CoverageComplete {
		t.Fatalf("source_binding = %+v", q)
	}
	off := mapJSON(t, "--source", "directory", "--set", "source.digest=off", root)
	if q := sourceBinding(t, off); off.Source.Digest != nil || q.Status != mapdoc.CoverageUnknown || q.Reasons[0] != "source_digest_disabled" {
		t.Fatalf("digest off: %+v %+v", off.Source.Digest, q)
	}
	raw := mapJSON(t, "--source", "directory", "--set", "source.digest=raw", root)
	if raw.Source.Digest == nil || raw.Source.Digest.Scope != "all_entries+raw" {
		t.Fatalf("raw digest = %+v", raw.Source.Digest)
	}
}

func TestDirectoryMapDigestEqualsGitTreeAndComparesAsSame(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git oracle unavailable")
	}
	root := t.TempDir()
	for name, content := range map[string]string{"main.go": "package main\n", ".gitignore": "*.log\n", "debug.log": "ignored\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "x"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir, cmd.Env = root, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	gitDoc := mapJSON(t, "--source", "git", root)
	dirDoc := mapJSON(t, "--source", "directory", root)
	if dirDoc.Source.Digest == nil || dirDoc.Source.Digest.Value != gitDoc.Source.Tree {
		t.Fatalf("directory digest %+v does not equal git tree %s", dirDoc.Source.Digest, gitDoc.Source.Tree)
	}
	dir := t.TempDir()
	for name, doc := range map[string]mapdoc.Document{"git.json": gitDoc, "dir.json": dirDoc} {
		data, _ := json.Marshal(doc)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := invoke("map", "compare", "--json", filepath.Join(dir, "git.json"), filepath.Join(dir, "dir.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		SourceBinding string `json:"source_binding"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.SourceBinding != "same" {
		t.Fatalf("compare source_binding = %q (%v)", report.SourceBinding, err)
	}
}

func TestUnreadableDirectoryMakesContentPartialUnderContinue(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission semantics differ")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	doc := mapJSON(t, "--source", "directory", "--on-error", "continue", root)
	for _, q := range doc.Coverage {
		if q.Question == "content" && q.Status == mapdoc.CoverageComplete {
			t.Fatalf("content claimed complete with an unreadable directory: %+v", q)
		}
	}
	if q := sourceBinding(t, doc); q.Status != mapdoc.CoverageUnknown || doc.Source.Digest != nil {
		t.Fatalf("an unreadable directory must leave the digest unavailable: %+v", q)
	}
}
