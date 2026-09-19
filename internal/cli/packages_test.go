package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dircue/pkg/profile"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestImportPackagesCLI(t *testing.T) {
	fixture, err := filepath.Abs("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "dotnet", "app")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "App.csproj"), []byte("<Project/>"), 0600); err != nil {
		t.Fatal(err)
	}
	// Import does not require an installed Syft, Git, package manager, or runtime.
	t.Setenv("PATH", t.TempDir())
	for _, prefix := range [][]string{{"analyze", "packages"}, {"analyze", "all", "--discovery", "--graph"}} {
		args := append(prefix, "--syft-report", fixture, "--syft-root", "/", "--source", "directory", "--json", root)
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var r profile.Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.PackageEvidence == nil || r.Projects == nil || r.SchemaVersion != profile.EnhancedSchemaVersion {
			t.Fatal("package evidence and project context missing")
		}
		p := r.PackageEvidence
		if len(p.Packages) != 6 || len(p.Relationships) != 15 || p.Source.Match != "unknown" || p.Coverage.ProviderScan != "unknown" {
			t.Fatalf("unexpected imported evidence: %+v", p)
		}
		foundCandidate := false
		for _, item := range p.Packages {
			for _, loc := range item.Locations {
				if len(loc.ProjectIDs) > 0 {
					foundCandidate = true
					if loc.Association != "source-unverified" {
						t.Fatalf("unattested source was treated as verified: %+v", loc)
					}
				}
			}
		}
		if !foundCandidate {
			t.Fatal("expected an explicit unverified project candidate")
		}
		if strings.Contains(out.String(), "fixture-cache-home") {
			t.Fatal("provider host paths leaked")
		}
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "packages", "--syft-report", fixture, "--json", "--source", "directory", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var unmapped profile.Report
	if err := json.Unmarshal(out.Bytes(), &unmapped); err != nil {
		t.Fatal(err)
	}
	for _, item := range unmapped.PackageEvidence.Packages {
		for _, loc := range item.Locations {
			if loc.Path != "" || len(loc.ProjectIDs) != 0 {
				t.Fatal("mapping must require explicit caller input")
			}
		}
	}
}

func TestPackageSourceBindingUsesSelectedTree(t *testing.T) {
	fixture, err := filepath.Abs("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "dotnet", "app", "App.csproj")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("<Project/>"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("dotnet/app/App.csproj"); err != nil {
		t.Fatal(err)
	}
	commit, err := w.Commit("Fixture", &git.CommitOptions{Author: &object.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	obj, err := repo.CommitObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	// The report binding names committed content, even with a broken dirty manifest.
	if err := os.WriteFile(projectPath, []byte("<broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reportDigest, tree, match string
		limit                           bool
	}{
		{"matched tree", digest, obj.TreeHash.String(), "matched", false},
		{"commit is not tree", digest, commit.String(), "mismatched", false},
		{"wrong report", strings.Repeat("0", 64), obj.TreeHash.String(), "mismatched", false},
		{"partial inventory", digest, obj.TreeHash.String(), "matched", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"analyze", "packages", "--json", "--source", "git", "--syft-report", fixture, "--syft-root", "/", "--syft-report-sha256", tc.reportDigest, "--syft-source-tree", tc.tree}
			if tc.limit {
				args = append(args, "--tree-size", "1")
			}
			args = append(args, root)
			var out, stderr bytes.Buffer
			if err := Execute(context.Background(), args, &out, &stderr); err != nil {
				t.Fatal(err)
			}
			var r profile.Report
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			p := r.PackageEvidence
			if p.Source.Match != tc.match {
				t.Fatalf("source match: %s", p.Source.Match)
			}
			assigned := 0
			for _, item := range p.Packages {
				for _, loc := range item.Locations {
					assigned += len(loc.ProjectIDs)
				}
			}
			if tc.match == "mismatched" || tc.limit {
				if assigned != 0 || p.Status != "partial" {
					t.Fatalf("incomplete or mismatched evidence attributed: %+v", p)
				}
			} else if assigned == 0 || r.Projects.Status != "complete" {
				t.Fatal("committed matching inventory was not used")
			}
		})
	}
}

func TestPackageImportArgumentAndInputFailures(t *testing.T) {
	fixture, err := filepath.Abs("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bad := filepath.Join(root, "broken.json")
	if err := os.WriteFile(bad, []byte("{} trailing"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"analyze", "packages", root},
		{"analyze", "all", "--syft-root", "/", root},
		{"analyze", "all", "--syft-report=", root},
		{"analyze", "packages", "--syft-report", bad, root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-report-max-bytes", "1", root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-report-max-bytes", "134217729", root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-source-tree", strings.Repeat("a", 40), root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-source-tree", strings.Repeat("a", 40), "--syft-report-sha256", strings.Repeat("b", 64), "--source", "directory", root},
		{"analyze", "packages", "--syft-report", root, root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-root", "../outside", root},
		{"analyze", "packages", "--syft-report", fixture, "--syft-root=", root},
	} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), args, &out, &stderr); err == nil || out.Len() != 0 {
			t.Fatalf("expected failure without report: %v => %s", args, out.String())
		}
	}
}

func TestPackageFlagErrorsPrecedeReportAndInventoryReads(t *testing.T) {
	var out, stderr bytes.Buffer
	err := Execute(context.Background(), []string{"analyze", "packages", "--syft-report", "/missing/report.json", "--syft-root", "../escape", "/missing/inventory"}, &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--syft-root") || out.Len() != 0 {
		t.Fatalf("expected actionable early mapping error, got %v", err)
	}
}

func TestPackageTextDisclosesUnverifiedAssociations(t *testing.T) {
	fixture, err := filepath.Abs("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Root.csproj"), []byte("<Project/>"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "packages", "--syft-report", fixture, "--syft-root", "/", "--source", "directory", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "source match: unknown") || !strings.Contains(out.String(), "Package locations, source-unverified:") {
		t.Fatalf("text hides the association's uncertainty: %s", out.String())
	}
}
