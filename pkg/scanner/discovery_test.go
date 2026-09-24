package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/profile"
)

func discoveryOptions() Options {
	return Options{Source: "directory", Discovery: true, DiscoveryOnly: true, Workers: 1}
}
func TestDiscoveryDoesNotReadFilePayload(t *testing.T) {
	opts := discoveryOptions()
	for _, filename := range []string{"pom.xml", "main.go", "app.dll", "data.xml", "unknown"} {
		v, err := analyzeFile(context.Background(), nil, job{path: filename, size: 2 << 30, read: func(int64) ([]byte, int64, error) { t.Fatal("discovery requested file content"); return nil, 0, nil }}, opts)
		if err != nil || v.discoveryFile == nil || v.discoveryFile.Size != 2<<30 {
			t.Fatalf("%s: %+v %v", filename, v, err)
		}
		if v.language != "" || v.metrics != nil || v.structural != nil || len(v.projectDocument.Projects) > 0 {
			t.Fatalf("content analysis ran: %+v", v)
		}
	}
}
func TestDiscoveryMixedDirectoryAndLegacyCompatibility(t *testing.T) {
	files := map[string]string{
		".gitattributes":        "vendor/tests/*.cs linguist-generated=true\n",
		"Directory.Build.props": "<not actually valid", "project/App.csproj": "not XML", "nested/pom.xml": "not a project", "logs/events.xml": strings.Repeat("<event/>\n", 1000), "vendor/package.jar": "fake archive", "vendor/tests/AppTests.cs": "class AppTests {}", "unknown.blob": "\x00\x01", "src/main.go": goSource,
	}
	root := fixtures(t, files)
	opts := discoveryOptions()
	opts.MaxFileBytes = 1
	r, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	d := r.Discovery
	if d == nil || d.Status != "complete" || d.Source.Mode != "directory" || d.Source.Tree != "" || d.Source.Consistency != "live_directory_metadata" {
		t.Fatalf("discovery: %+v", d)
	}
	var bytes int64
	for _, v := range files {
		bytes += int64(len(v))
	}
	if d.Inventory != (discovery.Counts{Files: int64(len(files)), Bytes: bytes}) || d.ClassificationBytesRead != 0 {
		t.Fatalf("inventory: %+v", d.Inventory)
	}
	candidates := map[string]string{}
	for _, c := range d.Candidates {
		candidates[c.Path] = c.Kind
	}
	for _, p := range []string{"project/App.csproj", "nested/pom.xml", "vendor/package.jar", "Directory.Build.props"} {
		if candidates[p] == "" {
			t.Fatalf("missing %s", p)
		}
	}
	if len(r.Languages) > 0 || r.Summary.LanguageBytes != 0 || r.Summary.AnalyzedFiles != 0 || r.Projects != nil || r.Structure != nil || r.Metrics != nil {
		t.Fatalf("unexpected deeper results: %+v", r)
	}
	old, err := Scan(context.Background(), root, Options{Source: "directory", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	additive, err := Scan(context.Background(), root, Options{Source: "directory", Workers: 4, Discovery: true})
	if err != nil {
		t.Fatal(err)
	}
	additive.Discovery = nil
	additive.SchemaVersion = old.SchemaVersion
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(additive)
	if string(a) != string(b) {
		t.Fatalf("legacy result changed\n%s\n%s", a, b)
	}
}
func TestDiscoveryGitSnapshotAndDirtyCheckout(t *testing.T) {
	root, repo, commit := gitFixture(t, map[string]string{"pom.xml": "committed", "x.dll": "artifact", ".gitattributes": "x.dll linguist-vendored=true\n"})
	if err := os.Remove(filepath.Join(root, "pom.xml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.dll"), []byte("changed size of working artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.csproj"), []byte("untracked"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("x.dll -linguist-vendored\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opts := discoveryOptions()
	opts.Source = "git"
	opts.Revision = commit.String()
	r, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	object, err := repo.CommitObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	if r.Discovery.Source.Tree != object.TreeHash.String() || r.Discovery.Source.Consistency != "selected_git_tree" {
		t.Fatalf("source: %+v", r.Discovery.Source)
	}
	got := map[string]int64{}
	for _, c := range r.Discovery.Candidates {
		got[c.Path] = c.Bytes
	}
	if got["pom.xml"] != 9 || got["x.dll"] != 8 || got["untracked.csproj"] != 0 {
		t.Fatalf("working tree leaked: %+v", got)
	}
	found := false
	for _, role := range r.Discovery.Roles {
		if role.Name == "vendored" && role.Basis == "gitattributes" && role.Files == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("committed override lost: %+v", r.Discovery.Roles)
	}
}
func TestDiscoveryLimitsEmptyAndUnsupportedInputs(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		r, err := Scan(context.Background(), t.TempDir(), discoveryOptions())
		if err != nil {
			t.Fatal(err)
		}
		if r.Discovery.Status != "complete" || r.Discovery.Inventory.Files != 0 || r.Discovery.Candidates == nil {
			t.Fatalf("empty: %+v", r.Discovery)
		}
	})
	for _, source := range []string{"directory", "git"} {
		t.Run(source, func(t *testing.T) {
			root, _, _ := gitFixture(t, map[string]string{"a.csproj": "x", "b.xml": "x"})
			opts := discoveryOptions()
			opts.Source = source
			opts.MaxTreeSize = 2
			r, err := Scan(context.Background(), root, opts)
			if err != nil {
				t.Fatal(err)
			}
			if r.Discovery.Status != "skipped" || r.Discovery.Omissions["tree_size_limit"] != 1 || r.Discovery.Inventory.Files != 0 {
				t.Fatalf("limit: %+v", r.Discovery)
			}
		})
	}
	t.Run("attributes", func(t *testing.T) {
		root := fixtures(t, map[string]string{".gitattributes": strings.Repeat(" ", int(maxAttributesBytes)+1), "a.csproj": "x"})
		r, err := Scan(context.Background(), root, discoveryOptions())
		if err != nil {
			t.Fatal(err)
		}
		if r.Discovery.Status != "partial" || r.Discovery.Omissions["unsupported_gitattributes"] != 1 {
			t.Fatalf("attributes: %+v", r.Discovery)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root := fixtures(t, map[string]string{"real.csproj": "x"})
		if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, "escape.dll")); err != nil {
			t.Skip(err)
		}
		r, err := Scan(context.Background(), root, discoveryOptions())
		if err != nil {
			t.Fatal(err)
		}
		if r.Discovery.Inventory.Files != 1 || r.Discovery.Omissions["non_regular_file"] != 1 {
			t.Fatalf("symlink: %+v", r.Discovery)
		}
	})
}
func TestDiscoveryOnlyRejectsImplicitFollowupsAndCancellation(t *testing.T) {
	root := t.TempDir()
	for _, opts := range []Options{{DiscoveryOnly: true}, {Discovery: true, DiscoveryOnly: true, Projects: true}, {Discovery: true, DiscoveryOnly: true, Metrics: &MetricsOptions{}}, {Discovery: true, DiscoveryOnly: true, Detectors: []profile.Detector{detectorFunc(observingDetector)}}} {
		if _, err := Scan(context.Background(), root, opts); err == nil {
			t.Fatalf("accepted: %+v", opts)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, root, discoveryOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestDiscoveryEvidenceLimitScannerDeterminism(t *testing.T) {
	files := map[string]string{"z/project/pom.xml": "invalid"}
	for n := 0; n < discovery.EvidenceLimitPerKind+4; n++ {
		files[fmt.Sprintf("a/%04d.jar", n)] = "x"
	}
	root := fixtures(t, files)
	opts := discoveryOptions()
	a, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Workers = 8
	b, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("worker count changed report")
	}
	if a.Discovery.Status != "partial" || a.Discovery.OmittedCandidates["artifact"] != 4 || a.Discovery.Inventory.Files != int64(len(files)) {
		t.Fatalf("budget: %+v", a.Discovery)
	}
}
func BenchmarkDiscoveryModes(b *testing.B) {
	files := map[string]string{"App.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"/>", "pom.xml": "<project/>"}
	for n := 0; n < 100; n++ {
		files[fmt.Sprintf("data/%04d.xml", n)] = strings.Repeat("<event/>\n", 1024)
		files[fmt.Sprintf("src/%04d.go", n)] = goSource
	}
	root := fixtures(b, files)
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"legacy_languages", Options{Source: "directory", Workers: 1}},
		{"languages_with_discovery", Options{Source: "directory", Workers: 1, Discovery: true}},
		{"discovery_only", discoveryOptions()},
		{"all_projects", Options{Source: "directory", Workers: 1, Projects: true}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r, err := Scan(context.Background(), root, tc.opts)
				if err != nil || r.Summary.ScannedFiles != int64(len(files)) {
					b.Fatalf("scan: %v", err)
				}
			}
		})
	}
}
