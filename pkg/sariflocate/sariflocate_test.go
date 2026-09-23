package sariflocate_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dircue/pkg/mapdoc"
	"dircue/pkg/sariflocate"
)

func fixtureMap(t *testing.T) mapdoc.Document {
	t.Helper()
	digest := &mapdoc.Digest{Algorithm: "sha256", Scope: "full_selected_tree", Value: strings.Repeat("a", 64)}
	c := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/go.mod"}, "go")
	c.Properties = map[string]string{"root": "services/api"}
	c.Coverage = complete()
	c.Evidence = []mapdoc.Evidence{evidence("services/api/go.mod", nil)}
	d := mapdoc.NewNode(mapdoc.NodeDeployable, []string{"services/api/Dockerfile"}, "container")
	d.Properties = map[string]string{"root": "services/api"}
	d.Coverage = complete()
	d.Evidence = []mapdoc.Evidence{evidence("services/api/Dockerfile", nil)}
	span := &mapdoc.Span{StartLine: 10, EndLine: 20}
	i := mapdoc.NewNode(mapdoc.NodeInterface, []string{"services/api/openapi.yaml"}, "GET /pets")
	i.Coverage = complete()
	i.Evidence = []mapdoc.Evidence{evidence("services/api/openapi.yaml", span)}
	content := mapdoc.NewNode(mapdoc.NodeContent, []string{"services/api/generated/client.go"}, "role:generated")
	content.Properties = map[string]string{"role": "generated"}
	content.Coverage = complete()
	content.Evidence = []mapdoc.Evidence{evidence("services/api/generated/client.go", nil)}
	doc := mapdoc.Document{SchemaVersion: mapdoc.SchemaVersion, Kind: "map", Status: mapdoc.CoverageComplete, Source: mapdoc.Source{Mode: "directory", Digest: digest}, Coverage: []mapdoc.QuestionCoverage{}, Nodes: []mapdoc.Node{c, d, i, content}, Edges: []mapdoc.Edge{}}
	normalized, err := mapdoc.Normalize(doc)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}
func complete() mapdoc.Coverage {
	return mapdoc.Coverage{Status: mapdoc.CoverageComplete, Reasons: []string{}}
}
func evidence(p string, s *mapdoc.Span) mapdoc.Evidence {
	return mapdoc.Evidence{Basis: mapdoc.BasisDeclaredConfig, Path: p, Span: s, SourceKind: mapdoc.SourceConfiguration, Rule: &mapdoc.Producer{ID: "test", Version: "1"}}
}

func sarif(uri, base string, line int, revision string) []byte {
	loc := map[string]any{"physicalLocation": map[string]any{"artifactLocation": map[string]any{"uri": uri}, "region": map[string]any{"startLine": line}}, "properties": map[string]any{"emitter": "kept"}, "unknownLocationField": true}
	if base != "" {
		loc["physicalLocation"].(map[string]any)["artifactLocation"].(map[string]any)["uriBaseId"] = base
	}
	result := map[string]any{"ruleId": "R1", "level": "warning", "message": map[string]any{"text": "unchanged"}, "properties": map[string]any{"result": "kept"}, "locations": []any{loc}}
	run := map[string]any{"tool": map[string]any{"driver": map[string]any{"name": "lint", "version": "2"}}, "results": []any{result}, "unknownRunField": "kept"}
	if base != "" {
		run["originalUriBaseIds"] = map[string]any{"SRCROOT": map[string]any{"uri": "file:///repo/"}}
	}
	if revision != "" {
		run["versionControlProvenance"] = []any{map[string]any{"revisionId": revision}}
	}
	b, _ := json.Marshal(map[string]any{"version": "2.1.0", "runs": []any{run}, "unknownRootField": 42})
	return b
}

