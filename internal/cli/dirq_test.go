package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func invokeAs(name string, args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := ExecuteAs(context.Background(), name, args, &out, &errOut)
	return out.String(), errOut.String(), err
}

// TestExecuteAsDircueHelpMatchesExecute verifies that ExecuteAs with "dircue"
// produces help output byte-identical to Execute for the root command.
func TestExecuteAsDircueHelpMatchesExecute(t *testing.T) {
	out1, _, _ := invoke("--help")
	out2, _, _ := invokeAs("dircue", "--help")
	if out1 != out2 {
		t.Errorf("Execute --help output differs from ExecuteAs(dircue) --help:\nExecute:\n%s\nExecuteAs:\n%s", out1, out2)
	}
}

// TestExecuteAsDirqHelpShowsDirq verifies that running as "dirq" shows "dirq"
// in help output, not "dircue".
func TestExecuteAsDirqHelpShowsDirq(t *testing.T) {
	out, _, _ := invokeAs("dirq", "--help")
	if !strings.Contains(out, "dirq") {
		t.Errorf("ExecuteAs(dirq) --help output does not contain 'dirq':\n%s", out)
	}
}

// TestExecuteAsDirqHelpNoDircue verifies the usage line says "dirq" not "dircue".
func TestExecuteAsDirqHelpNoDircueUsage(t *testing.T) {
	out, _, _ := invokeAs("dirq", "--help")
	// The usage line should say "dirq [path]" not "dircue [path]"
	if strings.Contains(out, "Usage:\n  dircue") {
		t.Errorf("ExecuteAs(dirq) --help still shows 'dircue' in Usage line:\n%s", out)
	}
}

// TestCapabilitiesCLIJSONIdentical verifies that capabilities --cli --json
// output is byte-identical between "dircue" and "dirq" invocations.
func TestCapabilitiesCLIJSONIdentical(t *testing.T) {
	outDircue, _, errDircue := invokeAs("dircue", "capabilities", "--cli", "--json")
	outDirq, _, errDirq := invokeAs("dirq", "capabilities", "--cli", "--json")
	if errDircue != nil {
		t.Fatalf("dircue capabilities --cli --json error: %v", errDircue)
	}
	if errDirq != nil {
		t.Fatalf("dirq capabilities --cli --json error: %v", errDirq)
	}
	if outDircue != outDirq {
		// Find first differing position to aid debugging
		minLen := len(outDircue)
		if len(outDirq) < minLen {
			minLen = len(outDirq)
		}
		diffPos := minLen
		for i := 0; i < minLen; i++ {
			if outDircue[i] != outDirq[i] {
				diffPos = i
				break
			}
		}
		start := diffPos - 30
		if start < 0 {
			start = 0
		}
		end := diffPos + 60
		if end > minLen {
			end = minLen
		}
		t.Errorf("capabilities --cli --json differs at byte %d:\ndircue[%d:%d]: %q\ndirq  [%d:%d]: %q",
			diffPos,
			start, end, outDircue[start:end],
			start, end, outDirq[start:end])
	}
}
