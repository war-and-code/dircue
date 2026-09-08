package detectors

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dircue/pkg/profile"
)

func detectFile(t *testing.T, name, content string) []profile.Finding {
	t.Helper()
	file := profile.File{Path: name, Content: []byte(content), Size: int64(len(content))}
	var result []profile.Finding
	for _, detector := range Default() {
		findings, err := detector.Detect(context.Background(), file)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, findings...)
	}
	return result
}

func TestManifestDetectionWithoutLanguage(t *testing.T) {
	got := detectFile(t, "apps/web/package.json", `{"dependencies":{"next":"^15","react":"^19"},"devDependencies":{"react":"^19"}}`)
	want := []profile.Finding{
		finding("ecosystem", "npm", "apps/web", "apps/web/package.json", "manifests"),
		finding("layout", "project-root", "apps/web", "apps/web/package.json", "manifests"),
		finding("framework", "Next.js", "apps/web", "apps/web/package.json", "manifests"),
		finding("framework", "React", "apps/web", "apps/web/package.json", "manifests"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestMalformedJSONManifest(t *testing.T) {
	for _, content := range []string{`{`, `null`, `[]`, `{"dependencies":[]}`, `{"dependencies":null}`, `{"dependencies":{"react":123}}`, `{"dependencies":{"react":null}}`, `{} {}`} {
		t.Run(content, func(t *testing.T) {
			findings, err := (manifests{}).Detect(context.Background(), profile.File{Path: "package.json", Content: []byte(content), Size: int64(len(content))})
			if err == nil {
				t.Fatal("expected invalid manifest warning")
			}
			if len(findings) != 2 || findings[0].Kind != "ecosystem" || findings[1].Kind != "layout" {
				t.Fatalf("filename evidence should survive malformed content: %#v", findings)
			}
		})
	}
}

func TestTruncatedManifest(t *testing.T) {
	content := `{"dependencies":{"react":"^19"}}`
	findings, err := (manifests{}).Detect(context.Background(), profile.File{Path: "package.json", Content: []byte(content), Size: int64(len(content) + 4)})
	if err == nil || len(findings) != 2 {
		t.Fatalf("truncated manifests must not establish frameworks: %#v, %v", findings, err)
	}
}

func TestMalformedDependencyWarningIsDeterministic(t *testing.T) {
	content := []byte(`{"dependencies":{"z":null,"a":123}}`)
	var first string
	for i := 0; i < 30; i++ {
		_, err := (manifests{}).Detect(context.Background(), profile.File{Path: "package.json", Content: content})
		if err == nil {
			t.Fatal("expected invalid manifest warning")
		}
		if i == 0 {
			first = err.Error()
		} else if err.Error() != first {
			t.Fatalf("warning changed between calls: %q versus %q", first, err.Error())
		}
	}
}

func TestNoSpeculativeFrameworkMatches(t *testing.T) {
	for _, test := range []struct{ path, content string }{
		{"package.json", `{"description":"uses react next vue","scripts":{"start":"next start"},"dependencies":{"react-dom":"^19","expressive":"1"}}`},
		{"main.py", "from django import forms\nimport flask\n"},
		{"requirements.txt", "# django==4\n-r flask.txt\n-e https://example.com/fastapi\nflask-app==1\nFlask is awesome\n"},
	} {
		t.Run(test.path, func(t *testing.T) {
			for _, finding := range detectFile(t, test.path, test.content) {
				if finding.Kind == "framework" {
					t.Fatalf("unexpected speculative framework: %#v", finding)
				}
			}
		})
	}
}

func TestPythonRequirements(t *testing.T) {
	findings := detectFile(t, "api/requirements-dev.txt", "Django>=4; python_version > '3.10'\nFlask[async]==3.0 # server\nfastapi @ https://example.com/pkg.whl\nDjango==4\n")
	var names []string
	for _, finding := range findings {
		if finding.Root != "api" {
			t.Fatalf("wrong root: %#v", finding)
		}
		if finding.Kind == "framework" {
			names = append(names, finding.Name)
		}
	}
	if want := []string{"Django", "FastAPI", "Flask"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
}

func TestTruncatedRequirementLine(t *testing.T) {
	file := profile.File{Path: "requirements.txt", Content: []byte("django==4\nflask"), Size: 100}
	findings, err := (manifests{}).Detect(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 3 || findings[2].Name != "Django" {
		t.Fatalf("partial line must not establish Flask: %#v", findings)
	}
}

func TestLayoutDotfilesAndNestedRoots(t *testing.T) {
	for _, test := range []struct{ file, name, root string }{
		{".github/workflows/build.yml", "github-actions", "."},
		{"services/api/.github/workflows/test.yaml", "github-actions", "services/api"},
		{".gitlab-ci.yml", "gitlab-ci", "."},
		{"services/api/Dockerfile", "docker", "services/api"},
		{"compose.yaml", "docker-compose", "."},
		{"ci/Jenkinsfile", "jenkins", "ci"},
	} {
		t.Run(test.file, func(t *testing.T) {
			got := detectFile(t, test.file, "")
			want := []profile.Finding{finding("layout", test.name, test.root, test.file, "layout")}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
	for _, file := range []string{".github/workflows/README.md", ".github/workflows/subdir/x.yml", "src/workflows/x.yml", "Dockerfile-example"} {
		if got := detectFile(t, file, ""); len(got) != 0 {
			t.Fatalf("unexpected layout evidence from %s: %#v", file, got)
		}
	}
}

func TestEcosystemManifests(t *testing.T) {
	for file, ecosystem := range map[string]string{
		"go.mod": "go", "go.work": "go", "pnpm-lock.yaml": "npm", "pyproject.toml": "python", "uv.lock": "python",
		"Cargo.toml": "cargo", "pom.xml": "maven", "build.gradle.kts": "gradle", "Gemfile": "bundler",
		"composer.lock": "composer", "App.csproj": "nuget",
	} {
		t.Run(file, func(t *testing.T) {
			got := detectFile(t, file, "")
			if len(got) != 2 || got[0].Name != ecosystem || got[0].Root != "." {
				t.Fatalf("unexpected ecosystem: %#v", got)
			}
		})
	}
}

func TestJavaAndDotNetMonorepoManifestRoots(t *testing.T) {
	tests := []struct {
		path      string
		ecosystem string
		root      string
	}{
		{"platform/pom.xml", "maven", "platform"},
		{"platform/services/orders/pom.xml", "maven", "platform/services/orders"},
		{"platform/settings.gradle.kts", "gradle", "platform"},
		{"platform/services/billing/build.gradle", "gradle", "platform/services/billing"},
		{"dotnet/Directory.Packages.props", "nuget", "dotnet"},
		{"dotnet/src/App/App.csproj", "nuget", "dotnet/src/App"},
		{"dotnet/src/Legacy/Legacy.csproj", "nuget", "dotnet/src/Legacy"},
		{"dotnet/src/Shared/packages.config", "nuget", "dotnet/src/Shared"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			got := detectFile(t, test.path, "manifest content is passive evidence only")
			want := []profile.Finding{
				finding("ecosystem", test.ecosystem, test.root, test.path, "manifests"),
				finding("layout", "project-root", test.root, test.path, "manifests"),
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestComposerFramework(t *testing.T) {
	got := detectFile(t, "composer.json", `{"require":{"laravel/framework":"^12","php":"^8"}}`)
	if len(got) != 3 || got[2].Name != "Laravel" {
		t.Fatalf("unexpected Composer findings: %#v", got)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, detector := range Default() {
		findings, err := detector.Detect(ctx, profile.File{Path: "package.json", Content: []byte("{}")})
		if !errors.Is(err, context.Canceled) || len(findings) != 0 {
			t.Fatalf("%s did not respect cancellation: %#v, %v", detector.Name(), findings, err)
		}
	}
}

func TestConcurrentCalls(t *testing.T) {
	for i := 0; i < 16; i++ {
		t.Run("concurrent", func(t *testing.T) {
			t.Parallel()
			got := detectFile(t, "package.json", `{"dependencies":{"vue":"^3"}}`)
			if len(got) != 3 || got[2].Name != "Vue" {
				t.Fatalf("unexpected findings: %#v", got)
			}
		})
	}
}
