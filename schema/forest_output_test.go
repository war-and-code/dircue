package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"dircue/internal/cli"
)

// TestForestOutputValidatesAgainstSchema validates a real forest document,
// not a hand-built one, so drift between the command and its schema fails.
func TestForestOutputValidatesAgainstSchema(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is needed only to create the fixture repository")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "projects", "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("loose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "x"}, {"remote", "add", "origin", root + "/elsewhere"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir, cmd.Env = repo, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	var out, errOut bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"map", "--forest", "--json", root}, &out, &errOut); err != nil {
		t.Fatalf("forest failed: %v\n%s", err, errOut.String())
	}
	if strings.Contains(out.String(), root) {
		t.Fatal("forest output contains the absolute input path")
	}
	var value any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	_, compiled := compileExportPair(t, "forest")
	if err := compiled.Validate(value); err != nil {
		t.Fatalf("forest output does not validate: %v\n%s", err, out.String())
	}
}
