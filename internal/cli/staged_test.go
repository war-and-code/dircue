package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/profile"
	"dircue/pkg/projects"
)

func stagedFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func stagedReport(t *testing.T, root string, flags ...string) (profile.Report, string) {
	t.Helper()
	args := append([]string{"analyze", "all", "--projects", "--source", "directory", "--json"}, flags...)
	stdout, stderr, err := invoke(append(args, root)...)
	if err != nil {
		t.Fatalf("first pass failed: %v\n%s", err, stderr)
	}
	var report profile.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("invalid first-pass JSON: %v\n%s", err, stdout)
	}
	if report.Projects == nil || report.SchemaVersion != "1.2.0" || report.Projects.Source != "directory" {
		t.Fatalf("missing directory project inventory: %s", stdout)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"metrics", "structure"} {
		if _, present := fields[name]; present {
			t.Fatalf("first pass unexpectedly enabled %s: %s", name, stdout)
		}
	}
	return report, stderr
}

func stagedRoles(report profile.Report) map[string]projects.Counts {
	roles := make(map[string]projects.Counts)
	for _, role := range report.Projects.Composition {
		roles[role.Name] = role.Counts
	}
	return roles
}

func TestStagedFirstPassDistinguishesDataFromUnclassifiedArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		roles map[string]projects.Counts
	}{
		{
			name:  "XML data",
			files: map[string]string{"events.xml": "<events><event>example</event></events>\n"},
			roles: map[string]projects.Counts{"data": {Files: 1, Bytes: 40}},
		},
		{
			name: "binary and unknown content",
			files: map[string]string{
				"library.dll":                "MZ\x00\x00fixture bytes",
				"content.dircue_unknown_ext": "opaque inventory record\n",
			},
			roles: map[string]projects.Counts{"binary": {Files: 1, Bytes: 17}, "unknown": {Files: 1, Bytes: 24}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := stagedFixture(t, tc.files)
			report, stderr := stagedReport(t, root)
			if stderr != "" || len(report.Warnings) != 0 || report.Projects.Status != "complete" {
				t.Fatalf("unexpected incomplete inspection: %+v, stderr %q", report, stderr)
			}
			if len(report.Languages) != 0 || len(report.Projects.Projects) != 0 || report.Summary.ScannedFiles != int64(len(tc.files)) {
				t.Fatalf("unexpected language or project evidence: %+v", report)
			}
			roles := stagedRoles(report)
			if len(roles) != len(tc.roles) {
				t.Fatalf("composition = %v; want %v", roles, tc.roles)
			}
			for role, want := range tc.roles {
				if roles[role] != want {
					t.Fatalf("%s = %+v; want %+v", role, roles[role], want)
				}
			}
			// Legacy empty language output intentionally carries none of this evidence.
			stdout, stderr, err := invoke("--json", "--source", "directory", root)
			if err != nil || stdout != "{}\n" || stderr != "" {
				t.Fatalf("legacy contract: stdout %q, stderr %q, error %v", stdout, stderr, err)
			}
		})
	}
}

func TestStagedFirstPassKeepsSmallProjectsBesideLargeXML(t *testing.T) {
	// Genuine XML keeps this portable test small; multi-GiB inputs belong to the
	// release benchmark. Disk share must never control manifest admission.
	xml := "<events>" + strings.Repeat(" ", 8<<20) + "</events>\n"
	root := stagedFixture(t, map[string]string{
		"events.xml":        xml,
		"java/pom.xml":      `<project><modelVersion>4.0.0</modelVersion><groupId>example</groupId><artifactId>app</artifactId><version>1</version></project>`,
		"dotnet/App.csproj": `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
	})
	report, stderr := stagedReport(t, root)
	if stderr != "" || len(report.Warnings) != 0 || report.Projects.Status != "complete" {
		t.Fatalf("unexpected incomplete inspection: %+v, stderr %q", report, stderr)
	}
	if len(report.Languages) != 0 || len(report.Projects.Projects) != 2 {
		t.Fatalf("manifest evidence lost behind data-only language breakdown: %+v", report)
	}
	ids := make(map[string]bool)
	for _, project := range report.Projects.Projects {
		ids[project.ID] = true
	}
	if !ids["java/pom.xml"] || !ids["dotnet/App.csproj"] {
		t.Fatalf("project IDs: %v", ids)
	}
	roles := stagedRoles(report)
	if roles["data"] != (projects.Counts{Files: 1, Bytes: int64(len(xml))}) || roles["configuration"].Files != 2 {
		t.Fatalf("data and XML manifests were conflated: %v", roles)
	}
	ecosystems := make(map[string]string)
	for _, finding := range report.Ecosystems {
		ecosystems[finding.Name] = finding.Root
	}
	if ecosystems["maven"] != "java" || ecosystems["nuget"] != "dotnet" {
		t.Fatalf("missing project-root ecosystem evidence: %+v", report.Ecosystems)
	}
}

func TestStagedFirstPassReportsLimitsWithoutChangingSuccessContract(t *testing.T) {
	for _, tc := range []struct {
		name, path, content, flag, status, warning, diagnostic string
	}{
		{"tree omitted", "events.xml", "<events/>", "--tree-size=1", "skipped", "tree_size_limit", "tree_size_limit"},
		{"manifest omitted", "App.csproj", "<Project>" + strings.Repeat(" ", 64) + "</Project>", "--max-file-bytes=32", "partial", "file_too_large", "manifest_too_large"},
		{"data unclassified", "events.xml", "<events>" + strings.Repeat(" ", 64) + "</events>", "--max-file-bytes=32", "complete", "file_too_large", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := stagedFixture(t, map[string]string{tc.path: tc.content})
			report, stderr := stagedReport(t, root, tc.flag)
			if report.Projects.Status != tc.status || len(report.Warnings) != 1 || report.Warnings[0].Code != tc.warning || !strings.Contains(stderr, tc.warning) {
				t.Fatalf("missing limit evidence: %+v, stderr %q", report, stderr)
			}
			if tc.diagnostic != "" && (len(report.Projects.Diagnostics) != 1 || report.Projects.Diagnostics[0].Code != tc.diagnostic) {
				t.Fatalf("missing project diagnostic: %+v", report.Projects)
			}
			if tc.status != "skipped" && stagedRoles(report)["unknown"].Files != 1 {
				t.Fatalf("unread file must remain unknown: %+v", report.Projects.Composition)
			}
			if len(report.Languages) != 0 || len(report.Projects.Projects) != 0 {
				t.Fatalf("limited inspection unexpectedly produced projects or languages: %+v", report)
			}
		})
	}
	t.Run("missing root is an error", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		stdout, _, err := invoke("analyze", "all", "--projects", "--source", "directory", "--json", root)
		if err == nil || stdout != "" {
			t.Fatalf("invalid root emitted successful report: stdout %q, error %v", stdout, err)
		}
	})
}
