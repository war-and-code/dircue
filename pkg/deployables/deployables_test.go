package deployables

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
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

func TestLaunchFacetsWithholdDockerArgumentsAndRetainCronSchedule(t *testing.T) {
	docker := "FROM python:3.12\nENTRYPOINT [\"python3\", \"-m\", \"http.server\", \"--password\", \"DO_NOT_DISCLOSE\"]\nCMD [\"custom-launcher\", \"DO_NOT_DISCLOSE\"]\n"
	kube := "# schedule: fake comment value\napiVersion: batch/v1\nkind: CronJob\nmetadata:\n  name: nightly\nspec:\n  schedule: \"0 3 * * *\"\n"
	files := []Candidate{}
	for name, body := range map[string]string{"Dockerfile": docker, "cron.yaml": kube} {
		name, body := name, body
		files = append(files, Candidate{Path: name, Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }})
	}
	report, err := Observe(context.Background(), files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var sawPython, sawWithheld, sawCustomWithheld, sawCron bool
	for _, def := range report.Definitions {
		for _, ref := range def.References {
			switch ref.Kind {
			case "docker_entrypoint":
				sawPython = ref.Value == "python3 -m http.server" && ref.Qualification == "declared"
			case "docker_entrypoint_arguments":
				sawWithheld = ref.Qualification == "withheld_arguments" && ref.Value == ""
			case "docker_cmd":
				sawCustomWithheld = ref.Value == "" && ref.Qualification == "withheld_arguments"
			case "cron_schedule":
				sawCron = ref.Value == "0 3 * * *" && ref.Qualification == "declared" && ref.Evidence.Line == 0
			}
		}
	}
	if !sawPython || !sawWithheld || !sawCustomWithheld || !sawCron {
		t.Fatalf("launch facets absent: python=%t withheld=%t custom=%t cron=%t defs=%+v", sawPython, sawWithheld, sawCustomWithheld, sawCron, report.Definitions)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "DO_NOT_DISCLOSE") || strings.Contains(string(encoded), "--password") || strings.Contains(string(encoded), "custom-launcher") {
		t.Fatalf("Docker argv leaked into report: %s", encoded)
	}

	templated, recognized, err := parseYAML("cron.yaml", []byte("apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: dynamic}\nspec:\n  schedule: '{{ .Values.schedule }}'\n"))
	if err != nil || !recognized || len(templated) != 1 || len(templated[0].References) != 1 || templated[0].References[0].Qualification != "unresolved" || templated[0].References[0].Value != "" {
		t.Fatalf("templated schedule: defs=%+v recognized=%t err=%v", templated, recognized, err)
	}
}

func TestDockerLaunchFormStageAndUnresolvedBoundaries(t *testing.T) {
	body := `FROM alpine AS build
ENTRYPOINT ["python3", "-m", "http.server", "--password", "DO_NOT_DISCLOSE"]
RUN printf ok \\
CMD ["node", "DO_NOT_DISCLOSE"]
FROM alpine AS runtime
CMD ["python3", "-m", "http.server"]
ENTRYPOINT []
ENTRYPOINT ["PyThOn", "app"]
ENTRYPOINT 'python3' app
CMD [
  "node",
  "app"
]
`
	defs, recognized, err := parseDockerfile("Dockerfile", []byte(body))
	if err != nil || !recognized || len(defs) != 1 {
		t.Fatalf("Dockerfile parse: defs=%+v recognized=%t err=%v", defs, recognized, err)
	}
	var buildEntry, runtimeCmd, emptyExec, mixedCase, quotedShell, multilineCmd, runContinuationCmd int
	for _, ref := range defs[0].References {
		if ref.Kind == "docker_entrypoint" && ref.Stage == "build" {
			buildEntry++
			if ref.Value != "python3 -m http.server" || ref.Qualification != "declared" || ref.Evidence.Basis != "dockerfile-instruction-exec" {
				t.Errorf("build stage entrypoint: %+v", ref)
			}
		}
		if ref.Kind == "docker_cmd" && ref.Stage == "runtime" && ref.Evidence.Line == 6 {
			runtimeCmd++
			if ref.Value != "python3 -m http.server" || ref.Qualification != "declared" || ref.Evidence.Basis != "dockerfile-instruction-exec" {
				t.Errorf("runtime command: %+v", ref)
			}
		}
		if ref.Kind == "docker_entrypoint" && ref.Qualification == "unresolved" && ref.Value == "" {
			emptyExec++
		}
		if ref.Kind == "docker_entrypoint" && ref.Evidence.Line == 8 && ref.Value == "" && ref.Qualification == "withheld_arguments" {
			mixedCase++
		}
		if ref.Kind == "docker_entrypoint" && ref.Evidence.Line == 9 && ref.Value == "" && ref.Qualification == "withheld_arguments" {
			quotedShell++
		}
		if ref.Kind == "docker_cmd" && ref.Evidence.Line == 10 && ref.Qualification == "unresolved" && ref.Value == "" && ref.Stage == "runtime" {
			multilineCmd++
		}
		if ref.Kind == "docker_cmd" && ref.Evidence.Line == 4 {
			runContinuationCmd++
		}
		if ref.Kind == "docker_cmd" && ref.Value == "node" && ref.Stage == "runtime" {
			t.Fatalf("launch in continuation promoted: %+v", ref)
		}
		if ref.Kind == "docker_cmd" && ref.Value == "node" {
			t.Fatalf("RUN continuation promoted to a launch instruction: %+v", ref)
		}
	}
	if buildEntry != 1 || runtimeCmd != 1 || emptyExec != 1 || mixedCase != 1 || quotedShell != 1 || multilineCmd != 1 || runContinuationCmd != 0 {
		t.Fatalf("launch boundaries build=%d runtime=%d empty=%d mixed=%d quoted=%d multiline=%d run-continuation=%d refs=%+v", buildEntry, runtimeCmd, emptyExec, mixedCase, quotedShell, multilineCmd, runContinuationCmd, defs[0].References)
	}
	if defs[0].DockerFinalStage != "runtime" {
		t.Fatalf("final declared stage = %q", defs[0].DockerFinalStage)
	}
}

