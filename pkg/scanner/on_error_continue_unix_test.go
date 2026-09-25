//go:build linux || darwin

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/structure"
)

// TestAnalyzeAllContinueSurvivesUnreadableAggregationCandidates covers
// r100/10 F1: with --on-error continue, a single permission-denied
// aggregation candidate (formats/declarations/registries) must degrade to a
// same-shaped file_read_error warning plus a per-module omission and MUST
// NOT abort the aggregate. The default fail policy still aborts, matching
// 0.8.0.
func TestAnalyzeAllContinueSurvivesUnreadableAggregationCandidates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	root := fixtures(t, map[string]string{
		"pyproject.toml":       "[project]\nname = \"m\"\nversion = \"1.0\"\n",
		"other/pyproject.toml": "[project]\nname = \"other\"\nversion = \"1.0\"\n",
		".npmrc":               "registry=https://example.com/\n",
		"nested/.npmrc":        "registry=https://ok.example.com/\n",
		"config.json":          "{\"a\":1}\n",
		"good.json":            "{\"b\":2}\n",
		"main.go":              goSource,
	})
	unreadable := []string{"pyproject.toml", ".npmrc", "config.json"}
	for _, p := range unreadable {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(p)), 0); err != nil {
			t.Fatal(err)
		}
		path := p
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, filepath.FromSlash(path)), 0644) })
	}
	base := Options{
		Source:       "directory",
		Formats:      true,
		Declarations: true,
		Registries:   true,
		ErrorPolicy:  ErrorPolicyContinue,
	}
	report, err := Scan(context.Background(), root, base)
	if err != nil {
		t.Fatalf("continue policy aborted the aggregate: %v", err)
	}
	if report.Formats == nil || report.Formats.Status != "partial" || report.Formats.Omissions["file_read_error"] == 0 {
		t.Fatalf("formats did not record file_read_error omission: %+v", report.Formats)
	}
	if report.Declarations == nil || report.Declarations.Status != "partial" {
		t.Fatalf("declarations did not degrade to partial: %+v", report.Declarations)
	}
	foundDecl := false
	for _, d := range report.Declarations.Diagnostics {
		if d.Code == "file-read-error" && strings.Contains(d.Path, "pyproject.toml") {
			foundDecl = true
		}
	}
	if !foundDecl {
		t.Fatalf("declarations did not attribute the read failure per-path: %+v", report.Declarations.Diagnostics)
	}
	if report.Registries == nil || report.Registries.Status != "partial" {
		t.Fatalf("registries did not degrade to partial: %+v", report.Registries)
	}
	foundReg := false
	for _, cfg := range report.Registries.Configurations {
		if cfg.Path == ".npmrc" && cfg.Omissions["file_read_error"] == 1 {
			foundReg = true
		}
	}
	if !foundReg {
		t.Fatalf("registries did not record file_read_error per configuration: %+v", report.Registries.Configurations)
	}
	warnPaths := map[string]int{}
	for _, w := range report.Warnings {
		if w.Code == "file_read_error" {
			warnPaths[w.Path]++
		}
	}
	for _, p := range []string{"pyproject.toml", ".npmrc", "config.json"} {
		if warnPaths[p] != 1 {
			t.Fatalf("got %d top-level file_read_error warnings for %q: %+v", warnPaths[p], p, report.Warnings)
		}
	}
	if report.Formats.Coverage.InspectedFiles == 0 {
		t.Fatalf("formats abandoned peer candidates: %+v", report.Formats.Coverage)
	}
	if report.Declarations.Coverage.ParsedManifests == 0 {
		t.Fatalf("declarations abandoned peer manifests: %+v", report.Declarations.Coverage)
	}
	registryOK := false
	for _, cfg := range report.Registries.Configurations {
		if cfg.Path == "nested/.npmrc" && len(cfg.Declarations) > 0 {
			registryOK = true
		}
	}
	if !registryOK {
		t.Fatalf("registries abandoned peer configuration: %+v", report.Registries.Configurations)
	}

	// Default policy still aborts on the same input.
	base.ErrorPolicy = ErrorPolicyFail
	if _, err := Scan(context.Background(), root, base); err == nil {
		t.Fatal("default policy no longer aborts on unreadable aggregation candidate")
	}

	// Single-module invocations honor the continue policy the same way.
	for _, only := range []Options{
		{Source: "directory", Formats: true, FormatsOnly: true, ErrorPolicy: ErrorPolicyContinue},
		{Source: "directory", Declarations: true, DeclarationsOnly: true, ErrorPolicy: ErrorPolicyContinue},
		{Source: "directory", Registries: true, RegistriesOnly: true, ErrorPolicy: ErrorPolicyContinue},
	} {
		if _, err := Scan(context.Background(), root, only); err != nil {
			t.Fatalf("single-module continue aborted: %+v %v", only, err)
		}
	}
}

