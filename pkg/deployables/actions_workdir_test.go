package deployables

import (
	"context"
	"strings"
	"testing"
)

// TestWorkdirPrecedenceStepOverridesJobDefault verifies that a step-level
// working-directory overrides the job's defaults.run.working-directory.
func TestWorkdirPrecedenceStepOverridesJobDefault(t *testing.T) {
	content := `name: CI
on: [push]
defaults:
  run:
    working-directory: ./top-level
jobs:
  build:
    defaults:
      run:
        working-directory: ./job-level
    steps:
      - uses: actions/checkout@v4
      - run: make
        working-directory: ./step-level
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.ParsedFiles != 1 {
		t.Fatalf("expected 1 parsed file, got %d diagnostics=%v", r.Coverage.ParsedFiles, r.Diagnostics)
	}
	// The step-level value must appear as a working_directory reference.
	var found []string
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "working_directory" {
				found = append(found, ref.Value)
			}
		}
	}
	if !containsStr(found, "./step-level") {
		t.Errorf("step-level working-directory not recorded; refs=%v", found)
	}
	if !containsStr(found, "./job-level") {
		t.Errorf("job-level default working-directory not recorded; refs=%v", found)
	}
	if !containsStr(found, "./top-level") {
		t.Errorf("workflow-level default working-directory not recorded; refs=%v", found)
	}
}

// TestWorkdirPrecedenceExpressionBlocksFallback verifies that an expression at
// a higher-precedence level is recorded as unresolved and does not fall back to
// the literal at a lower-precedence level.
func TestWorkdirPrecedenceExpressionBlocksFallback(t *testing.T) {
	content := `name: CI
on: [push]
jobs:
  build:
    defaults:
      run:
        working-directory: ${{ matrix.dir }}
    steps:
      - uses: actions/checkout@v4
      - run: make
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The expression reference should be present and have the expression marker.
	var hasExpr bool
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "expression" && ref.Qualification == "unresolved" {
				hasExpr = true
			}
		}
	}
	if !hasExpr {
		t.Errorf("expected unresolved expression reference; defs=%+v", r.Definitions)
	}
	// No literal working-directory reference should exist from the job default.
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "working_directory" && ref.Qualification != "unresolved" {
				t.Errorf("unexpected literal working_directory reference %q when expression present", ref.Value)
			}
		}
	}
}

// TestCheckoutPathRecorded verifies that actions/checkout with path: records a
// checkout_path reference with the declared literal value.
func TestCheckoutPathRecorded(t *testing.T) {
	content := `name: CI
on: [push]
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
        with:
          path: repo
      - run: make
        working-directory: repo/services/api
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "checkout_path" {
				found = append(found, ref.Value)
			}
		}
	}
	if !containsStr(found, "repo") {
		t.Errorf("checkout_path not recorded; refs=%v", found)
	}
}

// TestCheckoutPathExpressionNotRecorded verifies that an expression in
// actions/checkout path: is not recorded as a checkout_path (it must not
// become a local-qualified declaration).
func TestCheckoutPathExpressionNotRecorded(t *testing.T) {
	content := `name: CI
on: [push]
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
        with:
          path: ${{ inputs.path }}
      - run: make
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "checkout_path" {
				t.Errorf("checkout_path recorded for expression value: %+v", ref)
			}
		}
	}
}

// TestJobNeedsReference verifies that `needs:` in a GitHub Actions job records
// job_needs references for each declared dependency.
func TestJobNeedsReference(t *testing.T) {
	content := `name: CI
on: [push]
jobs:
  build:
    steps:
      - run: make
  test:
    needs: [build]
    steps:
      - run: make test
  deploy:
    needs: build
    steps:
      - run: make deploy
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var needs []string
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "job_needs" {
				needs = append(needs, ref.Value)
			}
		}
	}
	if !containsStr(needs, "build") {
		t.Errorf("expected job_needs 'build'; got %v", needs)
	}
	// Two jobs depend on build; verify at least two entries.
	count := 0
	for _, v := range needs {
		if v == "build" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 job_needs 'build' references; got %d", count)
	}
}

// TestJobNeedsStringAndList verifies that needs: accepts both a plain string
// and a sequence.
func TestJobNeedsStringAndList(t *testing.T) {
	content := `name: CI
on: [push]
jobs:
  a:
    steps:
      - run: echo a
  b:
    needs: a
    steps:
      - run: echo b
  c:
    needs: [a, b]
    steps:
      - run: echo c
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var needs []string
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "job_needs" {
				needs = append(needs, ref.Value)
			}
		}
	}
	// b depends on a (1), c depends on a and b (2) → 3 total.
	if len(needs) < 3 {
		t.Errorf("expected at least 3 job_needs entries; got %v", needs)
	}
}

// TestWorkflowLevelDefaultWithNoJobOrStepOverride verifies that when only a
// workflow-level default exists and no step or job overrides it, the default is
// recorded.
func TestWorkflowLevelDefaultWithNoJobOrStepOverride(t *testing.T) {
	content := `name: CI
on: [push]
defaults:
  run:
    working-directory: ./src
jobs:
  build:
    steps:
      - run: make
`
	files := []Candidate{makeCandidate(".github/workflows/ci.yml", content)}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wds []string
	for _, def := range r.Definitions {
		for _, ev := range def.Evidence {
			if strings.Contains(ev.Field, "defaults.run.working-directory") {
				wds = append(wds, ev.Value)
			}
		}
	}
	if !containsStr(wds, "./src") {
		t.Errorf("workflow-level default not recorded; evidence=%v", wds)
	}
}

// helpers.

func makeCandidate(path, content string) Candidate {
	c := []byte(content)
	return Candidate{Path: path, Size: int64(len(c)), Read: func(context.Context, int64) ([]byte, int64, error) { return c, int64(len(c)), nil }}
}

func containsStr(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// A checkout that names a repository is qualified external; a checkout of this
// repository, with or without an explicit ${{ github.repository }}, is local.
func TestCheckoutPathQualifiesOtherRepository(t *testing.T) {
	for _, tc := range []struct{ with, want string }{
		{"path: docs", "local"},
		{"path: docs\n          repository: ${{ github.repository }}", "local"},
		{"path: docs\n          repository: example/docs", "external"},
		{"path: docs\n          repository: ${{ inputs.repo }}", "external"},
	} {
		content := []byte("name: Docs\non: [push]\njobs:\n  docs:\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          " + tc.with + "\n")
		r, err := Observe(context.Background(), []Candidate{{Path: ".github/workflows/docs.yml", Size: int64(len(content)),
			Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }}}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, def := range r.Definitions {
			for _, ref := range def.References {
				if ref.Kind == "checkout_path" {
					got = ref.Qualification
				}
			}
		}
		if got != tc.want {
			t.Errorf("%q: checkout_path qualification = %q, want %q", tc.with, got, tc.want)
		}
	}
}

func TestBuildContextAfterForeignRootCheckoutIsNotLocal(t *testing.T) {
	content := []byte(`name: Build
on: [push]
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: example/other
      - uses: docker/build-push-action@v6
        with:
          context: .
`)
	r, err := Observe(context.Background(), []Candidate{{Path: ".github/workflows/build.yml", Size: int64(len(content)),
		Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return content, int64(len(content)), nil }}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, def := range r.Definitions {
		for _, ref := range def.References {
			if ref.Kind == "build_context" {
				found = true
				if ref.Qualification != "external" {
					t.Fatalf("root build context after foreign checkout must be external: %+v", ref)
				}
			}
		}
	}
	if !found {
		t.Fatal("build-push-action context was not observed")
	}
}
