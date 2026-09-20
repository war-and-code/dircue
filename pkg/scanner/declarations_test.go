package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeclarationsUseSelectedSnapshot(t *testing.T) {
	root, repo, first := gitFixture(t, map[string]string{
		"go.work":        "go 1.26\nuse ./lib\n",
		"lib/go.mod":     "module example.invalid/first\ngo 1.26\n",
		"pyproject.toml": "[project]\nname='first'\nversion='1.0'\n",
	})
	if err := os.WriteFile(filepath.Join(root, "lib/go.mod"), []byte("module example.invalid/second\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	commitFixture(t, root, repo)
	if err := os.WriteFile(filepath.Join(root, "lib/go.mod"), []byte("module example.invalid/dirty\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ source, revision, name string }{{"git", first.String(), "example.invalid/first"}, {"git", "", "example.invalid/second"}, {"directory", "", "example.invalid/dirty"}} {
		r, e := Scan(context.Background(), root, Options{Source: tc.source, Revision: tc.revision, Declarations: true})
		if e != nil {
			t.Fatal(e)
		}
		if r.Declarations.Source != tc.source {
			t.Fatal(r.Declarations)
		}
		if (r.Declarations.Tree != "") != (tc.source == "git") {
			t.Fatal("wrong tree identity")
		}
		found := false
		for _, p := range r.Declarations.Projects {
			if p.ID == "lib/go.mod" {
				found = true
				if p.Name != tc.name {
					t.Fatalf("%+v", p)
				}
			}
		}
		if !found {
			t.Fatal("missing module")
		}
	}
}
func TestDeclarationsDoNotChangeLanguageOrProjectReports(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{"App.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`, "App.cs": "class App {}\n", "package.json": `{"name":"app","scripts":{"test":"exit 99"}}`, "node_modules/x/package.json": "invalid and must not parse"} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old, e := Scan(context.Background(), root, Options{Source: "directory", Projects: true})
	if e != nil {
		t.Fatal(e)
	}
	next, e := Scan(context.Background(), root, Options{Source: "directory", Projects: true, Declarations: true})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(old.Languages, next.Languages) || !reflect.DeepEqual(old.Projects, next.Projects) || old.Summary != next.Summary {
		t.Fatal("new module altered existing module output")
	}
	if next.Declarations.Coverage.ManifestCandidates != 2 || next.Declarations.Status != "complete" {
		t.Fatalf("%+v", next.Declarations)
	}
	sequential, e := Scan(context.Background(), root, Options{Source: "directory", Projects: true, Declarations: true, Workers: 1})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(next)
	b, _ := json.Marshal(sequential)
	if string(a) != string(b) {
		t.Fatal("worker dependent output")
	}
}
func TestDeclarationCandidateDoesNotReadInstalledNPM(t *testing.T) {
	p := declarationCandidate(nil, job{path: "node_modules/tool/package.json", read: func(int64) ([]byte, int64, error) { t.Fatal("installed contents read"); return nil, 0, nil }})
	if p != nil {
		t.Fatal("selected installed manifest")
	}
}
func TestDeclarationsSkippedTreeHasNoObservations(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"x"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, e := Scan(context.Background(), root, Options{Source: "directory", Declarations: true, MaxTreeSize: 1})
	if e != nil {
		t.Fatal(e)
	}
	if r.Declarations.Status != "skipped" || len(r.Declarations.Projects) != 0 || r.SchemaVersion != "1.4.0" {
		t.Fatalf("%+v", r)
	}
}

func TestDeclarationsOnlyDoesNotReadOtherContents(t *testing.T) {
	reads := 0
	value, e := analyzeFile(context.Background(), nil, job{path: "logs/huge.xml", size: 2 << 30, read: func(int64) ([]byte, int64, error) { reads++; t.Fatal("nonmanifest payload read"); return nil, 0, nil }}, Options{Declarations: true, DeclarationsOnly: true})
	if e != nil || reads != 0 || !value.declarationSelected || !value.skipped || value.declarationFile != nil {
		t.Fatalf("%+v %v", value, e)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/demo\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	single, e := Scan(context.Background(), root, Options{Source: "directory", Declarations: true, DeclarationsOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	full, e := Scan(context.Background(), root, Options{Source: "directory", Declarations: true})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(single.Declarations, full.Declarations) {
		t.Fatal("module-only scan changed declarations")
	}
	if len(single.Languages) != 0 || single.Summary.AnalyzedFiles != 0 {
		t.Fatal("module-only scan classified languages")
	}
}
