package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/rules"
)

func TestRulesCLIExplicitAndAdditive(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"main.go": "package main\n", "build.gradle.kts": "plugins { id(\"custom.build\") }\n", "rules.json": "malformed repository hint", "notes.txt": "ordinary text"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	policy := []byte(`{"schema_version":"1.0.0","rules":[{"id":"go-name","match":{"extensions":[".go"]}},{"id":"build-convention","match":{"filenames":["build.gradle.kts"]},"content":{"contains_utf8":"custom.build"}}]}`)
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(policyPath, policy, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) profile.Report {
		t.Helper()
		var out, stderr bytes.Buffer
		args = append(args, "--json", "--source", "directory", root)
		if err := Execute(context.Background(), args, &out, &stderr); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, stderr.String())
		}
		var report profile.Report
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	baseline := run("analyze", "all")
	if baseline.Rules != nil || baseline.SchemaVersion != profile.SchemaVersion {
		t.Fatal("repository file enabled rules implicitly")
	}
	separate := run("analyze", "rules", "--rules-file", policyPath)
	withDiscovery := run("analyze", "rules", "--discovery", "--rules-file", policyPath)
	discovery := run("analyze", "discovery")
	combined := run("analyze", "all", "--rules-file", policyPath)
	digest := sha256.Sum256(policy)
	if separate.Rules == nil || separate.Rules.TotalMatches != 2 || separate.Rules.Status != "complete" || separate.Rules.RulesSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected report: %+v", separate.Rules)
	}
	if separate.SchemaVersion != profile.EnhancedSchemaVersion || len(separate.Languages) != 0 || separate.Structure != nil || separate.Projects != nil || separate.Metrics != nil {
		t.Fatal("rules enabled unrelated analysis")
	}
	if !bytes.Equal(mustJSON(t, separate.Rules), mustJSON(t, combined.Rules)) {
		t.Fatal("combined rules changed evidence")
	}
	if !bytes.Equal(mustJSON(t, withDiscovery.Discovery), mustJSON(t, discovery.Discovery)) {
		t.Fatal("adding rules changed metadata discovery")
	}
	withDiscovery.Discovery = nil
	if !bytes.Equal(mustJSON(t, withDiscovery), mustJSON(t, separate)) {
		t.Fatal("adding discovery changed rules or enabled unrelated analysis")
	}
	combined.Rules = nil
	combined.SchemaVersion = baseline.SchemaVersion
	if !bytes.Equal(mustJSON(t, baseline), mustJSON(t, combined)) {
		t.Fatal("rules changed existing analysis")
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "rules", "--rules-file", policyPath, root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Ruleset SHA-256:") || !strings.Contains(out.String(), "status: complete") {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), "custom.build") {
		t.Fatal("output exposed policy content")
	}
	out.Reset()
	if err := Execute(context.Background(), []string{"analyze", "rules", "--discovery", "--rules-file", policyPath, root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Metadata discovery;") || !strings.Contains(out.String(), "Observation rules:") || strings.Contains(out.String(), "Languages:") {
		t.Fatal("combined text output must describe both selected modules:", out.String())
	}
}

func TestRulesCLIRejectsInvalidPolicyBeforeScan(t *testing.T) {
	root := t.TempDir()
	invalid := filepath.Join(root, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"schema_version":"1.0.0","rules":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(root, "large.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte(" "), rules.MaxConfigBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"analyze", "rules"},
		{"analyze", "rules", "--rules-file="},
		{"analyze", "all", "--rules-file="},
		{"analyze", "rules", "--rules-file", invalid},
		{"analyze", "rules", "--rules-file", oversized},
		{"analyze", "rules", "--rules-file", root},
		{"analyze", "languages", "--rules-file", invalid},
	} {
		var out, stderr bytes.Buffer
		args = append(args, filepath.Join(root, "missing-target"))
		err := Execute(context.Background(), args, &out, &stderr)
		if err == nil || out.Len() != 0 {
			t.Fatalf("%v: error=%v stdout=%q", args, err, out.String())
		}
		if strings.Contains(err.Error(), "Syft") {
			t.Fatalf("rules error names unrelated module: %v", err)
		}
		if strings.Contains(err.Error(), "missing-target") {
			t.Fatalf("policy error should precede scan: %v", err)
		}
	}
}
