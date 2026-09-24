// Package buildcontract contains compile-time and go-list–based contract
// tests that guard the dircue binary's dependency surface.
//
// These tests do NOT run in the normal "go test ./..." pass because they
// invoke an external "go list" subprocess; they would be slow and would
// require the toolchain to be on PATH.  They are intended to be run as part
// of the pre-release gate:
//
//	go test -run TestNoNetworkDeps ./internal/buildcontract/
package buildcontract

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestNoNetworkDeps asserts that the dircue main package does not transitively
// import any of the network-related packages that were historically dragged in
// by go-git's transport layer.
//
// The check is intentionally broad: it rejects any sub-package of
// plumbing/transport (transport/http, transport/ssh, transport/client, …),
// as well as the net/http and crypto/tls standard-library packages, and the
// x/crypto/ssh family.
//
// The fix (removing the transport/client import from third_party/go-git/remote.go)
// reduced the binary from ~30.4 MB to ~24.9 MB.  A regression would
// immediately be visible here.
func TestNoNetworkDeps(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	// Run from the module root (two directories up from this file's package).
	cmd.Dir = moduleRoot(t)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps .: %v", err)
	}

	forbidden := []string{
		// go-git transport sub-packages (each imports crypto/tls or x/crypto/ssh)
		"github.com/go-git/go-git/v5/plumbing/transport/client",
		"github.com/go-git/go-git/v5/plumbing/transport/http",
		"github.com/go-git/go-git/v5/plumbing/transport/ssh",
		"github.com/go-git/go-git/v5/plumbing/transport/git",
		"github.com/go-git/go-git/v5/plumbing/transport/file",
		"github.com/go-git/go-git/v5/plumbing/transport/server",
		"github.com/go-git/go-git/v5/plumbing/transport/internal/common",
		// Standard-library network packages
		"net/http",
		"crypto/tls",
		// Extended crypto — SSH client stack
		"golang.org/x/crypto/ssh",
		"golang.org/x/crypto/ssh/agent",
		"golang.org/x/crypto/ssh/knownhosts",
		// Dialer helpers that imply outbound connections
		"net/rpc",
		"net/smtp",
	}

	deps := string(bytes.TrimSpace(out))
	var violations []string
	for _, pkg := range forbidden {
		for _, dep := range strings.Split(deps, "\n") {
			if strings.TrimSpace(dep) == pkg {
				violations = append(violations, dep)
				break
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("dircue main binary transitively imports forbidden network packages:\n  %s\n\nFix: ensure third_party/go-git/remote.go does not import transport/client.", strings.Join(violations, "\n  "))
	}
}

// moduleRoot returns the module root directory by querying "go list -m".
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "dircue").Output()
	if err != nil {
		// Fallback: this file lives at internal/buildcontract/, two levels
		// below the module root.
		return "../.."
	}
	return strings.TrimSpace(string(out))
}
