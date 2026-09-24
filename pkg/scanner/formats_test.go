package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/formats"
)

func formatsOptions() Options {
	return Options{Source: "directory", Formats: true, FormatsOnly: true, Workers: 1}
}
func TestFormatsMixedContentAndLegacyPreservation(t *testing.T) {
	dir := fixtures(t, map[string]string{"vendor/data.xml": "<events><event/></events>", "data.zip": "not a zip", "blob": "PK\x03\x04", "src/main.go": goSource, "records.json": "{\"a\":1}", "empty": ""})
	opts := formatsOptions()
	a, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Workers = 8
	b, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Formats, b.Formats) || a.Formats.Status != "complete" || a.Formats.Coverage.SelectedFiles != 6 || a.Formats.Coverage.InspectedFiles != 6 {
		t.Fatalf("formats %+v", a.Formats)
	}
	if a.Summary.AnalyzedFiles != 0 || len(a.Languages) != 0 || a.Projects != nil || a.Structure != nil || a.Declarations != nil {
		t.Fatal("standalone ran another profiler")
	}
	old, err := Scan(context.Background(), dir, Options{Source: "directory", Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	all, err := Scan(context.Background(), dir, Options{Source: "directory", Workers: 8, Formats: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Formats, all.Formats) {
		t.Fatal("standalone and aggregate differ")
	}
	all.Formats = nil
	all.SchemaVersion = old.SchemaVersion
	oldJSON, _ := json.Marshal(old)
	allJSON, _ := json.Marshal(all)
	if string(oldJSON) != string(allJSON) {
		t.Fatalf("legacy changed\n%s\n%s", oldJSON, allJSON)
	}
}
func TestFormatsGitUsesSelectedCommit(t *testing.T) {
	dir, _, commit := gitFixture(t, map[string]string{"data.xml": "<committed/>", "data.json": "{}"})
	if err := os.WriteFile(filepath.Join(dir, "data.xml"), []byte("broken XML"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "data.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked"), []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := formatsOptions()
	opts.Source = "git"
	opts.Revision = commit.String()
	r, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Formats.Source.Mode != "git" || r.Formats.Source.Tree == "" || r.Formats.Coverage.SelectedFiles != 2 || r.Formats.Coverage.CompleteReads != 2 {
		t.Fatalf("snapshot: %+v", r.Formats)
	}
	for _, o := range r.Formats.Observations {
		if len(o.Diagnostics) > 0 {
			t.Fatalf("dirty checkout affected source: %+v", o)
		}
	}
}
func TestFormatsLargeFileUsesBoundedPrefix(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "large.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("<root>" + strings.Repeat("x", int(formats.MaxFileBytes))); err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(2 << 30); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Scan(context.Background(), dir, formatsOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Formats.Coverage.InspectedBytes != formats.MaxFileBytes+1 || r.Formats.Coverage.PrefixReads != 1 || r.Formats.Coverage.CompleteReads != 0 || r.Formats.Coverage.SelectedBytes != 2<<30 {
		t.Fatalf("largefile coverage %+v", r.Formats.Coverage)
	}
	if r.Formats.Observations[0].ReadScope != "prefix" {
		t.Fatal("whole file validation claimed")
	}
}
func TestFormatsLimitsAndInvalidModes(t *testing.T) {
	dir := fixtures(t, map[string]string{"large.json": "{\"large\":true}", "small": "x"})
	opts := formatsOptions()
	opts.MaxFileBytes = 2
	r, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Formats.Coverage.InspectedFiles != 1 || r.Formats.Omissions["source_file_size_limit"] != 1 {
		t.Fatalf("max bytes %+v", r.Formats)
	}
	opts = formatsOptions()
	opts.MaxTreeSize = 1
	r, err = Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Formats.Status != "skipped" || r.Formats.Omissions["tree_size_limit"] != 1 {
		t.Fatalf("treecap %+v", r.Formats)
	}
	for _, bad := range []Options{{FormatsOnly: true}, {Formats: true, FormatsOnly: true, Discovery: true}, {Formats: true, Discovery: true, DiscoveryOnly: true}, {Formats: true, Declarations: true, DeclarationsOnly: true}, {Formats: true, Registries: true, RegistriesOnly: true}} {
		if _, err := Scan(context.Background(), dir, bad); err == nil {
			t.Fatalf("invalid mode accepted %+v", bad)
		}
	}
}

func TestFormatsNeverFollowsSymlinkOrReadsOnCollection(t *testing.T) {
	dir := fixtures(t, map[string]string{"safe.json": "{}"})
	outside := fixtures(t, map[string]string{"secret.xml": "<secret/>"})
	if err := os.Symlink(filepath.Join(outside, "secret.xml"), filepath.Join(dir, "escape.xml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r, err := Scan(context.Background(), dir, formatsOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Formats.Coverage.SelectedFiles != 1 || r.Formats.Coverage.InspectedFiles != 1 || r.Formats.Omissions["non_regular_file"] != 1 {
		t.Fatalf("symlink inventory: %+v", r.Formats)
	}
	v, err := analyzeFile(context.Background(), nil, job{path: "vendor/data.xml", size: 2 << 30, read: func(int64) ([]byte, int64, error) {
		t.Fatal("prefix read before deterministic selection")
		return nil, 0, nil
	}}, formatsOptions())
	if err != nil || v.formatFile == nil || v.language != "" {
		t.Fatalf("collection: %+v %v", v, err)
	}
}
