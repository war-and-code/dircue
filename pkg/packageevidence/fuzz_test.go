package packageevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const minimalNative = `{"artifacts":[],"artifactRelationships":[],"files":[],"source":{"id":"source","name":"fixture","version":"","type":"directory","metadata":{}},"descriptor":{"name":"syft","version":"1.52.0"},"schema":{"version":"16.1.10","url":"https://example.invalid/schema"},"distro":{}}`

func FuzzImport(f *testing.F) {
	for _, seed := range []string{"", "{}", minimalNative, `{"artifacts":[],"artifacts":[]}`, strings.Replace(minimalNative, `"source"`, `"\u0073ource"`, 1), strings.Replace(minimalNative, `"distro":{}`, `"distro":{"x":[null,true,1e999999,"x"]}`, 1)} {
		f.Add([]byte(seed))
	}
	data, err := os.ReadFile("../../tests/packageevidence/fixtures/syft-1.52.0.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		before := string(data)
		r, err := Import(context.Background(), bytes.NewReader(data), Options{Limits: Limits{Bytes: 64 << 10}})
		if string(data) != before {
			t.Fatal("input bytes mutated")
		}
		if err != nil {
			if r != nil || !(errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrLimit)) {
				t.Fatalf("unexpected result/error: %v %v", r, err)
			}
			return
		}
		if r == nil || r.ReportSHA256 != digest(data) || !utf8.Valid(data) {
			t.Fatal("accepted report identity or encoding mismatch")
		}
		again, err := Import(context.Background(), bytes.NewReader(data), Options{Limits: Limits{Bytes: 64 << 10}})
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("non-deterministic import")
		}
	})
}

func FuzzPackageLocations(f *testing.F) {
	for _, seed := range [][3]string{{"name", "pkg:npm/name@1", "/src/packages.lock.json"}, {"user:secret@host", "pkg:npm/a%23token=secret", "/src/user%3Asecret%40host"}, {"name", "pkg:npm/%40scope/name@1", "/src/%2e%2e/path"}, {"", "", "../outside"}, {"name", "pkg:nuget/Name@1", "/src/archive.jar:inside.jar"}} {
		f.Add(seed[0], seed[1], seed[2], uint8(0))
	}
	f.Fuzz(func(t *testing.T, name, purl, path string, mode uint8) {
		if len(name)+len(purl)+len(path) > 4096 {
			t.Skip()
		}
		var doc map[string]any
		_ = json.Unmarshal([]byte(minimalNative), &doc)
		doc["artifacts"] = []any{map[string]any{"id": "package", "name": name, "version": "1", "type": "npm", "foundBy": "javascript-package-cataloger", "locations": []any{map[string]any{"path": path, "accessPath": path}}, "licenses": []any{}, "language": "javascript", "cpes": []any{}, "purl": purl}}
		data, _ := json.Marshal(doc)
		r, err := Import(context.Background(), bytes.NewReader(data), Options{})
		if err != nil {
			t.Fatalf("valid generated report rejected: %v", err)
		}
		before, _ := json.Marshal(r)
		c := Context{ProjectStatus: "complete", Projects: []Project{{ID: "Root.csproj", Root: "."}, {ID: "src/App.csproj", Root: "src"}}, Mapping: &Mapping{ReportRoot: "/", InventoryRoot: "."}}
		if mode%3 == 1 {
			c.ProjectStatus = "partial"
		} else if mode%3 == 2 {
			c.SourceIdentity = Identity{Kind: "tree", Algorithm: "sha256", Digest: strings.Repeat("a", 64)}
			c.Binding = &Binding{ReportSHA256: strings.Repeat("0", 64), SourceIdentity: c.SourceIdentity}
		}
		out, err := Associate(r, c)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := json.Marshal(r)
		if !bytes.Equal(before, after) {
			t.Fatal("association mutated input")
		}
		loc := out.Packages[0].Locations[0]
		if mode%3 != 0 && len(loc.ProjectIDs) > 0 {
			t.Fatal("association invented for partial or mismatched input")
		}
		if loc.Path != "" && !relative(loc.Path) {
			t.Fatal("mapped outside inventory")
		}
		var serialized Report
		_ = json.Unmarshal(before, &serialized)
		remapped, err := Associate(&serialized, c)
		if err != nil || remapped.Packages[0].Locations[0].Path != "" || len(remapped.Packages[0].Locations[0].ProjectIDs) != 0 {
			t.Fatal("serialized input invented raw evidence")
		}
	})
}

func FuzzPURL(f *testing.F) {
	for _, seed := range []string{"pkg:npm/name@1", "pkg:npm/%40scope/name@1", "pkg:npm/name%23token=secret", "pkg:npm/a%2523secret", "pkg:npm/user%3Asecret%40host", "pkg:maven/org.example/a@1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 8192 {
			t.Skip()
		}
		out := safePURL(value)
		if out == "" {
			return
		}
		if out != value || safePURL(out) != out {
			t.Fatal("PURL sanitizer changes accepted evidence")
		}
		decoded := strings.TrimPrefix(out, "pkg:")
		for strings.Contains(decoded, "%") {
			var err error
			decoded, err = url.PathUnescape(decoded)
			if err != nil {
				t.Fatal("accepted malformed escaping")
			}
		}
		if strings.ContainsAny(decoded, "?#\\\x00") || strings.Contains(decoded, "//") || strings.Contains(decoded, ":") && strings.Contains(decoded, "@") {
			t.Fatalf("accepted hidden URL details: %q", value)
		}
	})
}
