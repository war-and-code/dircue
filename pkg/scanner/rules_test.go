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

	"dircue/pkg/profile"
	"dircue/pkg/rules"
)

func ruleProgram(t *testing.T, content bool) *rules.Program {
	t.Helper()
	extra := ""
	if content {
		extra = `,"content":{"contains_utf8":"MAGIC"}`
	}
	p, err := rules.Compile([]byte(`{"schema_version":"1.0.0","rules":[{"id":"marker","match":{"extensions":[".txt",".csproj",".cs",".xml"]}` + extra + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rulesOptions(t *testing.T, content bool) Options {
	return Options{Source: "directory", Rules: ruleProgram(t, content), RulesOnly: true, Workers: 1}
}

func TestRulesMetadataDoesNotReadPayload(t *testing.T) {
	opts := rulesOptions(t, false)
	opts.Discovery = true
	a, err := newRulesAccumulator(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := analyzeFile(context.Background(), nil, job{path: "vendor/huge.xml", size: 2 << 30, read: func(int64) ([]byte, int64, error) {
		t.Fatal("metadata-only rules read payload")
		return nil, 0, nil
	}}, opts)
	if err != nil || value.rulesFile == nil || value.discoveryFile == nil || value.language != "" || !value.skipped {
		t.Fatalf("metadata: %+v %v", value, err)
	}
	if err := a.add(value); err != nil {
		t.Fatal(err)
	}
	r, err := a.finish(context.Background(), nil)
	if err != nil || r.InventoryFiles != 1 || r.TotalMatches != 1 || r.ContentAdmittedFiles != 0 || r.Status != "complete" {
		t.Fatalf("report: %+v %v", r, err)
	}
}

func TestRulesIncludeExcludedContentAndPreserveLegacy(t *testing.T) {
	root := fixtures(t, map[string]string{
		".gitattributes":     "vendor/*.cs linguist-vendored=true\n*.xml linguist-generated=true\n",
		"vendor/Excluded.cs": "// MAGIC\nclass Excluded {}\n", "logs/data.xml": "<event>MAGIC</event>",
		"src/main.go": goSource, "negative.txt": "no matching marker", "invalid.txt": "\xffMAGIC",
	})
	opts := rulesOptions(t, true)
	r, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != profile.EnhancedSchemaVersion || r.Rules.Status != "partial" || r.Rules.TotalMatches != 2 || r.Rules.ContentEvaluatedFiles != 3 || r.Rules.Omissions[rules.InvalidUTF8] != 1 || r.Summary.AnalyzedFiles != 0 || len(r.Languages) != 0 {
		t.Fatalf("rules: %+v summary:%+v", r.Rules, r.Summary)
	}
	oldOptions := Options{Source: "directory", Workers: 1, Projects: true, IncludeFiles: true, Discovery: true}
	old, err := Scan(context.Background(), root, oldOptions)
	if err != nil {
		t.Fatal(err)
	}
	oldOptions.Rules = opts.Rules
	oldOptions.Workers = 8
	combined, err := Scan(context.Background(), root, oldOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(combined.Rules, r.Rules) {
		t.Fatalf("rule scope depends on other modules: %+v vs %+v", combined.Rules, r.Rules)
	}
	combined.Rules = nil
	combined.SchemaVersion = old.SchemaVersion
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(combined)
	if string(a) != string(b) {
		t.Fatalf("legacy output changed\n%s\n%s", a, b)
	}
}

func TestRulesGitUsesSelectedContentDespiteDirtyCheckout(t *testing.T) {
	root, repo, commit := gitFixture(t, map[string]string{"a.txt": "MAGIC", "vendor/b.txt": "MAGIC", ".gitattributes": "vendor/** linguist-vendored=true\n"})
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor/b.txt"), []byte("DIRTY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("MAGIC"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := rulesOptions(t, true)
	opts.Source, opts.Revision = "git", commit.String()
	r, err := Scan(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	object, err := repo.CommitObject(commit)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rules.Source.Kind != "git" || r.Rules.Source.Tree != object.TreeHash.String() || r.Rules.SourceConsistency != "selected_git_tree" || r.Rules.TotalMatches != 2 || r.Rules.InventoryFiles != 3 {
		t.Fatalf("snapshot: %+v", r.Rules)
	}
}

func TestRulesContentAdmissionIsLexicalAndSchedulingIndependent(t *testing.T) {
	opts := rulesOptions(t, true)
	paths := make([]string, 270)
	files := make(map[string]string)
	for i := range paths {
		paths[i] = fmt.Sprintf("%03d.txt", i)
		files[paths[i]] = "MAGIC"
	}
	var expected *rules.Report
	for _, reverse := range []bool{false, true} {
		order := slices.Clone(paths)
		if reverse {
			slices.Reverse(order)
		}
		a, err := newRulesAccumulator(opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		var reads []string
		for _, name := range order {
			value := result{path: name, rulesFile: &rules.File{Path: name, Size: 5}, rulesRead: func(limit int64) ([]byte, int64, error) {
				if limit != rules.MaxContentBytes+1 {
					t.Fatalf("read bound %d", limit)
				}
				reads = append(reads, name)
				return []byte("MAGIC"), 5, nil
			}}
			if err := a.add(value); err != nil {
				t.Fatal(err)
			}
			if len(a.pending) > rules.MaxContentFiles {
				t.Fatal("pending candidate bound exceeded")
			}
		}
		r, err := a.finish(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(reads, paths[:256]) || r.ContentEligibleFiles != 270 || r.ContentEvaluatedFiles != 256 || r.Omissions[rules.ContentBudget] != 14 || r.Status != "partial" {
			t.Fatalf("admission reads=%v report=%+v", reads, r)
		}
		if expected == nil {
			expected = r
		} else if !reflect.DeepEqual(expected, r) {
			t.Fatal("reverse arrival changed report")
		}
	}
	root := fixtures(t, files)
	for _, workers := range []int{1, 8} {
		opts.Workers = workers
		r, err := Scan(context.Background(), root, opts)
		if err != nil || !reflect.DeepEqual(expected, r.Rules) {
			t.Fatalf("workers=%d report=%+v err=%v", workers, r, err)
		}
	}
}

func TestRulesDeferredReadsUseOriginalReader(t *testing.T) {
	opts := rulesOptions(t, true)
	opts.RulesOnly, opts.Projects = false, true
	content, calls := "<Project>MAGIC</Project>", 0
	value, err := analyzeFile(context.Background(), nil, job{path: "a.csproj", size: int64(len(content)), read: func(int64) ([]byte, int64, error) {
		calls++
		return []byte(content), int64(len(content)), nil
	}}, opts)
	if err != nil || calls != 1 {
		t.Fatalf("initial cached read calls=%d err=%v", calls, err)
	}
	content = "<Project>OTHER</Project>"
	a, err := newRulesAccumulator(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.add(value); err != nil {
		t.Fatal(err)
	}
	r, err := a.finish(context.Background(), nil)
	if err != nil || calls != 2 || r.TotalMatches != 0 || r.ContentEvaluatedFiles != 1 {
		t.Fatalf("retained classifier buffer: calls=%d report=%+v err=%v", calls, r, err)
	}
}

func TestRulesDeferredReadFailuresAndLimits(t *testing.T) {
	for _, test := range []struct {
		name         string
		size, actual int64
		data         string
		limit        int64
		reason       rules.OmissionReason
		readError    bool
	}{
		{"growth", 5, 6, "MAGIC!", 0, rules.Incomplete, false},
		{"short", 5, 5, "MAG", 0, rules.Incomplete, false},
		{"shrink", 5, 3, "MAG", 0, rules.Incomplete, false},
		{"invalid-utf8", 5, 5, "\xffAGIC", 0, rules.InvalidUTF8, false},
		{"caller-limit", 5, 5, "MAGIC", 4, rules.CallerFileLimit, false},
		{"engine-limit", rules.MaxContentBytes + 1, 0, "", 0, rules.FileTooLarge, false},
		{"io-error", 5, 0, "", 0, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := rulesOptions(t, true)
			opts.MaxFileBytes = test.limit
			a, err := newRulesAccumulator(opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			if err := a.add(result{path: "x.txt", rulesFile: &rules.File{Path: "x.txt", Size: test.size}, rulesRead: func(limit int64) ([]byte, int64, error) {
				reads++
				if test.limit > 0 && limit > test.limit+1 {
					t.Fatal("caller byte limit ignored")
				}
				if test.readError {
					return nil, 0, errors.New("failed read")
				}
				return []byte(test.data), test.actual, nil
			}}); err != nil {
				t.Fatal(err)
			}
			r, err := a.finish(context.Background(), nil)
			if test.readError {
				if err == nil || r != nil {
					t.Fatalf("I/O accepted: %+v %v", r, err)
				}
				return
			}
			if err != nil || r.Omissions[test.reason] != 1 || r.ContentEvaluatedFiles != 0 {
				t.Fatalf("omission: %+v %v", r, err)
			}
			if (test.reason == rules.FileTooLarge || test.reason == rules.CallerFileLimit) && reads != 0 {
				t.Fatal("oversized content read")
			}
		})
	}
}

func TestRulesTreeLimitAndNonregularCoverage(t *testing.T) {
	for _, source := range []string{"git", "directory"} {
		root, _, _ := gitFixture(t, map[string]string{"a.txt": "MAGIC", "b.txt": "MAGIC"})
		opts := rulesOptions(t, true)
		opts.Source, opts.MaxTreeSize = source, 2
		r, err := Scan(context.Background(), root, opts)
		if err != nil || r.Rules.Status != "skipped" || r.Rules.InventoryFiles != 0 || r.Rules.Omissions[rules.TreeSizeLimit] != 1 || r.SchemaVersion != profile.EnhancedSchemaVersion {
			t.Fatalf("%s tree: %+v %v", source, r, err)
		}
	}
	root := fixtures(t, map[string]string{"real.txt": "MAGIC"})
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(root, "link.txt")); err != nil {
		t.Skip(err)
	}
	r, err := Scan(context.Background(), root, rulesOptions(t, true))
	if err != nil || r.Rules.InventoryFiles != 1 || r.Rules.Omissions[rules.NonRegularFile] != 1 || r.Rules.Status != "complete" {
		t.Fatalf("symlink: %+v %v", r, err)
	}
	a, err := newRulesAccumulator(rulesOptions(t, false), nil)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("x", rules.MaxPathBytes+1)
	if err := a.add(result{path: name, rulesFile: &rules.File{Path: name, Size: 0}}); err != nil {
		t.Fatal(err)
	}
	got, err := a.finish(context.Background(), nil)
	if err != nil || got.InventoryFiles != 0 || got.Omissions[rules.UnsupportedPath] != 1 || got.Status != "partial" {
		t.Fatalf("path omission: %+v %v", got, err)
	}
}

func TestRulesOnlyValidationAndCancellation(t *testing.T) {
	program := ruleProgram(t, true)
	for _, opts := range []Options{{RulesOnly: true}, {Rules: program, Discovery: true, DiscoveryOnly: true}, {Rules: program, RulesOnly: true, Projects: true}, {Rules: program, RulesOnly: true, Metrics: &MetricsOptions{}}, {Rules: program, RulesOnly: true, Detectors: []profile.Detector{detectorFunc(observingDetector)}}} {
		if r, err := Scan(context.Background(), t.TempDir(), opts); err == nil || r != nil {
			t.Fatalf("invalid accepted: %+v", opts)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := Scan(ctx, t.TempDir(), rulesOptions(t, true)); !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("cancel: %+v %v", r, err)
	}
	a, err := newRulesAccumulator(rulesOptions(t, true), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if err := a.add(result{path: "x.txt", rulesFile: &rules.File{Path: "x.txt", Size: 5}, rulesRead: func(int64) ([]byte, int64, error) { cancel(); return []byte("MAGIC"), 5, nil }}); err != nil {
		t.Fatal(err)
	}
	if r, err := a.finish(ctx, nil); !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("deferred cancel: %+v %v", r, err)
	}
}

func TestRulesExactByteBoundaryAndDeferredDirectoryMutation(t *testing.T) {
	for _, limit := range []int64{5, rules.MaxContentBytes} {
		content := "MAGIC" + strings.Repeat("x", int(limit)-5)
		root := fixtures(t, map[string]string{"x.txt": content})
		opts := rulesOptions(t, true)
		opts.MaxFileBytes = limit
		r, err := Scan(context.Background(), root, opts)
		if err != nil || r.Rules.TotalMatches != 1 || r.Rules.Status != "complete" {
			t.Fatalf("exact boundary %d: %+v %v", limit, r, err)
		}
	}
	for _, mutation := range []string{"grow", "delete", "symlink"} {
		t.Run(mutation, func(t *testing.T) {
			dir := fixtures(t, map[string]string{"x.txt": "MAGIC"})
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			a, err := newRulesAccumulator(rulesOptions(t, true), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.add(result{path: "x.txt", rulesFile: &rules.File{Path: "x.txt", Size: 5}}); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "grow":
				err = os.WriteFile(filepath.Join(dir, "x.txt"), []byte("MAGIC!"), 0600)
			case "delete":
				err = os.Remove(filepath.Join(dir, "x.txt"))
			case "symlink":
				if err = os.Remove(filepath.Join(dir, "x.txt")); err == nil {
					err = os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(dir, "x.txt"))
				}
			}
			if err != nil {
				t.Skip(err)
			}
			r, err := a.finish(context.Background(), root)
			if mutation == "grow" {
				if err != nil || r.Omissions[rules.Incomplete] != 1 {
					t.Fatalf("growth: %+v %v", r, err)
				}
			} else if err == nil || r != nil {
				t.Fatalf("filesystem failure accepted: %+v %v", r, err)
			}
		})
	}
}

func TestRulesEmptyFinishHonorsCancellation(t *testing.T) {
	a, err := newRulesAccumulator(rulesOptions(t, false), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := a.finish(ctx, nil); !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("empty finish ignored cancellation: %+v %v", r, err)
	}
}
