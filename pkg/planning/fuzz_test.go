package planning_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/capabilities"
	"github.com/war-and-code/dircue/pkg/planning"
	"github.com/war-and-code/dircue/pkg/reportdiff"
)

// FuzzLoadAndBuildPlan feeds hostile saved-report bytes through the reportdiff
// loader that the planner accepts as input, then hands surviving profiles to
// planning.Build with a handful of realistic selections. The loader must
// reject anything it cannot fully validate; when it accepts a profile, Build
// must never panic, must obey planning's declared limits, and must not
// silently emit an executable step (execution is forbidden by contract).
func FuzzLoadAndBuildPlan(f *testing.F) {
	// Minimal-but-valid dircue profile bytes; the CLI-produced form is much
	// larger, but this is enough to exercise Load's shape checks.
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`{"schema_version":"1.3.0","root":"/x","summary":{"scanned_files":0,"analyzed_files":0,"skipped_files":0,"language_bytes":0},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[]}`),
		[]byte(`{"schema_version":"1.3.0","root":"","summary":{"scanned_files":0,"analyzed_files":0,"skipped_files":0,"language_bytes":0},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[]}`),
		[]byte(`{"schema_version":"1.3.0","root":"/x","summary":{"scanned_files":0,"analyzed_files":0,"skipped_files":0,"language_bytes":0},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[],"discovery":{"provider":"dircue","provider_version":"1.0.0","source":{"mode":"directory","consistency":"live_directory"},"coverage":{"selected_inventory_complete":true,"omitted_files":0,"omitted_reasons":{}},"files":[{"path":"a.go","bytes":10}]}}`),
		[]byte(`{"schema_version":"1.3.0","root":"/x","summary":{"scanned_files":0,"analyzed_files":0,"skipped_files":0,"language_bytes":1e10000},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[]}`),
		// NaN, dup keys, deep nesting the loader must reject rather than
		// pass through to Build.
		[]byte(`{"schema_version":"1.3.0","root":"/x","summary":{"scanned_files":NaN,"analyzed_files":0,"skipped_files":0,"language_bytes":0},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[]}`),
		[]byte(`{"schema_version":"1.3.0","root":"/x","root":"/y","summary":{"scanned_files":0,"analyzed_files":0,"skipped_files":0,"language_bytes":0},"languages":[],"ecosystems":[],"frameworks":[],"layouts":[],"warnings":[]}`),
		[]byte(strings.Repeat(`{"schema_version":"1.3.0","root":`, 200) + strings.Repeat(`}`, 200)),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	selections := []planning.Selection{
		{Modules: []string{"declarations"}},
		{Modules: []string{"discovery"}},
		{Modules: []string{"structure"}, Inputs: []string{"structural-worker"}},
		{Questions: []string{"content-formats"}},
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		snapshot, err := reportdiff.Load(bytes.NewReader(data))
		if err != nil {
			return // the loader is the front line; rejection is a valid outcome
		}
		if snapshot == nil {
			t.Fatal("Load returned nil snapshot without error")
		}
		// The exact profile accessor is unexported; snapshot cannot be used
		// directly. Instead reuse ReadEvidence which returns the profile.
		profile, digest, err := reportdiff.ReadEvidence(bytes.NewReader(data))
		if err != nil {
			return
		}
		if profile == nil {
			t.Fatal("ReadEvidence returned nil profile without error")
		}
		if len(digest) != 64 {
			t.Fatalf("digest length %d", len(digest))
		}
		for _, sel := range selections {
			r, err := planning.Build(context.Background(), planning.Input{
				Profile: profile, ReportSHA256: digest,
				Capabilities: capabilities.Dircue("test"), Selection: sel,
			})
			if err != nil {
				continue // Build's own rejections are fine
			}
			if r == nil {
				t.Fatal("Build returned nil report on nil error")
			}
			if len(r.Steps) > planning.MaxRequests {
				t.Fatalf("step limit exceeded: %d > %d", len(r.Steps), planning.MaxRequests)
			}
			if len(r.Evidence) > planning.MaxEvidence {
				t.Fatalf("evidence limit exceeded: %d > %d", len(r.Evidence), planning.MaxEvidence)
			}
			for _, step := range r.Steps {
				if step.Command.Executable {
					t.Fatalf("planner produced executable step: %+v", step.Command)
				}
				for _, arg := range step.Command.Argv {
					if strings.Contains(arg, "\x00") {
						t.Fatalf("argv carries NUL: %q", arg)
					}
					// The planner must never leak the caller's declared root
					// (arbitrary or malicious) into argv.
					if profile.Root != "" && strings.Contains(arg, profile.Root) {
						t.Fatalf("argv leaks profile.Root %q: %q", profile.Root, arg)
					}
				}
			}
		}
	})
}
