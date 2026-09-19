package packageevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestNativeFieldAliasesCannotReplaceEvidence(t *testing.T) {
	for name, alter := range map[string]func(map[string]any){
		"artifacts":  func(v map[string]any) { v["Artifacts"] = []any{} },
		"schema":     func(v map[string]any) { v["schema"].(map[string]any)["Version"] = SupportedSchema },
		"source":     func(v map[string]any) { v["source"].(map[string]any)["Type"] = "image" },
		"descriptor": func(v map[string]any) { v["descriptor"].(map[string]any)["Version"] = "wrong" },
		"package":    func(v map[string]any) { v["artifacts"].([]any)[0].(map[string]any)["Locations"] = []any{} },
		"location": func(v map[string]any) {
			v["artifacts"].([]any)[0].(map[string]any)["locations"].([]any)[0].(map[string]any)["Path"] = "/different"
		},
		"file": func(v map[string]any) { v["files"].([]any)[0].(map[string]any)["ID"] = "new-id" },
		"file-location": func(v map[string]any) {
			v["files"].([]any)[0].(map[string]any)["location"].(map[string]any)["Path"] = "/different"
		},
		"relationship": func(v map[string]any) { v["artifactRelationships"].([]any)[0].(map[string]any)["Child"] = "wrong" },
	} {
		t.Run(name, func(t *testing.T) { mustFail(t, edit(t, alter), ErrInvalid, Options{}) })
	}
	data := edit(t, func(v map[string]any) {
		v["source"].(map[string]any)["metadata"] = map[string]any{"Name": "one", "name": "two"}
	})
	if _, err := Import(context.Background(), bytes.NewReader(data), Options{}); err != nil {
		t.Fatalf("case-sensitive arbitrary metadata rejected: %v", err)
	}
	mustFail(t, []byte(strings.Replace(minimalNative, `"artifacts":[]`, `"artifacts":[],"\u0061rtifacts":[]`, 1)), ErrInvalid, Options{})
}

func TestEncodedCredentialLocationsAndPURLFragmentsAreRedacted(t *testing.T) {
	for _, purl := range []string{"pkg:npm/name%23token=canary", "pkg:npm/name%2523token=canary", "pkg:npm/name%3ftoken=canary"} {
		if safePURL(purl) != "" {
			t.Fatalf("fragment/query retained: %q", purl)
		}
	}
	for _, raw := range []string{"/src/user%3Acanary%40host", "/src/user%253Acanary%2540host", "/src/https%3A%2F%2Fhost%3Ftoken=canary"} {
		if mapped, ok := mapPath(raw, Mapping{ReportRoot: "/", InventoryRoot: "."}); ok {
			t.Fatalf("credential coordinate exposed: %q", mapped)
		}
	}
	// Native filesystem coordinates are not URLs: percent escapes must never
	// become separators or parent traversal when associating projects.
	if mapped, ok := mapPath("/src/%2e%2e/pkg", Mapping{ReportRoot: "/", InventoryRoot: "."}); !ok || mapped != "src/%2e%2e/pkg" {
		t.Fatalf("native path decoded: %q %v", mapped, ok)
	}
}

func TestLocationIdentityHasUnambiguousBoundaries(t *testing.T) {
	a := newLocation(nativeLocation{Path: "a\x00b", AccessPath: "c"})
	b := newLocation(nativeLocation{Path: "a", AccessPath: "b\x00c"})
	if a.OriginalSHA256 == b.OriginalSHA256 {
		t.Fatal("distinct location pairs share evidence identity")
	}
}

func TestSerializedAssociationRetainsUnknownCoordinatesAndPartialCoverage(t *testing.T) {
	r := imported(t)
	encoded, _ := json.Marshal(r)
	var serialized Report
	_ = json.Unmarshal(encoded, &serialized)
	serialized.Coverage.Import = "partial"
	serialized.Status = "partial"
	out, err := Associate(&serialized, testContext(r))
	if err != nil || out.Status != "partial" {
		t.Fatalf("partial imported evidence upgraded: %v %+v", err, out)
	}
	for _, p := range out.Packages {
		for _, l := range p.Locations {
			if l.State != "unmapped" || l.Path != "" || len(l.ProjectIDs) > 0 {
				t.Fatalf("unknown original coordinate guessed: %+v", l)
			}
		}
	}
	ctx := testContext(r)
	ctx.Projects[0].ID = string([]byte{0xff})
	if _, err := Associate(r, ctx); !errors.Is(err, ErrContext) {
		t.Fatal("invalid UTF-8 project identity accepted")
	}
}

func TestLimitsAtIntegerAndArrayBoundaries(t *testing.T) {
	for _, limits := range []Limits{{Bytes: math.MaxInt64}, {Artifacts: math.MaxInt}, {Relationships: -1}, {Files: -1}, {Locations: -1}, {Artifacts: 100001}, {Relationships: 250001}, {Files: 100001}, {Locations: 250001}} {
		if _, err := effectiveLimits(limits); !errors.Is(err, ErrLimit) {
			t.Fatalf("unbounded limits accepted: %+v", limits)
		}
	}
	if got, err := effectiveLimits(Limits{}); err != nil || !reflect.DeepEqual(got, DefaultLimits()) {
		t.Fatal("zero limits changed defaults")
	}
	data := []byte(minimalNative)
	if _, err := Import(context.Background(), bytes.NewReader(data), Options{Limits: Limits{Bytes: int64(len(data))}}); err != nil {
		t.Fatal(err)
	}
	mustFail(t, data, ErrLimit, Options{Limits: Limits{Bytes: int64(len(data) - 1)}})
	// Reject oversized typed arrays before allocating every RawMessage. Invalid
	// trailing bytes distinguish this early bound from eventual full decoding.
	for _, data := range []string{`{"artifacts":[{},BROKEN`, `{"artifactRelationships":[{},BROKEN`, `{"files":[{},BROKEN`, `{"artifacts":[{"locations":[{},BROKEN`} {
		err := validateJSON(context.Background(), []byte(data), Limits{Artifacts: 1, Relationships: 1, Files: 1, Locations: 1})
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("array bound not enforced before second item: %s: %v", data, err)
		}
	}
}

type cancelAtEOFReader struct {
	data   *bytes.Reader
	cancel context.CancelFunc
}

func (r cancelAtEOFReader) Read(p []byte) (int, error) {
	n, err := r.data.Read(p)
	if r.data.Len() == 0 {
		r.cancel()
	}
	return n, err
}

func TestCancellationAfterReadReturnsNoReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := Import(ctx, cancelAtEOFReader{bytes.NewReader([]byte(minimalNative)), cancel}, Options{})
	if r != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read yielded report: %+v %v", r, err)
	}
}
