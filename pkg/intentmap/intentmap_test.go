package intentmap

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/profile"
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

func TestGoNetHTTPOnlyAttributesOutboundClientUse(t *testing.T) {
	tests := []struct {
		name, source string
		wantClient   bool
	}{
		{name: "server handler", source: "package p\nimport \"net/http\"\nfunc handle(w http.ResponseWriter, r *http.Request) {}\n"},
		{name: "aliased server handler", source: "package p\nimport web \"net/http\"\nfunc handle(w web.ResponseWriter, r *web.Request) {}\n"},
		{name: "client type", source: "package p\nimport \"net/http\"\nvar client *http.Client\n", wantClient: true},
		{name: "client function", source: "package p\nimport web \"net/http\"\nfunc fetch() { _, _ = web.Get(\"https://example.test\") }\n", wantClient: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foundClient := false
			for _, observation := range parseGoImports("http.go", []byte(tt.source)) {
				if observation.Kind == KindCapability && observation.Name == "net:http-client" {
					foundClient = true
				}
			}
			if foundClient != tt.wantClient {
				t.Fatalf("outbound HTTP client capability = %v, want %v", foundClient, tt.wantClient)
			}
		})
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

// ── F-04: Interfaces recall ───────────────────────────────────────────────────

func TestGoInterfacesSurviveFullCapabilityHeap(t *testing.T) {
	// Regression for F-04. With a small heap and many capability observations,
	// the interface observation must not be evicted because kindPriority gives
	// interfaces the highest retention priority.
	d := New(Options{MaxObservations: 8})
	// Fill the heap with capability-producing imports.
	for i := 0; i < 20; i++ {
		src := "package p\nimport \"github.com/jackc/pgx/v5\"\n"
		path := fmt.Sprintf("svc/%02d.go", i)
		_, _ = d.Detect(context.Background(), profile.File{Path: path, Size: int64(len(src)), Content: []byte(src)})
	}
	// Now add a file that declares an interface (binary).
	mainSrc := []byte("package main\nfunc main() {}\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: "cmd/api/main.go", Size: int64(len(mainSrc)), Content: mainSrc})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.Kind == KindInterface && o.Name == "api" && o.Properties["interface_kind"] == "binary" {
			return
		}
	}
	t.Fatalf("binary interface was evicted from full heap; observations: %+v", r.Observations)
}

func TestGoBinaryNameUsesParentDirectory(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"cmd/loki/main.go", "loki"},
		{"cmd/querier/main.go", "querier"},
		{"main.go", "main"},
		{"api/main.go", "api"},
	}
	for _, tt := range tests {
		got := goBinaryName(tt.path)
		if got != tt.want {
			t.Errorf("goBinaryName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDockerfileExposeProducesInterfaceObservation(t *testing.T) {
	content := []byte("FROM alpine\nEXPOSE 3000\nEXPOSE 8080/tcp\nRUN echo ok\nEXPOSE 9090 9091\n")
	obs := parseDockerfileExpose("services/api/Dockerfile", content)
	wantPorts := map[string]bool{"3000": false, "8080": false, "9090": false, "9091": false}
	for _, o := range obs {
		if o.Kind != KindInterface {
			t.Errorf("unexpected kind %v", o.Kind)
		}
		if o.Properties["interface_kind"] != "declared_port" {
			t.Errorf("interface_kind = %q, want declared_port", o.Properties["interface_kind"])
		}
		port := o.Properties["port"]
		if _, ok := wantPorts[port]; !ok {
			t.Errorf("unexpected port %q", port)
		}
		wantPorts[port] = true
		if o.Name != "port:"+port {
			t.Errorf("name = %q, want port:%s", o.Name, port)
		}
	}
	for port, seen := range wantPorts {
		if !seen {
			t.Errorf("port %q not detected", port)
		}
	}
}

// ── F-10: Capability recall ───────────────────────────────────────────────────

func TestConfigCapabilityDBHostProducesRelational(t *testing.T) {
	// DB_HOST is an unambiguous relational-database key. The catalog maps it to
	// datastore:relational rather than a vendor-specific name because the key
	// alone does not distinguish PostgreSQL from MySQL.
	d := New(Options{})
	content := []byte("DB_HOST=db.example\nDB_NAME=production\nDB_USER=app\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: ".env.production.sample", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.Kind == KindCapability && o.Name == "datastore:relational" {
			return
		}
	}
	t.Fatalf("DB_HOST did not produce datastore:relational; observations: %+v", r.Observations)
}

func TestConfigCapabilityESHostProducesSearchElasticsearch(t *testing.T) {
	d := New(Options{})
	content := []byte("ES_HOST=elasticsearch.example\nES_PORT=9200\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: ".env.production.sample", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.Kind == KindCapability && o.Name == "search:elasticsearch" {
			return
		}
	}
	t.Fatalf("ES_HOST did not produce search:elasticsearch; observations: %+v", r.Observations)
}

