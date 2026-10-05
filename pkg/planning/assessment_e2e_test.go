package planning_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/planning"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

func TestAssessmentPlanAcceptsNewSchemaAndQualifiesPartialMeasurements(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"discovery", "assessment", "limited"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"analyze", mode, "--source", "directory", "--json", root}
			if mode == "limited" {
				args = []string{"analyze", "assessment", "--max-file-bytes", "1", "--source", "directory", "--json", root}
			}
			var out, stderr bytes.Buffer
			if err := cli.Execute(context.Background(), args, &out, &stderr); err != nil {
				t.Fatalf("%v %s", err, stderr.String())
			}
			p, digest, err := reportdiff.ReadEvidence(bytes.NewReader(out.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := planning.Build(context.Background(), planning.Input{Profile: p, ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"assessment"}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Decisions) != 1 {
				t.Fatal(plan)
			}
			d := plan.Decisions[0]
			if mode == "assessment" {
				if d.Applicability != "already_present" || len(plan.Steps) != 0 {
					t.Fatalf("complete assessment not retained: %+v", plan)
				}
			} else {
				if len(plan.Steps) != 1 {
					t.Fatal(plan)
				}
				step := plan.Steps[0]
				if step.Command.Executable || !slices.Contains(step.Command.Argv, "assessment") || step.Cost.ExternalProcess {
					t.Fatalf("unsafe recipe: %+v", step)
				}
				if mode == "limited" && d.Applicability != "retained_partial" {
					t.Fatalf("partial measurements satisfied complete request: %+v", d)
				}
			}
		})
	}
}
