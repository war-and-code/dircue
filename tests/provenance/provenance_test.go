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

func TestMaintainedEnrySourceMatchesProvenance(t *testing.T) {
	root := filepath.Join("..", "..", "third_party")
	fork := filepath.Join(root, "go-enry")
	data, err := os.ReadFile(filepath.Join(fork, "PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		Files           map[string]string `json:"files"`
		PatchHash       string            `json:"patch_sha256"`
		GeneratorHash   string            `json:"generator_script_sha256"`
		LinguistVersion string            `json:"linguist_version"`
		EnryVersion     string            `json:"enry_version"`
	}
	if err := json.Unmarshal(data, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.LinguistVersion != "9.7.0" || provenance.EnryVersion != "v2.9.6" || len(provenance.Files) == 0 {
		t.Fatal("unexpected or empty source provenance")
	}
	check := func(filename, expected string) {
		t.Helper()
		content, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != expected {
			t.Errorf("unrecorded source change: %s; regenerate with third_party/update_enry.py", filename)
		}
	}
	for filename, expected := range provenance.Files {
		if !fs.ValidPath(filename) {
			t.Fatalf("unsafe provenance path %q", filename)
		}
		check(filepath.Join(fork, filepath.FromSlash(filename)), expected)
	}
	check(filepath.Join(root, "patches", "enry-linguist-9.7.patch"), provenance.PatchHash)
	check(filepath.Join(root, "update_enry.py"), provenance.GeneratorHash)
	err = filepath.WalkDir(fork, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(fork, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "PROVENANCE.json" || relative == "GENERATOR_WARNINGS.txt" || relative == "README.md" {
			return nil
		}
		if _, exists := provenance.Files[relative]; !exists {
			t.Errorf("unrecorded file in maintained dependency: %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
