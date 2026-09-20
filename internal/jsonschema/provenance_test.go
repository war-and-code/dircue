package jsonschema_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestPinnedSourceProvenance(t *testing.T) {
	data, err := os.ReadFile("PROVENANCE.json")
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Module        string            `json:"module"`
		Version       string            `json:"version"`
		Commit        string            `json:"upstream_commit"`
		ModuleSum     string            `json:"module_sum"`
		ModSum        string            `json:"go_mod_sum"`
		Archive       string            `json:"archive_sha256"`
		Files         map[string]string `json:"files"`
		Upstream      map[string]string `json:"upstream_files"`
		Patch         string            `json:"patch"`
		PatchHash     string            `json:"patch_sha256"`
		Generator     string            `json:"generator_script"`
		GeneratorHash string            `json:"generator_script_sha256"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	const module = "github.com/santhosh-tekuri/jsonschema/v5"
	if record.Module != module || record.Version != "v5.3.1" || record.Commit != "16bce71af51f6a4a775f11e649a347a8803940d3" || record.ModuleSum != "h1:lZUw3E0/J3roVtGQ+SCrUrg3ON6NgVqpn3+iol9aGu4=" || record.ModSum != "h1:uToXkOrWAZ6/Oc07xWQrPOhJotwFIyu2bBVN41fcDUY=" || record.Archive != "6c953c3751cca3003d0e7f6d775c7c3b2e4b1eeb1fa2e8d68786ead53b083094" {
		t.Fatal("upstream identity changed; review oracle and snapshot together")
	}
	verify := func(path, expected string) {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("snapshot input is not a regular file: %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != expected {
			t.Fatalf("snapshot hash differs: %s; regenerate after reviewing the patch", path)
		}
	}
	if len(record.Files) != 12 || len(record.Upstream) != 12 {
		t.Fatal("retained snapshot population changed")
	}
	for file, hash := range record.Files {
		if filepath.Base(file) != file || file == "." || file == ".." {
			t.Fatal("unexpected snapshot path")
		}
		if len(record.Upstream[file]) != 64 {
			t.Fatal("missing upstream file hash")
		}
		verify(file, hash)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") && record.Files[entry.Name()] == "" {
			t.Fatalf("untracked runtime snapshot entry: %s", entry.Name())
		}
	}
	if record.Patch != "third_party/patches/jsonschema-lazy-metaschemas.patch" || record.Generator != "third_party/update_jsonschema.py" {
		t.Fatal("unexpected provenance resource path")
	}
	verify(filepath.Join("../..", record.Patch), record.PatchHash)
	verify(filepath.Join("../..", record.Generator), record.GeneratorHash)
	manifest, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, requirement := range mod.Require {
		if requirement.Mod.Path == module {
			found = requirement.Mod.Version == record.Version
		}
	}
	if !found {
		t.Fatal("independent upstream oracle version changed")
	}
	for _, replacement := range mod.Replace {
		if replacement.Old.Path == module {
			t.Fatal("oracle must use unmodified upstream, not a replacement")
		}
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range build.Deps {
			if dep.Path == module && (dep.Version != record.Version || dep.Replace != nil) {
				t.Fatal("linked oracle differs from pinned upstream")
			}
		}
	}
}
