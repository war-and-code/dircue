package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/environments"
)

func toolchainDeclaration(report *environments.Report, sourcePath string) (environments.ToolchainDeclaration, bool) {
	for _, declaration := range report.ToolchainDeclarations {
		if declaration.SourcePath == sourcePath {
			return declaration, true
		}
	}
	return environments.ToolchainDeclaration{}, false
}

func TestToolchainDeclarationsUseSelectedGitSnapshotAndDirectoryState(t *testing.T) {
	root, _, selected := gitFixture(t, map[string]string{
		".python-version": "3.11.9\n",
		"App.csproj":      `<Project Sdk="Microsoft.NET.Sdk"/>`,
	})
	if err := os.WriteFile(filepath.Join(root, ".python-version"), []byte("3.12.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".node-version"), []byte("22.14.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitReport, err := Scan(t.Context(), root, Options{Source: "git", Revision: selected.String(), Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := toolchainDeclaration(gitReport.Environments, ".python-version"); !ok || !reflect.DeepEqual(got.Values, []string{"3.11.9"}) {
		t.Fatalf("git report read dirty selector: %+v, found=%v", got, ok)
	}
	if _, ok := toolchainDeclaration(gitReport.Environments, ".node-version"); ok {
		t.Fatal("untracked directory selector leaked into selected Git snapshot")
	}
	directoryReport, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := toolchainDeclaration(directoryReport.Environments, ".python-version"); !ok || !reflect.DeepEqual(got.Values, []string{"3.12.4"}) {
		t.Fatalf("directory report did not use live selector: %+v, found=%v", got, ok)
	}
	if got, ok := toolchainDeclaration(directoryReport.Environments, ".node-version"); !ok || !reflect.DeepEqual(got.Values, []string{"22.14.0"}) {
		t.Fatalf("directory report omitted untracked selector: %+v, found=%v", got, ok)
	}
}

func TestToolchainScannerRetainsNestedAndNonregularCandidates(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{
		".nvmrc":                "lts/*\n",
		"service/.node-version": "22.15.0\n",
		"service/.nvmrc":        "20.18.0\n",
		"service/App.csproj":    `<Project Sdk="Microsoft.NET.Sdk"/>`,
	})
	if err := os.MkdirAll(filepath.Join(root, "linked"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.nvmrc", filepath.Join(root, "linked", ".nvmrc")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := toolchainDeclaration(r.Environments, ".nvmrc"); !ok || !reflect.DeepEqual(got.Values, []string{"lts/*"}) {
		t.Fatalf("root alias missing: %+v, found=%v", got, ok)
	}
	if got, ok := toolchainDeclaration(r.Environments, "service/.node-version"); !ok || !reflect.DeepEqual(got.Values, []string{"22.15.0"}) {
		t.Fatalf("nested node selector missing: %+v, found=%v", got, ok)
	}
	if got, ok := toolchainDeclaration(r.Environments, "linked/.nvmrc"); !ok || got.State != "unresolved" || len(got.Values) != 0 {
		t.Fatalf("symlink candidate should remain unresolved: %+v, found=%v", got, ok)
	}
	foundConflict, foundNested, foundNonregular := false, false, false
	for _, conflict := range r.Environments.Conflicts {
		if conflict.Dimension == "toolchain-declaration" && conflict.ContextID == "toolchain:node@service" {
			foundConflict = true
		}
	}
	for _, boundary := range r.Environments.Boundaries {
		foundNested = foundNested || boundary.Reason == "nested-toolchain-declaration"
		foundNonregular = foundNonregular || boundary.Path == "linked/.nvmrc" && boundary.Reason == "toolchain-file-not-regular"
	}
	if !foundConflict || !foundNested || !foundNonregular {
		t.Fatalf("missing conflict/nested/symlink bounds: %+v %+v", r.Environments.Conflicts, r.Environments.Boundaries)
	}
}

func TestToolchainDirectoryWorkersBoundsCancellationAndOfflineInputs(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{
		".python-version":         "3.12.4\n",
		".node-version":           "22.15.0\n",
		"app/rust-toolchain.toml": "[toolchain]\nchannel = \"stable\"\n",
		"app/App.csproj":          `<Project Sdk="Microsoft.NET.Sdk"/>`,
	})
	// An empty PATH proves that observation does not need Python/Node/Rust
	// managers installed or executable. Unusable proxies make accidental HTTP
	// requests fail immediately; the local scan consumes only selected bytes.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	one, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	eight, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, Workers: 8})
	if err != nil {
		t.Fatal(err)
	}
	oneJSON, _ := json.Marshal(one.Environments)
	eightJSON, _ := json.Marshal(eight.Environments)
	if string(oneJSON) != string(eightJSON) {
		t.Fatalf("environment declarations depend on worker count:\n%s\n%s", oneJSON, eightJSON)
	}
	limited, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, MaxFileBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := toolchainDeclaration(limited.Environments, ".python-version"); !ok || got.State != "unresolved" || limited.Environments.Coverage.OmittedToolchainFiles == 0 {
		t.Fatalf("max-file-bytes did not bound toolchain reads: %+v, found=%v", got, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root, Options{Source: "directory", Environments: true, DeclarationsOnly: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	treeLimited, err := Scan(context.Background(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, MaxTreeSize: 1})
	if err != nil || treeLimited.Environments == nil || treeLimited.Environments.Status != "skipped" {
		t.Fatalf("tree bound report=%+v err=%v", treeLimited.Environments, err)
	}
}

func TestToolchainContinueOnUnreadableSelectedFile(t *testing.T) {
	root := t.TempDir()
	writeEnvironmentFixture(t, root, map[string]string{".nvmrc": "22.15.0\n", "App.csproj": `<Project Sdk="Microsoft.NET.Sdk"/>`})
	if err := os.Chmod(filepath.Join(root, ".nvmrc"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".nvmrc"), 0600) })
	if _, err := os.ReadFile(filepath.Join(root, ".nvmrc")); err == nil {
		t.Skip("current user can read mode-000 files")
	}
	r, err := Scan(t.Context(), root, Options{Source: "directory", Environments: true, DeclarationsOnly: true, ErrorPolicy: ErrorPolicyContinue})
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := toolchainDeclaration(r.Environments, ".nvmrc")
	if !ok || declaration.State != "unresolved" || r.Environments.Status != "partial" {
		t.Fatalf("continue policy lost unresolved declaration: %+v", r.Environments)
	}
	found := false
	for _, diagnostic := range r.Environments.Diagnostics {
		found = found || diagnostic.Code == "file-read-error" && diagnostic.Path == ".nvmrc"
	}
	if !found {
		t.Fatalf("continue policy omitted read diagnostic: %+v", r.Environments.Diagnostics)
	}
	for _, diagnostic := range r.Environments.Diagnostics {
		if diagnostic.Code == "file-read-error" && diagnostic.Message != "Selected toolchain file could not be read." {
			t.Fatalf("read diagnostic leaked content or changed the bounded message: %+v", diagnostic)
		}
	}
}
