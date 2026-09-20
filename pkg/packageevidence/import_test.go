package packageevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func imported(t *testing.T) *Report {
	t.Helper()
	r, err := Import(context.Background(), bytes.NewReader(fixture(t)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func edit(t *testing.T, modify func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(fixture(t), &value); err != nil {
		t.Fatal(err)
	}
	modify(value)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func mustFail(t *testing.T, data []byte, want error, options Options) {
	t.Helper()
	r, err := Import(context.Background(), bytes.NewReader(data), options)
	if r != nil || !errors.Is(err, want) {
		t.Fatalf("report=%v error=%v want=%v", r, err, want)
	}
}
func testContext(r *Report) Context {
	return Context{ProjectStatus: "complete", Projects: []Project{{ID: "root/pom.xml", Root: "."}, {ID: "dotnet/app/App.csproj", Root: "dotnet/app"}, {ID: "java/pom.xml", Root: "java"}}, Mapping: &Mapping{ReportRoot: "/", InventoryRoot: "."}, SourceIdentity: Identity{Kind: "git-tree", Algorithm: "git-sha1", Digest: strings.Repeat("a", 40)}, Binding: &Binding{ReportSHA256: r.ReportSHA256, SourceIdentity: Identity{Kind: "git-tree", Algorithm: "git-sha1", Digest: strings.Repeat("a", 40)}}}
}

func TestRealSyftFixture(t *testing.T) {
	r := imported(t)
	if r.Status != "complete" || r.Source.Match != "unknown" || r.Coverage.ProviderScan != "unknown" || r.Provider.Version != "1.52.0" || r.Provider.SchemaVersion != SupportedSchema {
		t.Fatalf("provenance: %+v", r)
	}
	if len(r.Packages) != 6 || len(r.Relationships) != 15 || len(r.Files) != 3 {
		t.Fatalf("counts packages=%d relationships=%d files=%d", len(r.Packages), len(r.Relationships), len(r.Files))
	}
	if r.ReportSHA256 != digest(fixture(t)) {
		t.Fatal("report digest")
	}
	counts := map[string]int{}
	for _, p := range r.Packages {
		counts[p.Basis]++
		if p.Locations[0].Path != "" {
			t.Fatal("implicit absolute path mapping")
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"built-artifact": 2, "built-artifact-manifest": 2, "lockfile-resolution": 2}) {
		t.Fatalf("basis: %+v", counts)
	}
	if len(r.Provider.Configuration.Catalogers) < 10 || r.Provider.Configuration.Settings["packages.java-archive.use-network"] {
		t.Fatal("configuration subset")
	}
	mapped, err := Associate(r, testContext(r))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range mapped.Packages {
		if p.Name == "inner" {
			if p.Locations[0].State != "nested-archive" || len(p.Locations[0].ProjectIDs) != 0 || p.Locations[0].AccessPath != "java/app.jar:BOOT-INF/lib/inner.jar" {
				t.Fatalf("nested: %+v", p)
			}
		} else {
			if p.Locations[0].Association != "matched" || len(p.Locations[0].ProjectIDs) != 1 {
				t.Fatalf("owner: %+v", p)
			}
		}
	}
	types := map[string]int{}
	for _, edge := range mapped.Relationships {
		types[edge.Type]++
		if edge.ParentKind == "unknown" || edge.ChildKind == "unknown" {
			t.Fatal("lost endpoint")
		}
	}
	if types["contains"] != 6 || types["dependency-of"] != 3 || types["evident-by"] != 6 {
		t.Fatalf("relationships changed meaning: %+v", types)
	}
	if r.Source.Match != "unknown" || r.Packages[0].Locations[0].Path != "" {
		t.Fatal("Associate mutated input")
	}
}

func TestMalformedAndIncompleteReports(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("{}"), append(fixture(t), []byte(" {}")...), fixture(t)[:100], []byte(`{"schema":{},"schema":{}}`), edit(t, func(v map[string]any) { delete(v, "artifacts") }), edit(t, func(v map[string]any) { v["artifacts"] = nil }), edit(t, func(v map[string]any) { v["files"] = nil }), edit(t, func(v map[string]any) { delete(v["source"].(map[string]any), "metadata") })} {
		mustFail(t, data, ErrInvalid, Options{})
	}
	mustFail(t, edit(t, func(v map[string]any) { v["schema"].(map[string]any)["version"] = "16.1.11" }), ErrUnsupported, Options{})
	mustFail(t, edit(t, func(v map[string]any) { p := v["artifacts"].([]any); v["artifacts"] = append(p, p[0]) }), ErrInvalid, Options{})
	mustFail(t, edit(t, func(v map[string]any) {
		p := v["artifactRelationships"].([]any)
		v["artifactRelationships"] = append(p, p[0])
	}), ErrInvalid, Options{})
	mustFail(t, []byte(strings.Replace(string(fixture(t)), `"name": "Example.Core"`, `"name":"ignored","name": "Example.Core"`, 1)), ErrInvalid, Options{})
}

func TestLimitsAndCancellation(t *testing.T) {
	for _, limits := range []Limits{{Bytes: 10}, {Artifacts: 5}, {Relationships: 14}, {Files: 2}, {Locations: 8}, {Bytes: -1}, {Bytes: 129 << 20}} {
		mustFail(t, fixture(t), ErrLimit, Options{Limits: limits})
	}
	depth := []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))
	mustFail(t, depth, ErrLimit, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Import(ctx, bytes.NewReader(fixture(t)), Options{})
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v %+v", err, r)
	}
}

func TestSourceIdentityUnknownMatchedAndStale(t *testing.T) {
	r := imported(t)
	ctx := testContext(r)
	ctx.Binding = nil
	out, err := Associate(r, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source.Match != "unknown" {
		t.Fatal("invented identity")
	}
	for _, p := range out.Packages {
		if p.Name != "inner" && p.Locations[0].Association != "source-unverified" {
			t.Fatalf("unknown identity ownership: %+v", p)
		}
	}
	for _, change := range []func(*Context){func(c *Context) { c.Binding.ReportSHA256 = strings.Repeat("0", 64) }, func(c *Context) { c.SourceIdentity.Digest = strings.Repeat("b", 40) }} {
		ctx = testContext(r)
		change(&ctx)
		out, err = Associate(r, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != "partial" || out.Source.Match != "mismatched" {
			t.Fatalf("stale report: %+v", out)
		}
		for _, p := range out.Packages {
			for _, l := range p.Locations {
				if len(l.ProjectIDs) > 0 {
					t.Fatal("stale ownership retained")
				}
			}
		}
	}
}

func TestAmbiguousUnassignedExternalAndNested(t *testing.T) {
	r := imported(t)
	ctx := testContext(r)
	ctx.Projects = []Project{{ID: "java/A/pom.xml", Root: "java"}, {ID: "java/B/pom.xml", Root: "java"}}
	out, err := Associate(r, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Packages {
		switch p.Name {
		case "app":
			if p.Locations[0].Association != "ambiguous" || len(p.Locations[0].ProjectIDs) != 2 {
				t.Fatalf("ambiguity: %+v", p)
			}
		case "inner":
			if p.Locations[0].Association != "nested-archive" {
				t.Fatal("archive guessed")
			}
		default:
			if p.Locations[0].Association != "unassigned" {
				t.Fatal("unassigned guessed")
			}
		}
	}
	ctx.Mapping = &Mapping{ReportRoot: "/other-root", InventoryRoot: "."}
	out, err = Associate(r, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Packages {
		if p.Locations[0].State != "external" || p.Locations[0].Path != "" {
			t.Fatal("external path exposed")
		}
	}
	ctx = testContext(r)
	ctx.Projects[0].Root = "../outside"
	if _, err = Associate(r, ctx); !errors.Is(err, ErrContext) {
		t.Fatal("invalid context accepted")
	}
}

func TestRedactionAndNoArbitraryMetadata(t *testing.T) {
	data := edit(t, func(v map[string]any) {
		v["source"].(map[string]any)["metadata"] = map[string]any{"path": "/Users/private/secret", "password": "secret-value"}
		v["descriptor"].(map[string]any)["configuration"].(map[string]any)["auth"] = map[string]any{"url": "https://user:password@example.test?token=secret-value"}
		p := v["artifacts"].([]any)[0].(map[string]any)
		p["purl"] = "pkg:nuget/Example@1?repository_url=https://user:password@example.test"
		p["metadata"].(map[string]any)["token"] = "secret-value"
		l := p["locations"].([]any)[0].(map[string]any)
		l["path"] = "/Users/private/secret"
		l["accessPath"] = "/Users/private/secret"
	})
	r, err := Import(context.Background(), bytes.NewReader(data), Options{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	for _, forbidden := range []string{"/Users/", "secret-value", "user:password", "fixture-cache", "repository_url"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("exposed %s", forbidden)
		}
	}
	if r.Status != "partial" {
		t.Fatal("redacted unsupported PURL not disclosed")
	}
}

func TestUnknownRelationshipsAndPartialMarkers(t *testing.T) {
	data := edit(t, func(v map[string]any) {
		v["status"] = "partial"
		edges := v["artifactRelationships"].([]any)
		edges[0].(map[string]any)["child"] = "missing-node"
		edges[1].(map[string]any)["type"] = "possible-future-edge"
	})
	r, err := Import(context.Background(), bytes.NewReader(data), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "partial" || r.Coverage.ProviderScan != "unknown" || len(r.Relationships) != 15 {
		t.Fatalf("partial evidence lost: %+v", r)
	}
	found := false
	for _, edge := range r.Relationships {
		if edge.Type == "possible-future-edge" {
			found = true
			if edge.State != "unsupported-type" {
				t.Fatal("unknown edge silently supported")
			}
		}
	}
	if !found {
		t.Fatal("unknown relationship omitted")
	}
}

func BenchmarkImportSyftFixture(b *testing.B) {
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if _, err := Import(context.Background(), bytes.NewReader(data), Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCredentialFormsAndScopedPURL(t *testing.T) {
	for _, value := range []string{"user:password@host", "https%3A%2F%2Fuser%3Apassword%40host", "https%253A%252F%252Fhost", "%2FUsers%2Fprivate", "private\tvalue", "user%3Apassword%40host"} {
		if safeText(value) != "" {
			t.Fatalf("unsafe text accepted: %q", value)
		}
		if safePURL("pkg:npm/"+value) != "" {
			t.Fatalf("unsafe purl accepted: %q", value)
		}
	}
	for _, value := range []string{"pkg:npm/%40scope/name@1.2.3", "pkg:nuget/Example.Core@1.2.3", "pkg:maven/org.example/app@1.0.0"} {
		if safePURL(value) != value {
			t.Fatalf("safe purl lost: %q", value)
		}
	}
	mustFail(t, edit(t, func(v map[string]any) { v["artifacts"].([]any)[0].(map[string]any)["id"] = "user:password@host" }), ErrInvalid, Options{})
}

func TestEmptyValidReportHasUnknownProviderCoverage(t *testing.T) {
	data := edit(t, func(v map[string]any) {
		v["artifacts"] = []any{}
		v["artifactRelationships"] = []any{}
		v["files"] = []any{}
	})
	r, err := Import(context.Background(), bytes.NewReader(data), Options{})
	if err != nil || r.Status != "complete" || r.Coverage.ProviderScan != "unknown" || len(r.Packages) != 0 {
		t.Fatalf("empty evidence: %+v %v", r, err)
	}
}

func TestAmbiguousAccessAndImageSources(t *testing.T) {
	for _, image := range []bool{false, true} {
		data := edit(t, func(v map[string]any) {
			if image {
				v["source"].(map[string]any)["type"] = "image"
			} else {
				p := v["artifacts"].([]any)[0].(map[string]any)
				p["locations"].([]any)[0].(map[string]any)["accessPath"] = "/different/packages.lock.json"
			}
		})
		r, err := Import(context.Background(), bytes.NewReader(data), Options{})
		if err != nil {
			t.Fatal(err)
		}
		out, err := Associate(r, testContext(r))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range out.Packages {
			for _, l := range p.Locations {
				if image && (l.State != "external" || len(l.ProjectIDs) != 0) {
					t.Fatal("image assigned directory ownership")
				}
				if l.State == "ambiguous-access-path" {
					found = true
					if len(l.ProjectIDs) != 0 {
						t.Fatal("ambiguous access assigned ownership")
					}
				}
			}
		}
		if !image && !found {
			t.Fatal("access path disagreement lost")
		}
	}
}

func TestInvalidEncodingAndDigestLengths(t *testing.T) {
	mustFail(t, append([]byte{0xff}, fixture(t)...), ErrInvalid, Options{})
	result := metadata("java-archive", json.RawMessage(`{"digest":[{"algorithm":"sha256","value":"abcdef"},{"algorithm":"sha1","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`))
	if len(result.Digests) != 1 || result.Digests[0].Algorithm != "sha1" {
		t.Fatalf("invalid digest accepted: %+v", result)
	}
	for _, bad := range []string{"../x", "x/../y", "x\ty", "x\x1by", "C:/Users/x"} {
		if _, ok := mapPath(bad, Mapping{ReportRoot: ".", InventoryRoot: "."}); ok {
			t.Fatalf("unsafe mapping: %q", bad)
		}
	}
}

func TestIncompleteProjectInventoryWithholdsOwnership(t *testing.T) {
	r := imported(t)
	for _, status := range []string{"", "unknown", "partial", "skipped"} {
		c := testContext(r)
		c.ProjectStatus = status
		// A missing nested project must not hand its packages to the enclosing root.
		c.Projects = c.Projects[:1]
		out, err := Associate(r, c)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != "partial" || out.Coverage.Import != "complete" || out.Source.Match != "matched" {
			t.Fatalf("incomplete attribution conflated with import: %+v", out)
		}
		for _, p := range out.Packages {
			for _, l := range p.Locations {
				if len(l.ProjectIDs) != 0 {
					t.Fatalf("incomplete project inventory assigned %+v", l)
				}
			}
		}
	}
	c := testContext(r)
	c.ProjectStatus = "invented"
	if _, err := Associate(r, c); !errors.Is(err, ErrContext) {
		t.Fatal("unknown project status accepted")
	}
}

func TestRepeatedAssociationDoesNotRetainOldMismatch(t *testing.T) {
	r := imported(t)
	c := testContext(r)
	c.Binding.ReportSHA256 = strings.Repeat("0", 64)
	stale, err := Associate(r, c)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Associate(stale, testContext(r))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "complete" || len(restored.Diagnostics) != 0 || restored.Source.Match != "matched" {
		t.Fatalf("stale association retained: %+v", restored)
	}
}
