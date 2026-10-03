package reportdiff

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/environments"
)

func environmentProfileJSON(t *testing.T) []byte {
	t.Helper()
	p := emptyProfile()
	p.SchemaVersion = "1.7.0"
	var err error
	p.Declarations, err = declarations.New("directory", "", 0).Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.Environments, err = environments.Analyze(context.Background(), environments.Input{Source: "directory", InventoryComplete: true, Declarations: *p.Declarations}, environments.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadEvidenceRejectsEnvironmentSemanticInconsistency(t *testing.T) {
	valid := environmentProfileJSON(t)
	if _, _, err := ReadEvidence(bytes.NewReader(valid)); err != nil {
		t.Fatalf("valid environment report: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(valid, &value); err != nil {
		t.Fatal(err)
	}
	env := value["environments"].(map[string]any)
	coverage := env["coverage"].(map[string]any)
	coverage["global_json_read"] = float64(1)
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReadEvidence(bytes.NewReader(b)); err == nil {
		t.Fatal("accepted impossible environment coverage")
	}
}

func TestLegacyEnvironmentProviderScopeMakesNewToolchainRowsUnavailable(t *testing.T) {
	decls, err := declarations.New("directory", "", 0).Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := environments.Analyze(context.Background(), environments.Input{Source: "directory", InventoryComplete: true, Declarations: *decls}, environments.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	legacy.ProviderVersion = environments.LegacyProviderVersion
	legacy.SemanticsReference = "https://learn.microsoft.com/en-us/dotnet/core/tools/global-json (last updated 2026-03-09; accessed 2026-09-21)"
	legacy.Limits.ToolchainFiles = 0
	legacy.Limits.ToolchainFileBytes = 0
	legacy.Limits.ToolchainInputBytes = 0
	if err := environments.ValidateReport(legacy); err != nil {
		t.Fatalf("legacy environment fixture invalid: %v", err)
	}
	baseProfile := emptyProfile()
	baseProfile.SchemaVersion = "1.7.0"
	baseProfile.Declarations, baseProfile.Environments = decls, legacy

	value := ".python-version"
	current, err := environments.Analyze(context.Background(), environments.Input{
		Source: "directory", InventoryComplete: true, Declarations: *decls,
		Inventory: []environments.File{{Path: value, Size: 7}},
		ReadSelected: func(_ context.Context, _ string, _ int64) ([]byte, int64, error) {
			return []byte("3.12.1\n"), 7, nil
		},
	}, environments.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(current.ToolchainDeclarations) != 1 {
		t.Fatalf("fixture did not produce a toolchain declaration: %+v", current)
	}
	headProfile := emptyProfile()
	headProfile.SchemaVersion = "1.7.0"
	headProfile.Declarations, headProfile.Environments = decls, current
	base, err := Load(mustJSON(t, baseProfile))
	if err != nil {
		t.Fatalf("load legacy environment provider report: %v", err)
	}
	head, err := Load(mustJSON(t, headProfile))
	if err != nil {
		t.Fatalf("load current environment provider report: %v", err)
	}
	comparison, err := Compare(base, head)
	if err != nil {
		t.Fatal(err)
	}
	module := moduleNamed(t, comparison, "environments")
	if module.Compatibility != "observed_only" || module.Counts.Added != 0 || module.Counts.Unavailable != 1 || !strings.Contains(strings.Join(module.Reasons, " "), "provider_scope_changed") {
		t.Fatalf("provider scope change made an unsupported absence claim: %+v", module)
	}
}

func mustJSON(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}
