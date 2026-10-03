package deployables

import (
	"reflect"
	"strings"
	"testing"
)

func FuzzCandidateParsersDeterministicAndBounded(f *testing.F) {
	seeds := []struct {
		name string
		body string
	}{
		{"Dockerfile", "FROM golang:1.25 AS build\nCOPY --from=build /out /app\n"},
		{"main.tf", `resource "aws_s3_bucket" "assets" {}`},
		{"Jenkinsfile", "pipeline {\n  stages {}\n}\n"},
		{"compose.yaml", "services:\n  api:\n    build: ./api\n    image: example/api:1\n"},
		{".github/workflows/ci.yml", "name: CI\non: push\njobs:\n  test:\n    steps:\n      - uses: actions/checkout@v4\n"},
		{".gitlab-ci.yml", "test:\n  script: go test ./...\n"},
		{"Chart.yaml", "apiVersion: v2\nname: sample\nversion: 1.0.0\n"},
		{"serverless.yaml", "service: sample\nprovider:\n  name: aws\nfunctions:\n  api:\n    handler: app.run\n"},
		{"deployment.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n"},
		{"template.yaml", "AWSTemplateFormatVersion: '2010-09-09'\nResources:\n  Bucket:\n    Type: AWS::S3::Bucket\n"},
		{"Procfile", "web: gunicorn api.app:app -b 0.0.0.0:$PORT\n"},
		{"Procfile", "# comment-only Procfile\n\n"},
		{"Host.AppHost/Program.cs", "var builder = DistributedApplication.CreateBuilder(args);\nbuilder.AddProject<Projects.Service>(\"service\");\n"},
		{"Host.AppHost/Program.cs", "var builder = DistributedApplication.CreateBuilder(args);\nvar ignored = $$\"\"\"{{ builder.AddProject<Projects.Fake>(\"fake\") }}\"\"\";\nbuilder.AddProject<Projects.Real>(\"real\");\n"},
	}
	for _, seed := range seeds {
		f.Add(seed.name, []byte(seed.body))
	}
	f.Fuzz(func(t *testing.T, name string, content []byte) {
		if len(name) > 1024 || len(content) > int(DefaultFileBytes) {
			return
		}
		first, firstRecognized, firstErr := parse(name, content)
		second, secondRecognized, secondErr := parse(name, content)
		if (firstErr == nil) != (secondErr == nil) || firstRecognized != secondRecognized || !reflect.DeepEqual(first, second) {
			t.Fatalf("parser is nondeterministic: recognized=%t/%t err=%v/%v", firstRecognized, secondRecognized, firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if firstRecognized != (len(first) > 0) {
			t.Fatalf("recognized=%t definitions=%d", firstRecognized, len(first))
		}
		lines := 1 + strings.Count(string(content), "\n")
		for _, definition := range first {
			if definition.Kind == "" || definition.Provider == "" || definition.Name == "" || len(definition.Name) > DefaultStringBytes {
				t.Fatalf("invalid or unbounded definition: %+v", definition)
			}
			for _, evidence := range definition.Evidence {
				assertFuzzEvidenceLine(t, evidence, lines)
			}
			for _, reference := range definition.References {
				if len(reference.Value) > DefaultStringBytes {
					t.Fatalf("unbounded reference: %d bytes", len(reference.Value))
				}
				switch reference.Qualification {
				case "local", "external", "unresolved", "declared":
				default:
					t.Fatalf("invalid reference qualification %q", reference.Qualification)
				}
				assertFuzzEvidenceLine(t, reference.Evidence, lines)
			}
		}
	})
}

func assertFuzzEvidenceLine(t *testing.T, evidence Evidence, lines int) {
	t.Helper()
	if evidence.Line < 0 || evidence.Line > lines {
		t.Fatalf("evidence line %d outside 0..%d: %+v", evidence.Line, lines, evidence)
	}
}
