package declarations_adversarial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dircue/pkg/declarations"
	"dircue/pkg/scanner"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestHostileManifestShapes(t *testing.T) {
	cases := []struct{ name, path, content string }{
		{"toml-dotted-depth", "Cargo.toml", strings.Repeat("a.", 100000) + "key=1\n"},
		{"toml-nested-array", "pyproject.toml", "x=" + strings.Repeat("[", 10000) + "0" + strings.Repeat("]", 10000)},
		{"json-duplicate", "package.json", `{"name":"first","name":"second"}`},
		{"json-depth", "package.json", `{"x":` + strings.Repeat("[", 10000) + "0" + strings.Repeat("]", 10000) + "}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := declarations.Parse(tc.path, []byte(tc.content))
			if d == nil || d.Parsed || len(d.Diagnostics) == 0 {
				t.Fatalf("hostile shape did not produce explicit parse failure: %+v", d)
			}
		})
	}
}

func TestWorkspacePatternDepthAndRepeatedStars(t *testing.T) {
	for _, pattern := range []string{strings.Repeat("**/", 65) + "x", "**a", "{a,b}", "../*"} {
		if _, err := declarations.MatchPattern(pattern, "x"); err == nil {
			t.Fatalf("unsupported pattern accepted: %q", pattern)
		}
	}
	pattern := strings.Repeat("**/", 60) + "leaf"
	for i := 0; i < 256; i++ {
		match, err := declarations.MatchPattern(pattern, strings.Repeat("a/", 100)+"leaf")
		if err != nil || !match {
			t.Fatalf("bounded glob changed semantics: %v %v", match, err)
		}
	}
}

func candidate(name, content string) *declarations.Candidate {
	return &declarations.Candidate{Path: name, Size: int64(len(content)), Read: func(ctx context.Context, _ int64) ([]byte, int64, error) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		return []byte(content), int64(len(content)), nil
	}}
}
func TestCappedManifestSelectionIndependentOfArrival(t *testing.T) {
	build := func(reverse bool) []byte {
		c := declarations.New("directory", "", 0)
		for n := 0; n < declarations.MaxDocuments+9; n++ {
			i := n
			if reverse {
				i = declarations.MaxDocuments + 8 - n
			}
			name := fmt.Sprintf("m%05d/go.mod", i)
			c.Add(name, candidate(name, "module example.invalid/m\ngo 1.24\n"))
		}
		r, e := c.Finish(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if r.Status != "partial" || len(r.Projects) != declarations.MaxDocuments || r.Coverage.OmittedFiles != 9 {
			t.Fatalf("incorrect capped coverage: %+v projects=%d", r.Coverage, len(r.Projects))
		}
		b, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	if !bytes.Equal(build(false), build(true)) {
		t.Fatal("manifest arrival changed capped report")
	}
}

func TestCanceledSelectedReadStopsCollector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := declarations.New("directory", "", 0)
	d := candidate("go.mod", "module example.invalid/m\n")
	d.Read = func(context.Context, int64) ([]byte, int64, error) { cancel(); return nil, 0, context.Canceled }
	c.Add("go.mod", d)
	r, e := c.Finish(ctx)
	if !errors.Is(e, context.Canceled) || r != nil {
		t.Fatalf("cancellation returned report: %v %+v", e, r)
	}
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}
func scan(t *testing.T, root, source, rev string) []byte {
	t.Helper()
	r, e := scanner.Scan(context.Background(), root, scanner.Options{Source: source, Revision: rev, Declarations: true, DeclarationsOnly: true, Workers: 2})
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(r.Declarations)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestSelectedGitRevisionAndSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "Cargo.toml", "[package]\nname='outside-secret'\nversion='1.0.0'\n")
	write(t, root, "go.mod", "module example.invalid/committed\ngo 1.24\n")
	write(t, root, "package.json", `{"name":"committed","scripts":{"build":"touch SHOULD_NOT_EXIST"}}`)
	repo, e := git.PlainInit(root, false)
	if e != nil {
		t.Fatal(e)
	}
	wt, e := repo.Worktree()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wt.Add("."); e != nil {
		t.Fatal(e)
	}
	hash, e := wt.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(0, 0)}})
	if e != nil {
		t.Fatal(e)
	}
	write(t, root, "go.mod", "module example.invalid/dirty\ngo 1.24\n")
	write(t, root, "package.json", `{"name":"dirty"}`)
	write(t, root, "pyproject.toml", "[project]\nname='untracked'\nversion='1.0'\n")
	linked := os.Symlink(filepath.Join(outside, "Cargo.toml"), filepath.Join(root, "Cargo.toml")) == nil
	a, b := scan(t, root, "git", hash.String()), scan(t, root, "directory", "")
	if !bytes.Contains(a, []byte("example.invalid/committed")) || bytes.Contains(a, []byte("dirty")) || bytes.Contains(a, []byte("untracked")) {
		t.Fatalf("selected Git snapshot mixed source: %s", a)
	}
	if !bytes.Contains(b, []byte("example.invalid/dirty")) || !bytes.Contains(b, []byte("untracked")) || bytes.Contains(b, []byte("outside-secret")) {
		t.Fatalf("directory isolation failed: %s", b)
	}
	if linked && !bytes.Contains(b, []byte(`"omitted_files":1`)) {
		t.Fatalf("symlink omission not disclosed: %s", b)
	}
	if _, e := os.Stat(filepath.Join(root, "SHOULD_NOT_EXIST")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("declaration command executed: %v", e)
	}
}

func TestMultiEcosystemHostPathsRemainUnresolved(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"go.work": "go 1.24\nuse /host/private/module\n", "package.json": `{"name":"app","workspaces":["/host/private/*"]}`,
		"Cargo.toml":     "[package]\nname='app'\nversion='1.0.0'\n[dependencies]\nremote={path='/host/private/crate'}\n",
		"pyproject.toml": "[project]\nname='app'\nversion='1.0'\ndependencies=['dep']\n[tool.uv.sources]\ndep={path='/host/private/wheel'}\n",
		"App.csproj":     `<Project><ItemGroup><ProjectReference Include="/host/private/Secret.csproj" /></ItemGroup></Project>`,
	} {
		write(t, root, name, content)
	}
	b := scan(t, root, "directory", "")
	if bytes.Contains(b, []byte("/host/private")) {
		t.Fatalf("host declaration path leaked: %s", b)
	}
	if !bytes.Contains(b, []byte(`"status":"partial"`)) {
		t.Fatalf("unsupported declarations looked complete: %s", b)
	}
}
