package declarations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"dircue/pkg/declarations"
	"dircue/pkg/profile"
	"dircue/pkg/reportdiff"
)

// Exercise the producer/consumer boundary as well as the individual parsers.
// Malformed manifests must still yield a valid, qualified saved report.
func FuzzManifestReportContract(f *testing.F) {
	f.Add(uint8(0), []byte(`{"name":"root","workspaces":["member"],"scripts":{"test":"do not execute"}}`))
	f.Add(uint8(1), []byte("module example.invalid/root\ngo 1.24\nreplace example.invalid/member => ./member\n"))
	f.Add(uint8(2), []byte("go 1.24\nuse ./member\n"))
	f.Add(uint8(3), []byte("[project]\nname='root'\nversion='1.0'\n[tool.uv.workspace]\nmembers=['member']\n"))
	f.Add(uint8(4), []byte("[workspace]\nmembers=['member']\n"))
	f.Add(uint8(5), []byte("<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>"))
	f.Add(uint8(6), []byte("<project><artifactId>root</artifactId></project>"))
	names := []string{"package.json", "go.mod", "go.work", "pyproject.toml", "Cargo.toml", "App.csproj", "pom.xml"}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		c := declarations.New("directory", "", 0)
		name := "project/" + names[int(kind)%len(names)]
		c.Add(name, &declarations.Candidate{Path: name, Size: int64(len(data)), Read: func(context.Context, int64) ([]byte, int64, error) {
			return data, int64(len(data)), nil
		}})
		module, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		p := profile.Report{SchemaVersion: profile.DeclarationsSchemaVersion, Root: "/fixture", Declarations: module,
			Languages: []profile.Language{}, Ecosystems: []profile.Finding{}, Frameworks: []profile.Finding{}, Layouts: []profile.Finding{}, Warnings: []profile.Warning{}}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := reportdiff.Load(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("manifest emitted an invalid report: %v\n%s", err, encoded)
		}
		compared, err := reportdiff.Compare(snapshot, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range compared.Modules {
			if result.Counts.Added+result.Counts.Removed+result.Counts.Changed != 0 {
				t.Fatal("a saved report differs from itself")
			}
		}
	})
}
