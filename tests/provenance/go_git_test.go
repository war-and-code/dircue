package provenance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintainedGoGitSourceMatchesProvenance(t *testing.T) {
	root := filepath.Join("..", "..")
	fork := filepath.Join(root, "third_party", "go-git")
	data, err := os.ReadFile(filepath.Join(fork, "PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Module        string            `json:"module"`
		Version       string            `json:"version"`
		Files         map[string]string `json:"files"`
		Patch         string            `json:"patch"`
		PatchHash     string            `json:"patch_sha256"`
		Generator     string            `json:"generator_script"`
		GeneratorHash string            `json:"generator_script_sha256"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Module != "github.com/go-git/go-git/v5" || manifest.Version != "v5.19.2" || len(manifest.Files) == 0 {
		t.Fatal("unexpected or empty go-git provenance")
	}
	check := func(base, relative, expected string) {
		t.Helper()
		if !fs.ValidPath(relative) {
			t.Fatalf("invalid provenance path %q", relative)
		}
		content, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != expected {
			t.Errorf("unrecorded source change: %s; verify with third_party/update_go_git.py --check", relative)
		}
	}
	for relative, expected := range manifest.Files {
		check(fork, relative, expected)
	}
	check(root, manifest.Patch, manifest.PatchHash)
	check(root, manifest.Generator, manifest.GeneratorHash)
	if err := filepath.WalkDir(fork, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(fork, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative != "PROVENANCE.json" {
			if _, ok := manifest.Files[relative]; !ok {
				t.Errorf("unrecorded go-git file: %s", relative)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
