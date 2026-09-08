package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonpositiveTreeLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []string{"0", "-1"} {
		output, _, err := invoke("--json", "--tree-size="+limit, root)
		if err != nil || strings.TrimSpace(output) != "{}" {
			t.Fatalf("limit %s: %q %v", limit, output, err)
		}
		output, _, err = invoke("--json", "--tree-size="+limit, filepath.Join(root, "source.go"))
		if err != nil || !strings.Contains(output, `"language":"Go"`) {
			t.Fatalf("single-file limit %s: %q %v", limit, output, err)
		}
	}
}

// Verified against the pinned Linguist 9.7.0 Docker reference: language
// attributes can force empty and binary files into repository statistics.
func TestAttributedEmptyAndBinaryLanguageOutput(t *testing.T) {
	for _, content := range [][]byte{nil, []byte("print(1)\n\x00\xff\x01\x02")} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.py linguist-language=Python\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "source.py"), content, 0600); err != nil {
			t.Fatal(err)
		}
		output, _, err := invoke("--json", "--breakdown", root)
		if err != nil {
			t.Fatal(err)
		}
		var languages map[string]struct {
			Size       int      `json:"size"`
			Percentage string   `json:"percentage"`
			Files      []string `json:"files"`
		}
		if err := json.Unmarshal([]byte(output), &languages); err != nil {
			t.Fatal(err)
		}
		got := languages["Python"]
		percentage := "100.00"
		if len(content) == 0 {
			percentage = "NaN"
		}
		if len(languages) != 1 || got.Size != len(content) || got.Percentage != percentage || len(got.Files) != 1 || got.Files[0] != "source.py" {
			t.Fatalf("unexpected output: %s", output)
		}
		if len(content) == 0 {
			text, _, err := invoke(root)
			if err != nil || !strings.HasPrefix(text, "NaN%") {
				t.Fatalf("empty attributed text: %q, %v", text, err)
			}
			// Full reports keep numeric percentages JSON-safe rather than emit NaN.
			output, _, err := invoke("analyze", "all", "--json", root)
			if err != nil || !json.Valid([]byte(output)) {
				t.Fatalf("invalid structured JSON: %q, %v", output, err)
			}
		}
	}
}