func TestAnnotatesAndPreservesUnknownAndSemanticFields(t *testing.T) {
	doc := fixtureMap(t)
	out, summary, err := sariflocate.Annotate(sarif("services/api/generated/client.go", "", 3, ""), doc, sariflocate.Options{Digest: doc.Source.Digest})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if json.Unmarshal(out, &got) != nil {
		t.Fatal("invalid output")
	}
	run := got["runs"].([]any)[0].(map[string]any)
	result := run["results"].([]any)[0].(map[string]any)
	loc := result["locations"].([]any)[0].(map[string]any)
	if got["unknownRootField"] != float64(42) || run["unknownRunField"] != "kept" || result["ruleId"] != "R1" || result["level"] != "warning" || loc["unknownLocationField"] != true {
		t.Fatalf("fields changed: %s", out)
	}
	props := loc["properties"].(map[string]any)
	if props["emitter"] != "kept" {
		t.Fatal("existing property lost")
	}
	a := props[sariflocate.PropertyName].(map[string]any)
	if a["resolution"] != "resolved" || a["content_role"] != "generated" || len(a["components"].([]any)) != 1 || len(a["deployables"].([]any)) != 1 {
		t.Fatalf("annotation=%v", a)
	}
	if summary.Runs[0].Binding != "matched" || summary.Resolutions["resolved"] != 1 || len(summary.NodeCounts) != 2 {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestURIBasePercentEncodingInterfaceSpanAndConfinement(t *testing.T) {
	doc := fixtureMap(t)
	for name, tc := range map[string]struct {
		input      []byte
		want       string
		interfaces int
	}{
		"base":       {input: sarif("services/api/openapi%2Eyaml", "SRCROOT", 12, ""), want: "resolved", interfaces: 1},
		"outside":    {input: sarif("../secret", "", 1, ""), want: "outside_root"},
		"bad escape": {input: sarif("bad%ZZ", "", 1, ""), want: "unresolvable_uri"},
		"remote URI": {input: sarif("https://example.invalid/main.go", "", 1, ""), want: "unresolvable_uri"},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := sariflocate.Annotate(tc.input, doc, sariflocate.Options{SourceURI: "/repo", Digest: doc.Source.Digest})
			if err != nil {
				t.Fatal(err)
			}
			a := annotation(t, out)
			if a["resolution"] != tc.want {
				t.Fatalf("%v", a)
			}
			if got := len(a["interfaces"].([]any)); got != tc.interfaces {
				t.Fatalf("interfaces=%d", got)
			}
		})
	}
}

func TestWindowsPathsAreCaseInsensitiveAndConfined(t *testing.T) {
	doc := fixtureMap(t)
	out, _, err := sariflocate.Annotate(sarif(`C:\Repo\SERVICES\API\generated\client.go`, "", 1, ""), doc, sariflocate.Options{SourceURI: `C:\repo`, Digest: doc.Source.Digest})
	if err != nil {
		t.Fatal(err)
	}
	a := annotation(t, out)
	if a["resolution"] != "resolved" || a["content_role"] != "generated" {
		t.Fatalf("%v", a)
	}
}

func TestRevisionMismatchPreventsAttribution(t *testing.T) {
	doc := fixtureMap(t)
	doc.Source = mapdoc.Source{Mode: "git", Revision: strings.Repeat("a", 40), Tree: "tree"}
	out, s, err := sariflocate.Annotate(sarif("services/api/generated/client.go", "", 1, strings.Repeat("b", 40)), doc, sariflocate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if annotation(t, out)["resolution"] != "not_in_snapshot" || s.Runs[0].Binding != "mismatch" || len(s.NodeCounts) != 0 {
		t.Fatalf("annotation=%v summary=%+v", annotation(t, out), s)
	}
}

func TestSymbolicRevisionCannotClaimBinding(t *testing.T) {
	doc := fixtureMap(t)
	doc.Source = mapdoc.Source{Mode: "git", Revision: "HEAD", Tree: "tree"}
	out, summary, err := sariflocate.Annotate(sarif("services/api/generated/client.go", "", 1, strings.Repeat("a", 40)), doc, sariflocate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Runs[0].Binding != "unknown" || annotation(t, out)["resolution"] != "resolved" {
		t.Fatalf("symbolic revision manufactured binding: annotation=%v summary=%+v", annotation(t, out), summary)
	}
}

func TestMultipleComponentsAreAmbiguous(t *testing.T) {
	doc := fixtureMap(t)
	other := mapdoc.NewNode(mapdoc.NodeComponent, []string{"services/api/package.json"}, "npm")
	other.Properties = map[string]string{"root": "services/api"}
	other.Coverage = complete()
	other.Evidence = []mapdoc.Evidence{evidence("services/api/package.json", nil)}
	doc.Nodes = append(doc.Nodes, other)
	out, _, err := sariflocate.Annotate(sarif("services/api/generated/client.go", "", 1, ""), doc, sariflocate.Options{Digest: doc.Source.Digest})
	if err != nil {
		t.Fatal(err)
	}
	a := annotation(t, out)
	if a["resolution"] != "ambiguous" || len(a["components"].([]any)) != 2 {
		t.Fatalf("%v", a)
	}
}

func TestHardLimits(t *testing.T) {
	doc := fixtureMap(t)
	_, _, err := sariflocate.Annotate(sarif("a", "", 1, ""), doc, sariflocate.Options{Limits: sariflocate.Limits{Bytes: 1, Runs: 1, Results: 1, Locations: 1}})
	if !errors.Is(err, sariflocate.ErrLimit) {
		t.Fatalf("err=%v", err)
	}
	_, _, err = sariflocate.Annotate(sarif("a", "", 1, ""), doc, sariflocate.Options{Limits: sariflocate.Limits{Bytes: 1 << 20, Runs: 1, Results: 1, Locations: 0}})
	if !errors.Is(err, sariflocate.ErrLimit) {
		t.Fatalf("err=%v", err)
	}
}

func annotation(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	loc := root["runs"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	return loc["properties"].(map[string]any)[sariflocate.PropertyName].(map[string]any)
}
