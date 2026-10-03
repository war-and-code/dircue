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

func TestLockfilesPlanUsesSavedDiscoveryAndKeepsExecutionInert(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"package.json":      `{"dependencies":{"left-pad":"1.0.0"}}`,
		"package-lock.json": `{"name":"fixture","lockfileVersion":3,"packages":{"":{"dependencies":{"left-pad":"1.0.0"}},"node_modules/left-pad":{"version":"1.0.0"}}}`,
		".python-version":   "3.11.9\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var output, diagnostics bytes.Buffer
	err := cli.Execute(context.Background(), []string{"analyze", "discovery", "--source", "directory", "--json", root}, &output, &diagnostics)
	if err != nil {
		t.Fatalf("generate discovery profile: %v; stderr=%s", err, diagnostics.String())
	}
	profile, digest, err := reportdiff.ReadEvidence(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("load saved discovery profile: %v", err)
	}
	plan, err := planning.Build(context.Background(), planning.Input{Profile: profile, ReportSHA256: digest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"lockfiles"}}})
	if err != nil {
		t.Fatalf("build lockfiles plan: %v", err)
	}
	if len(plan.Decisions) != 1 || plan.Decisions[0].Module != "lockfiles" || plan.Decisions[0].Applicability == "already_present" || len(plan.Steps) != 1 {
		t.Fatalf("expected a proposed lockfiles step from discovery-only report: %+v", plan)
	}
	step := plan.Steps[0]
	if step.Module != "lockfiles" || step.Cost.Inspection != "bounded-content" || step.Cost.ExternalProcess || step.Command.Executable || step.Scope.EvidenceStatus != "available" {
		t.Fatalf("unsafe or mis-scoped plan: %+v", step)
	}
	if !slices.Contains(step.Scope.CandidatePaths, "package.json") || !slices.Contains(step.Scope.CandidatePaths, "package-lock.json") {
		t.Fatalf("candidate population omitted lockfile pair: %+v", step.Scope)
	}
	if !slices.Contains(step.Command.Argv, "lockfiles") || step.Command.SourcePlaceholder == "" || step.Command.Argv[len(step.Command.Argv)-1] != step.Command.SourcePlaceholder {
		t.Fatalf("command is not an inert source-placeholder recipe: %+v", step.Command)
	}

	// A retained partial module cannot satisfy a complete-scope request.
	output.Reset()
	diagnostics.Reset()
	err = cli.Execute(context.Background(), []string{"analyze", "all", "--source", "directory", "--discovery", "--lockfiles", "--max-file-bytes", "1", "--json", root}, &output, &diagnostics)
	if err != nil {
		t.Fatalf("generate partial lockfiles profile: %v; stderr=%s", err, diagnostics.String())
	}
	partial, partialDigest, err := reportdiff.ReadEvidence(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("load partial profile: %v", err)
	}
	partialPlan, err := planning.Build(context.Background(), planning.Input{Profile: partial, ReportSHA256: partialDigest, Capabilities: capabilities.Dircue("test"), Selection: planning.Selection{Modules: []string{"lockfiles"}}})
	if err != nil {
		t.Fatalf("plan partial profile: %v", err)
	}
	if len(partialPlan.Steps) != 1 || partialPlan.Decisions[0].Applicability != "retained_partial" {
		t.Fatalf("partial lockfiles module was treated as complete: %+v", partialPlan)
	}
}
