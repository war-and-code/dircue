package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

func TestFormatsCommandsAndComparison(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"data.xml": "<records><record/></records>", "package.json": `{"name":"example"}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var standalone *profile.Report
	for _, args := range [][]string{{"analyze", "formats"}, {"analyze", "all", "--formats"}, {"analyze", "all", "--formats", "--declarations", "--discovery", "--graph"}} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), append(args, "--json", root), &out, &stderr); err != nil {
			t.Fatal(err)
		}
		var p profile.Report
		if err := json.Unmarshal(out.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.SchemaVersion != "1.5.0" || p.Formats == nil || p.Formats.Coverage.InspectedFiles != 2 || stderr.Len() != 0 {
			t.Fatalf("unexpected report: %s; stderr %s", out.String(), stderr.String())
		}
		s, err := reportdiff.Load(bytes.NewReader(out.Bytes()))
		if err != nil {
			t.Fatalf("actual report failed import: %v", err)
		}
		compared, err := reportdiff.Compare(s, s)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range compared.Modules {
			if m.Name == "formats" {
				found = true
				if m.Status != "unchanged" || m.Counts.Changed != 0 {
					t.Fatalf("self-comparison changed: %+v", m)
				}
			}
		}
		if !found {
			t.Fatal("formats comparison missing")
		}
		if standalone == nil {
			standalone = &p
			if len(p.Languages) != 0 || p.Declarations != nil {
				t.Fatal("standalone format inspection ran other profilers")
			}
		} else {
			a, _ := json.Marshal(standalone.Formats)
			b, _ := json.Marshal(p.Formats)
			if !bytes.Equal(a, b) {
				t.Fatal("combined format evidence differs")
			}
		}
	}
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "formats", root}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "complete_validation") || !strings.Contains(out.String(), "Inspected 2 of 2") {
		t.Fatal(out.String())
	}
}

func TestContentOptionsStayExplicit(t *testing.T) {
	for _, args := range [][]string{{"--formats"}, {"--hotspots"}, {"analyze", "all", "--hotspots"}, {"analyze", "discovery", "--formats"}} {
		var out, stderr bytes.Buffer
		if err := Execute(context.Background(), args, &out, &stderr); err == nil || out.Len() != 0 {
			t.Fatalf("unexpected success/output for %v", args)
		}
	}
}
