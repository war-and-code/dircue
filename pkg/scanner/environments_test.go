package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeEnvironmentFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnvironmentsDirectoryNearestGlobalJSON(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{
		"global.json":        `{"sdk":{"version":"8.0.100","rollForward":"disable"}}`,
		"src/global.json":    `{"sdk":{"version":"9.0.200","allowPrerelease":false}}`,
		"src/App/App.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`,
	})
	r, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Environments == nil || r.Environments.Status != "complete" || len(r.Environments.Selections) != 1 {
		t.Fatalf("report: %+v", r.Environments)
	}
	s := r.Environments.Selections[0]
	if s.GlobalJSON != "src/global.json" || s.SDKVersion != "9.0.200" || s.StartBasis != "modeled-project-root" {
		t.Fatalf("selection: %+v", s)
	}
	for _, q := range r.Environments.Requirements {
		if q.Kind == "target-framework" && q.Dimension != "target-framework" {
			t.Fatalf("requirement: %+v", q)
		}
	}
}

func TestEnvironmentsGitSnapshotIgnoresDirtyGlobalJSON(t *testing.T) {
	root, repo, first := gitFixture(t, map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`, "App.csproj": `<Project Sdk="Microsoft.NET.Sdk"/>`})
	if err := os.WriteFile(filepath.Join(root, "global.json"), []byte(`{"sdk":{"version":"9.0.100"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	commitFixture(t, root, repo)
	if err := os.WriteFile(filepath.Join(root, "global.json"), []byte(`{"sdk":{"version":"10.0.100"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Scan(context.Background(), root, Options{Source: "git", Revision: first.String(), Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Environments.Selections[0].SDKVersion; got != "8.0.100" {
		t.Fatalf("selected dirty file: %s", got)
	}
}

func TestEnvironmentsNearestNonregularBlocksParent(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`, "src/App.csproj": `<Project/>`})
	if err := os.Symlink("../global.json", filepath.Join(root, "src/global.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Environments.Selections[0]
	if s.GlobalJSON != "src/global.json" || s.State != "unresolved" || s.SDKVersion != "" {
		t.Fatalf("selection: %+v", s)
	}
}

func TestEnvironmentsLimitsCancellationAndWorkers(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`, "a/App.csproj": `<Project/>`, "b/Lib.csproj": `<Project/>`})
	if _, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, MaxFileBytes: 8}); err != nil {
		t.Fatal(err)
	}
	one, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	eight, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, Workers: 8})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(one.Environments)
	b, _ := json.Marshal(eight.Environments)
	if string(a) != string(b) {
		t.Fatal("worker dependent environments")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root, Options{Source: "directory", Environments: true, DeclarationsOnly: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error: %v", err)
	}
	if _, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, MaxTreeSize: 1}); err == nil || !strings.Contains(err.Error(), "environment inventory omitted") {
		t.Fatalf("tree limit error: %v", err)
	}
}

func TestEnvironmentsDoNotChangeDefaultReports(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{"global.json": `{"sdk":{"version":"8.0.100"}}`, "App.csproj": `<Project/>`, "App.cs": "class App {}"})
	old, err := Scan(context.Background(), root, Options{Source: "directory", Projects: true})
	if err != nil {
		t.Fatal(err)
	}
	next, err := Scan(context.Background(), root, Options{Source: "directory", Projects: true, Environments: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Languages, next.Languages) || !reflect.DeepEqual(old.Projects, next.Projects) || old.Summary != next.Summary {
		t.Fatal("environment analysis changed existing reports")
	}
}