func TestConfigCapabilitySMTPServerProducesMessagingSMTP(t *testing.T) {
	d := New(Options{})
	content := []byte("SMTP_SERVER=smtp.mailgun.org\nSMTP_PORT=587\nSMTP_FROM_ADDRESS=noreply@example.com\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: ".env.production.sample", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.Kind == KindCapability && o.Name == "messaging:smtp" {
			return
		}
	}
	t.Fatalf("SMTP_SERVER did not produce messaging:smtp; observations: %+v", r.Observations)
}

func TestConfigCapabilityS3BucketProducesStorageObject(t *testing.T) {
	// S3_BUCKET maps to storage:object (the catalog does not distinguish S3 from
	// MinIO or other compatible stores given only a bucket key).
	d := New(Options{})
	content := []byte("S3_ENABLED=true\nS3_BUCKET=my-prod-bucket\nS3_REGION=us-east-1\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: ".env.production.sample", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.Kind == KindCapability && o.Name == "storage:object" {
			return
		}
	}
	t.Fatalf("S3_BUCKET did not produce storage:object; observations: %+v", r.Observations)
}

func TestConnectionStringURISchemeProducesCapability(t *testing.T) {
	// URI scheme detection via connectionStringCapability: postgres:// maps to
	// datastore:relational, redis:// maps to cache:redis.
	d := New(Options{})
	content := []byte("DATABASE_URL=postgres://user:secret@db.example/prod\nREDIS_URL=redis://cache.example:6379\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: ".env", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundPg, foundRedis := false, false
	for _, o := range r.Observations {
		if o.Kind != KindCapability {
			continue
		}
		if o.Name == "datastore:relational" {
			foundPg = true
		}
		if o.Name == "cache:redis" {
			foundRedis = true
		}
	}
	if !foundPg {
		t.Error("postgres:// URI did not produce datastore:relational")
	}
	if !foundRedis {
		t.Error("redis:// URI did not produce cache:redis")
	}
}

// ── P0: Attribution honesty ───────────────────────────────────────────────────

func TestContainmentAttributedObservationSetsProjectAttribution(t *testing.T) {
	// Regression for P0/ACC-F01: observations whose ProjectID is set by
	// directory-containment heuristic must carry ProjectAttribution =
	// "directory_containment", not an empty string.
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "app/go.mod", Root: "app"}})
	content := []byte("REDIS_HOST=cache.example\n")
	_, _ = d.Detect(context.Background(), profile.File{Path: "app/.env", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range r.Observations {
		if o.ProjectID != "app/go.mod" {
			continue
		}
		if o.ProjectAttribution != "directory_containment" {
			t.Errorf("observation %q projectID set by containment but ProjectAttribution = %q, want directory_containment", o.Name, o.ProjectAttribution)
		}
		return
	}
	t.Fatal("no observation found with projectID app/go.mod")
}

// ── Python import capability inference (#57 / #80) ───────────────────────────

func TestParsePythonImportsBasicImportForm(t *testing.T) {
	// `import jose` must match the auth:jwt catalog entry for python-jose.
	observations := parsePythonImports("app/auth.py", []byte("import jose\n"))
	found := false
	for _, o := range observations {
		if o.Kind == KindCapability && o.Name == "auth:jwt" && o.Basis == "imported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("import jose did not produce auth:jwt; got %+v", observations)
	}
}

func TestParsePythonImportsFromForm(t *testing.T) {
	// `from jose import jwt` must match auth:jwt.
	observations := parsePythonImports("app/auth.py", []byte("from jose import jwt\n"))
	found := false
	for _, o := range observations {
		if o.Kind == KindCapability && o.Name == "auth:jwt" && o.Basis == "imported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("from jose import jwt did not produce auth:jwt; got %+v", observations)
	}
}

func TestParsePythonImportsPsycopg2(t *testing.T) {
	// `import psycopg2` must match datastore:postgresql.
	observations := parsePythonImports("app/db.py", []byte("import psycopg2\n"))
	found := false
	for _, o := range observations {
		if o.Kind == KindCapability && o.Name == "datastore:postgresql" && o.Basis == "imported" {
			found = true
		}
	}
	if !found {
		t.Fatalf("import psycopg2 did not produce datastore:postgresql; got %+v", observations)
	}
}

func TestParsePythonImportsRelativeImportSkipped(t *testing.T) {
	// `from .models import User` is a relative import; no capability must be inferred.
	observations := parsePythonImports("app/views.py", []byte("from .models import User\n"))
	if len(observations) != 0 {
		t.Fatalf("relative import produced unexpected observations: %+v", observations)
	}
}

func TestParsePythonImportsCommentSkipped(t *testing.T) {
	// Comments must not be parsed as imports.
	observations := parsePythonImports("app/main.py", []byte("# import psycopg2\n"))
	if len(observations) != 0 {
		t.Fatalf("comment produced unexpected observations: %+v", observations)
	}
}

func TestParsePythonImportsSubmodule(t *testing.T) {
	// `from jose.jwt import decode` — only the top-level package jose is used.
	observations := parsePythonImports("app/auth.py", []byte("from jose.jwt import decode\n"))
	found := false
	for _, o := range observations {
		if o.Kind == KindCapability && o.Name == "auth:jwt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("from jose.jwt import decode did not produce auth:jwt; got %+v", observations)
	}
}

func TestPythonImportCapabilityViaDetect(t *testing.T) {
	// End-to-end: a .py file with FastAPI-style PostgreSQL and JWT imports must
	// produce both datastore:postgresql and auth:jwt capability observations.
	d := New(Options{})
	content := []byte(`from jose import jwt
import psycopg2

# DATABASE_URL = "postgres://..."  (not parsed; this is a comment)
`)
	_, _ = d.Detect(context.Background(), profile.File{Path: "app/main.py", Size: int64(len(content)), Content: content})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := map[string]bool{"datastore:postgresql": false, "auth:jwt": false}
	for _, o := range r.Observations {
		if o.Kind == KindCapability {
			wantCaps[o.Name] = true
		}
	}
	for cap, found := range wantCaps {
		if !found {
			t.Errorf("missing capability %q in observations: %+v", cap, r.Observations)
		}
	}
}

func TestCatalogPythonJoseEntries(t *testing.T) {
	// python-jose (PyPI name) and jose (import name) both must map to auth:jwt.
	for _, tc := range []struct{ kind, coord string }{
		{"python-dependency", "python-jose"},
		{"python-import", "jose"},
	} {
		caps := capabilitiesFor(tc.kind, tc.coord)
		coord := tc.coord
		found := false
		for _, c := range caps {
			if c == "auth:jwt" {
				found = true
			}
		}
		if !found {
			t.Errorf("catalog coordinate %q did not map to auth:jwt; got %v", coord, caps)
		}
	}
}

func TestCatalogPsycopg2Entry(t *testing.T) {
	// psycopg2 and psycopg2-binary both must map to datastore:postgresql.
	for _, coord := range []string{"psycopg2", "psycopg2-binary"} {
		caps := capabilitiesFor("python-dependency", coord)
		found := false
		for _, c := range caps {
			if c == "datastore:postgresql" {
				found = true
			}
		}
		if !found {
			t.Errorf("catalog coordinate %q did not map to datastore:postgresql; got %v", coord, caps)
		}
	}
}

func TestParsePythonImportsIndentedImportSkipped(t *testing.T) {
	// Indented imports (inside functions, try/except, TYPE_CHECKING guards) must
	// not produce capability observations.  Flask cli.py line 818 is an example:
	//   import cryptography  # noqa: F401  — indented inside a function body.
	content := []byte("def check_runtime():\n    import cryptography  # noqa: F401\n")
	observations := parsePythonImports("src/flask/cli.py", content)
	for _, o := range observations {
		if o.Name == "crypto:library" {
			t.Fatalf("indented import produced unexpected capability %q at line %d; top-level-only rule violated", o.Name, o.StartLine)
		}
	}
}

func TestNpmDevDependenciesDoNotBecomeCapabilities(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "package.json", Root: ".", References: []declarations.Reference{
		{Kind: "npm-dependency", Value: "pg@^8.0.0", State: "declared", Evidence: "package.json", Condition: "dependencies"},
		{Kind: "npm-dependency", Value: "connect-redis@^8.0.1", State: "declared", Evidence: "package.json", Condition: "devDependencies"},
	}}})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, o := range r.Observations {
		if o.Kind == KindCapability {
			names[o.Name] = true
		}
	}
	if !names["datastore:postgresql"] || names["cache:redis"] {
		t.Fatalf("capabilities = %v, want datastore:postgresql only", names)
	}
}

