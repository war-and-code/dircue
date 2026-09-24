package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/pkg/structure"
)

const registryNPM = "registry=https://registry.npmjs.org/\n"
const registryNuGet = `<configuration><packageSources><add key="public" value="https://api.nuget.org/v3/index.json"/></packageSources></configuration>`

func registryOptions() Options {
	return Options{Source: "directory", Registries: true, RegistriesOnly: true, Workers: 1}
}

func TestRegistriesSelectOnlySupportedNamesBeforeReads(t *testing.T) {
	opts := registryOptions()
	opts.Discovery = true
	a, err := newRegistryAccumulator(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vendor/huge.xml", ".npmrc.example", "npmrc", "main.go"} {
		value, err := analyzeFile(context.Background(), nil, job{path: name, size: 2 << 30, read: func(int64) ([]byte, int64, error) { t.Fatal("unselected file read"); return nil, 0, nil }}, opts)
		if err != nil || value.registryFile != nil || value.discoveryFile == nil || !value.skipped || value.language != "" {
			t.Fatalf("selection %+v %v", value, err)
		}
		if err := a.add(value); err != nil {
			t.Fatal(err)
		}
	}
	r, err := a.finish(context.Background())
	if err != nil || r.Coverage.CandidateFiles != 0 || r.Coverage.ReadFiles != 0 || r.Status != "complete" {
		t.Fatalf("unexpected reads %+v %v", r, err)
	}
	disabled, err := newRegistryAccumulator(Options{}, nil)
	if err != nil || disabled != nil {
		t.Fatalf("opt-out allocated collector: %v %v", disabled, err)
	}
	vendored := true
	value, err := analyzeFile(context.Background(), nil, job{path: "vendor/.npmrc", size: 20, attrs: overrides{vendored: &vendored}, read: func(int64) ([]byte, int64, error) { t.Fatal("opt-out vendored candidate read"); return nil, 0, nil }}, Options{})
	if err != nil || value.registryFile != nil {
		t.Fatalf("opt-out retained callback: %+v %v", value, err)
	}
}

func TestRegistriesIncludeVendorDataAndPreserveOtherReports(t *testing.T) {
	dir := fixtures(t, map[string]string{".gitattributes": "vendor/** linguist-vendored=true\n*.Config linguist-generated=true\n", "vendor/NuGet.Config": registryNuGet, "nested/.npmrc": registryNPM, "main.go": goSource, "notes.xml": "<log/>"})
	opts := registryOptions()
	opts.Discovery = true
	r, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != profile.EnhancedSchemaVersion || r.Registries.Status != "complete" || r.Registries.Coverage.CandidateFiles != 2 || r.Registries.Coverage.ReadFiles != 2 || r.Registries.Coverage.RetainedDeclarations != 2 || r.Summary.AnalyzedFiles != 0 || len(r.Languages) != 0 || r.Discovery == nil {
		t.Fatalf("registry report %+v", r)
	}
	var origins []string
	for _, cfg := range r.Registries.Configurations {
		for _, d := range cfg.Declarations {
			if d.Endpoint != nil {
				origins = append(origins, d.Endpoint.Origin)
			}
		}
	}
	if !reflect.DeepEqual(origins, []string{"https://registry.npmjs.org", "https://api.nuget.org"}) {
		t.Fatalf("origins %v", origins)
	}
	for _, base := range []Options{{Source: "directory", Workers: 1, IncludeFiles: true}, {Source: "directory", Workers: 1, Projects: true, Discovery: true, Metrics: &MetricsOptions{}, Rules: ruleProgram(t, false)}} {
		old, err := Scan(context.Background(), dir, base)
		if err != nil {
			t.Fatal(err)
		}
		base.Registries = true
		base.Workers = 8
		combined, err := Scan(context.Background(), dir, base)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(combined.Registries, r.Registries) {
			t.Fatal("registry scope depends on another profiler")
		}
		combined.Registries = nil
		combined.SchemaVersion = old.SchemaVersion
		left, _ := json.Marshal(old)
		right, _ := json.Marshal(combined)
		if string(left) != string(right) {
			t.Fatalf("ordinary output changed\n%s\n%s", left, right)
		}
	}
}

