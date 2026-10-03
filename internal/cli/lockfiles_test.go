package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/schema"
)

const cliLockfileManifest = `{"name":"app","dependencies":{"left-pad":"1.0.0"}}`
const cliLockfileMatch = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}}}}`
const cliLockfileDifferent = `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"2.0.0"}}}}`

func writeCLILockfileFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func decodeLockfilesProfile(t *testing.T, output string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode profile: %v\n%s", err, output)
	}
	if err := schema.ValidateProfile(report); err != nil {
		t.Fatalf("profile schema rejected lockfile profile: %v", err)
	}
	return report
}

func TestLockfilesCLIReportsMissingMatchAndDifferentAssociations(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lock        string
		association string
		check       string
	}{
		{name: "missing", association: "missing", check: ""},
		{name: "match", lock: cliLockfileMatch, association: "observed", check: "match"},
		{name: "different", lock: cliLockfileDifferent, association: "observed", check: "different"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"app/package.json": cliLockfileManifest}
			if tc.lock != "" {
				files["app/package-lock.json"] = tc.lock
			}
			writeCLILockfileFixture(t, root, files)
			output, stderr, err := invoke("analyze", "lockfiles", "--source", "directory", "--json", root)
			if err != nil || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q err=%v", output, stderr, err)
			}
			report := decodeLockfilesProfile(t, output)
			if report["schema_version"] != "1.8.0" {
				t.Fatalf("schema_version=%v; want 1.8.0", report["schema_version"])
			}
			module, ok := report["lockfiles"].(map[string]any)
			if !ok {
				t.Fatalf("lockfiles field missing: %+v", report)
			}
			contexts, ok := module["contexts"].([]any)
			if !ok || len(contexts) != 1 {
				t.Fatalf("contexts=%v", module["contexts"])
			}
			context := contexts[0].(map[string]any)
			if context["association_state"] != tc.association {
				t.Fatalf("association=%v; want %s", context["association_state"], tc.association)
			}
			checks := context["checks"].([]any)
			if tc.check == "" {
				if len(checks) != 0 {
					t.Fatalf("missing lock produced a comparison: %v", checks)
				}
			} else if len(checks) != 1 || checks[0].(map[string]any)["status"] != tc.check {
				t.Fatalf("checks=%v; want status %s", checks, tc.check)
			}
		})
	}
}

func TestLockfilesAllFlagIsOptionalAndUsesStandaloneSchema(t *testing.T) {
	root := t.TempDir()
	writeCLILockfileFixture(t, root, map[string]string{"app/package.json": cliLockfileManifest, "app/package-lock.json": cliLockfileMatch})

	without, stderr, err := invoke("analyze", "all", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("default analyze all stdout=%q stderr=%q err=%v", without, stderr, err)
	}
	defaultProfile := decodeLockfilesProfile(t, without)
	if _, present := defaultProfile["lockfiles"]; present {
		t.Fatal("analyze all enabled lockfile analysis without its opt-in flag")
	}

	with, stderr, err := invoke("analyze", "all", "--lockfiles", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("opt-in analyze all stdout=%q stderr=%q err=%v", with, stderr, err)
	}
	profile := decodeLockfilesProfile(t, with)
	if profile["schema_version"] != "1.8.0" || profile["lockfiles"] == nil {
		t.Fatalf("opt-in profile lacks schema 1.8 lockfiles field: %+v", profile)
	}

	standalone, stderr, err := invoke("capabilities", "--schema", "lockfiles")
	if err != nil || stderr != "" {
		t.Fatalf("standalone schema stdout=%q stderr=%q err=%v", standalone, stderr, err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(standalone), &document); err != nil {
		t.Fatalf("decode standalone schema: %v", err)
	}
	if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" || !strings.Contains(standalone, "lockfiles.schema.json") {
		t.Fatalf("standalone lockfiles schema is not an exported Draft 2020-12 schema: %s", standalone)
	}
}

func TestLockfilesCLIAcceptsNPMShrinkwrapPrecedence(t *testing.T) {
	root := t.TempDir()
	writeCLILockfileFixture(t, root, map[string]string{
		"app/package.json":        cliLockfileManifest,
		"app/package-lock.json":   cliLockfileDifferent,
		"app/npm-shrinkwrap.json": cliLockfileMatch,
	})
	output, stderr, err := invoke("analyze", "lockfiles", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q err=%v", output, stderr, err)
	}
	profile := decodeLockfilesProfile(t, output)
	module := profile["lockfiles"].(map[string]any)
	context := module["contexts"].([]any)[0].(map[string]any)
	if context["lockfile_path"] != "app/npm-shrinkwrap.json" || context["association_state"] != "observed" {
		t.Fatalf("shrinkwrap did not win association: %+v", context)
	}
}
