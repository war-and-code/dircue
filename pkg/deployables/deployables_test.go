package deployables

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dircue/pkg/profile"
)

func TestObserveStaticDeclarationKindsAndQualifications(t *testing.T) {
	inputs := map[string]string{
		"Dockerfile":                 "FROM golang:1.26 AS build\nCOPY --from=build /x /x\nFROM ${BASE}\n",
		"deploy/compose.yaml":        "services:\n  api:\n    build:\n      context: ../services/api\n      dockerfile: Dockerfile.api\n      target: runtime\n    depends_on: [db]\n  db:\n    image: postgres:18\n",
		"deploy/k8s/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n",
		"infra/main.tf":              "resource \"aws_s3_bucket\" \"assets\" {}\nmodule \"vpc\" { source = \"./vpc\" }\n",
		"chart/Chart.yaml":           "apiVersion: v2\nname: api-chart\nversion: 1.0.0\n",
		"serverless.yml":             "service: api\nprovider:\n  name: aws\nfunctions:\n  hello:\n    handler: handler.hello\n",
		".github/workflows/ci.yml":   "name: CI\non: [push]\njobs:\n  test:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./actions/build\n      - run: go test ./...\n        working-directory: services/api\n",
		".gitlab-ci.yml":             "test:\n  script: go test ./...\ninclude: local.yml\n",
		"Jenkinsfile":                "pipeline {\n  stages {}\n}\n",
		"tekton/task.yaml":           "apiVersion: tekton.dev/v1\nkind: Task\nmetadata:\n  name: compile\n",
		"infra/template.yaml":        "AWSTemplateFormatVersion: '2010-09-09'\nResources:\n  Bucket:\n    Type: AWS::S3::Bucket\n",
	}
	files := make([]Candidate, 0, len(inputs))
	for name, value := range inputs {
		name, value := name, value
		files = append(files, Candidate{Path: name, Size: int64(len(value)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(value), int64(len(value)), nil }})
	}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" || r.Coverage.ParsedFiles != int64(len(inputs)) {
		t.Fatalf("unexpected coverage: %+v diagnostics=%+v", r.Coverage, r.Diagnostics)
	}
	providers := map[string]bool{}
	qualifications := map[string]bool{}
	for _, d := range r.Definitions {
		providers[d.Provider] = true
		for _, ref := range d.References {
			qualifications[ref.Qualification] = true
		}
	}
	for _, want := range []string{"dockerfile", "compose", "kubernetes", "terraform", "helm", "serverless-framework", "github-actions", "gitlab-ci", "jenkins", "tekton", "cloudformation"} {
		if !providers[want] {
			t.Errorf("missing provider %q in %#v", want, providers)
		}
	}
	for _, want := range []string{"local", "external", "unresolved"} {
		if !qualifications[want] {
			t.Errorf("missing qualification %q", want)
		}
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "go test ./...") || strings.Contains(string(encoded), "handler.hello") {
		t.Fatalf("retained arbitrary command/config value: %s", encoded)
	}
}

func TestMisleadingFilenamesDoNotCreateClaims(t *testing.T) {
	for _, tc := range []struct{ name, body string }{{"Dockerfile", "this is documentation"}, {"deployment.yaml", "kind: of misleading prose"}, {".github/workflows/ci.yml", "name: merely a name"}, {"main.tf", "# resource \"fake\" \"fake\" {}"}} {
		r, err := Observe(context.Background(), []Candidate{{Path: tc.name, Size: int64(len(tc.body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(tc.body), int64(len(tc.body)), nil }}}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Definitions) != 0 {
			t.Errorf("%s produced claims: %+v", tc.name, r.Definitions)
		}
	}
}

func TestDocumentationAndNestedWorkflowPathsAreNotPromoted(t *testing.T) {
	inputs := map[string]string{
		"docs/deployment.yaml":            "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: prose}\n",
		".github/workflows/nested/ci.yml": "jobs:\n  test:\n    steps: []\n",
	}
	files := []Candidate{}
	for name, body := range inputs {
		name, body := name, body
		files = append(files, Candidate{Path: name, Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }})
	}
	r, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Definitions) != 0 {
		t.Fatalf("documentation/nested workflow promoted: %+v", r.Definitions)
	}
}

