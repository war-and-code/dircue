package reportdiff_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

// These modules reuse declarations, so combining their individually valid
// records from different selected sources is not a valid aggregate report.
func TestSavedDependencyContextRejectsConflictingDeclarationSources(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"package.json":      `{"dependencies":{"example":"1"}}`,
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"example":"1"}}}}`,
		".python-version":   "3.12.1\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	original := analyzeSavedProfile(t, root)
	for _, tc := range []struct {
		name   string
		mutate func(*profile.Report)
		valid  bool
	}{
		{"same directory", func(*profile.Report) {}, true},
		{"same git tree", func(p *profile.Report) {
			p.Declarations.Source, p.Declarations.Tree = "git", strings.Repeat("a", 40)
			p.Environments.Source, p.Environments.Tree = "git", p.Declarations.Tree
			p.Lockfiles.Source, p.Lockfiles.Tree = "git", p.Declarations.Tree
		}, true},
		{"environment source mode", func(p *profile.Report) {
			p.Environments.Source, p.Environments.Tree = "git", strings.Repeat("a", 40)
		}, false},
		{"lockfile source mode", func(p *profile.Report) {
			p.Lockfiles.Source, p.Lockfiles.Tree = "git", strings.Repeat("a", 40)
		}, false},
		{"environment tree", func(p *profile.Report) {
			p.Declarations.Source, p.Declarations.Tree = "git", strings.Repeat("a", 40)
			p.Environments.Source, p.Environments.Tree = "git", strings.Repeat("b", 40)
			p.Lockfiles.Source, p.Lockfiles.Tree = "git", p.Declarations.Tree
		}, false},
		{"lockfile tree", func(p *profile.Report) {
			p.Declarations.Source, p.Declarations.Tree = "git", strings.Repeat("a", 40)
			p.Environments.Source, p.Environments.Tree = "git", p.Declarations.Tree
			p.Lockfiles.Source, p.Lockfiles.Tree = "git", strings.Repeat("b", 40)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p profile.Report
			if err := json.Unmarshal(original, &p); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&p)
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			_, loadErr := reportdiff.Load(bytes.NewReader(data))
			_, _, evidenceErr := reportdiff.ReadEvidence(bytes.NewReader(data))
			if (loadErr == nil) != tc.valid || (evidenceErr == nil) != tc.valid {
				t.Fatalf("valid=%t: comparison loader=%v, evidence loader=%v", tc.valid, loadErr, evidenceErr)
			}
		})
	}
}
