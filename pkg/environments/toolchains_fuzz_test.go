package environments

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// FuzzAnalyzeToolchainFiles exercises every selected toolchain parser through
// the bounded inventory/read pipeline. Every produced report must validate
// and repeated analyses of the same selected bytes must be deterministic.
func FuzzAnalyzeToolchainFiles(f *testing.F) {
	for _, seed := range []string{
		"3.12.1\n",
		"3.11.8\n3.12.2\n# pyenv multi-version\n",
		"lts/* # latest LTS\nfuture_option=value\n",
		"nightly-2025-01-01\n",
		"[toolchain]\nchannel = \"stable\"\ncomponents = [\"rustfmt\"]\n",
		"[toolchain]\nchannel = \"stable\"\ncomponents = { " + strings.Repeat("key.", 65) + "leaf = 1 }\n",
		"AKIAIOSFODNN7EXAMPLE",
		"\xff\x00",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > int(DefaultMaxToolchainFileBytes) {
			t.Skip()
		}
		body := string(content)
		files := map[string]string{
			".python-version":     body,
			".node-version":       body,
			".nvmrc":              body,
			"rust-toolchain":      body,
			"rust-toolchain.toml": body,
		}
		input := envInput(files, nil)
		first, err := Analyze(context.Background(), input, Limits{})
		if err != nil {
			t.Fatalf("Analyze rejected bounded toolchain contents: %v", err)
		}
		if err := ValidateReport(first); err != nil {
			t.Fatalf("ValidateReport rejected toolchain analysis: %v", err)
		}
		second, err := Analyze(context.Background(), input, Limits{})
		if err != nil {
			t.Fatalf("second Analyze rejected bounded toolchain contents: %v", err)
		}
		if err := ValidateReport(second); err != nil {
			t.Fatalf("second ValidateReport rejected toolchain analysis: %v", err)
		}
		firstJSON, err := json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		secondJSON, err := json.Marshal(second)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(firstJSON, secondJSON) {
			t.Fatal("toolchain analysis was nondeterministic")
		}
	})
}