func TestDockerLaunchContinuationCannotCreateStageOrOpcode(t *testing.T) {
	body := "FROM alpine AS first\nCMD [\\\nFROM alpine AS fake\nENV LEAK=value\n]\nFROM alpine AS last\nCMD [\"node\"]\n"
	defs, recognized, err := parseDockerfile("Dockerfile", []byte(body))
	if err != nil || !recognized || len(defs) != 1 {
		t.Fatalf("Dockerfile parse: defs=%+v recognized=%t err=%v", defs, recognized, err)
	}
	if defs[0].DockerFinalStage != "last" {
		t.Fatalf("continuation text changed final stage: %q", defs[0].DockerFinalStage)
	}
	for _, ref := range defs[0].References {
		if ref.Kind == "docker_cmd" && ref.Stage == "fake" {
			t.Fatalf("continuation text created a stage-scoped launch fact: %+v", ref)
		}
	}
}

func TestCronJobFacetsAreTypedBoundedAndUnresolvedWhenUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name, body                          string
		wantSchedule, wantSuspend, wantZone string
	}{
		{"valid", "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: x}\nspec:\n  schedule: '*/5 * * * *'\n  suspend: false\n  timeZone: Etc/UTC\n", "declared", "declared", "withheld_value"},
		{"invalid schedule", "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: x}\nspec:\n  schedule: '60 * * * *'\n  suspend: nonsense\n  timeZone: 'user:secret@example'\n", "", "", "withheld_value"},
		{"missing schedule", "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: x}\nspec:\n  suspend: true\n", "", "declared", ""},
		{"dynamic", "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: x}\nspec:\n  schedule: '{{ .Values.schedule }}'\n  suspend: '{{ .Values.suspend }}'\n  timeZone: '{{ .Values.zone }}'\n", "unresolved", "unresolved", "unresolved"},
		{"wrong types", "apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: x}\nspec:\n  schedule: [1, 2]\n  suspend: 1\n  timeZone: 7\n", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defs, recognized, err := parseYAML("cron.yaml", []byte(tc.body))
			if err != nil || !recognized || len(defs) != 1 {
				t.Fatalf("parse: defs=%+v recognized=%t err=%v", defs, recognized, err)
			}
			got := map[string]Reference{}
			for _, ref := range defs[0].References {
				got[ref.Kind] = ref
			}
			for kind, want := range map[string]string{"cron_schedule": tc.wantSchedule, "cron_suspend": tc.wantSuspend, "cron_timezone": tc.wantZone} {
				ref, ok := got[kind]
				if want == "" {
					if ok {
						t.Errorf("unexpected %s: %+v", kind, ref)
					}
					continue
				}
				if !ok || ref.Qualification != want {
					t.Errorf("%s = %+v, want %s", kind, ref, want)
				}
				if kind == "cron_timezone" && ref.Value != "" {
					t.Errorf("timezone value leaked: %+v", ref)
				}
				if kind == "cron_schedule" && ref.Evidence.Line != 0 {
					t.Errorf("unverified source coordinate: %+v", ref)
				}
			}
		})
	}
}

func TestMavenArchiveParserAcceptsSingleByteXML(t *testing.T) {
	body := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><artifactId>caf\xe9</artifactId><version>1</version><packaging>war</packaging></project>")
	definitions, matched, err := parsePomXML("pom.xml", body)
	if err != nil || !matched || len(definitions) != 1 || definitions[0].Name != "café-1.war" {
		t.Fatalf("ISO-8859-1 WAR omitted: matched=%t defs=%+v err=%v", matched, definitions, err)
	}
	ascii := []byte(`<?xml version="1.0" encoding="US-ASCII"?><project><artifactId>web</artifactId><version>1</version><packaging>ear</packaging></project>`)
	definitions, matched, err = parsePomXML("pom.xml", ascii)
	if err != nil || !matched || len(definitions) != 1 || definitions[0].Name != "web-1.ear" {
		t.Fatalf("US-ASCII EAR omitted: matched=%t defs=%+v err=%v", matched, definitions, err)
	}
	text := `<?xml version="1.0" encoding="utf-16"?><project><artifactId>web</artifactId><version>1</version><packaging>war</packaging></project>`
	utf16Body := make([]byte, 2+2*len([]rune(text)))
	utf16Body[0], utf16Body[1] = 0xff, 0xfe
	for i, r := range text {
		binary.LittleEndian.PutUint16(utf16Body[2+i*2:], uint16(r))
	}
	definitions, matched, err = parsePomXML("pom.xml", utf16Body)
	if err != nil || !matched || len(definitions) != 1 || definitions[0].Name != "web-1.war" {
		t.Fatalf("UTF-16 WAR omitted: matched=%t defs=%+v err=%v", matched, definitions, err)
	}
}

func TestTerraformOnlyLiteralLocalModuleSourcesAreQualified(t *testing.T) {
	defs, matched, err := parseTerraform("infra/main.tf", []byte(`resource "x_y" "z" {}
module "local" {
  source = "../modules/network" # literal local source
}
module "registry" { source = "example/network/aws" }
module "dynamic" { source = "../${var.name}" }
module "commented" {
  # source = "../not-a-module"
}
`))
	if err != nil || !matched || len(defs) != 1 {
		t.Fatalf("parse Terraform: matched=%t defs=%+v err=%v", matched, defs, err)
	}
	want := map[string]string{"../modules/network": "local", "example/network/aws": "external", "../${var.name}": "unresolved", "uninspected": "unresolved"}
	for _, ref := range defs[0].References {
		if ref.Kind != "module_source" {
			continue
		}
		if q, ok := want[ref.Value]; !ok || q != ref.Qualification {
			t.Errorf("unexpected module source %+v", ref)
		}
		delete(want, ref.Value)
	}
	if len(want) != 0 {
		t.Fatalf("missing module source observations: %v", want)
	}
}

func TestTerraformModuleSourcesDoNotReadCommentsNestedValuesOrAmbiguousSyntax(t *testing.T) {
	body := []byte(`resource "x_y" "z" {}
module "nested" {
  local = { source = "../fake" }
}
module "duplicate" {
  source = "../first"
  source = "../second"
}
module "heredoc" {
  marker = <<EOF
source = "../fake"
}
EOF
  source = "../real"
}
module "block-comment" {
  /* source = "../fake" */
  source = "../real"
}
`)
	defs, matched, err := parseTerraform("infra/main.tf", body)
	if err != nil || !matched || len(defs) != 1 {
		t.Fatalf("parse Terraform: matched=%t defs=%+v err=%v", matched, defs, err)
	}
	for _, ref := range defs[0].References {
		if ref.Kind == "module_source" && (ref.Qualification != "unresolved" || ref.Value != "uninspected") {
			t.Errorf("ambiguous or nested Terraform source became linkable: %+v", ref)
		}
	}
}

