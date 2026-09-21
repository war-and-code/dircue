package scanner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"dircue/pkg/focus"
	"dircue/pkg/profile"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func focusedMetricFiles(t *testing.T, metrics *profile.MetricsReport) map[string]profile.FileMetrics {
	t.Helper()
	if metrics == nil || metrics.Files == nil {
		t.Fatal("missing focused metric file evidence")
	}
	files := map[string]profile.FileMetrics{}
	for _, file := range *metrics.Files {
		files[file.Path] = file
	}
	return files
}

func TestFocusedMetricsKeepPrimaryRelatedAndContextSeparate(t *testing.T) {
	root := fixtures(t, map[string]string{
		"Directory.Build.props":     `<Project><PropertyGroup><LangVersion>preview</LangVersion></PropertyGroup></Project>`,
		"src/a/a.csproj":            `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><ProjectReference Include="../../lib/b/b.csproj" /></ItemGroup></Project>`,
		"src/a/A.cs":                "class A {}\n",
		"src/a/nested/package.json": `{ "name": "nested" }`,
		"src/a/nested/hidden.ts":    "export const hidden = true;\n",
		"lib/b/b.csproj":            `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><ProjectReference Include="../../src/a/a.csproj" /></ItemGroup></Project>`,
		"lib/b/B.cs":                "class B {}\n",
		"outside.cs":                "class Outside {}\n",
	})
	report, err := Scan(context.Background(), root, Options{
		Source:  "directory",
		Focus:   &focus.Request{Project: "src/a/a.csproj", Related: []string{"lib/b/b.csproj"}},
		Metrics: &MetricsOptions{IncludeFiles: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != profile.TargetedSchemaVersion || report.Focus == nil || report.FocusedMetrics == nil || report.Metrics != nil {
		t.Fatalf("focused modules = %#v", report)
	}
	if report.FocusedMetrics.ScopeID != report.Focus.Scope.ID || len(report.FocusedMetrics.Related) != 1 {
		t.Fatalf("scope binding = %#v", report.FocusedMetrics)
	}
	primary := focusedMetricFiles(t, report.FocusedMetrics.Primary)
	if primary["src/a/A.cs"].Status != "counted" || primary["src/a/nested/hidden.ts"].Status != "" || primary["lib/b/B.cs"].Status != "" {
		t.Fatalf("primary metric population = %#v", primary)
	}
	related := focusedMetricFiles(t, report.FocusedMetrics.Related[0].Metrics)
	if report.FocusedMetrics.Related[0].Project != "lib/b/b.csproj" || related["lib/b/B.cs"].Status != "counted" || related["src/a/A.cs"].Status != "" {
		t.Fatalf("related metric population = %#v", report.FocusedMetrics.Related[0])
	}
	if report.FocusedMetrics.Primary.Totals.Files != 1 || report.FocusedMetrics.Related[0].Metrics.Totals.Files != 1 {
		t.Fatal("primary and related denominators were combined")
	}
	contextPaths := []string{}
	for _, item := range report.Focus.Context {
		if item.ProjectID == "src/a/a.csproj" {
			contextPaths = append(contextPaths, item.Path)
		}
	}
	if !slices.Contains(contextPaths, "Directory.Build.props") || !slices.Contains(contextPaths, "src/a/a.csproj") || !slices.Contains(contextPaths, "lib/b/b.csproj") {
		t.Fatalf("focus context = %#v", report.Focus.Context)
	}
}

func TestFocusedInvalidNestedManifestDoesNotLeakIntoParentMetrics(t *testing.T) {
	root := fixtures(t, map[string]string{
		"app/app.csproj":     `<Project Sdk="Microsoft.NET.Sdk" />`,
		"app/Own.cs":         "class Own {}\n",
		"app/bad/bad.csproj": `<not-project />`,
		"app/bad/Hidden.cs":  "class Hidden {}\n",
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Focus: &focus.Request{Project: "app/app.csproj"}, Metrics: &MetricsOptions{IncludeFiles: true}})
	if err != nil {
		t.Fatal(err)
	}
	files := focusedMetricFiles(t, report.FocusedMetrics.Primary)
	if files["app/Own.cs"].Status != "counted" {
		t.Fatalf("own source missing: %#v", files)
	}
	if _, found := files["app/bad/Hidden.cs"]; found {
		t.Fatal("invalid nested project source leaked into parent metrics")
	}
	if report.Focus.Status != "partial" || report.Focus.Coverage.UnresolvedFiles == 0 {
		t.Fatalf("invalid boundary was not qualified: %#v", report.Focus)
	}
}

func TestFocusedNonregularNestedManifestIsOwnershipBarrier(t *testing.T) {
	root := fixtures(t, map[string]string{"app/app.csproj": `<Project Sdk="Microsoft.NET.Sdk" />`, "app/Own.cs": "class Own {}\n", "app/nested/Hidden.cs": "class Hidden {}\n"})
	if err := os.Symlink("missing", filepath.Join(root, "app/nested/pyproject.toml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	report, err := Scan(context.Background(), root, Options{Source: "directory", Focus: &focus.Request{Project: "app/app.csproj"}, Metrics: &MetricsOptions{IncludeFiles: true}})
	if err != nil {
		t.Fatal(err)
	}
	files := focusedMetricFiles(t, report.FocusedMetrics.Primary)
	if _, found := files["app/nested/Hidden.cs"]; found {
		t.Fatal("source below nonregular manifest entered parent metrics")
	}
	if report.Focus.Coverage.OwnershipBarriers != 1 || report.FocusedMetrics.Primary.Status != "partial" {
		t.Fatalf("barrier qualification = %#v / %#v", report.Focus.Coverage, report.FocusedMetrics.Primary)
	}
}

func TestFocusedStoredGitSymlinkManifestIsOwnershipBarrier(t *testing.T) {
	root, repo, _ := gitFixture(t, map[string]string{"app/app.csproj": `<Project Sdk="Microsoft.NET.Sdk" />`, "app/Own.cs": "class Own {}\n", "app/nested/Hidden.cs": "class Hidden {}\n"})
	commitStoredSymlink(t, repo, "app/nested/pyproject.toml", "missing")
	report, err := Scan(context.Background(), root, Options{Focus: &focus.Request{Project: "app/app.csproj"}, Metrics: &MetricsOptions{IncludeFiles: true}})
	if err != nil {
		t.Fatal(err)
	}
	files := focusedMetricFiles(t, report.FocusedMetrics.Primary)
	if _, found := files["app/nested/Hidden.cs"]; found {
		t.Fatal("source below stored Git symlink manifest entered parent metrics")
	}
	if report.Focus.Coverage.OwnershipBarriers != 1 {
		t.Fatalf("Git barrier coverage = %#v", report.Focus.Coverage)
	}
}

func commitStoredSymlink(t *testing.T, repo *git.Repository, name, target string) {
	t.Helper()
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	blob := repo.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(target)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	blobHash, err := repo.Storer.SetEncodedObject(blob)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(name, "/")
	treeHash := storeTreeEntry(t, repo, parent.TreeHash, parts, object.TreeEntry{Name: parts[len(parts)-1], Mode: filemode.Symlink, Hash: blobHash})
	when := time.Unix(1700000001, 0)
	commit := &object.Commit{Author: object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: when}, Committer: object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: when}, Message: "stored symlink", TreeHash: treeHash, ParentHashes: []plumbing.Hash{parent.Hash}}
	encoded := repo.Storer.NewEncodedObject()
	if err = commit.Encode(encoded); err != nil {
		t.Fatal(err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Storer.SetReference(plumbing.NewHashReference(head.Name(), commitHash)); err != nil {
		t.Fatal(err)
	}
}

func storeTreeEntry(t *testing.T, repo *git.Repository, treeHash plumbing.Hash, parts []string, leaf object.TreeEntry) plumbing.Hash {
	t.Helper()
	tree, err := repo.TreeObject(treeHash)
	if err != nil {
		t.Fatal(err)
	}
	entries := slices.Clone(tree.Entries)
	if len(parts) == 1 {
		entries = append(entries, leaf)
	} else {
		index := slices.IndexFunc(entries, func(entry object.TreeEntry) bool { return entry.Name == parts[0] && entry.Mode == filemode.Dir })
		if index < 0 {
			t.Fatalf("missing parent tree %q", parts[0])
		}
		entries[index].Hash = storeTreeEntry(t, repo, entries[index].Hash, parts[1:], leaf)
	}
	slices.SortFunc(entries, func(a, b object.TreeEntry) int { return strings.Compare(a.Name, b.Name) })
	encoded := repo.Storer.NewEncodedObject()
	if err = (&object.Tree{Entries: entries}).Encode(encoded); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestAffectedByFocusHasNoMetricsOrPrimarySelection(t *testing.T) {
	root := fixtures(t, map[string]string{
		"Directory.Build.props": `<Project />`,
		"a/a.csproj":            `<Project Sdk="Microsoft.NET.Sdk" />`,
		"b/b.csproj":            `<Project Sdk="Microsoft.NET.Sdk" />`,
	})
	report, err := Scan(context.Background(), root, Options{Source: "directory", Focus: &focus.Request{AffectedBy: "Directory.Build.props"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Focus == nil || report.Focus.AffectedProjects == nil || len(report.Focus.AffectedProjects.Projects) != 2 || report.Focus.PrimaryProject != nil || report.FocusedMetrics != nil {
		t.Fatalf("affected-by report = %#v", report.Focus)
	}
	if _, err := Scan(context.Background(), root, Options{Source: "directory", Focus: &focus.Request{AffectedBy: "Directory.Build.props"}, Metrics: &MetricsOptions{}}); err == nil || !strings.Contains(err.Error(), "does not support metrics") {
		t.Fatalf("affected-by metrics error = %v", err)
	}
}