func TestRegistriesGitUsesSelectedTreeDespiteDirtyFiles(t *testing.T) {
	dir, repo, commit := gitFixture(t, map[string]string{".npmrc": registryNPM, "vendor/NuGet.Config": registryNuGet, ".gitattributes": "vendor/** linguist-vendored=true\n"})
	if err := os.Remove(filepath.Join(dir, ".npmrc")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor/NuGet.Config"), []byte("DIRTY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "untracked"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked/.npmrc"), []byte("registry=https://untracked.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := registryOptions()
	opts.Source = "git"
	opts.Revision = commit.String()
	r, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	object, err := repo.CommitObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	if r.Registries.Source.Mode != "git" || r.Registries.Source.Tree != object.TreeHash.String() || r.Registries.Source.Consistency != "selected_git_tree" || r.Registries.Coverage.ReadFiles != 2 || r.Registries.Coverage.RetainedDeclarations != 2 || r.Registries.Status != "complete" {
		t.Fatalf("wrong selected source %+v", r.Registries)
	}
	opts.Source = "directory"
	opts.Revision = ""
	live, err := Scan(context.Background(), dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(live.Registries)
	if !strings.Contains(string(raw), "untracked.example") || live.Registries.Source.Consistency != "live_directory" || live.Registries.Source.Tree != "" {
		t.Fatalf("live source %+v", live.Registries)
	}
}

func TestRegistriesLexicalAdmissionAndWorkerDeterminism(t *testing.T) {
	opts := registryOptions()
	files := map[string]string{}
	var paths []string
	for i := range 70 {
		for name, data := range map[string]string{".npmrc": registryNPM, "NuGet.Config": registryNuGet} {
			p := fmt.Sprintf("%03d/%s", i, name)
			files[p] = data
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	var expected *registries.Report
	for _, reverse := range []bool{false, true} {
		order := slices.Clone(paths)
		if reverse {
			slices.Reverse(order)
		}
		a, err := newRegistryAccumulator(opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		var reads []string
		for _, name := range order {
			data := files[name]
			candidate := registryCandidate(nil, job{path: name, size: int64(len(data)), read: func(limit int64) ([]byte, int64, error) {
				if limit != registries.MaxFileBytes+1 {
					t.Fatalf("read bound %d", limit)
				}
				reads = append(reads, name)
				return []byte(data), int64(len(data)), nil
			}})
			if err := a.add(result{path: name, registryFile: candidate}); err != nil {
				t.Fatal(err)
			}
		}
		r, err := a.finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(reads, paths[:128]) || r.Coverage.CandidateFiles != 140 || r.Coverage.ReadFiles != 128 || r.Omissions["configuration_limit"] != 12 || r.Status != "partial" {
			t.Fatalf("lexical admission reads=%v report=%+v", reads, r)
		}
		if expected == nil {
			expected = r
		} else if !reflect.DeepEqual(expected, r) {
			t.Fatal("reverse arrival changed report")
		}
	}
	dir := fixtures(t, files)
	for _, workers := range []int{1, 8} {
		opts.Workers = workers
		r, err := Scan(context.Background(), dir, opts)
		if err != nil || !reflect.DeepEqual(expected, r.Registries) {
			t.Fatalf("workers=%d err=%v report=%+v", workers, err, r)
		}
	}
}

func TestRegistriesDeferredReaderPrecedesProjectCache(t *testing.T) {
	opts := registryOptions()
	opts.RegistriesOnly = false
	opts.Projects = true
	content := `<configuration><packageSources><add key="feed" value="https://one.example"/></packageSources></configuration>`
	calls := 0
	value, err := analyzeFile(context.Background(), nil, job{path: "NuGet.Config", size: int64(len(content)), read: func(int64) ([]byte, int64, error) { calls++; return []byte(content), int64(len(content)), nil }}, opts)
	if err != nil || calls != 1 || value.registryFile == nil {
		t.Fatalf("initial reads %d err=%v", calls, err)
	}
	content = strings.ReplaceAll(content, "one.example", "two.example")
	a, err := newRegistryAccumulator(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.add(value); err != nil {
		t.Fatal(err)
	}
	r, err := a.finish(context.Background())
	if err != nil || calls != 2 || len(r.Configurations) != 1 || r.Configurations[0].Declarations[0].Endpoint.Origin != "https://two.example" {
		t.Fatalf("cached payload survived: calls=%d report=%+v err=%v", calls, r, err)
	}
}

func TestRegistriesDeferredLimitsAndSafeReadFailures(t *testing.T) {
	for _, test := range []struct {
		name                string
		size, actual, limit int64
		data, reason        string
		readError           bool
	}{
		{name: "growth", size: 5, actual: 6, data: "abcdef", reason: "incomplete_content"},
		{name: "short", size: 5, actual: 5, data: "abc", reason: "incomplete_content"},
		{name: "shrink", size: 5, actual: 3, data: "abc", reason: "incomplete_content"},
		{name: "caller-limit", size: 5, actual: 5, limit: 4, data: "abcde", reason: "file_size_limit"},
		{name: "engine-limit", size: registries.MaxFileBytes + 1, reason: "file_size_limit"},
		{name: "io-failure", size: 5, readError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := registryOptions()
			opts.MaxFileBytes = test.limit
			a, err := newRegistryAccumulator(opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			candidate := registryCandidate(nil, job{path: ".npmrc", size: test.size, read: func(limit int64) ([]byte, int64, error) {
				calls++
				if test.limit > 0 && limit > test.limit+1 {
					t.Fatal("caller limit ignored")
				}
				if test.readError {
					return nil, 0, errors.New("SECRET /private/host/password")
				}
				return []byte(test.data), test.actual, nil
			}})
			if err := a.add(result{path: ".npmrc", registryFile: candidate}); err != nil {
				t.Fatal(err)
			}
			r, err := a.finish(context.Background())
			if test.readError {
				if r != nil || err != registries.ErrRead {
					t.Fatalf("unsafe failure report=%+v err=%v", r, err)
				}
				return
			}
			if err != nil || r.Status != "partial" || len(r.Configurations) != 1 || r.Configurations[0].Omissions[test.reason] != 1 {
				t.Fatalf("limits report=%+v err=%v", r, err)
			}
			if test.reason == "file_size_limit" && calls != 0 {
				t.Fatal("oversized candidate read")
			}
		})
	}
}

func TestRegistriesCancellationAndTreeLimits(t *testing.T) {
	for _, source := range []string{"directory", "git"} {
		t.Run(source, func(t *testing.T) {
			dir := fixtures(t, map[string]string{".npmrc": registryNPM, "NuGet.Config": registryNuGet})
			if source == "git" {
				dir, _, _ = gitFixture(t, map[string]string{".npmrc": registryNPM, "NuGet.Config": registryNuGet})
			}
			opts := registryOptions()
			opts.Source = source
			opts.MaxTreeSize = 1
			r, err := Scan(context.Background(), dir, opts)
			if err != nil || r.Registries.Status != "skipped" || r.Registries.Coverage.EnumerationComplete || r.Registries.Coverage.ReadFiles != 0 || r.SchemaVersion != profile.EnhancedSchemaVersion {
				t.Fatalf("tree limit report=%+v err=%v", r, err)
			}
		})
	}
	for _, withCandidate := range []bool{false, true} {
		a, err := newRegistryAccumulator(registryOptions(), nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if withCandidate {
			candidate := registryCandidate(nil, job{path: ".npmrc", size: int64(len(registryNPM)), read: func(int64) ([]byte, int64, error) { cancel(); return []byte(registryNPM), int64(len(registryNPM)), nil }})
			if err := a.add(result{path: ".npmrc", registryFile: candidate}); err != nil {
				t.Fatal(err)
			}
		} else {
			cancel()
		}
		r, err := a.finish(ctx)
		cancel()
		if r != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation ignored %+v %v", r, err)
		}
	}
}

func TestRegistriesValidateExclusiveModes(t *testing.T) {
	dir := t.TempDir()
	cases := []Options{{RegistriesOnly: true}, {Registries: true, RegistriesOnly: true, Detectors: []profile.Detector{nil}}, {Registries: true, RegistriesOnly: true, Structure: &structure.Client{}}, {Registries: true, RegistriesOnly: true, Projects: true}, {Registries: true, RegistriesOnly: true, Metrics: &MetricsOptions{}}, {Registries: true, RegistriesOnly: true, Rules: ruleProgram(t, false)}, {Registries: true, Discovery: true, DiscoveryOnly: true}, {Registries: true, Rules: ruleProgram(t, false), RulesOnly: true}}
	for _, opts := range cases {
		r, err := Scan(context.Background(), dir, opts)
		if r != nil || err == nil {
			t.Fatalf("invalid options accepted %+v", opts)
		}
	}
	opts := registryOptions()
	opts.Discovery = true
	if r, err := Scan(context.Background(), dir, opts); err != nil || r.Discovery == nil || r.Registries == nil {
		t.Fatalf("metadata combination rejected %+v %v", r, err)
	}
}

func TestRegistriesUnsafeCandidatePathsAreOmitted(t *testing.T) {
	a, err := newRegistryAccumulator(registryOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../.npmrc", "bad\x00/.npmrc", strings.Repeat("x", 4096) + "/.npmrc"} {
		candidate := registryCandidate(nil, job{path: name, size: 4, read: func(int64) ([]byte, int64, error) { t.Fatal("unsafe path read"); return nil, 0, nil }})
		if err := a.add(result{path: name, registryFile: candidate}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := a.finish(context.Background())
	if err != nil || r.Omissions["unsupported_path"] != 3 || r.Coverage.ReadFiles != 0 || len(r.Configurations) != 0 {
		t.Fatalf("unsafe path handling %+v %v", r, err)
	}
}

func TestRegistriesExactByteLimits(t *testing.T) {
	for _, limit := range []int64{1, 64, registries.MaxFileBytes} {
		content := "\n"
		if limit >= int64(len(registryNPM)) {
			content = registryNPM
		}
		content += strings.Repeat("\n", int(limit)-len(content))
		dir := fixtures(t, map[string]string{".npmrc": content})
		opts := registryOptions()
		opts.MaxFileBytes = limit
		r, err := Scan(context.Background(), dir, opts)
		if err != nil || r.Registries.Status != "complete" || r.Registries.Coverage.BytesRead != limit || r.Registries.Coverage.ParsedFiles != 1 {
			t.Fatalf("exact limit %d report=%+v err=%v", limit, r, err)
		}
	}
}

func TestRegistriesMixedProfilerReadErrorsStaySafe(t *testing.T) {
	opts := registryOptions()
	opts.RegistriesOnly = false
	_, err := analyzeFile(context.Background(), nil, job{path: ".npmrc", size: 5, read: func(int64) ([]byte, int64, error) { return nil, 0, errors.New("SECRET /private/host/file") }}, opts)
	if err != registries.ErrRead {
		t.Fatalf("classifier leaked registry read error: %v", err)
	}
}
