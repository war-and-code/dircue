package lockfiles

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// FuzzAnalyzeLockfileContent covers npm and NuGet JSON lock paths
// through candidate association, selected reads, report construction, and
// validation. Inputs stay below the selected-file bound.
func FuzzAnalyzeLockfileContent(f *testing.F) {
	for _, seed := range []string{
		`{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}}}`,
		`{"version":1,"dependencies":{"net8.0":{"Newtonsoft.Json":{"type":"Direct","requested":"13.0.1","resolved":"13.0.1","contentHash":"abc"}}}}`,
		`{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":false}}}}`,
		`{"lockfileVersion":3,"packages":{"":{"dependencies":{"a":"1.0.0"}}},"x":` + string([]byte{0xff}),
		`<Project><ItemGroup><PackageReference Include="A" Version="1.0.0" /></ItemGroup></Project>`,
		`<packages><package id="A" version="1.0.0" /></packages>`,
		"\x00\xff",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > 64<<10 {
			t.Skip()
		}
		body := string(content)
		input := testInput(
			[]declarations.ProjectRecord{
				npmRecord("web", npmRef("a@1.0.0", "dependencies")),
				nugetRecord("dotnet", "dotnet/app.csproj"),
			},
			map[string]string{
				"web/package-lock.json":     body,
				"dotnet/packages.lock.json": body,
			},
			true,
		)
		first, err := Analyze(context.Background(), input, Limits{})
		if err != nil {
			t.Fatalf("Analyze rejected bounded lockfile contents: %v", err)
		}
		if err := ValidateReport(first); err != nil {
			t.Fatalf("ValidateReport rejected lockfile analysis: %v", err)
		}
		second, err := Analyze(context.Background(), input, Limits{})
		if err != nil {
			t.Fatalf("second Analyze rejected bounded lockfile contents: %v", err)
		}
		if err := ValidateReport(second); err != nil {
			t.Fatalf("second ValidateReport rejected lockfile analysis: %v", err)
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
			t.Fatal("lockfile analysis was nondeterministic")
		}
	})
}