// TestEnvironmentsContinuePolicyDegradesUnreadableGlobalJSON exercises the
// environments-side branch: a chmod'd global.json under --on-error continue
// must produce a partial environments report with an unresolved selection
// and a per-path diagnostic, not abort the scan.
func TestEnvironmentsContinuePolicyDegradesUnreadableGlobalJSON(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 has no effect when running as root")
	}
	root := fixtures(t, map[string]string{
		"src/App.csproj":  `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`,
		"src/global.json": `{"sdk":{"version":"8.0.100"}}`,
	})
	if err := os.Chmod(filepath.Join(root, "src", "global.json"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "src", "global.json"), 0644) })

	report, err := Scan(context.Background(), root, Options{
		Source:       "directory",
		Declarations: true,
		Environments: true,
		ErrorPolicy:  ErrorPolicyContinue,
	})
	if err != nil {
		t.Fatalf("continue policy aborted: %v", err)
	}
	if report.Environments == nil {
		t.Fatal("no environments report produced")
	}
	if report.Environments.Status != "partial" {
		t.Fatalf("environments status: %+v", report.Environments)
	}
	foundDiag := false
	for _, d := range report.Environments.Diagnostics {
		if d.Code == "file-read-error" && strings.Contains(d.Path, "global.json") {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Fatalf("no file-read-error diagnostic: %+v", report.Environments.Diagnostics)
	}
	foundWarn := false
	for _, w := range report.Warnings {
		if w.Code == "file_read_error" && strings.Contains(w.Path, "global.json") {
			foundWarn = true
		}
	}
	if !foundWarn {
		t.Fatalf("no top-level file_read_error warning: %+v", report.Warnings)
	}
	if len(report.Environments.Selections) == 0 {
		t.Fatalf("no selection preserved: %+v", report.Environments)
	}
	if report.Environments.Selections[0].State != "unresolved" {
		t.Fatalf("expected unresolved state: %+v", report.Environments.Selections[0])
	}
}

// TestStructureContinueDegradesPerFileWorkerTimeoutAndCrash covers r100/04
// F2: under --on-error continue, a single-file worker timeout or non-zero
// exit must degrade to a per-file structural omission with a distinct
// reason and preserve the rest of the scan. Protocol violations remain
// fatal (unchanged and exercised by the existing pkg/structure tests).
//
// The fake workers are POSIX shell scripts under testdata so the test
// needs no Rust toolchain; capability probes never run because we do not
// request Hotspots or Functions.
func TestStructureContinueDegradesPerFileWorkerTimeoutAndCrash(t *testing.T) {
	for _, kind := range []string{"timeout", "crash"} {
		t.Run(kind, func(t *testing.T) {
			worker := writeFakeStructuralWorker(t, kind)
			// A short client-side timeout means the timeout variant does not
			// block the test; the crash variant returns immediately.
			client, err := structure.New(structure.Options{Worker: worker, Timeout: 300 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{
				Source:      "directory",
				Structure:   client,
				ErrorPolicy: ErrorPolicyContinue,
				Workers:     1,
			}
			dir := fixtures(t, map[string]string{"a.py": "print('a')\n"})
			report, err := Scan(context.Background(), dir, opts)
			if err != nil {
				t.Fatalf("continue policy aborted on worker %s: %v", kind, err)
			}
			if report.Structure == nil {
				t.Fatal("no structure report")
			}
			var expectedReason string
			switch kind {
			case "timeout":
				expectedReason = "structural_timeout"
			case "crash":
				expectedReason = "structural_worker_failure"
			}
			if report.Structure.Omissions[expectedReason] == 0 {
				t.Fatalf("no %s omission recorded: %+v", expectedReason, report.Structure.Omissions)
			}
			if report.Structure.Status != "partial" {
				t.Fatalf("structure status: %+v", report.Structure)
			}
			foundWarn := false
			for _, w := range report.Warnings {
				if w.Code == expectedReason {
					foundWarn = true
					if strings.Contains(w.Message, "SECRET-WORKER-STDERR") || strings.ContainsAny(w.Message, "\x1b\r\n") {
						t.Fatalf("worker stderr escaped into successful report: %+v", w)
					}
				}
			}
			if !foundWarn {
				t.Fatalf("no top-level %s warning: %+v", expectedReason, report.Warnings)
			}
			// Default policy still aborts, preserving the 0.8.0 trust boundary
			// disposition for structural worker failures.
			opts.ErrorPolicy = ErrorPolicyFail
			if _, err := Scan(context.Background(), dir, opts); err == nil {
				t.Fatal("default policy no longer aborts on worker failure")
			}
		})
	}
}

// writeFakeStructuralWorker materializes a small POSIX shell script that
// impersonates the structural worker in one of two ways:
//   - "timeout": consumes stdin then sleeps forever, so the client's
//     per-file WithTimeout cancels the child. Parent context stays live.
//   - "crash":   consumes stdin and exits 134 (SIGABRT), which the
//     exec/*Cmd surfaces as a non-zero exit with stderr text.
//
// Both variants are pure textdata; no Rust toolchain is required. The
// script is materialized into t.TempDir() so multiple runs remain isolated.
func writeFakeStructuralWorker(t *testing.T, kind string) string {
	t.Helper()
	dir := t.TempDir()
	worker := filepath.Join(dir, "fake-worker.sh")
	var body string
	switch kind {
	case "timeout":
		// Drain stdin (client sends a JSON request) then block forever.
		body = "#!/bin/sh\ncat >/dev/null\nwhile true; do sleep 1; done\n"
	case "crash":
		body = "#!/bin/sh\ncat >/dev/null\nprintf 'SECRET-WORKER-STDERR\\033[31m\\n' >&2\nexit 134\n"
	default:
		t.Fatalf("unknown fake worker kind %q", kind)
	}
	if err := os.WriteFile(worker, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return worker
}
