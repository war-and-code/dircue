package scanner

import (
	"context"
	"dircue/pkg/projects"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProjectInventoryIgnoresLanguageExclusions(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".gitattributes":         "*.xml linguist-detectable=false\nvendor/** linguist-vendored=true\n",
		"app/App.csproj":         `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><ItemGroup><ProjectReference Include="../lib/Lib.csproj"/></ItemGroup></Project>`,
		"lib/Lib.csproj":         `<Project Sdk="Microsoft.NET.Sdk"/>`,
		"app/Main.cs":            "class Main {}",
		"app/tests/MainTests.cs": "class MainTests {}",
		"vendor/J/pom.xml":       `<project><modelVersion>4.0.0</modelVersion><groupId>a</groupId><artifactId>b</artifactId><version>1</version></project>`,
		"logs.xml":               "<logs>just data</logs>",
	}
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Scan(context.Background(), root, Options{Source: "directory", Projects: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Projects == nil || r.SchemaVersion != "1.2.0" || len(r.Projects.Projects) != 3 {
		t.Fatalf("projects %#v", r.Projects)
	}
	for _, p := range r.Projects.Projects {
		if p.ID == "app/App.csproj" {
			if len(p.References) != 1 || p.References[0].TargetStatus != "present" {
				t.Fatalf("references %#v", p.References)
			}
		}
	}
	roles := map[string]int64{}
	for _, c := range r.Projects.Composition {
		roles[c.Name] = c.Files
	}
	if roles["data"] != 1 || roles["test"] != 1 || roles["vendored"] != 2 {
		t.Fatalf("roles %v", roles)
	}
	plain, err := Scan(context.Background(), root, Options{Source: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Languages, plain.Languages) {
		t.Fatal("project inventory changed language output")
	}
	again, err := Scan(context.Background(), root, Options{Source: "directory", Projects: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("worker-dependent report")
	}
}
func TestProjectsManifestBoundsAndTreeLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Large.csproj"), []byte(strings.Repeat("x", int(projects.MaxManifestBytes+1))), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Scan(context.Background(), root, Options{Projects: true, Source: "directory"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Projects.Status != "partial" || len(r.Projects.Diagnostics) != 1 || r.Projects.Diagnostics[0].Code != "manifest_too_large" {
		t.Fatalf("%+v", r.Projects)
	}
	r, err = Scan(context.Background(), root, Options{Projects: true, Source: "directory", MaxTreeSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != "1.2.0" || r.Projects.Status != "skipped" {
		t.Fatalf("%+v", r)
	}
}
func TestProfileReadCache(t *testing.T) {
	calls := 0
	data := []byte("0123456789")
	read := cachedFileReader(nil, job{path: "test", read: func(n int64) ([]byte, int64, error) {
		calls++
		return data[:min(int64(len(data)), n)], int64(len(data)), nil
	}})
	for _, n := range []int64{4, 4, 11, 20, 3} {
		if _, _, err := read(n); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("%d reads", calls)
	}
}

func TestProjectsUseSelectedGitTree(t *testing.T) {
	root, repo, first := gitFixture(t, map[string]string{
		"App.csproj":     `<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><ItemGroup><ProjectReference Include="Lib/Lib.csproj"/></ItemGroup></Project>`,
		"Lib/Lib.csproj": `<Project/>`,
	})
	if err := os.WriteFile(filepath.Join(root, "App.csproj"), []byte(`<Project><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "Lib/Lib.csproj")); err != nil {
		t.Fatal(err)
	}
	commitFixture(t, root, repo)
	if err := os.WriteFile(filepath.Join(root, "App.csproj"), []byte(`<Project><PropertyGroup><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source, revision, framework string
		count                       int
	}{
		{"git", first.String(), "net8.0", 2},
		{"git", "", "net9.0", 1},
		{"directory", "", "net10.0", 1},
	} {
		r, err := Scan(context.Background(), root, Options{Source: tc.source, Revision: tc.revision, Projects: true})
		if err != nil {
			t.Fatal(err)
		}
		if r.Projects.Source != tc.source || len(r.Projects.Projects) != tc.count {
			t.Fatalf("%+v", r.Projects)
		}
		p := r.Projects.Projects[0]
		if p.ID != "App.csproj" || len(p.Requirements) != 1 || p.Requirements[0].Value != tc.framework {
			t.Fatalf("%+v", p)
		}
		if tc.revision != "" && (len(p.References) != 1 || p.References[0].TargetStatus != "present") {
			t.Fatalf("%+v", p.References)
		}
		if tc.source == "git" && len(r.Projects.Tree) != 40 {
			t.Fatal("missing tree provenance")
		}
		if tc.source == "directory" && r.Projects.Tree != "" {
			t.Fatal("directory claimed Git provenance")
		}
	}
}