func TestUnsupportedYAMLAliasIsPartialBeforeParsing(t *testing.T) {
	body := "services:\n  base: &base\n    image: alpine\n  copy: *base\n"
	r, err := Observe(context.Background(), []Candidate{{Path: "compose.yaml", Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Omissions["parse_error"] != 1 || len(r.Definitions) != 0 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestBoundedAndMalformedAreExplicitlyPartial(t *testing.T) {
	body := "services:\n  api: ["
	r, err := Observe(context.Background(), []Candidate{{Path: "compose.yaml", Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }}, {Path: "huge.yaml", Size: 999, Read: func(context.Context, int64) ([]byte, int64, error) {
		t.Fatal("oversized file read")
		return nil, 0, nil
	}}}, Options{FileBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Omissions["parse_error"] != 1 || r.Omissions["read_limit"] != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
}

func TestCollectorIsConcurrentDeterministicAndDoesNotEmitLegacyFindings(t *testing.T) {
	files := []profile.File{{Path: "z/main.tf", Size: 28, Content: []byte("resource \"x\" \"z\" {}\n")}, {Path: "a/main.tf", Size: 28, Content: []byte("resource \"x\" \"a\" {}\n")}, {Path: "README.md", Size: 20, Content: []byte("# says Kubernetes")}}
	for i := range files {
		files[i].Size = int64(len(files[i].Content))
	}
	run := func() *Report {
		c := NewCollector(Options{})
		var wg sync.WaitGroup
		for _, f := range files {
			f := f
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := c.Detect(context.Background(), f)
				if err != nil {
					t.Error(err)
				}
				if got != nil {
					t.Errorf("legacy findings: %+v", got)
				}
			}()
		}
		wg.Wait()
		return c.Finish()
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic:\n%+v\n%+v", a, b)
	}
	if len(a.Definitions) != 2 || a.Definitions[0].Path != "a/main.tf" {
		t.Fatalf("unexpected definitions: %+v", a.Definitions)
	}
}

func TestCollectorRejectsTruncatedScannerViews(t *testing.T) {
	c := NewCollector(Options{})
	content := []byte("resource \"x\" \"a\" {}\n")
	_, err := c.Detect(context.Background(), profile.File{Path: "main.tf", Size: int64(len(content) + 1), Content: content})
	if err != nil {
		t.Fatal(err)
	}
	r := c.Finish()
	if len(r.Definitions) != 0 || r.Omissions["incomplete_read"] != 1 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestCollectorSelectionBudgetIsOrderIndependent(t *testing.T) {
	files := []profile.File{
		{Path: "z/Dockerfile", Content: []byte("FROM scratch\n")},
		{Path: "m/Dockerfile", Content: []byte("FROM scratch\n")},
		{Path: "a/Dockerfile", Content: []byte("FROM scratch\n")},
	}
	for i := range files {
		files[i].Size = int64(len(files[i].Content))
	}
	run := func(order []int) *Report {
		c := NewCollector(Options{Files: 2, InputBytes: 26})
		for _, i := range order {
			if _, err := c.Detect(context.Background(), files[i]); err != nil {
				t.Fatal(err)
			}
		}
		return c.Finish()
	}
	a, b := run([]int{0, 1, 2}), run([]int{2, 1, 0})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("budget selection depends on worker order:\n%+v\n%+v", a, b)
	}
	if a.Coverage.ReadFiles != 2 || a.Omissions["selection_budget"] != 1 || a.Status != "partial" {
		t.Fatalf("unexpected budget coverage: %+v", a)
	}
	for _, d := range a.Definitions {
		if d.Path == "z/Dockerfile" {
			t.Fatalf("retained lexically late declaration: %+v", a.Definitions)
		}
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Observe(ctx, []Candidate{{Path: "main.tf"}}, Options{})
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	c := NewCollector(Options{})
	_, err = c.Detect(ctx, profile.File{})
	if err != context.Canceled {
		t.Fatalf("detector got %v", err)
	}
}
