package availability

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestParseGitmodulesRetainsOnlyPaths(t *testing.T) {
	content := `[submodule "private name"]
	path = third_party/lib
	url = ssh://secret.example/private/repo
	branch = confidential
[submodule "quoted"]
	path = "components/api"
`
	parsed := ParseGitmodules(".gitmodules", []byte(content), int64(len(content)), DefaultGitmodulesBytes)
	if !parsed.Complete || len(parsed.Declarations) != 2 || parsed.Declarations[0].Path != "components/api" || parsed.Declarations[1].Path != "third_party/lib" {
		t.Fatalf("parsed: %+v", parsed)
	}
	encoded, _ := json.Marshal(parsed)
	for _, secret := range []string{"private name", "secret.example", "confidential", "ssh://"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %q in %s", secret, encoded)
		}
	}
}

func TestParseGitmodulesRejectsAmbiguousAndIncompleteInput(t *testing.T) {
	content := `[submodule "escape"]
	path = ../outside
[submodule "duplicate"]
	path = components/a
	path = components/b
[submodule "missing"]
	url = https://example.invalid/repo
`
	parsed := ParseGitmodules(".gitmodules", []byte(content), int64(len(content)), DefaultGitmodulesBytes)
	if parsed.Complete || len(parsed.Declarations) != 1 || len(parsed.Diagnostics) != 3 {
		t.Fatalf("parsed: %+v", parsed)
	}
	truncated := ParseGitmodules(".gitmodules", []byte(content[:10]), int64(len(content)), DefaultGitmodulesBytes)
	if truncated.Complete || len(truncated.Declarations) != 0 || truncated.Diagnostics[0].Code != "incomplete-gitmodules" {
		t.Fatalf("truncated: %+v", truncated)
	}
}

func TestParseGitmodulesBoundsIntermediateEvidenceAndDiagnostics(t *testing.T) {
	var declarations strings.Builder
	for i := DefaultEvidenceLimit + 499; i >= 0; i-- {
		fmt.Fprintf(&declarations, "[submodule %q]\npath = vendor/%05d\n", fmt.Sprintf("s%d", i), i)
	}
	parsed := ParseGitmodules(".gitmodules", []byte(declarations.String()), int64(declarations.Len()), DefaultGitmodulesBytes)
	if parsed.Complete || len(parsed.Declarations) != DefaultEvidenceLimit || parsed.OmittedDeclarations != 500 {
		t.Fatalf("declaration cap: declarations=%d omitted=%d complete=%t", len(parsed.Declarations), parsed.OmittedDeclarations, parsed.Complete)
	}
	if parsed.Declarations[0].Path != "vendor/00000" || parsed.Declarations[len(parsed.Declarations)-1].Path != "vendor/04095" {
		t.Fatalf("declaration cap did not retain lexical prefix: first=%q last=%q", parsed.Declarations[0].Path, parsed.Declarations[len(parsed.Declarations)-1].Path)
	}

	var malformed strings.Builder
	for i := 0; i < DefaultDiagnosticLimit+500; i++ {
		fmt.Fprintf(&malformed, "[submodule %q]\nurl = ignored\n", fmt.Sprintf("m%d", i))
	}
	parsed = ParseGitmodules(".gitmodules", []byte(malformed.String()), int64(malformed.Len()), DefaultGitmodulesBytes)
	if parsed.Complete || len(parsed.Diagnostics) != DefaultDiagnosticLimit || parsed.OmittedDiagnostics != 500 {
		t.Fatalf("diagnostic cap: diagnostics=%d omitted=%d complete=%t", len(parsed.Diagnostics), parsed.OmittedDiagnostics, parsed.Complete)
	}
}
