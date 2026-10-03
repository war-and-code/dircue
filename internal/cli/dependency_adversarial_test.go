package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/profile"
	registrypkg "github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/schema"
)

// These cases exercise the exact selected config/toolchain paths through the
// CLI. Manifest-only Cargo.toml fixtures do not exercise the registry or
// rustup configuration readers.
func runDependencyAdversarialCLI(t *testing.T, files map[string]string) profile.Report {
	t.Helper()
	root := t.TempDir()
	writeCLILockfileFixture(t, root, files)
	output, stderr, err := invoke("analyze", "all", "--registries", "--environments", "--source", "directory", "--json", root)
	if err != nil || stderr != "" {
		t.Fatalf("selected dependency analysis stdout=%q stderr=%q err=%v", output, stderr, err)
	}
	var decoded any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode aggregate profile: %v\n%s", err, output)
	}
	if err := schema.ValidateProfile(decoded); err != nil {
		t.Fatalf("native profile schema rejected selected dependency report: %v", err)
	}
	var report profile.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode typed aggregate profile: %v", err)
	}
	if report.Registries == nil || report.Environments == nil {
		t.Fatalf("selected optional modules are missing from profile: registries=%v environments=%v", report.Registries != nil, report.Environments != nil)
	}
	if err := environments.ValidateReport(report.Environments); err != nil {
		t.Fatalf("environment component rejected its CLI report: %v", err)
	}
	return report
}

func TestRegistryCLISelectsCargoConfigAndSeparatesURLFromDirectory(t *testing.T) {
	report := runDependencyAdversarialCLI(t, map[string]string{
		".cargo/config.toml": `[registries.bare]
index = "registry/index"

[source.vendor]
directory = "vendor/cache"
`,
	})
	registries := report.Registries
	if registries.Coverage.CandidateFiles != 1 || registries.Coverage.AdmittedFiles != 1 || registries.Coverage.ReadFiles != 1 || registries.Coverage.ParsedFiles != 1 {
		t.Fatalf("selected Cargo config was not read and parsed: %+v", registries.Coverage)
	}
	if registries.Status != "partial" || len(registries.Configurations) != 1 {
		t.Fatalf("unexpected selected registry report: %+v", registries)
	}
	cfg := registries.Configurations[0]
	if cfg.Path != ".cargo/config.toml" || cfg.Ecosystem != "cargo" || len(cfg.Declarations) != 3 {
		t.Fatalf("unexpected Cargo config selection: %+v", cfg)
	}
	var indexStatus, directoryStatus string
	for _, declaration := range cfg.Declarations {
		switch {
		case declaration.Section == "cargoRegistry" && declaration.Endpoint != nil:
			indexStatus = declaration.Endpoint.Status
		case declaration.Section == "cargoSource" && declaration.Scope != nil && declaration.Scope.Value == "directory" && declaration.Endpoint != nil:
			directoryStatus = declaration.Endpoint.Status
		}
	}
	if indexStatus != "invalid" {
		t.Fatalf("bare Cargo registry index should remain invalid, got status %q: %+v", indexStatus, cfg.Declarations)
	}
	if directoryStatus != "local_path" {
		t.Fatalf("Cargo source directory should be classified as local_path, got %q: %+v", directoryStatus, cfg.Declarations)
	}
}

func TestRegistryCLICargoDeepTOMLPreflightRetainsValidSibling(t *testing.T) {
	deepKey := strings.Repeat("nested.", 65) + "leaf = 1\n"
	report := runDependencyAdversarialCLI(t, map[string]string{
		".cargo/config.toml":         deepKey,
		"service/.cargo/config.toml": "[source.vendor]\ndirectory = \"vendor/cache\"\n",
	})
	registries := report.Registries
	if registries.Coverage.CandidateFiles != 2 || registries.Coverage.AdmittedFiles != 2 || registries.Coverage.ReadFiles != 2 || registries.Coverage.ParsedFiles != 1 {
		t.Fatalf("TOML preflight coverage did not distinguish both selected files: %+v", registries.Coverage)
	}
	if registries.Status != "partial" || len(registries.Configurations) != 2 {
		t.Fatalf("deep selected TOML did not degrade coverage: %+v", registries)
	}
	var deep, sibling *registrypkg.Configuration
	for i := range registries.Configurations {
		cfg := &registries.Configurations[i]
		switch cfg.Path {
		case ".cargo/config.toml":
			deep = cfg
		case "service/.cargo/config.toml":
			sibling = cfg
		}
	}
	if deep == nil || deep.SyntaxStatus != "incomplete" || deep.Status != "partial" || deep.Omissions["toml_depth_limit"] != 1 || len(deep.Declarations) != 0 {
		t.Fatalf("deep TOML was not rejected before producing declarations: %+v", deep)
	}
	if sibling == nil || sibling.SyntaxStatus != "complete" || sibling.Status != "complete" || len(sibling.Declarations) != 2 {
		t.Fatalf("valid sibling config was lost with the deep file: %+v", sibling)
	}
	foundLocal := false
	for _, declaration := range sibling.Declarations {
		if declaration.Endpoint != nil && declaration.Endpoint.Status == "local_path" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatalf("valid sibling's directory declaration was not retained: %+v", sibling.Declarations)
	}
}

func TestEnvironmentCLIRustMalformedOptionalMetadataPreservesChannelAndSibling(t *testing.T) {
	report := runDependencyAdversarialCLI(t, map[string]string{
		"rust-toolchain.toml":         "[toolchain]\nchannel = \"stable\"\ncomponents = \"rustfmt\"\n",
		"service/rust-toolchain.toml": "[toolchain]\nchannel = \"1.85.0\"\ncomponents = [\"rustfmt\"]\n",
	})
	env := report.Environments
	if env.Status != "partial" || env.Coverage.ToolchainCandidates != 2 || env.Coverage.ToolchainRead != 2 || env.Coverage.ToolchainDeclarations != 2 {
		t.Fatalf("Rust toolchain candidates/partial status were not reported: %+v", env)
	}
	values := map[string]string{}
	for _, declaration := range env.ToolchainDeclarations {
		if declaration.State == "declared" && len(declaration.Values) == 1 {
			values[declaration.SourcePath] = declaration.Values[0]
		}
	}
	if values["rust-toolchain.toml"] != "stable" || values["service/rust-toolchain.toml"] != "1.85.0" {
		t.Fatalf("supported channel or valid sibling was lost: %+v", env.ToolchainDeclarations)
	}
	foundDiagnostic := false
	for _, diagnostic := range env.Diagnostics {
		if diagnostic.Path == "rust-toolchain.toml" && diagnostic.Code == "unsupported-toolchain-toml" {
			foundDiagnostic = true
		}
	}
	if !foundDiagnostic {
		t.Fatalf("malformed optional metadata lacked its partial diagnostic: %+v", env.Diagnostics)
	}
}
