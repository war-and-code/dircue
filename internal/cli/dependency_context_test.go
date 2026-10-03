package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/schema"
)

func TestEnvironmentToolchainDeclarationsCLIHelpJSONAndText(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		".python-version":             "3.12.4\n",
		".nvmrc":                      "lts/*\n",
		"service/rust-toolchain.toml": "[toolchain]\nchannel = \"stable\"\n",
		"service/App.csproj":          `<Project Sdk="Microsoft.NET.Sdk"/>`,
	} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	help, stderr, err := invoke("analyze", "environments", "--help")
	if err != nil || stderr != "" || !strings.Contains(help, "Python, Node, and Rust toolchain declarations from the selected source") {
		t.Fatalf("environment help output=%q stderr=%q err=%v", help, stderr, err)
	}

	output, stderr, err := invoke("analyze", "environments", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("JSON command output=%q stderr=%q err=%v", output, stderr, err)
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(output), &profile); err != nil {
		t.Fatalf("decode profile: %v\n%s", err, output)
	}
	if err := schema.ValidateProfile(profile); err != nil {
		t.Fatalf("profile schema rejected environment toolchains: %v", err)
	}
	environmentData, ok := profile["environments"]
	if !ok {
		t.Fatal("JSON profile omitted environments")
	}
	encoded, err := json.Marshal(environmentData)
	if err != nil {
		t.Fatal(err)
	}
	var report environments.Report
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if err := environments.ValidateReport(&report); err != nil {
		t.Fatalf("standalone environment schema rejected report: %v", err)
	}
	if len(report.ToolchainDeclarations) != 3 {
		t.Fatalf("JSON toolchain declarations: %+v", report.ToolchainDeclarations)
	}
	textOutput, stderr, err := invoke("analyze", "environments", root)
	if err != nil || stderr != "" {
		t.Fatalf("text command output=%q stderr=%q err=%v", textOutput, stderr, err)
	}
	for _, fact := range []string{".python-version", "3.12.4", ".nvmrc", "lts/*", "service/rust-toolchain.toml", "stable", "scope \"service\""} {
		if !strings.Contains(textOutput, fact) {
			t.Errorf("text output omitted %q:\n%s", fact, textOutput)
		}
	}
}
