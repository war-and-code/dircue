package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectsCLIOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "App.csproj"), []byte(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"analyze", "projects", "--json", root}, {"analyze", "all", "--projects", "--json", root}, {"analyze", "all", "--projects", "--metrics", "--json", root}} {
		out, _, err := invoke(args...)
		if err != nil {
			t.Fatal(err)
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		if r["schema_version"] != "1.2.0" || r["projects"] == nil {
			t.Fatal(out)
		}
	}
	out, _, err := invoke("analyze", "all", "--json", root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"projects"`) {
		t.Fatal("projects changed existing all output")
	}
	out, _, err = invoke("analyze", "projects", root)
	if err != nil || !strings.Contains(out, "net8.0") {
		t.Fatalf("%s %v", out, err)
	}
}
func TestStructuralCLIRequiresExplicitOptIn(t *testing.T) {
	for _, args := range [][]string{{"analyze", "structure"}, {"analyze", "all", "--structural-worker", "missing"}, {"analyze", "all", "--projects", "--files"}, {"analyze", "structure", "--structural-timeout", "0s"}, {"analyze", "structure", "--structural-max-file-bytes", "0"}} {
		if _, _, err := invoke(args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
