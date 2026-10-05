package assessment_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/war-and-code/dircue/internal/jsonschema"
	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/profile"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

func TestAssessmentGeneratedReports(t *testing.T) {
	directory := os.Getenv("DIRCUE_ASSESSMENT_REPORTS")
	if directory == "" {
		t.Skip("set DIRCUE_ASSESSMENT_REPORTS")
	}
	content, err := os.ReadFile(filepath.Join(directory, "candidate-profile.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.LoadURL = func(string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("unbundled schema resource refused")
	}
	const schemaURL = "https://dircue.invalid/schema/profile.schema.json"
	if err = compiler.AddResource(schemaURL, bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(directory, "*.stdout.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := strconv.Atoi(os.Getenv("DIRCUE_ASSESSMENT_REPORT_COUNT"))
	if err != nil || len(paths) != expected || expected == 0 {
		t.Fatalf("expected %d CLI reports, got %d: %v", expected, len(paths), err)
	}
	nativeGuards := 0
	for _, path := range paths {
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report map[string]any
		if err = json.Unmarshal(content, &report); err != nil {
			t.Fatal(err)
		}
		if err = compiled.Validate(report); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err = reportdiff.Load(bytes.NewReader(content)); err != nil {
			t.Fatalf("native saved-report validation %s: %v", path, err)
		}
		var native profile.Report
		if err = json.Unmarshal(content, &native); err != nil {
			t.Fatal(err)
		}
		native.Assessment.UnsupportedEcosystemProjects.Count++
		if assessment.ValidateReport(native.Assessment) == nil {
			t.Fatalf("contradictory unsupported ecosystem population accepted in %s", path)
		}
		native.Assessment.UnsupportedEcosystemProjects.Count--
		nativeGuards++
		for _, evidence := range [][]assessment.RelationshipEvidence{native.Assessment.WorkspaceEvidence, native.Assessment.LocalDependencyEvidence} {
			if len(evidence) == 0 {
				continue
			}
			kind := evidence[0].Kind
			evidence[0].Kind = "counterfeit-kind"
			if assessment.ValidateReport(native.Assessment) == nil {
				t.Fatalf("foreign relationship evidence kind accepted in %s", path)
			}
			evidence[0].Kind = kind
			nativeGuards++
		}
		// The schema must reject unknown and missing assessment fields too.
		assessmentMap := report["assessment"].(map[string]any)
		assessmentMap["unexpected_generated_guard"] = 1
		if compiled.Validate(report) == nil {
			t.Fatalf("unknown field accepted in %s", path)
		}
		delete(assessmentMap, "unexpected_generated_guard")
		saved := assessmentMap["projects"]
		delete(assessmentMap, "projects")
		if compiled.Validate(report) == nil {
			t.Fatalf("missing projects accepted in %s", path)
		}
		assessmentMap["projects"] = saved
		report["schema_version"] = "0.0.0-invalid"
		if compiled.Validate(report) == nil {
			t.Fatalf("invalid version accepted in %s", path)
		}
	}
	t.Logf("Validated %d actual CLI reports through native saved-report Load and three schema guards per report against candidate-exported offline schema", expected)
	t.Logf("Rejected %d native aggregate/evidence corruption guards", nativeGuards)
}
