package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/schema"
)

func executeAssessmentJSON(t *testing.T, args ...string) (*profile.Report, []byte) {
	t.Helper()
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), args, &out, &stderr); err != nil {
		t.Fatalf("Execute(%v): %v; stderr=%s", args, err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics for %v: %s", args, stderr.String())
	}
	var report profile.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode %v: %v\n%s", args, err, out.String())
	}
	if report.Assessment == nil || report.SchemaVersion != profile.AssessmentSchemaVersion {
		t.Fatalf("missing current assessment schema: %+v", report)
	}
	if err := assessment.ValidateReport(report.Assessment); err != nil {
		t.Fatalf("native assessment validator: %v", err)
	}
	var value any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateProfile(value); err != nil {
		t.Fatalf("bundled schema validator: %v", err)
	}
	return &report, bytes.Clone(out.Bytes())
}

func TestAssessmentCLIStandaloneAndAllPreserveLanguagesOffline(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"main.go":      "package main\nfunc main() {}\n",
		"README.md":    "assessment fixture\n",
		"package.json": `{"name":"cli-fixture"}`,
		"go.mod":       "module example.invalid/cli-fixture\n\ngo 1.23\n",
	} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Directory mode and an empty PATH prove this command needs neither Git nor
	// a Syft executable to produce native measurements.
	t.Setenv("PATH", t.TempDir())
	standalone, _ := executeAssessmentJSON(t, "analyze", "assessment", "--source", "directory", "--json", root)
	combined, _ := executeAssessmentJSON(t, "analyze", "all", "--assessment", "--source", "directory", "--json", root)
	if standalone.Assessment.Source.Mode != "directory" || standalone.PackageEvidence != nil {
		t.Fatalf("standalone assessment used an external source/provider: source=%+v packages=%+v", standalone.Assessment.Source, standalone.PackageEvidence)
	}
	if !reflect.DeepEqual(standalone.Languages, combined.Languages) || !reflect.DeepEqual(standalone.Assessment, combined.Assessment) {
		t.Fatalf("standalone/all assessment diverged: standalone languages=%+v all=%+v", standalone.Languages, combined.Languages)
	}
	var legacyOut, legacyErr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "languages", "--source", "directory", "--json", root}, &legacyOut, &legacyErr); err != nil {
		t.Fatal(err)
	}
	var legacyLanguages map[string]any
	if err := json.Unmarshal(legacyOut.Bytes(), &legacyLanguages); err != nil {
		t.Fatal(err)
	}
	if len(legacyLanguages) != len(standalone.Languages) {
		t.Fatalf("assessment changed existing language population: legacy=%v assessment=%+v", legacyLanguages, standalone.Languages)
	}
	if !strings.Contains(legacyOut.String(), "Go") || len(standalone.Languages) != 1 || standalone.Languages[0].Name != "Go" {
		t.Fatalf("language output mismatch: legacy=%s assessment=%+v", legacyOut.String(), standalone.Languages)
	}
	var textOut, textErr bytes.Buffer
	if err := Execute(context.Background(), []string{"analyze", "assessment", "--source", "directory", root}, &textOut, &textErr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOut.String(), "Repository measurements") || !strings.Contains(textOut.String(), "logical bytes") || !strings.Contains(textOut.String(), "connected project groups") || !strings.Contains(textOut.String(), "Static entry-point observations") {
		t.Fatalf("assessment text output missing summary: %s", textOut.String())
	}
}

func TestAssessmentOptionalSyftImportIsAdditiveToNativeMeasurements(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"package.json":      `{"name":"native-app","dependencies":{"left-pad":"1.0.0"}}`,
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}}}}`,
		"main.go":           "package main\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	syft, err := filepath.Abs("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	// Import uses the committed fixture and must not search PATH for Syft.
	t.Setenv("PATH", t.TempDir())
	baseline, _ := executeAssessmentJSON(t, "analyze", "all", "--assessment", "--source", "directory", "--json", root)
	imported, _ := executeAssessmentJSON(t, "analyze", "all", "--assessment", "--syft-report", syft, "--syft-root", "/", "--source", "directory", "--json", root)
	if imported.PackageEvidence == nil {
		t.Fatal("optional Syft fixture was not imported")
	}
	if !reflect.DeepEqual(baseline.Assessment, imported.Assessment) || !reflect.DeepEqual(baseline.Declarations, imported.Declarations) || !reflect.DeepEqual(baseline.Lockfiles, imported.Lockfiles) {
		t.Fatalf("optional provider input changed native counts, roots, relations, or lock observations:\nbefore=%+v\nafter=%+v", baseline.Assessment, imported.Assessment)
	}
}

func TestAssessmentSchemaIsAvailableOffline(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"capabilities", "--schema", "assessment"}, &out, &stderr); err != nil {
		t.Fatalf("export embedded assessment schema: %v (%s)", err, stderr.String())
	}
	var exported map[string]any
	if err := json.Unmarshal(out.Bytes(), &exported); err != nil {
		t.Fatalf("decode exported schema: %v", err)
	}
	if exported["$schema"] == nil || exported["$id"] == nil || !strings.Contains(out.String(), "assessment") {
		t.Fatalf("exported assessment schema is not a self-described JSON schema: %s", out.String())
	}
}

func TestAssessmentLocalRelationshipsRequireDeclaredEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  map[string]string
		reason string
	}{
		{"self-reference", map[string]string{"App.csproj": `<Project><ItemGroup><ProjectReference Include="App.csproj"/></ItemGroup></Project>`}, "self_reference_qualified"},
		{"coordinate-only", map[string]string{
			"app/pom.xml":     `<project><modelVersion>4.0.0</modelVersion><groupId>x</groupId><artifactId>app</artifactId><version>1</version><dependencies><dependency><groupId>x</groupId><artifactId>lib</artifactId><version>1</version></dependency></dependencies></project>`,
			"archive/pom.xml": `<project><modelVersion>4.0.0</modelVersion><groupId>x</groupId><artifactId>lib</artifactId><version>1</version></project>`,
		}, "coordinate_match_without_declared_reactor"},
		{"coordinate-case", map[string]string{
			"pom.xml":     `<project><modelVersion>4.0.0</modelVersion><groupId>x</groupId><artifactId>root</artifactId><version>1</version><packaging>pom</packaging><modules><module>app</module><module>lib</module></modules></project>`,
			"app/pom.xml": `<project><modelVersion>4.0.0</modelVersion><groupId>x</groupId><artifactId>app</artifactId><version>1</version><dependencies><dependency><groupId>X</groupId><artifactId>Lib</artifactId><version>1</version></dependency></dependencies></project>`,
			"lib/pom.xml": `<project><modelVersion>4.0.0</modelVersion><groupId>x</groupId><artifactId>lib</artifactId><version>1</version></project>`,
		}, "coordinate_case_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				filename := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, _ := executeAssessmentJSON(t, "analyze", "assessment", "--source", "directory", "--json", root)
			d := r.Assessment.Structure.Dependencies
			if d.DefiniteEdges.Count != 0 || d.QualifiedReferenceCount < 1 {
				t.Fatalf("local declaration became a definite edge or vanished: %+v", d)
			}
			var qualified bool
			for _, c := range r.Assessment.Structure.Coverage {
				if c.Scope == "project_dependencies" && c.Status == "partial" && slices.Contains(c.Reasons, tc.reason) {
					qualified = true
				}
			}
			if !qualified {
				t.Fatalf("missing qualification %s: %+v", tc.reason, r.Assessment.Structure.Coverage)
			}
		})
	}
}
