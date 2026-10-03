package schema_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/internal/cli"
	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/profile"
	private "github.com/war-and-code/dircue/schema"
)

func TestEnvironmentGitTreeSchemaMatchesVersionedNativeContract(t *testing.T) {
	oracle := parityOracle(t)
	var out, diagnostics bytes.Buffer
	if err := cli.Execute(context.Background(), []string{"analyze", "environments", "--source", "directory", "--json", t.TempDir()}, &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var original profile.Report
	if err := json.Unmarshal(out.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tree    string
		legacy, valid bool
	}{
		{"current exact tree", strings.Repeat("a", 40), false, true},
		{"current opaque tree", "not-a-tree", false, false},
		{"current uppercase tree", strings.Repeat("A", 40), false, false},
		{"current nonhex tree", strings.Repeat("g", 40), false, false},
		{"current longer hash", strings.Repeat("a", 64), false, false},
		{"current trailing newline", strings.Repeat("a", 40) + "\n", false, false},
		{"legacy opaque identity", "retained-legacy-identity", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := original
			env := *original.Environments
			p.Environments = &env
			// Keep the required declarations module. Cross-module source
			// correlation is a separate report-loader contract.
			env.Source, env.Tree = "git", tc.tree
			if tc.legacy {
				env.ProviderVersion = environments.LegacyProviderVersion
				env.SemanticsReference = "https://learn.microsoft.com/en-us/dotnet/core/tools/global-json (last updated 2026-03-09; accessed 2026-09-21)"
				env.Limits.ToolchainFiles, env.Limits.ToolchainFileBytes, env.Limits.ToolchainInputBytes = 0, 0, 0
			}
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				t.Fatal(err)
			}
			for name, err := range map[string]error{
				"native":             environments.ValidateReport(&env),
				"bundled schema":     private.ValidateProfile(value),
				"independent schema": oracle.Validate(value),
			} {
				if (err == nil) != tc.valid {
					t.Errorf("%s valid=%t: %v", name, tc.valid, err)
				}
			}
		})
	}
}
