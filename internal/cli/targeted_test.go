package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func targetedExecute(t *testing.T, args ...string) (map[string]any, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), args, &out, &stderr); err != nil {
		t.Fatalf("%v: %v (%s)", args, err, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode %v: %v", args, err)
	}
	return report, out.String()
}
func TestTargetedFlagsRejectUnappliedChoices(t *testing.T) {
	for _, args := range [][]string{
		{"analyze", "focus"}, {"analyze", "focus", "--project", "app.csproj", "--affected-by", "Directory.Build.props"},
		{"analyze", "focus", "--affected-by", "Directory.Build.props", "--metrics"},
		{"analyze", "focus", "--project", "app.csproj", "--files"},
		{"analyze", "focus", "--project", "app.csproj", "--strategies"},
		{"analyze", "explain"}, {"analyze", "explain", "--file", "a.go", "--project", "go.mod"},
		{"analyze", "explain", "--file", "a.go", "--report", "unused.json", "."},
		{"analyze", "explain", "--file", "a.go", "--report", "unused.json", "--source", "directory"},
		{"analyze", "explain", "--file", "../escape.go"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			if err := Execute(context.Background(), args, &out, &stderr); err == nil {
				t.Fatal("accepted unsupported flags")
			}
			if out.Len() != 0 {
				t.Fatal("error emitted partial structured stdout")
			}
		})
	}
}
func TestFreshExplanationAndSavedEvidenceNeverRescan(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"file.go": "package example\n", ".gitattributes": "file.go linguist-language=Python linguist-detectable\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fresh, raw := targetedExecute(t, "analyze", "explain", "--file", "file.go", "--source", "directory", "--json", root)
	explanation := fresh["explanation"].(map[string]any)
	if explanation["decision"].(map[string]any)["reported_language"] != "Python" {
		t.Fatal("actual override absent")
	}
	saved := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(saved, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	retained, retainedRaw := targetedExecute(t, "analyze", "explain", "--file", "file.go", "--report", saved, "--json")
	e := retained["explanation"].(map[string]any)
	if e["scope"].(map[string]any)["evidence"] != "retained" || e["decision"].(map[string]any)["reported_language"] != "Python" {
		t.Fatal("retained trace changed")
	}
	if len(e["source"].(map[string]any)["report_sha256"].(string)) != 64 {
		t.Fatal("missing saved byte identity")
	}
	second := filepath.Join(t.TempDir(), "retained.json")
	if err := os.WriteFile(second, []byte(retainedRaw), 0600); err != nil {
		t.Fatal(err)
	}
	again, _ := targetedExecute(t, "analyze", "explain", "--file", "file.go", "--report", second, "--json")
	currentDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(retainedRaw)))
	if again["explanation"].(map[string]any)["source"].(map[string]any)["report_sha256"] != currentDigest {
		t.Fatal("re-explanation retained the previous input digest")
	}
	other, _ := targetedExecute(t, "analyze", "explain", "--file", "different.go", "--report", saved, "--json")
	if other["explanation"].(map[string]any)["decision"].(map[string]any)["status"] != "unavailable" {
		t.Fatal("different target inherited original decision")
	}
}
func TestLegacySavedLanguageMembershipHasUnknownSource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main(){}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, raw := targetedExecute(t, "analyze", "all", "--breakdown", "--json", root)
	saved := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(saved, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	report, _ := targetedExecute(t, "analyze", "explain", "--file", "main.go", "--report", saved, "--json")
	e := report["explanation"].(map[string]any)
	if e["source"].(map[string]any)["mode"] != "unknown" {
		t.Fatal("invented source identity")
	}
	if e["decision"].(map[string]any)["reported_language"] != "Go" {
		t.Fatal("retained inclusion unavailable")
	}
}
func TestFreshProjectExplanationDoesNotClaimCompilerResolution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.csproj"), []byte(`<Project><ItemGroup><ProjectReference Include="missing.csproj" /></ItemGroup></Project>`), 0600); err != nil {
		t.Fatal(err)
	}
	report, raw := targetedExecute(t, "analyze", "explain", "--project", "app.csproj", "--json", root)
	if report["explanation"].(map[string]any)["scope"].(map[string]any)["evidence"] != "fresh" {
		t.Fatal("fresh collection mislabeled")
	}
	if !strings.Contains(raw, "missing.csproj") || !strings.Contains(raw, `"state": "missing"`) {
		t.Fatal("missing relationship evidence")
	}
}
