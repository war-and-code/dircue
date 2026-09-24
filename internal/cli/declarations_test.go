package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dircue/pkg/profile"
)

func TestDeclarationsOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo","scripts":{"test":"do-not-output-secret"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--json", root}, {"analyze", "all", "--json", root}, {"analyze", "projects", "--json", root}} {
		var out, errOut bytes.Buffer
		if err := Execute(context.Background(), args, &out, &errOut); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), `"declarations"`) || strings.Contains(out.String(), "do-not-output-secret") {
			t.Fatal(out.String())
		}
	}
	var out, errOut bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "declarations", "--json", root}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var report profile.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Declarations == nil || len(report.Declarations.Projects) != 1 || report.SchemaVersion != "1.4.0" {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), "do-not-output-secret") {
		t.Fatal("raw script command escaped")
	}
	out.Reset()
	if err := Execute(context.Background(), []string{"analyze", "declarations", root}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	// Developer-task scripts ("test") are not surfaced as interfaces; only the
	// raw command text must remain absent (security check).
	if strings.Contains(out.String(), "do-not-output-secret") {
		t.Fatal(out.String())
	}
}
