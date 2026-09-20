package reportdiff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"dircue/pkg/declarations"
)

func benchmarkInput(b *testing.B, changed bool) []byte {
	b.Helper()
	p := emptyProfile()
	p.SchemaVersion = "1.4.0"
	var err error
	p.Declarations, err = declarations.New("directory", "", 0).Finish(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("packages/p%04d/package.json", i)
		node := ">=22"
		if changed && i%10 == 0 {
			node = ">=24"
		}
		p.Declarations.Projects = append(p.Declarations.Projects, declarations.Project{ID: name, Root: fmt.Sprintf("packages/p%04d", i), Kind: "npm", Name: fmt.Sprintf("package-%d", i), Version: "1.0.0", Requirements: []declarations.Requirement{{Kind: "npm-engine", Value: "node " + node, State: "declared", Evidence: name}}, References: []declarations.Reference{}, Interfaces: []declarations.Interface{{Kind: "script", Name: "test", State: "declared", Evidence: name}}})
	}
	p.Declarations.Coverage.SelectedFiles = 500
	p.Declarations.Coverage.ManifestCandidates = 500
	p.Declarations.Coverage.ParsedManifests = 500
	p.Declarations.Coverage.RetainedObservations = 1000
	data, err := json.Marshal(p)
	if err != nil {
		b.Fatal(err)
	}
	return data
}

func BenchmarkSavedReportLoad500Projects(b *testing.B) {
	data := benchmarkInput(b, false)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Load(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompare500Projects(b *testing.B) {
	a, err := Load(bytes.NewReader(benchmarkInput(b, false)))
	if err != nil {
		b.Fatal(err)
	}
	h, err := Load(bytes.NewReader(benchmarkInput(b, true)))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compare(a, h); err != nil {
			b.Fatal(err)
		}
	}
}
