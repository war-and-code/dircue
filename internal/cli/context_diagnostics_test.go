package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/lockfiles"
)

func TestMalformedToolchainTextDisclosesDiagnosticAndNestedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"rust-toolchain.toml":       "[toolchain]\nchannel = 'stable'\n",
		"child/rust-toolchain.toml": "[toolchain]\nchannel = 'stable'\ncomponents = 17\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, stderr, err := invoke("analyze", "environments", "--source", "directory", root)
	if err != nil || stderr != "" {
		t.Fatalf("text analysis failed: %v; stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"Declared environments: partial",
		`Unresolved "child/rust-toolchain.toml": nested-toolchain-declaration`,
		`Diagnostic "child/rust-toolchain.toml": unsupported-toolchain-toml`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("text omitted %q:\n%s", want, output)
		}
	}
}

func TestSkippedLockfileTextExplainsInventoryOmission(t *testing.T) {
	var output bytes.Buffer
	if err := writeLockfiles(&output, lockfiles.Skip("directory", "", "inventory_path_limit")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Lockfile observations: skipped") || !strings.Contains(output.String(), `Diagnostic ".": inventory_path_limit`) {
		t.Fatalf("skipped work was unexplained: %s", output.String())
	}
}
