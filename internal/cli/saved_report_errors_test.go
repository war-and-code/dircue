package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestSavedReportOpenErrorsRetainCauseWithoutLeakingPath(t *testing.T) {
	privatePath := t.TempDir() + "/private-value-missing.json"
	for _, args := range [][]string{
		{"plan", privatePath, "--module", "metrics"},
		{"compare", privatePath, privatePath},
		{"analyze", "explain", "--report", privatePath, "--file", "main.go"},
	} {
		var out, stderr bytes.Buffer
		err := Execute(t.Context(), args, &out, &stderr)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%v: missing-file cause lost: %v", args, err)
		}
		public := ""
		if err != nil {
			public = err.Error()
		}
		if err == nil || strings.Contains(public, "private-value") {
			t.Errorf("%v: unsafe public error %q", args, public)
		}
		if out.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("%v: stdout=%q stderr=%q", args, out.String(), stderr.String())
		}
	}
}