func TestGitHubBuildPushActionRequiresExplicitStaticContext(t *testing.T) {
	defs, matched, err := parseYAML(".github/workflows/build.yml", []byte(`name: build
jobs:
  image:
    steps:
      - uses: actions/checkout@v4
        with:
          path: repo
      - uses: docker/build-push-action@v6
        with:
          context: repo/services/api
          file: services/api/Dockerfile
      - uses: docker/build-push-action@v6
        with:
          context: ${{ github.workspace }}
`))
	if err != nil || !matched || len(defs) != 1 {
		t.Fatalf("parse workflow: matched=%t defs=%+v err=%v", matched, defs, err)
	}
	contexts := 0
	for _, ref := range defs[0].References {
		if ref.Kind != "build_context" {
			continue
		}
		contexts++
		if ref.Value == "repo/services/api" && (ref.Qualification != "local" || ref.Checkout != "repo") {
			t.Errorf("literal context/check-out binding lost: %+v", ref)
		}
		if strings.Contains(ref.Value, "${{") && ref.Qualification != "unresolved" {
			t.Errorf("expression context was resolved: %+v", ref)
		}
	}
	if contexts != 2 {
		t.Fatalf("expected explicit context observations only, got %+v", defs[0].References)
	}
}

func TestMakefileReadsOnlyStaticDockerBuildFileAndContext(t *testing.T) {
	body := `OCI_BUILD := DOCKER_BUILDKIT=1 docker buildx build $(BUILD_ARGS)
OCI_BUILD := DOCKER_BUILDKIT=1 docker build $(BUILD_ARGS)

image:
	$(OCI_BUILD) -t $(IMAGE) -f cmd/loki/Dockerfile .
	docker build -f cmd/api/Dockerfile services/api
	docker build -f $(DOCKERFILE) .
	$(UNKNOWN_BUILD) -f cmd/nope/Dockerfile .
	$(OCI_BUILD) -f cmd/dynamic/Dockerfile $(BUILD_CONTEXT)
`
	defs, matched, err := parseMakefile("Makefile", []byte(body))
	if err != nil || !matched || len(defs) != 2 {
		t.Fatalf("static Makefile builds: matched=%t defs=%+v err=%v", matched, defs, err)
	}
	wanted := map[string]string{"cmd/loki/Dockerfile": ".", "cmd/api/Dockerfile": "services/api"}
	for _, def := range defs {
		file, context := "", ""
		for _, ref := range def.References {
			switch ref.Kind {
			case "dockerfile":
				file = ref.Value
			case "build_context":
				context = ref.Value
			}
		}
		if wanted[file] != context || context == "" {
			t.Errorf("unexpected build context pair: %+v", def.References)
		}
		delete(wanted, file)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing static build invocations: %v", wanted)
	}
	file := []byte(body)
	report, err := Observe(context.Background(), []Candidate{{Path: "Makefile", Size: int64(len(file)), Read: func(context.Context, int64) ([]byte, int64, error) { return file, int64(len(file)), nil }}}, Options{})
	if err != nil || len(report.BuildContexts) != 2 || len(report.Definitions) != 0 {
		t.Fatalf("Makefile contexts should remain private observer metadata, report=%+v err=%v", report, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "BuildContexts") || strings.Contains(string(encoded), "makefile") {
		t.Fatalf("Makefile plumbing changed public report JSON: %s err=%v", encoded, err)
	}
	collector := NewCollector(Options{})
	if _, err := collector.Detect(context.Background(), profile.File{Path: "Makefile", Size: int64(len(file)), Content: file}); err != nil {
		t.Fatal(err)
	}
	collected := collector.Finish()
	if len(collected.BuildContexts) != 2 || len(collected.Definitions) != 0 {
		t.Fatalf("scanner collector dropped private Makefile contexts: %+v", collected)
	}
}

func TestMakefileRejectsUnsafeDockerBuildVariablePrefixes(t *testing.T) {
	for _, assignment := range []string{
		"OCI_BUILD := docker build ; echo unsafe",
		"OCI_BUILD := docker build -f hidden/Dockerfile .",
		"OCI_BUILD := docker build hidden-context",
	} {
		t.Run(assignment, func(t *testing.T) {
			body := assignment + "\nimage:\n\t$(OCI_BUILD) -f services/api/Dockerfile services/api\n"
			defs, matched, err := parseMakefile("Makefile", []byte(body))
			if err != nil || matched || len(defs) != 0 {
				t.Fatalf("unsafe variable command was attributed: matched=%t defs=%+v err=%v", matched, defs, err)
			}
		})
	}
}

func TestMakefileRejectsAmbiguousDirectDockerBuildArguments(t *testing.T) {
	for _, command := range []string{
		"docker build -f wrong/Dockerfile other-context -f services/api/Dockerfile services/api",
		"docker build -f services/api/Dockerfile other-context services/api",
	} {
		t.Run(command, func(t *testing.T) {
			body := "image:\n\t" + command + "\n"
			defs, matched, err := parseMakefile("Makefile", []byte(body))
			if err != nil || matched || len(defs) != 0 {
				t.Fatalf("ambiguous Docker command was attributed: matched=%t defs=%+v err=%v", matched, defs, err)
			}
		})
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

func TestYAMLAliasIsParsedWithinParserLimits(t *testing.T) {
	body := "services:\n  base: &base\n    image: alpine\n  copy: *base\n"
	r, err := Observe(context.Background(), []Candidate{{Path: "compose.yaml", Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "complete" || r.Coverage.ParsedFiles != 1 || len(r.Definitions) != 2 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestYAMLAdmissionAcceptsValidPunctuationAndCron(t *testing.T) {
	body := "name: CI\non:\n  schedule:\n    - cron: '*/5 * * * *'\njobs:\n  test:\n    steps:\n      - run: echo foo && echo bar\n"
	r := observeOne(t, ".github/workflows/ci.yml", body)
	if r.Status != "complete" || len(r.Definitions) != 1 {
		t.Fatalf("valid YAML syntax was rejected: %+v", r)
	}
}

func TestKubernetesMultiDocumentYAML(t *testing.T) {
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          image: example/api:v1\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: api\n"
	r := observeOne(t, "deploy/k8s.yaml", body)
	if r.Status != "complete" || r.Coverage.ParsedFiles != 1 || len(r.Definitions) != 2 {
		t.Fatalf("multi-document manifest was not fully parsed: %+v", r)
	}
	for _, d := range r.Definitions {
		if d.Provider == "kubernetes" && d.Kind == "workload" && d.Name == "api" && len(d.References) != 1 {
			t.Errorf("expected one workload image reference: %+v", d)
		}
	}
}

func TestKubernetesResourcesKeepKindAndNameIdentity(t *testing.T) {
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: checkout\nspec:\n  template:\n    spec:\n      containers:\n        - name: checkout\n          image: example/checkout:v1\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: checkout\nspec:\n  ports:\n    - port: 8080\n---\napiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: checkout\n"
	r := observeOne(t, "deploy/k8s.yaml", body)
	if r.Status != "complete" || r.Coverage.ParsedFiles != 1 || len(r.Definitions) != 3 {
		t.Fatalf("expected all three Kubernetes objects: %+v", r)
	}
	want := map[string]string{
		"Deployment":     "workload",
		"Service":        "service",
		"ServiceAccount": "infrastructure",
	}
	ids := map[string]bool{}
	for _, definition := range r.Definitions {
		resourceKind := ""
		for _, evidence := range definition.Evidence {
			if evidence.Field == "kind" {
				resourceKind = evidence.Value
				break
			}
		}
		if definition.Provider != "kubernetes" || definition.Name != "checkout" {
			t.Errorf("resource lost provider or metadata.name: %+v", definition)
		}
		if got := want[resourceKind]; got == "" || definition.Kind != got {
			t.Errorf("%s classified as %q, want %q: %+v", resourceKind, definition.Kind, got, definition)
		}
		if ids[definition.ID] {
			t.Errorf("same-name cross-kind resources collided at ID %q", definition.ID)
		}
		ids[definition.ID] = true
		if resourceKind == "Deployment" && (len(definition.References) != 1 || definition.References[0].Value != "example/checkout:v1") {
			t.Errorf("workload image was not retained: %+v", definition)
		}
		if resourceKind != "Deployment" && len(definition.References) != 0 {
			t.Errorf("non-workload resource gained workload references: %+v", definition)
		}
	}
}

func TestKubernetesWorkloadKindCatalog(t *testing.T) {
	for _, resourceKind := range []string{"Pod", "ReplicationController", "ReplicaSet", "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"} {
		if got := kubernetesDefinitionKind(resourceKind); got != "workload" {
			t.Errorf("%s classified as %q, want workload", resourceKind, got)
		}
	}
	if got := kubernetesDefinitionKind("Service"); got != "service" {
		t.Errorf("Service classified as %q, want service", got)
	}
	for _, resourceKind := range []string{"ServiceAccount", "ConfigMap", "Secret", "Namespace", "PersistentVolume", "PersistentVolumeClaim", "StorageClass", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding", "NetworkPolicy"} {
		if got := kubernetesDefinitionKind(resourceKind); got != "infrastructure" {
			t.Errorf("%s classified as %q, want infrastructure", resourceKind, got)
		}
	}
	if got := kubernetesDefinitionKind("CustomResource"); got != "resource" {
		t.Errorf("unknown API kind classified as %q, want neutral resource", got)
	}
}

func TestTektonResourceDefinitionKeepsExistingImageReferences(t *testing.T) {
	// Keep resourceDefinition's existing Tekton handling when Kubernetes
	// workload-specific image extraction is narrowed.
	body := "apiVersion: tekton.dev/v1\nkind: Task\nmetadata:\n  name: build\nspec:\n  containers:\n    - name: worker\n      image: example/worker:v1\n"
	r := observeOne(t, "pipeline/task.yaml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("Tekton task was not retained: %+v", r)
	}
	d := r.Definitions[0]
	if d.Provider != "tekton" || d.Kind != "workflow" || len(d.References) != 1 {
		t.Fatalf("Tekton image reference behavior changed: %+v", d)
	}
	if ref := d.References[0]; ref.Kind != "image" || ref.Value != "example/worker:v1" {
		t.Fatalf("unexpected Tekton reference: %+v", ref)
	}
}

func TestUnknownKubernetesResourceStaysQualifiedAndIdentifiable(t *testing.T) {
	body := "apiVersion: apps.example.dev/v1\nkind: CustomWorkload\nmetadata:\n  name: example\n"
	r := observeOne(t, "deploy/custom.yaml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("unknown resource declaration was dropped: %+v", r)
	}
	d := r.Definitions[0]
	if d.Provider != "kubernetes" || d.Kind != "resource" || d.Name != "example" || d.Coverage != "qualified" {
		t.Fatalf("unknown resource was overclassified: %+v", d)
	}
	if len(d.Evidence) == 0 || d.Evidence[0].Value != "CustomWorkload" {
		t.Fatalf("original API kind is not retained as evidence: %+v", d)
	}
}

func TestTerraformAliasedProvidersHaveDistinctIDs(t *testing.T) {
	// One .tf file → one module-level definition whose evidence records every block.
	// Two provider blocks in the same file must both appear in evidence so no
	// information is lost, even though they share a single deployable identity.
	body := "provider \"aws\" {}\nprovider \"aws\" {\n  alias = \"west\"\n}\n"
	r := observeOne(t, "infra/providers.tf", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("expected one module-level definition, got: %+v", r.Definitions)
	}
	d := r.Definitions[0]
	if d.Provider != "terraform" || d.Kind != "infrastructure" {
		t.Fatalf("wrong kind/provider for terraform module definition: %+v", d)
	}
	// Both provider blocks must appear as evidence.
	if len(d.Evidence) < 2 {
		t.Fatalf("expected at least 2 evidence items (one per provider block), got: %+v", d.Evidence)
	}
}

func TestHelmTemplateLiteralFieldDoesNotConsumeFollowingLines(t *testing.T) {
	// Template files inside charts/templates/ are Go-template sources, not
	// deployed resources in their own right. The chart node (from Chart.yaml) is
	// the deployable; per-template nodes would inflate the count 100×. Template
	// files are intentionally skipped so callers see zero definitions here.
	body := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: {{ .Values.name }}\nspec: {{- .Values.spec | toYaml | nindent 2 }}\n"
	r := observeOne(t, "chart/templates/deployment.yaml", body)
	if len(r.Definitions) != 0 {
		t.Fatalf("helm template file should produce no definitions, got: %+v", r.Definitions)
	}
}

func TestHelmConditionalServiceAccountRemainsQualifiedInfrastructure(t *testing.T) {
	// Same rule as above: template files are skipped. The ServiceAccount is part
	// of the chart, not a standalone deployable. Expect zero definitions.
	body := "apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: {{ .Values.serviceAccount.name }}\n"
	r := observeOne(t, "chart/templates/serviceaccount.yaml", body)
	if len(r.Definitions) != 0 {
		t.Fatalf("helm template file should produce no definitions, got: %+v", r.Definitions)
	}
}

func TestHelmValuesYAMLImageRefsEnrichChartDefinition(t *testing.T) {
	// A co-located values.yaml that declares image.repository + image.tag
	// should add image references to the Chart.yaml definition.
	chart := "apiVersion: v2\nname: my-service\nversion: 1.0.0\n"
	values := "image:\n  repository: myrepo/my-service\n  tag: \"2.3.4\"\n"
	r, err := Observe(context.Background(), []Candidate{
		{Path: "charts/my-service/Chart.yaml", Size: int64(len(chart)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return []byte(chart), int64(len(chart)), nil }},
		{Path: "charts/my-service/values.yaml", Size: int64(len(values)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
			return []byte(values), int64(len(values)), nil
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d: %+v", len(r.Definitions), r.Definitions)
	}
	def := r.Definitions[0]
	if def.Provider != "helm" || def.Name != "my-service" {
		t.Fatalf("unexpected definition: %+v", def)
	}
	var imageRef *Reference
	for i := range def.References {
		if def.References[i].Kind == "image" {
			imageRef = &def.References[i]
			break
		}
	}
	if imageRef == nil {
		t.Fatalf("no image reference found in %+v", def.References)
	}
	if imageRef.Value != "myrepo/my-service:2.3.4" {
		t.Errorf("image value = %q; want myrepo/my-service:2.3.4", imageRef.Value)
	}
	if imageRef.Qualification != "external" {
		t.Errorf("image qualification = %q; want external", imageRef.Qualification)
	}
}

func TestHelmValuesYAMLLatestTagOmitted(t *testing.T) {
	// "latest" tag is too dynamic to be a useful reference — omit the tag.
	chart := "apiVersion: v2\nname: my-svc\nversion: 0.1.0\n"
	values := "image:\n  repository: myorg/my-svc\n  tag: latest\n"
	r, err := Observe(context.Background(), []Candidate{
		{Path: "charts/my-svc/Chart.yaml", Size: int64(len(chart)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return []byte(chart), int64(len(chart)), nil }},
		{Path: "charts/my-svc/values.yaml", Size: int64(len(values)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
			return []byte(values), int64(len(values)), nil
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	def := r.Definitions[0]
	for _, ref := range def.References {
		if ref.Kind == "image" && ref.Value == "myorg/my-svc:latest" {
			t.Error("latest tag should not be included in image reference")
		}
		if ref.Kind == "image" && ref.Value == "myorg/my-svc" {
			return // ok: repository without tag
		}
	}
}

func TestHelmValuesYAMLTemplateTagSkipped(t *testing.T) {
	// A Go-template tag expression in the tag field cannot be resolved statically.
	chart := "apiVersion: v2\nname: tmpl-svc\nversion: 0.1.0\n"
	values := "image:\n  repository: myorg/tmpl-svc\n  tag: \"{{ .Chart.AppVersion }}\"\n"
	r, err := Observe(context.Background(), []Candidate{
		{Path: "charts/tmpl-svc/Chart.yaml", Size: int64(len(chart)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) { return []byte(chart), int64(len(chart)), nil }},
		{Path: "charts/tmpl-svc/values.yaml", Size: int64(len(values)), Read: func(_ context.Context, _ int64) ([]byte, int64, error) {
			return []byte(values), int64(len(values)), nil
		}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	def := r.Definitions[0]
	for _, ref := range def.References {
		if ref.Kind == "image" && strings.Contains(ref.Value, "{{") {
			t.Errorf("template expression should not appear in image reference: %q", ref.Value)
		}
	}
}

func TestHelmValuesYAMLWithoutCoLocatedChartIsNotAdded(t *testing.T) {
	// A values.yaml without a co-located Chart.yaml should not produce
	// any definitions on its own.
	values := "image:\n  repository: myrepo/standalone\n  tag: \"1.0.0\"\n"
	r := observeOne(t, "charts/standalone/values.yaml", values)
	if len(r.Definitions) != 0 {
		t.Fatalf("lone values.yaml should not produce definitions, got: %+v", r.Definitions)
	}
}

func TestSAMCodeURICapturedAsLocalReference(t *testing.T) {
	body := "service: orders\nResources:\n  Function:\n    Type: AWS::Serverless::Function\n    Properties:\n      CodeUri: src/\n"
	r := observeOne(t, "template.yaml", body)
	if len(r.Definitions) != 1 || len(r.Definitions[0].References) != 1 {
		t.Fatalf("CodeUri reference missing: %+v", r.Definitions)
	}
	ref := r.Definitions[0].References[0]
	if ref.Kind != "code_uri" || ref.Value != "src/" || ref.Qualification != "local" {
		t.Fatalf("unexpected CodeUri reference: %+v", ref)
	}
}

func TestSkaffoldBuildArtifactReferencesImageAndContext(t *testing.T) {
	body := "apiVersion: skaffold/v3\nkind: Config\nbuild:\n  artifacts:\n    - image: cartservice\n      context: src/cartservice/src\n      docker:\n        dockerfile: Dockerfile\n"
	r := observeOne(t, "skaffold.yaml", body)
	if len(r.Definitions) != 1 || r.Definitions[0].Provider != "skaffold" || r.Definitions[0].Kind != "container_build" {
		t.Fatalf("Skaffold config was not recognized as a build declaration: %+v", r.Definitions)
	}
	refs := map[string]Reference{}
	for _, ref := range r.Definitions[0].References {
		refs[ref.Kind] = ref
	}
	if refs["image"].Value != "cartservice" || refs["build_context"].Value != "src/cartservice/src" || refs["dockerfile"].Value != "Dockerfile" {
		t.Fatalf("missing Skaffold artifact mapping: %+v", refs)
	}
}

func TestYAMLOmissionDiagnosticIdentifiesFile(t *testing.T) {
	body := "services:\n  api: [\n"
	r := observeOne(t, "deploy/compose.yaml", body)
	if r.Status != "partial" || r.Omissions["parse_error"] != 1 || len(r.Diagnostics) != 1 {
		t.Fatalf("expected one parse omission diagnostic: %+v", r)
	}
	if r.Diagnostics[0].Path != "deploy/compose.yaml" || r.Diagnostics[0].Message == "" {
		t.Fatalf("diagnostic lacks file context: %+v", r.Diagnostics[0])
	}
}

func observeOne(t *testing.T, name, body string) *Report {
	t.Helper()
	r, err := Observe(context.Background(), []Candidate{{Path: name, Size: int64(len(body)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(body), int64(len(body)), nil }}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r
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

func TestDockerfileDisplayNameDerivedFromParentDirectory(t *testing.T) {
	// Verify that parseDockerfile uses the parent directory name rather than
	// the hard-coded "default" sentinel.
	cases := []struct {
		path string
		want string
	}{
		{"Dockerfile", "(root)"},                  // root Dockerfile → "(root)"
		{"src/api/Dockerfile", "api"},             // service sub-directory
		{"services/auth/Dockerfile", "auth"},      // deeper path
		{"src/cartservice/src/Dockerfile", "src"}, // nested src/ uses parent dir
		{"Dockerfile.prod", "(root)"},             // variant at root
		{"apps/web/Dockerfile.staging", "web"},    // variant in sub-directory
	}
	for _, tc := range cases {
		got := dockerfileDisplayName(tc.path)
		if got != tc.want {
			t.Errorf("dockerfileDisplayName(%q) = %q; want %q", tc.path, got, tc.want)
		}
	}
}

func TestDockerfileObservationUsesPathDerivedName(t *testing.T) {
	// End-to-end: a Dockerfile at "services/api/Dockerfile" should produce a
	// definition whose Name is "api", not "default".
	body := "FROM golang:1.26 AS build\nFROM scratch\n"
	r := observeOne(t, "services/api/Dockerfile", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("expected one definition, got %d: %+v", len(r.Definitions), r.Definitions)
	}
	if r.Definitions[0].Name != "api" {
		t.Errorf("Dockerfile name = %q; want %q", r.Definitions[0].Name, "api")
	}
}

func TestRootDockerfileObservationUsesRootSentinel(t *testing.T) {
	body := "FROM node:22\n"
	r := observeOne(t, "Dockerfile", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("expected one definition, got %d: %+v", len(r.Definitions), r.Definitions)
	}
	if r.Definitions[0].Name != "(root)" {
		t.Errorf("root Dockerfile name = %q; want \"(root)\"", r.Definitions[0].Name)
	}
}

// Maven WAR/EAR packaging tests.

func TestMavenWARWithFinalName(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <groupId>org.owasp</groupId>
    <artifactId>benchmark</artifactId>
    <version>1.2</version>
    <packaging>war</packaging>
    <build>
        <finalName>benchmark</finalName>
    </build>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d: %+v", len(r.Definitions), r.Definitions)
	}
	d := r.Definitions[0]
	if d.Kind != "archive" || d.Provider != "maven" {
		t.Errorf("kind=%q provider=%q; want archive/maven", d.Kind, d.Provider)
	}
	if d.Format != "war" {
		t.Errorf("format=%q; want war", d.Format)
	}
	if d.Name != "benchmark.war" {
		t.Errorf("name=%q; want benchmark.war", d.Name)
	}
	if d.Coverage != "complete" {
		t.Errorf("coverage=%q; want complete", d.Coverage)
	}
	// Evidence must include packaging and finalName.
	fields := map[string]bool{}
	for _, ev := range d.Evidence {
		fields[ev.Field] = true
	}
	for _, want := range []string{"packaging", "finalName"} {
		if !fields[want] {
			t.Errorf("missing evidence field %q", want)
		}
	}
}

func TestDockerfileRecordsStaticCopySources(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM tomcat:10\nCOPY --chown=1000:1000 webapp/target/openmrs.war /usr/local/tomcat/webapps/ROOT.war\nCOPY ${ARTIFACT} /tmp/app.war\n")
	if len(r.Definitions) != 1 {
		t.Fatalf("want one Dockerfile definition, got %+v", r.Definitions)
	}
	var sources []Reference
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" {
			sources = append(sources, ref)
		}
	}
	if len(sources) != 1 || sources[0].Value != "webapp/target/openmrs.war" || sources[0].Evidence.Line != 2 {
		t.Fatalf("static source evidence mismatch: %+v", sources)
	}
}

func TestNestedDockerfileMarksBuildContextAsUnknown(t *testing.T) {
	r := observeOne(t, "services/api/Dockerfile", "FROM tomcat:10\nCOPY webapp/target/app.war /app/app.war\n")
	if len(r.Definitions) != 1 || !r.Definitions[0].DockerContextUnknown {
		t.Fatalf("nested Dockerfile should retain context ambiguity: %+v", r.Definitions)
	}
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" && ref.Value != "webapp/target/app.war" {
			t.Fatalf("COPY source must remain raw and context-relative: %+v", ref)
		}
	}
}

func TestRootDockerfileBuildContextIsNotMarkedUnknown(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM tomcat:10\nCOPY webapp/target/app.war /app/app.war\n")
	if len(r.Definitions) != 1 || r.Definitions[0].DockerContextUnknown {
		t.Fatalf("root Dockerfile context should retain existing default: %+v", r.Definitions)
	}
}

func TestDockerfileRecordsCopyFromStageArtifactSource(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS dev\nCOPY --from=dev /openmrs/distribution/openmrs_core/openmrs.war /usr/local/tomcat/webapps/openmrs.war\n")
	if len(r.Definitions) != 1 {
		t.Fatalf("want one Dockerfile definition, got %+v", r.Definitions)
	}
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source_stage" {
			if ref.Value != "/openmrs/distribution/openmrs_core/openmrs.war" || ref.Qualification != "local" || ref.Evidence.Field != "COPY --from source" || ref.Evidence.Line != 2 {
				t.Fatalf("unexpected stage-copy source: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing source path copied from declared stage")
}

func TestDockerfileCopyFromNumericIndexResolvesNamedStage(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\nFROM alpine:3.20 AS runtime\nCOPY --from=0 /app/target/app.war /app/app.war\n")
	if len(r.Definitions) != 1 {
		t.Fatalf("want one Dockerfile definition, got %+v", r.Definitions)
	}
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source_stage" {
			if ref.Value != "/app/target/app.war" || ref.Qualification != "local" || ref.SourceStage != "build" || ref.Stage != "runtime" {
				t.Fatalf("numeric stage reference was not resolved: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing source path copied from numeric stage index 0")
}

func TestDockerfileCopyFromAllowsFlagsBeforeFrom(t *testing.T) {
	for _, instruction := range []string{
		"COPY --chown=1000:1000 --from=build /app/target/app.war /app/app.war",
		"COPY --from=build --chown=1000:1000 /app/target/app.war /app/app.war",
		"COPY --link --from=build /app/target/app.war /app/app.war",
		"COPY --from=build --link /app/target/app.war /app/app.war",
	} {
		t.Run(instruction, func(t *testing.T) {
			r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\n"+instruction+"\n")
			var stageSource, localSource bool
			for _, ref := range r.Definitions[0].References {
				if ref.Kind == "copy_source_stage" && ref.Value == "/app/target/app.war" && ref.Qualification == "local" {
					stageSource = true
				}
				if ref.Kind == "copy_source" && ref.Value == "/app/target/app.war" {
					localSource = true
				}
			}
			if !stageSource || localSource {
				t.Fatalf("expected stage source only, stage=%v local=%v refs=%+v", stageSource, localSource, r.Definitions[0].References)
			}
		})
	}
}

func TestDockerfileBroadContextCopyRemainsPublicEvidence(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\nCOPY . .\nCOPY --from=build /app/target/app.war /app/app.war\n")
	found := false
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" && ref.Value == "." {
			found = true
		}
	}
	if !found {
		t.Fatal("COPY . . should remain public build-context evidence")
	}
}

func TestDockerfileRetainsContextDestinationAndWriteBarriersPrivately(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\nWORKDIR /workspace\nCOPY . .\nADD https://example.invalid/ROOT.war /workspace/webapp/target/ROOT.war\nRUN curl -o /workspace/webapp/target/ROOT.war https://example.invalid/ROOT.war\n")
	if len(r.Definitions) != 1 {
		t.Fatalf("want one Dockerfile definition, got %+v", r.Definitions)
	}
	d := r.Definitions[0]
	foundContext := false
	for _, ref := range d.References {
		if ref.Kind == "copy_source" && ref.Value == "." {
			foundContext = ref.TargetPath == "/workspace"
		}
	}
	if !foundContext {
		t.Fatalf("COPY destination was not retained: %+v", d.References)
	}
	foundAdd, foundRun := false, false
	for _, ref := range d.DockerPathWrites {
		foundAdd = foundAdd || ref.Kind == "add_source" && ref.Evidence.Line == 4
		foundRun = foundRun || ref.Kind == "run_instruction" && strings.Contains(ref.Value, "curl") && ref.Evidence.Line == 5
	}
	if !foundAdd || !foundRun {
		t.Fatalf("missing private overwrite barriers: %+v", d.DockerPathWrites)
	}
}

func TestDockerfileRetainsBoundedRunCopyPathsForStageAttribution(t *testing.T) {
	r := observeOne(t, "Dockerfile", `FROM maven:3.9 AS dev
RUN mkdir -p /openmrs/distribution/openmrs_core/ \
    && cp -a /openmrs_core/webapp/target/openmrs.war /openmrs/distribution/openmrs_core/openmrs.war \
    && rm -rf /tmp/build
`)
	if len(r.Definitions[0].DockerPathCopies) != 1 {
		t.Fatalf("expected one bounded RUN cp path observation: %+v", r.Definitions[0].DockerPathCopies)
	}
	ref := r.Definitions[0].DockerPathCopies[0]
	if ref.Stage != "dev" || ref.SourcePath != "/openmrs_core/webapp/target/openmrs.war" || ref.TargetPath != "/openmrs/distribution/openmrs_core/openmrs.war" || ref.Evidence.Line != 3 {
		t.Fatalf("unexpected RUN cp observation: %+v", ref)
	}
}

func TestDockerfileContextCopyTargetTracksAbsoluteWorkdir(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\nWORKDIR /openmrs_core\nCOPY . .\n")
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" && ref.Value == "." {
			if ref.TargetPath != "/openmrs_core" || ref.Evidence.Field != "COPY source" || ref.Evidence.Line != 3 {
				t.Fatalf("unexpected resolved COPY target: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing COPY source evidence")
}

func TestDockerfileContextCopyTargetTracksRelativeWorkdir(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM alpine:3.20\nWORKDIR /app\nWORKDIR build\nCOPY . output\n")
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" && ref.Value == "." {
			if ref.TargetPath != "/app/build/output" {
				t.Fatalf("relative WORKDIR destination resolved incorrectly: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing COPY source evidence")
}

func TestDockerfileCopyTargetRetainsDirectoryDestination(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM alpine:3.20\nCOPY webapp/target/ROOT.war /workspace/\n")
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" {
			if ref.TargetPath != "/workspace/" {
				t.Fatalf("COPY directory destination lost its trailing slash: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing COPY source")
}

func TestDockerfileCopyTargetRemainsUnknownForDynamicOrInheritedWorkdir(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM alpine:3.20\nWORKDIR /known\nFROM ${BASE}\nCOPY . relative\nWORKDIR ${APP_DIR}\nCOPY . .\nCOPY . /absolute\n")
	var relative, afterDynamic, absolute string
	for _, ref := range r.Definitions[0].References {
		if ref.Kind != "copy_source" || ref.Value != "." {
			continue
		}
		switch ref.Evidence.Line {
		case 4:
			relative = ref.TargetPath
		case 6:
			afterDynamic = ref.TargetPath
		case 7:
			absolute = ref.TargetPath
		}
	}
	if relative != "" || afterDynamic != "" || absolute != "/absolute" {
		t.Fatalf("unexpected target paths: relative=%q afterDynamic=%q absolute=%q", relative, afterDynamic, absolute)
	}
}

func TestDockerfileUnsupportedWorkdirDoesNotReusePreviousDestination(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM alpine:3.20\nWORKDIR /known\nWORKDIR $APP_DIR\nCOPY . relative\nWORKDIR \"/quoted path\"\nCOPY . another\n")
	for _, ref := range r.Definitions[0].References {
		if ref.Kind == "copy_source" && ref.TargetPath != "" {
			t.Fatalf("unsupported WORKDIR reused a stale destination: %+v", ref)
		}
	}
}

func TestDockerfileCommentedOrEchoedRunCopyDoesNotCreatePathEvidence(t *testing.T) {
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS dev\n# RUN cp -a /webapp/target/app.war /download/app.war\nRUN echo cp -a /webapp/target/app.war /download/app.war\n")
	if len(r.Definitions[0].DockerPathCopies) != 0 {
		t.Fatalf("commented or echoed cp should not create path evidence: %+v", r.Definitions[0].DockerPathCopies)
	}
}

func TestDockerfileMultiSourceRunCopyDoesNotInventAPathTransfer(t *testing.T) {
	for _, command := range []string{
		"RUN cp /webapp/target/ROOT.war /ROOT.war /tmp/output/",
		"RUN true && cp -a /webapp/target/ROOT.war /ROOT.war /tmp/output/",
		"RUN cp /workspace/webapp/target/ROOT.war /workspace/ROOT.war /tmp/extra.war",
		"RUN cp -t /usr/local/tomcat/webapps/ /tmp/foreign/ROOT.war",
		"RUN cp --target-directory=/usr/local/tomcat/webapps/ /tmp/foreign/ROOT.war",
	} {
		r := observeOne(t, "Dockerfile", "FROM alpine AS build\n"+command+"\n")
		if len(r.Definitions) != 1 || len(r.Definitions[0].DockerPathCopies) != 0 {
			t.Fatalf("multi-source cp must not imply first-to-second transfer: %q: %+v", command, r.Definitions)
		}
	}
}

func TestDockerfileOverlongRunWriteIsOpaque(t *testing.T) {
	line := "RUN mvn clean install " + strings.Repeat("x", DefaultStringBytes) + " && curl -o /workspace/webapp/target/ROOT.war https://example.invalid/ROOT.war"
	r := observeOne(t, "Dockerfile", "FROM maven:3.9 AS build\nWORKDIR /workspace\nCOPY . .\n"+line+"\n")
	for _, ref := range r.Definitions[0].DockerPathWrites {
		if strings.HasPrefix(ref.Evidence.Value, "RUN ") && ref.Evidence.Line == 4 {
			if ref.Kind != "run_instruction_opaque" {
				t.Fatalf("overlong RUN was retained as classifiable: %+v", ref)
			}
			return
		}
	}
	t.Fatal("missing overlong RUN write marker")
}

func TestMavenWARDefaultName(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <groupId>com.example</groupId>
    <artifactId>myapp</artifactId>
    <version>2.0.0</version>
    <packaging>war</packaging>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	d := r.Definitions[0]
	if d.Name != "myapp-2.0.0.war" {
		t.Errorf("name=%q; want myapp-2.0.0.war", d.Name)
	}
}

func TestMavenEAR(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <artifactId>enterprise-app</artifactId>
    <version>1.0</version>
    <packaging>ear</packaging>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	d := r.Definitions[0]
	if d.Kind != "archive" || !strings.HasSuffix(d.Name, ".ear") {
		t.Errorf("kind=%q name=%q; want archive/*ear", d.Kind, d.Name)
	}
	if d.Format != "ear" {
		t.Errorf("format=%q; want ear", d.Format)
	}
}

func TestMavenJARIsNotDeployable(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <artifactId>lib</artifactId>
    <version>1.0</version>
    <packaging>jar</packaging>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 0 {
		t.Errorf("jar packaging should produce no deployable, got %d definitions", len(r.Definitions))
	}
}

func TestMavenWARNoPackagingElementIsNotDeployable(t *testing.T) {
	// default Maven packaging is jar, so no <packaging> means no deployable.
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <artifactId>lib</artifactId>
    <version>1.0</version>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 0 {
		t.Errorf("no packaging element should produce no deployable, got %d definitions", len(r.Definitions))
	}
}

func TestMavenWARUnresolvedPropertyIsPartial(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <artifactId>myapp</artifactId>
    <version>${revision}</version>
    <packaging>war</packaging>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	d := r.Definitions[0]
	if d.Coverage != "qualified" {
		t.Errorf("coverage=%q; want qualified for unresolved property", d.Coverage)
	}
}

func TestMavenWARNestedInSubdirectory(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
    <artifactId>webapp</artifactId>
    <version>1.0</version>
    <packaging>war</packaging>
</project>
`
	r := observeOne(t, "modules/webapp/pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	if r.Definitions[0].Name != "webapp-1.0.war" {
		t.Errorf("name=%q; want webapp-1.0.war", r.Definitions[0].Name)
	}
}

func TestMavenWARIgnoresParentCoordinatesAtAnyIndentation(t *testing.T) {
	body := "<project xmlns=\"http://maven.apache.org/POM/4.0.0\">\n" +
		"\t<parent>\n\t\t<artifactId>platform-parent</artifactId>\n\t\t<version>9.9</version>\n\t</parent>\n" +
		"\t<artifactId>portal</artifactId>\n\t<packaging>war</packaging>\n" +
		"\t<dependencies>\n\t\t<dependency>\n\t\t\t<artifactId>lib</artifactId>\n\t\t\t<version>1.0</version>\n\t\t</dependency>\n\t</dependencies>\n" +
		"</project>\n"
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	d := r.Definitions[0]
	// The version is inherited from <parent>, as Maven does.
	if d.Name != "portal-9.9.war" || d.Coverage != "complete" {
		t.Errorf("name=%q coverage=%q; want portal-9.9.war complete", d.Name, d.Coverage)
	}
}

func TestMavenWARResolvesLocalProperties(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <artifactId>web</artifactId>
  <version>2.1</version>
  <packaging>war</packaging>
  <properties>
    <webapp.name>clinic</webapp.name>
  </properties>
  <build>
    <finalName>${webapp.name}-${project.version}</finalName>
  </build>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 1 {
		t.Fatalf("want 1 definition, got %d", len(r.Definitions))
	}
	if d := r.Definitions[0]; d.Name != "clinic-2.1.war" || d.Coverage != "complete" {
		t.Errorf("name=%q coverage=%q; want clinic-2.1.war complete", d.Name, d.Coverage)
	}
}

func TestMavenWARProfilePackagingIsIgnored(t *testing.T) {
	body := `<project xmlns="http://maven.apache.org/POM/4.0.0">
  <artifactId>lib</artifactId>
  <version>1.0</version>
  <profiles><profile><id>x</id><packaging>war</packaging></profile></profiles>
</project>
`
	r := observeOne(t, "pom.xml", body)
	if len(r.Definitions) != 0 {
		t.Errorf("profile-level packaging must not create a deployable, got %+v", r.Definitions)
	}
}

func TestMavenWARSingleByteEncodingMatchesComponentParser(t *testing.T) {
	body := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><artifactId>caf\xe9</artifactId><version>1</version><packaging>war</packaging></project>")
	r := observeOne(t, "pom.xml", string(body))
	if len(r.Definitions) != 1 || r.Definitions[0].Name != "café-1.war" {
		t.Errorf("ISO-8859-1 WAR was not parsed consistently with Maven components: %+v", r.Definitions)
	}
}
