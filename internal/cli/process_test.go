package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Exercise the installed-binary contract, including main's nonzero error exit
// and revision selection while the worktree contains deliberately different code.
func TestBuiltBinaryGitPipelineContract(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required to create the committed fixture")
	}
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "dircue")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun("init", "--quiet", "--initial-branch=main")
	write("hello.go", goSource)
	gitRun("add", ".")
	gitRun("commit", "--quiet", "-m", "Go revision")
	gitRun("tag", "go-only")
	write("hello.py", "print('python')\n")
	gitRun("add", ".")
	gitRun("commit", "--quiet", "-m", "Python revision")
	write("hello.go", "package dirty\n")
	write("untracked.rb", "puts 'ruby'\n")
	for _, tc := range []struct {
		name      string
		args      []string
		languages []string
		goBytes   int
	}{
		{"default committed HEAD", []string{"-bj", root}, []string{"Go", "Python"}, len(goSource)},
		{"explicit revision", []string{root, "-j", "-r", "go-only"}, []string{"Go"}, len(goSource)},
		{"structured revision", []string{"analyze", "languages", root, "-j", "--rev=go-only"}, []string{"Go"}, len(goSource)},
		{"working directory", []string{"--source=directory", "-j", root}, []string{"Go", "Python", "Ruby"}, len("package dirty\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binary, tc.args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || stderr.Len() != 0 {
				t.Fatalf("invoke: %v %s", err, stderr.String())
			}
			var result map[string]struct {
				Size int `json:"size"`
			}
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatalf("JSON: %v %s", err, out)
			}
			if len(result) != len(tc.languages) || result["Go"].Size != tc.goBytes {
				t.Fatalf("unexpected result: %s", out)
			}
			for _, language := range tc.languages {
				if _, ok := result[language]; !ok {
					t.Fatalf("missing %s: %s", language, out)
				}
			}
		})
	}
	for _, args := range [][]string{{"--rev=missing", root}, {"--unknown", root}, {"--source=directory", "--rev=HEAD", root}} {
		cmd := exec.Command(binary, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err == nil || len(out) != 0 || stderr.Len() == 0 {
			t.Fatalf("invalid invocation %v: %q %q %v", args, out, stderr.String(), err)
		}
	}
}
