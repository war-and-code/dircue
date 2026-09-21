package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/internal/cli"
	"dircue/pkg/reportdiff"
	"dircue/schema"
)

func targetedCLIReport(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
		t.Fatalf("%v: %v (%s)", args, err, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(v); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return v
}

func TestTargetedSchemaAndComparisonBoundary(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"app.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>", "App.cs": "class App {}\n", "blob.dat": "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 999999\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"analyze", "focus", "--project", "app.csproj", "--metrics", "--files", "--json", "--source", "directory", root},
		{"analyze", "focus", "--affected-by", "app.csproj", "--json", "--source", "directory", root},
		{"analyze", "availability", "--json", "--source", "directory", root},
		{"analyze", "all", "--availability", "--formats", "--declarations", "--json", "--source", "directory", root},
		{"analyze", "explain", "--file", "App.cs", "--json", "--source", "directory", root},
	} {
		t.Run(strings.Join(args[1:4], "-"), func(t *testing.T) {
			v := targetedCLIReport(t, args...)
			if v["schema_version"] != "1.6.0" {
				t.Fatal("new report did not declare schema 1.6")
			}
			data, _ := json.Marshal(v)
			if _, err := reportdiff.Load(bytes.NewReader(data)); !errors.Is(err, reportdiff.ErrUnsupported) {
				t.Fatalf("comparison accepted unsupported scope: %v", err)
			}
			if _, digest, err := reportdiff.ReadEvidence(bytes.NewReader(data)); err != nil || len(digest) != 64 {
				t.Fatalf("retained evidence load: %s %v", digest, err)
			}
			for _, old := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0"} {
				v["schema_version"] = old
				if schema.ValidateProfile(v) == nil {
					t.Fatalf("targeted fields accepted as %s", old)
				}
			}
		})
	}
}

func TestFocusSchemaRejectsMixedDenominatorsAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='app'\nversion='1'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := targetedCLIReport(t, "analyze", "focus", "--project", "pyproject.toml", "--metrics", "--json", root)
	focused := v["focused_metrics"].(map[string]any)
	v["metrics"] = focused["primary"]
	if schema.ValidateProfile(v) == nil {
		t.Fatal("focused and repository-wide metrics accepted together")
	}
	delete(v, "metrics")
	v["focus"].(map[string]any)["compiler_verified"] = true
	if schema.ValidateProfile(v) == nil {
		t.Fatal("undeclared assertion accepted")
	}
}

func TestRetainedFocusMetricsRequireMatchingPopulationAndSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='app'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := targetedCLIReport(t, "analyze", "focus", "--project", "pyproject.toml", "--metrics", "--json", root)
	raw, _ := json.Marshal(original)
	for _, mutation := range []string{"scope", "source", "related"} {
		t.Run(mutation, func(t *testing.T) {
			var value map[string]any
			json.Unmarshal(raw, &value)
			m := value["focused_metrics"].(map[string]any)
			switch mutation {
			case "scope":
				m["scope_id"] = strings.Repeat("b", 64)
			case "source":
				primary := m["primary"].(map[string]any)
				primary["source"] = "git"
				primary["tree"] = strings.Repeat("a", 40)
			case "related":
				m["related"] = []any{map[string]any{"project": "not-selected/pyproject.toml", "metrics": m["primary"]}}
			}
			data, _ := json.Marshal(value)
			if _, _, err := reportdiff.ReadEvidence(bytes.NewReader(data)); err == nil {
				t.Fatal("accepted conflicting metric population")
			}
		})
	}
}
