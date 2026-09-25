//go:build linux || darwin

package scanner

import (
	"context"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/rules"
)

func TestRulesFIFOIsNotOpened(t *testing.T) {
	root := fixtures(t, map[string]string{"real.txt": "MAGIC"})
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := Scan(ctx, root, rulesOptions(t, true))
	if err != nil || r.Rules.InventoryFiles != 1 || r.Rules.Omissions[rules.NonRegularFile] != 1 || r.Rules.TotalMatches != 1 {
		t.Fatalf("FIFO: %+v %v", r, err)
	}
}

// TestAttachFIFOSIGTERMTerminatesProcess is a regression test for the FIFO
// hang found in the #94 review: `dircue map --attach sarif=<FIFO> DIR` used to
// block reading the FIFO and ignore SIGTERM. The fix (open with O_NONBLOCK in
// pkg/providerjoin/open_unix.go) means the process must exit within 2 seconds
// of receiving SIGTERM.
//
// This test builds the dircue binary and exercises the SIGTERM-terminates
// contract by spawning a subprocess. It is skipped when the binary cannot be
// built (e.g. on systems without a Go toolchain in PATH). The test is also
// Linux/macOS-only because mkfifo is a POSIX primitive.
func TestAttachFIFOSIGTERMTerminatesProcess(t *testing.T) {
	// Build the binary into a temp dir; skip if go build fails.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "dircue-test")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = findModuleRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("go build failed (skipping subprocess test): %v\n%s", err, out)
	}

	// Create a FIFO for the --attach path.
	fifoDir := t.TempDir()
	fifoPath := filepath.Join(fifoDir, "report.sarif")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	// Create a minimal directory to map.
	scanDir := t.TempDir()

	// Launch: dircue map --attach sarif=<FIFO> <scanDir>
	// The attach path should reject the FIFO immediately (O_NONBLOCK), so the
	// process exits non-zero (not zero) before SIGTERM is needed.
	// We send SIGTERM after 200 ms regardless and verify process exits within 2 s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "map", "--attach", "sarif="+fifoPath, scanDir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Give the process a moment to attempt the open.
	time.Sleep(200 * time.Millisecond)
	// Send SIGTERM; the process must exit promptly.
	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-waitDone:
		// Process exited (exit code may be non-zero due to FIFO error or SIGTERM).
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("dircue map --attach FIFO did not exit within 2 s of SIGTERM")
	}
}

// findModuleRoot returns the dircue module root by walking up from this file.
// It falls back to the binary's own build path if needed.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	// The test binary is built from the module root; use go env GOMOD to find it.
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Skipf("go env GOMOD failed: %v", err)
	}
	modfile := filepath.Dir(string(out[:len(out)-1])) // trim newline, then dirname
	return modfile
}
