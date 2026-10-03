package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
)

func TestMavenOnlyRegistryCLIHandlesXMLDeclaration(t *testing.T) {
	root := t.TempDir()
	settings := `<?xml version="1.0" encoding="UTF-8"?><settings xmlns="http://maven.apache.org/SETTINGS/1.2.0"></settings>`
	if err := os.WriteFile(filepath.Join(root, "settings.xml"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "registries", "--source", "directory", "--json", root}, &out, &stderr); err != nil {
		t.Fatalf("Maven-only registry analysis failed: %v; stderr=%s", err, stderr.String())
	}
	var report profile.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Registries == nil || report.Registries.Status != "complete" || len(report.Registries.Configurations) != 1 || report.Registries.Configurations[0].SyntaxStatus != "complete" {
		t.Fatalf("unexpected Maven-only registry report: %+v", report.Registries)
	}
}
