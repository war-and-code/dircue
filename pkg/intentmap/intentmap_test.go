package intentmap

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dircue/pkg/declarations"
	"dircue/pkg/profile"
)

func TestProtoServicesAreDeclaredWithoutPromotingRoutes(t *testing.T) {
	d := New(Options{})
	content := []byte("syntax = \"proto3\";\npackage shop.v1;\n// service Fake { rpc Nope(Empty) returns (Empty); }\nservice Cart {\n rpc Add(AddRequest) returns (AddReply);\n /* rpc Hidden(Empty) returns (Empty); */\n rpc Stream(stream Item) returns (stream Item);\n}")
	findings, err := d.Detect(context.Background(), profile.File{Path: "api/cart.proto", Size: int64(len(content)), Content: content})
	if err != nil || findings != nil {
		t.Fatalf("Detect() = %#v, %v", findings, err)
	}
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shop.v1.Cart", "shop.v1.Cart/Add", "shop.v1.Cart/Stream"}
	var got []string
	for _, o := range r.Observations {
		if o.Kind != KindInterface || o.Basis != "declared_contract" {
			t.Fatalf("unexpected observation: %+v", o)
		}
		if _, exists := o.Properties["route"]; exists {
			t.Fatalf("protobuf declaration claimed a route: %+v", o)
		}
		got = append(got, o.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
}

func TestCommentsStringsAndConfigValuesAreNotEvidence(t *testing.T) {
	d := New(Options{})
	goSource := []byte("package p\nimport \"github.com/jackc/pgx/v5\"\n// github.com/redis/go-redis/v9\nvar s = `github.com/redis/go-redis/v9`\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: "app/main.go", Size: int64(len(goSource)), Content: goSource})
	config := []byte("# REDIS_URL=comment\nDATABASE_URL=postgres://user:secret@example/db\nPORT=8080\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: "app/.env", Size: int64(len(config)), Content: config})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	text := string(b)
	for _, forbidden := range []string{"user:secret", "example/db", "github.com/redis"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"datastore:postgresql", "PORT", "DATABASE_URL"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %q: %s", required, text)
		}
	}
}

func TestExistingDeclarationsAreConvertedWithoutReadsAndOwnedByContainment(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "app/go.mod", Root: "app", Requirements: []declarations.Requirement{{Kind: "go-require", Value: "github.com/redis/go-redis/v9 v9.0.0", State: "declared", Evidence: "app/go.mod"}}, Interfaces: []declarations.Interface{{Kind: "binary", Name: "example/tool", State: "declared", Evidence: "app/go.mod"}}}})
	proto := []byte("service Health { rpc Check(Req) returns (Res); }")
	_, _ = d.Detect(context.Background(), profile.File{Path: "app/api/health.proto", Size: int64(len(proto)), Content: proto})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.ProjectID != "app/go.mod" {
			t.Fatalf("missing ownership: %+v", o)
		}
	}
}

func TestConcurrentDetectionIsDeterministicAndBounded(t *testing.T) {
	run := func() *Report {
		d := New(Options{MaxObservations: 8})
		var wg sync.WaitGroup
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b := []byte("package p\nimport \"net/http\"")
				_, _ = d.Detect(context.Background(), profile.File{Path: "main.go", Size: int64(len(b)), Content: b})
			}()
		}
		wg.Wait()
		r, err := d.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic:\n%+v\n%+v", a, b)
	}
	if a.Coverage.Status != "partial" || a.Coverage.Omissions["observation_limit"] == 0 || len(a.Observations) > 8 {
		t.Fatalf("bounds not reported: %+v", a)
	}
}

func TestOversizeAndCancellation(t *testing.T) {
	d := New(Options{MaxFileBytes: 4})
	_, _ = d.Detect(context.Background(), profile.File{Path: "x.proto", Size: 5, Content: []byte("12345")})
	r, _ := d.Finish(context.Background())
	if r.Coverage.Omissions["file_bytes"] != 1 {
		t.Fatalf("coverage: %+v", r.Coverage)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Detect(ctx, profile.File{}); err == nil {
		t.Fatal("expected cancellation")
	}
	if _, err := d.Finish(ctx); err == nil {
		t.Fatal("expected cancellation")
	}
}
