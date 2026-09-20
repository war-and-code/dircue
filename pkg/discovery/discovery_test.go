package discovery

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestMetadataCandidatesAndOverlappingRoles(t *testing.T) {
	yes, no := true, false
	c := New("directory", "", 1000)
	c.Add(File{Path: "vendor/tests/AppTests.cs", Size: 10, Generated: &yes})
	c.Add(File{Path: "vendor/own.cs", Size: 20, Vendored: &no})
	c.Add(File{Path: "project/pom.xml", Size: 30})
	c.Add(File{Path: "logs/events.xml", Size: 2 << 30})
	c.Add(File{Path: "vendor/lib/libfoo.so.1.2", Size: 40})
	c.Add(File{Path: "unknown.something-new", Size: 50})
	r := c.Finish()
	if r.Inventory != (Counts{6, (2 << 30) + 150}) || r.ClassificationBytesRead != 0 {
		t.Fatalf("counts: %+v", r)
	}
	total := Counts{}
	for _, g := range r.Categories {
		total.Files += g.Files
		total.Bytes += g.Bytes
		if g.Name == "logs" {
			t.Fatal("XML must not imply logs")
		}
	}
	if total != r.Inventory {
		t.Fatalf("exclusive category totals: %+v", total)
	}
	got := map[string]Counts{}
	for _, g := range r.Roles {
		got[g.Name] = g.Counts
	}
	if got["generated"].Files != 1 || got["test_candidate"].Files != 1 || got["vendored"].Files != 2 {
		t.Fatalf("roles: %+v", r.Roles)
	}
	want := []Candidate{{"project/pom.xml", "project", "manifest", "maven", "filename", 30}, {"vendor/lib/libfoo.so.1.2", "vendor/lib", "artifact", "shared_library", "filename", 40}}
	if !reflect.DeepEqual(r.Candidates, want) {
		t.Fatalf("candidates: %+v", r.Candidates)
	}
}
func TestCandidateBudgetRetainsSeparateKindsAndExactCounts(t *testing.T) {
	collect := func(reverse bool) *Report {
		c := New("git", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10000)
		for n := 0; n < EvidenceLimitPerKind+20; n++ {
			i := n
			if reverse {
				i = EvidenceLimitPerKind + 19 - n
			}
			c.Add(File{Path: fmt.Sprintf("packages/%04d.jar", i), Size: 7})
		}
		c.Add(File{Path: "z/project/pom.xml", Size: 11})
		c.Add(File{Path: "Directory.Build.props", Size: 13})
		return c.Finish()
	}
	a, b := collect(false), collect(true)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatal("output depends on arrival order")
	}
	if a.Status != "partial" || a.OmittedCandidates["artifact"] != 20 || len(a.Candidates) != EvidenceLimitPerKind+2 {
		t.Fatalf("bounded evidence: %+v", a)
	}
	if a.Candidates[len(a.Candidates)-1].Path != "z/project/pom.xml" {
		t.Fatal("artifact population displaced manifest")
	}
	if a.Inventory.Files != EvidenceLimitPerKind+22 {
		t.Fatal("evidence cap altered metadata population")
	}
	for _, g := range a.CandidateCounts {
		if g.Name == "artifact" && g.Files != EvidenceLimitPerKind+20 {
			t.Fatal("artifact total truncated")
		}
	}
}
func TestCandidateFormatHintsDoNotInspectOrValidate(t *testing.T) {
	for _, tc := range []struct{ path, kind, format string }{
		{"App.csproj", "manifest", "dotnet"}, {"x/packages.lock.json", "manifest", "nuget_lock"}, {"x/app.nupkg", "artifact", "nuget_package"}, {"x/opaque.dll", "artifact", "dotnet_or_native_library"}, {"x/pkg.whl", "artifact", "python_wheel"}, {"x/go.work", "shared_configuration", "go_workspace"}, {"x/nuget.config", "shared_configuration", "nuget"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			_, _, got := classify(tc.path)
			if got == nil || got.Kind != tc.kind || got.Format != tc.format {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, name := range []string{"lib.so.secret", "x.xml", "x.log", "opaque", "looks.jar.txt"} {
		_, _, got := classify(name)
		if got != nil {
			t.Fatalf("unexpected candidate %s: %+v", name, got)
		}
	}
}
