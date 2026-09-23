package intentmap

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestRootProjectOwnsRootFiles(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "go.mod", Root: "."}})
	source := []byte("package p\nimport \"net/http\"\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: "main.go", Size: int64(len(source)), Content: source})
	report, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range report.Observations {
		if observation.ProjectID != "go.mod" {
			t.Fatalf("root observation is unowned: %+v", observation)
		}
	}
}

func TestGoImportEvidenceUsesSourceLine(t *testing.T) {
	source := []byte("\n\npackage p\n\nimport \"net/http\"\n")
	observations := parseGoImports("main.go", source)
	if len(observations) == 0 {
		t.Fatal("expected import observation")
	}
	for _, observation := range observations {
		if observation.StartLine != 5 || observation.EndLine != 5 {
			t.Fatalf("evidence line = %d-%d, want 5", observation.StartLine, observation.EndLine)
		}
	}
}

func TestGoBinaryRequiresPackageMainAndMainFunction(t *testing.T) {
	observations := parseGoImports("cmd/tool/main.go", []byte("package main\n\nfunc main() {}\n"))
	found := false
	for _, o := range observations {
		found = found || o.Kind == KindInterface && o.Properties["interface_kind"] == "binary" && o.StartLine == 3
	}
	if !found {
		t.Fatalf("main package did not produce binary entry observation: %+v", observations)
	}
	for _, source := range []string{"package main\n", "package cli\nfunc main() {}\n"} {
		for _, o := range parseGoImports("main.go", []byte(source)) {
			if o.Kind == KindInterface {
				t.Fatalf("incomplete/non-main source overclaimed binary: %+v", o)
			}
		}
	}
}

func TestAppsettingsRedisAndEventBusCapabilitiesUseKeyEvidence(t *testing.T) {
	d := New(Options{})
	body := []byte(`{"ConnectionStrings":{"Redis":"redis://secret"},"EventBus":"amqps://user:secret@rabbitmq.example/path","RabbitMQ":{"HostName":"broker"}}`)
	if _, err := d.Detect(context.Background(), profile.File{Path: "appsettings.json", Size: int64(len(body)), Content: body}); err != nil {
		t.Fatal(err)
	}
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundRedis, foundAMQP, foundBus := false, false, false
	for _, o := range r.Observations {
		if o.Kind != KindCapability {
			continue
		}
		switch o.Name {
		case "cache:redis":
			foundRedis = true
		case "messaging:amqp":
			foundAMQP = true
		case "messaging:event-bus":
			foundBus = true
		}
	}
	if !foundRedis || !foundAMQP || !foundBus {
		t.Fatalf("missing appsettings capability evidence: %+v", r.Observations)
	}
	encoded, _ := json.Marshal(r)
	for _, forbidden := range []string{"secret", "rabbitmq.example", "user", "amqps://"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("configuration value %q leaked: %s", forbidden, encoded)
		}
	}
}

func TestDotnetRedisCachingPackageDeclaresCapability(t *testing.T) {
	// The pinned microservices-demo cartservice project references this NuGet
	// package directly; the absence of an appsettings key must not hide it.
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{
		ID: "src/cartservice/src/cartservice.csproj", Root: "src/cartservice/src", Kind: "dotnet",
		Requirements: []declarations.Requirement{{
			Kind: "package-reference", Value: "Microsoft.Extensions.Caching.StackExchangeRedis@10.0.11",
			State: "declared", Evidence: "src/cartservice/src/cartservice.csproj",
		}},
	}})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range r.Observations {
		if observation.Kind == KindCapability && observation.Name == "cache:redis" && observation.Basis == "declared_dependency" {
			return
		}
	}
	t.Fatalf("missing declared Redis caching capability: %+v", r.Observations)
}

func TestDeclaredPythonRequirementsCreateCapabilitiesAndEntryInterfaces(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "svc/pyproject.toml", Root: "svc", Kind: "python", Requirements: []declarations.Requirement{{Kind: "python-dependency", Value: "redis>=5", State: "declared", Evidence: "svc/pyproject.toml"}, {Kind: "python-dependency", Value: "aio-pika>=9", State: "declared", Evidence: "svc/pyproject.toml"}}, Interfaces: []declarations.Interface{{Kind: "python-console-script", Name: "svc", Target: "svc.cli:main", State: "declared", Evidence: "svc/pyproject.toml"}}}})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundRedis, foundAMQP, foundEntry := false, false, false
	for _, o := range r.Observations {
		foundRedis = foundRedis || o.Kind == KindCapability && o.Name == "cache:redis" && o.Basis == "declared_dependency"
		foundAMQP = foundAMQP || o.Kind == KindCapability && o.Name == "messaging:amqp" && o.Basis == "declared_dependency"
		foundEntry = foundEntry || o.Kind == KindInterface && o.Name == "svc" && o.Properties["target"] == "svc.cli:main"
	}
	if !foundRedis || !foundAMQP || !foundEntry {
		t.Fatalf("missing declared Python evidence: %+v", r.Observations)
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

func TestCandidateScopeAndLexicalObservationLimit(t *testing.T) {
	goSource := []byte("package p\nimport \"net/http\"\n")
	run := func(reverse bool) *Report {
		d := New(Options{MaxObservations: 3})
		for i := 0; i < 10; i++ {
			index := i
			if reverse {
				index = 9 - i
			}
			name := fmt.Sprintf("p/%02d.go", index)
			_, _ = d.Detect(context.Background(), profile.File{Path: name, Size: int64(len(goSource)), Content: goSource})
		}
		// A large non-candidate must not consume this observer's budget.
		_, _ = d.Detect(context.Background(), profile.File{Path: "logs/huge.xml", Size: 1 << 30})
		r, err := d.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a, b := run(false), run(true)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("bounded results differ by delivery order:\n%+v\n%+v", a, b)
	}
	if a.Coverage.SelectedFiles != 10 || len(a.Observations) != 3 || a.Observations[0].Path != "p/00.go" || a.Observations[2].Path != "p/02.go" {
		t.Fatalf("wrong candidate scope or retained set: %+v", a)
	}
}