func TestPythonDependencyGroupsDoNotBecomeCapabilities(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "pyproject.toml", Root: ".", Requirements: []declarations.Requirement{
		{Kind: "python-dependency", Value: "psycopg>=3", State: "declared", Evidence: "pyproject.toml"},
		{Kind: "python-dependency", Value: "pyyaml>=6", State: "conditional", Evidence: "pyproject.toml", Condition: "group:docs"},
	}}})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, o := range r.Observations {
		if o.Kind == KindCapability {
			names[o.Name] = true
		}
	}
	if !names["datastore:postgresql"] || names["serialization:yaml"] {
		t.Fatalf("capabilities = %v, want datastore:postgresql only", names)
	}
}

// TestPrismaSchemaProviderEmitsSpecificDatastoreCapability verifies that
// schema.prisma datasource.provider declarations emit the correct specific
// capability (N-01/N-02).
func TestPrismaSchemaProviderEmitsSpecificDatastoreCapability(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		wantCap string
		wantNot string
	}{
		{
			name:    "postgresql provider",
			content: "datasource db {\n  provider = \"postgresql\"\n  url      = env(\"DATABASE_URL\")\n}\n",
			wantCap: "datastore:postgresql",
			wantNot: "datastore:mysql",
		},
		{
			name:    "mysql provider",
			content: "datasource db {\n  provider = \"mysql\"\n  url      = env(\"DATABASE_URL\")\n}\n",
			wantCap: "datastore:mysql",
		},
		{
			name:    "mongodb provider",
			content: "datasource db {\n  provider = \"mongodb\"\n  url      = env(\"DATABASE_URL\")\n}\n",
			wantCap: "datastore:mongodb",
		},
		{
			name:    "sqlite provider maps to relational",
			content: "datasource db {\n  provider = \"sqlite\"\n  url      = \"file:./dev.db\"\n}\n",
			wantCap: "datastore:relational",
		},
		{
			name:    "generator provider before the datasource is not read",
			content: "generator client {\n  provider = \"go\"\n}\n\ndatasource db {\n  provider = \"mysql\"\n  url      = env(\"DATABASE_URL\")\n}\n",
			wantCap: "datastore:mysql",
		},
		{
			name:    "generator-only schema declares no datastore",
			content: "generator client {\n  provider = \"mongodb\"\n}\n",
			wantNot: "datastore:mongodb",
		},
		{
			name:    "cockroachdb is relational, not postgresql",
			content: "datasource db {\n  provider = \"cockroachdb\"\n}\n",
			wantCap: "datastore:relational",
			wantNot: "datastore:postgresql",
		},
		{
			name:    "env provider is ignored (runtime-resolved)",
			content: "datasource db {\n  provider = env(\"DB_PROVIDER\")\n  url      = env(\"DATABASE_URL\")\n}\n",
			wantNot: "datastore:postgresql",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := New(Options{})
			content := []byte(tt.content)
			_, _ = d.Detect(context.Background(), profile.File{Path: "prisma/schema.prisma", Size: int64(len(content)), Content: content})
			r, err := d.Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			caps := map[string]bool{}
			for _, o := range r.Observations {
				if o.Kind == KindCapability {
					caps[o.Name] = true
				}
			}
			if tt.wantCap != "" && !caps[tt.wantCap] {
				t.Errorf("want capability %q, got %v", tt.wantCap, caps)
			}
			if tt.wantNot != "" && caps[tt.wantNot] {
				t.Errorf("want no capability %q, got %v", tt.wantNot, caps)
			}
		})
	}
}
