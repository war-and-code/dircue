package intentmap

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

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
		wantLine     int
	}{
		{name: "server handler", source: "package p\nimport \"net/http\"\nfunc handle(w http.ResponseWriter, r *http.Request) {}\n"},
		{name: "aliased server handler", source: "package p\nimport web \"net/http\"\nfunc handle(w web.ResponseWriter, r *web.Request) {}\n"},
		{name: "dot import is not attributed", source: "package p\nimport . \"net/http\"\nfunc fetch() { _ = Get }\n"},
		{name: "client type", source: "package p\nimport \"net/http\"\nvar client *http.Client\n", wantClient: true, wantLine: 3},
		{name: "aliased client function", source: "package p\nimport web \"net/http\"\nfunc fetch() {\n _, _ = web.Get(\"https://example.test\")\n}\n", wantClient: true, wantLine: 4},
		{name: "shadowed alias is not package use", source: "package p\nimport web \"net/http\"\nfunc handler(w web.ResponseWriter) {}\nfunc fetch(web struct{ Get func(string) }) {\n web.Get(\"not http\")\n}\n"},
		{name: "client transport", source: "package p\nimport \"net/http\"\nvar transport http.RoundTripper\n", wantClient: true, wantLine: 3},
		// NewRequest and NewRequestWithContext construct *http.Request values
		// heavily used in server-side test helpers (httptest recorder +
		// handler.ServeHTTP). They are not evidence of an outbound client at the
		// component level: any component making genuine outbound requests also
		// references http.Client, http.DefaultClient, or a method shortcut.
		{name: "NewRequest alone is not client evidence", source: "package p\nimport \"net/http\"\nfunc makeReq() {\n r, _ := http.NewRequest(\"GET\", \"/\", nil)\n _ = r\n}\n"},
		{name: "NewRequestWithContext alone is not client evidence", source: "package p\nimport \"net/http\"\nimport \"context\"\nfunc makeReq(ctx context.Context) {\n r, _ := http.NewRequestWithContext(ctx, \"GET\", \"/\", nil)\n _ = r\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foundClient := false
			// foundImport is a positive-edge latch: set to true when we see the
			// import observation at line 2, never reset to false by a subsequent
			// observation. The previous assignment pattern ("foundImport = cond")
			// would overwrite true with false if multiple observations appeared.
			foundImport := false
			for _, observation := range parseGoImports("http.go", []byte(tt.source)) {
				if observation.Kind == KindImport && observation.Name == "net/http" {
					if observation.StartLine == 2 && observation.EndLine == 2 {
						foundImport = true
					}
				}
				if observation.Kind == KindCapability && observation.Name == "net:http-client" {
					foundClient = true
					if observation.Basis != "code_syntax" || observation.StartLine != tt.wantLine || observation.EndLine != tt.wantLine {
						t.Errorf("client capability evidence = basis %q lines %d-%d, want code_syntax at line %d", observation.Basis, observation.StartLine, observation.EndLine, tt.wantLine)
					}
				}
			}
			if !foundImport {
				t.Errorf("net/http import observation should remain on line 2")
			}
			if foundClient != tt.wantClient {
				t.Fatalf("outbound HTTP client capability = %v, want %v", foundClient, tt.wantClient)
			}
		})
	}
}

// TestGoNetHTTPSubpackageRouting verifies that net/http sub-packages are handled
// per-policy: excluded sub-packages (pprof, httptest, fcgi, cgi) never emit
// net:http-client; client-only sub-packages (cookiejar, httptrace) emit it with
// basis "imported"; httputil requires a client-side symbol; unknown sub-packages
// fail closed.
func TestGoNetHTTPSubpackageRouting(t *testing.T) {
	tests := []struct {
		name       string
		importPath string
		source     string // package body after the import line
		wantClient bool
		wantBasis  string // "code_syntax" or "imported"; ignored when !wantClient
		wantLine   int    // capability line; ignored when !wantClient
	}{
		// Excluded: server-side or tool-only sub-packages.
		{name: "pprof never client", importPath: "net/http/pprof",
			source: "package p\nimport _ \"net/http/pprof\"\n"},
		{name: "httptest never client", importPath: "net/http/httptest",
			source: "package p\nimport \"net/http/httptest\"\nfunc h(w httptest.ResponseRecorder) {}\n"},
		{name: "fcgi never client", importPath: "net/http/fcgi",
			source: "package p\nimport \"net/http/fcgi\"\nvar _ = fcgi.Serve\n"},
		{name: "cgi never client", importPath: "net/http/cgi",
			source: "package p\nimport \"net/http/cgi\"\nvar _ = cgi.Serve\n"},
		// Client-side sub-packages: import alone is sufficient.
		{name: "cookiejar is client", importPath: "net/http/cookiejar",
			source:     "package p\nimport \"net/http/cookiejar\"\nvar _ *cookiejar.Jar\n",
			wantClient: true, wantBasis: "imported", wantLine: 2},
		{name: "httptrace is client", importPath: "net/http/httptrace",
			source:     "package p\nimport \"net/http/httptrace\"\nvar _ *httptrace.ClientTrace\n",
			wantClient: true, wantBasis: "imported", wantLine: 2},
		// httputil: client-side symbol triggers; server-only usage does not.
		{name: "httputil ReverseProxy is client", importPath: "net/http/httputil",
			source:     "package p\nimport \"net/http/httputil\"\nvar _ *httputil.ReverseProxy\n",
			wantClient: true, wantBasis: "code_syntax", wantLine: 3},
		{name: "httputil NewSingleHostReverseProxy is client", importPath: "net/http/httputil",
			source:     "package p\nimport \"net/http/httputil\"\nvar _ = httputil.NewSingleHostReverseProxy\n",
			wantClient: true, wantBasis: "code_syntax", wantLine: 3},
		{name: "httputil DumpRequestOut is client", importPath: "net/http/httputil",
			source:     "package p\nimport \"net/http/httputil\"\nvar _ = httputil.DumpRequestOut\n",
			wantClient: true, wantBasis: "code_syntax", wantLine: 3},
		{name: "httputil DumpRequest alone is not client", importPath: "net/http/httputil",
			source: "package p\nimport \"net/http/httputil\"\nvar _ = httputil.DumpRequest\n"},
		{name: "httputil ServerConn alone is not client", importPath: "net/http/httputil",
			source: "package p\nimport \"net/http/httputil\"\nvar _ *httputil.ServerConn\n"},
		// Unknown sub-package: fail closed.
		{name: "unknown subpackage fails closed", importPath: "net/http/internal",
			source: "package p\nimport \"net/http/internal\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foundClient := false
			for _, observation := range parseGoImports("http.go", []byte(tt.source)) {
				if observation.Kind == KindCapability && observation.Name == "net:http-client" {
					foundClient = true
					if tt.wantClient {
						if observation.Basis != tt.wantBasis {
							t.Errorf("client capability basis = %q, want %q", observation.Basis, tt.wantBasis)
						}
						if observation.StartLine != tt.wantLine || observation.EndLine != tt.wantLine {
							t.Errorf("client capability lines = %d-%d, want %d", observation.StartLine, observation.EndLine, tt.wantLine)
						}
					}
				}
			}
			if foundClient != tt.wantClient {
				t.Fatalf("outbound HTTP client capability = %v, want %v (import: %s)", foundClient, tt.wantClient, tt.importPath)
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

func TestAdditionalImportScannersBoundaries(t *testing.T) {
	cases := []struct {
		name, src string
		parse     func(string, []byte) []Observation
		want      string
		absent    bool
	}{
		{"java", "// import org.postgresql.Driver;\nimport static org.springframework.data.jpa.JpaRepository.*;\nclass A {}", parseJVMImports, "datastore:relational", false},
		{"java-string", "class A { String s = \"import org.postgresql.Driver;\"; }", parseJVMImports, "datastore:postgresql", true},
		{"csharp", "// using Npgsql;\nusing Db = Npgsql;\nclass A {}", parseDotnetImports, "datastore:postgresql", false},
		{"csharp-indented", "class A {\n using Npgsql;\n}", parseDotnetImports, "datastore:postgresql", true},
		{"typescript", "// import pg from 'pg';\nimport type { Pool } from 'pg';", parseJSImports, "datastore:postgresql", false},
		{"typescript-shadow", "const require = fake; require('pg');", parseJSImports, "datastore:postgresql", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			os := tt.parse("src/a", []byte(tt.src))
			found := false
			for _, o := range os {
				if o.Name == tt.want {
					found = true
				}
			}
			if found == tt.absent {
				t.Fatalf("found=%v want absent=%v observations=%+v", found, tt.absent, os)
			}
		})
	}
}

func TestMavenTestScopeFilteringIsMapPrivate(t *testing.T) {
	// A test declaration by itself grants no capability.
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{ID: "pom.xml", Requirements: []declarations.Requirement{{Kind: "maven-dependency", Value: "com.h2database:h2:2.2", State: "declared", Evidence: "pom.xml", Scope: "test"}}}})
	r, _ := d.Finish(context.Background())
	for _, o := range r.Observations {
		if o.Name == "datastore:relational" {
			t.Fatal("test-scoped Maven dependency promoted")
		}
	}
	// The same coordinate with runtime scope still grants evidence; filtering one test occurrence must not suppress its runtime sibling.
	d = New(Options{})
	reqs := []declarations.Requirement{{Kind: "maven-dependency", Value: "com.h2database:h2:2.2", State: "declared", Evidence: "pom.xml", Scope: "test"}, {Kind: "maven-dependency", Value: "com.h2database:h2:2.2", State: "declared", Evidence: "pom.xml", Scope: "compile"}, {Kind: "maven-dependency", Value: "org.postgresql:postgresql:42", State: "declared", Evidence: "pom.xml", Scope: "provided"}}
	d.AddDeclarations([]declarations.Project{{ID: "pom.xml", Requirements: reqs}})
	r, _ = d.Finish(context.Background())
	relational, postgres := false, false
	for _, o := range r.Observations {
		if o.Name == "datastore:relational" && o.Basis == "declared_dependency" {
			relational = true
		}
		if o.Name == "datastore:postgresql" && o.Basis == "declared_dependency" {
			postgres = true
		}
	}
	if !relational || !postgres {
		t.Fatalf("runtime duplicate or provided scope lost: %+v", r.Observations)
	}
	raw, _ := json.Marshal(reqs)
	if strings.Contains(string(raw), "Scope") || strings.Contains(string(raw), "scope") {
		t.Fatalf("map-private Maven scope leaked to declaration JSON: %s", raw)
	}
}

func TestLexicalImportEvidenceAdversarialBoundaries(t *testing.T) {
	tests := []struct {
		name, path, source, want, absent string
		qualifier                        string
	}{
		{"kotlin alias", "A.kt", "import org.postgresql.Driver as PgDriver", "datastore:postgresql", "", ""},
		{"java multiline", "A.java", "import org.\npostgresql.Driver;", "datastore:postgresql", "", ""},
		{"java text block", "A.java", "class A { String x = \"\"\"\nimport org.postgresql.Driver;\n\"\"\"; }\nimport org.postgresql.Driver;", "datastore:postgresql", "", ""},
		{"java namespace lookalike", "A.java", "import org.postgresqlish.Driver;", "", "datastore:postgresql", ""},
		{"C sharp raw literal", "A.cs", "class A { string s = \"\"\"\nusing Npgsql;\n\"\"\"; }\nusing Db = Npgsql;", "datastore:postgresql", "", ""},
		{"C sharp namespace lookalike", "A.cs", "using NpgsqlFake;", "", "datastore:postgresql", ""},
		{"C sharp false case", "A.cs", "using npgsql;", "", "datastore:postgresql", ""},
		{"Java false case", "A.java", "import Org.postgresql.Driver;", "", "datastore:postgresql", ""},
		{"C sharp multiline", "A.cs", "using Npgsql.\n EntityFrameworkCore.PostgreSQL;", "datastore:postgresql", "", ""},
		{"C sharp namespace using", "A.cs", "namespace X { using Npgsql; class C { void M() { using var x = new Npgsql(); } } }", "datastore:postgresql", "", ""},
		{"C sharp method using only", "A.cs", "namespace X { class C { void M() { using Npgsql; } } }", "", "datastore:postgresql", ""},
		{"VB apostrophe", "A.vb", "' Imports Npgsql\nImports Db = Npgsql", "datastore:postgresql", "", ""},
		{"VB namespace case insensitive", "A.vb", "Imports npgsql", "datastore:postgresql", "", ""},
		{"typescript template", "a.ts", "const x = `\nimport pg from 'pg';\n`;\nimport type { Pool } from 'pg';", "datastore:postgresql", "", "type_only"},
		{"typescript type import-equals require", "a.ts", `import type Pg = require("pg");`, "datastore:postgresql", "", "type_only"},
		{"typescript runtime import-equals require", "a.ts", `import Pg = require("pg");`, "datastore:postgresql", "", ""},
		{"commonjs string", "a.js", `const x = "require('pg')";`, "", "datastore:postgresql", ""},
		{"commonjs regex", "a.js", `const re = /require\('pg'\)/;`, "", "datastore:postgresql", ""},
		{"ES regex", "a.js", `const re = /import pg from 'pg'/;`, "", "datastore:postgresql", ""},
		{"commonjs destructured shadow", "a.js", `const { require } = fake; require('pg');`, "", "datastore:postgresql", ""},
		{"ES import with shadow", "a.ts", `const { require } = fake; import pg from 'pg';`, "datastore:postgresql", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Observation
			switch strings.ToLower(path.Ext(tt.path)) {
			case ".java", ".kt":
				got = parseJVMImports(tt.path, []byte(tt.source))
			case ".cs", ".vb":
				got = parseDotnetImports(tt.path, []byte(tt.source))
			default:
				got = parseJSImports(tt.path, []byte(tt.source))
			}
			found, absent, qual := false, false, ""
			for _, o := range got {
				if o.Name == tt.want && tt.want != "" {
					found = true
					qual = o.Properties["import_qualifier"]
				}
				if o.Name == tt.absent && tt.absent != "" {
					absent = true
				}
			}
			if tt.want != "" && !found {
				t.Fatalf("missing %s evidence: %+v", tt.want, got)
			}
			if tt.absent != "" && absent {
				t.Fatalf("false positive %s: %+v", tt.absent, got)
			}
			if tt.qualifier != "" && qual != tt.qualifier {
				t.Fatalf("import qualifier = %q observations=%+v", qual, got)
			}
		})
	}
}

func TestImportEvidenceUsesTestPathConvention(t *testing.T) {
	for _, p := range []string{"src/test/java/AppTest.java", "tests/test_db.py", "__tests__/db.test.ts", "pkg/client_test.go", "src/utils/test_helper.py", "src/generated/test_utils_test.py", "src/ThingTest.cs", "src/WidgetTestCase.java"} {
		if importEvidenceScope(p) != "test_path_convention" {
			t.Errorf("%q scope = %q", p, importEvidenceScope(p))
		}
	}
	for _, p := range []string{"src/main/java/App.java", "src/client.ts", "src/test_utils.ts", "src/test_helpers.js", "src/test_utils.go", "src/test_utils.cs", "src/Contest.java", "src/Latest.cs", "src/Latest.vb"} {
		if importEvidenceScope(p) != "non_test_path_convention" {
			t.Errorf("%q scope = %q", p, importEvidenceScope(p))
		}
	}
}

func TestJSImportParserIgnoresTemplateAndJSXText(t *testing.T) {
	sources := []string{
		"const copy = `require('pg') and import pg from 'pg'`;",
		"const copy = `outer ${`nested require('pg')`} import pg from 'pg'`;",
		"const copy = `${ /`/.test(value) ? '' : '' } import pg from 'pg'`;",
		"const view = <div>require('pg') import pg from 'pg'</div>;",
		"const view = <div>\nimport pg from 'pg'\nrequire('pg')\n</div>;",
		"const view = <>\nimport pg from 'pg'\nrequire('pg')\n</>;",
		"const view = <div>\nimport pg from 'pg'\nrequire('pg')",
	}
	for _, source := range sources {
		if got := parseJSImports("src/view.jsx", []byte(source)); len(got) != 0 {
			t.Errorf("non-code template/JSX text produced import evidence for %q: %+v", source, got)
		}
	}
	valid := parseJSImports("src/view.jsx", []byte("import pg from 'pg';"))
	if len(valid) == 0 || valid[0].Name != "datastore:postgresql" {
		t.Fatalf("valid source import no longer produces evidence: %+v", valid)
	}
	validAfterSelfClose := parseJSImports("src/view.jsx", []byte("const icon = <div />;\nimport pg from 'pg';"))
	if len(validAfterSelfClose) == 0 || validAfterSelfClose[0].Name != "datastore:postgresql" {
		t.Fatalf("valid import after self-closing JSX was masked: %+v", validAfterSelfClose)
	}
	validAfterComponentSelfClose := parseJSImports("src/view.jsx", []byte("const icon = <Icon />;\nimport pg from 'pg';"))
	if len(validAfterComponentSelfClose) == 0 || validAfterComponentSelfClose[0].Name != "datastore:postgresql" {
		t.Fatalf("valid import after self-closing JSX component was masked: %+v", validAfterComponentSelfClose)
	}
	validAfterRegexTemplate := parseJSImports("src/view.jsx", []byte("const copy = `${ /`/.test(value) ? '' : '' }`;\nimport pg from 'pg';"))
	if len(validAfterRegexTemplate) == 0 || validAfterRegexTemplate[0].Name != "datastore:postgresql" {
		t.Fatalf("valid import after a regex-containing template was masked: %+v", validAfterRegexTemplate)
	}
	validAfterTypeAssertion := parseJSImports("src/assertion.ts", []byte("const value = <number>1;\nimport pg from 'pg';"))
	if len(validAfterTypeAssertion) != 1 || validAfterTypeAssertion[0].Name != "datastore:postgresql" {
		t.Fatalf("valid import after a TypeScript angle-bracket assertion was masked: %+v", validAfterTypeAssertion)
	}
	importsAfterTypeEquals := parseJSImports("src/types.ts", []byte("import type Pg = require(\"pg\")\nimport redis from 'redis'"))
	qualifiers := map[string]string{}
	for _, observation := range importsAfterTypeEquals {
		qualifiers[observation.Name] = observation.Properties["import_qualifier"]
	}
	if qualifiers["datastore:postgresql"] != "type_only" || qualifiers["cache:redis"] != "" {
		t.Fatalf("semicolonless import-equals swallowed or upgraded a following import: %+v", importsAfterTypeEquals)
	}
}

func TestJSImportParserPreservesMixedDefaultAndTypeImports(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		typeOnly     bool
	}{
		{"default plus type specifier", `import Pg, { type Config } from "pg";`, false},
		{"value named type alias", `import { type as Pg } from "pg";`, false},
		{"type-only import of symbol as", `import { type as } from "pg";`, true},
		{"type specifier with alias", `import { type Config as Pg } from "pg";`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseJSImports("src/database.ts", []byte(tc.source))
			if len(got) != 1 || got[0].Name != "datastore:postgresql" {
				t.Fatalf("import did not produce PostgreSQL evidence: %+v", got)
			}
			isTypeOnly := got[0].Properties["import_qualifier"] == "type_only"
			if isTypeOnly != tc.typeOnly {
				t.Fatalf("type-only = %v, want %v: %+v", isTypeOnly, tc.typeOnly, got[0])
			}
		})
	}
}

func TestJSImportParserIgnoresRegexLiteralContents(t *testing.T) {
	falsePositiveCases := []struct {
		name   string
		source string
	}{
		{"array element", `const patterns = [/import pg from "pg";/];`},
		{"if consequent", `if (ready) /import pg from "pg";/.test(source);`},
		{"if block then expression", `if (ready) {} /import pg from "pg";/.test(source);`},
		{"typeof operand", `const kind = typeof /import pg from "pg";/;`},
		{"return operand", `function match() { return /import pg from "pg";/; }`},
	}
	for _, tc := range falsePositiveCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseJSImports("src/regex.js", []byte(tc.source)); len(got) != 0 {
				t.Fatalf("regex literal contents produced import evidence: %+v", got)
			}
		})
	}
	validAfterRegex := parseJSImports("src/regex.js", []byte("const pattern = /value/;\nimport pg from \"pg\";"))
	if len(validAfterRegex) != 1 || validAfterRegex[0].Name != "datastore:postgresql" {
		t.Fatalf("valid import after a regular expression was lost: %+v", validAfterRegex)
	}
	validAfterObjectDivision := parseJSImports("src/regex.js", []byte("const ratio = ({value: 4}) / 2;\nimport pg from \"pg\";"))
	if len(validAfterObjectDivision) != 1 || validAfterObjectDivision[0].Name != "datastore:postgresql" {
		t.Fatalf("division after an object expression hid a following import: %+v", validAfterObjectDivision)
	}
}

func TestLexerMasksNestedAndUnterminatedLiteralRegions(t *testing.T) {
	cases := []struct {
		name, lang, source string
		want               bool
	}{
		{"Kotlin nested comment", "kotlin", "/* outer /* import org.postgresql.Driver */ still comment */\nimport org.postgresql.Driver", true},
		{"C sharp four quote raw", "cs", "var s = \"\"\"\"\nusing Npgsql;\n\"\"\"\"; using Db = Npgsql;", true},
		{"JS unterminated quote", "js", "const x = \"require('pg')\nrequire('pg')", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tokens, _ := lexSource(tt.source, tt.lang)
			found := false
			for _, tk := range tokens {
				if (tt.lang == "kotlin" && tk.text == "import") || (tt.lang == "cs" && tk.text == "using") || (tt.lang == "js" && tk.text == "require") {
					found = true
				}
			}
			if found != tt.want {
				t.Fatalf("found code token=%v, want %v; tokens=%+v", found, tt.want, tokens)
			}
		})
	}
}

func TestLexerEscapeParityAndImportBindingShadow(t *testing.T) {
	if escapedAt(`\\\"`, 2) {
		t.Fatal("quote after an even number of backslashes was considered escaped")
	}
	if !escapedAt(`\"`, 1) {
		t.Fatal("quote after one backslash was not considered escaped")
	}
	got := parseJSImports("a.ts", []byte(`import { createRequire as require } from "node:module"; require("pg"); import type from "pg";`))
	count := 0
	for _, o := range got {
		if o.Name == "datastore:postgresql" {
			count++
		}
		if o.Properties["import_qualifier"] == "type_only" {
			t.Fatal("default binding named type was mislabeled type-only")
		}
	}
	if count != 1 {
		t.Fatalf("ES imports should survive local require binding; module evidence count=%d observations=%+v", count, got)
	}
}

func TestImportLexerTokenLimitMarksCoveragePartial(t *testing.T) {
	content := []byte("import org.postgresql.Driver;\n" + strings.Repeat(";", DefaultMaxLexicalTokensPerFile+10))
	d := New(Options{})
	if _, err := d.Detect(context.Background(), profile.File{Path: "src/test/java/DbTest.java", Size: int64(len(content)), Content: content}); err != nil {
		t.Fatal(err)
	}
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.Status != "partial" || r.Coverage.Omissions["import_token_limit"] != 1 {
		t.Fatalf("token overflow coverage = %+v", r.Coverage)
	}
	if len(r.Observations) == 0 || r.Observations[0].Name != "datastore:postgresql" {
		t.Fatalf("evidence before the bounded cutoff was lost: %+v", r.Observations)
	}
}

func TestImportLexerRetainsFixedTokenCountOnDenseSource(t *testing.T) {
	source := strings.Repeat(";", 1<<20)
	tokens, limited := lexSource(source, "cs")
	if !limited {
		t.Fatal("dense source did not report lexical token truncation")
	}
	if len(tokens) != DefaultMaxLexicalTokensPerFile {
		t.Fatalf("retained %d tokens; want fixed cap %d", len(tokens), DefaultMaxLexicalTokensPerFile)
	}
}

func BenchmarkLexVBREMIdentifiers(b *testing.B) {
	for _, n := range []int{8 << 10, 48 << 10} {
		source := strings.Repeat("x REM ", n/6)
		b.Run(fmt.Sprintf("%d-bytes", len(source)), func(b *testing.B) {
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for range b.N {
				_, _ = lexSource(source, "vb")
			}
		})
	}
}

func BenchmarkLexicalTokenLimitDenseSource(b *testing.B) {
	source := strings.Repeat(";", 1<<20)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = lexSource(source, "cs")
	}
}

func TestVBRemAndTypeScriptInlineTypeImports(t *testing.T) {
	vb := parseDotnetImports("src/App.vb", []byte("REM Imports Npgsql\nImports StackExchange.Redis"))
	foundRedis, foundPg := false, false
	for _, o := range vb {
		foundRedis = foundRedis || o.Name == "cache:redis"
		foundPg = foundPg || o.Name == "datastore:postgresql"
	}
	if !foundRedis || foundPg {
		t.Fatalf("VB REM/import classification wrong: %+v", vb)
	}
	ts := parseJSImports("src/db.ts", []byte("import { type Pool, type Client } from 'pg';"))
	for _, o := range ts {
		if o.Name == "datastore:postgresql" && o.Properties["import_qualifier"] == "type_only" {
			return
		}
	}
	t.Fatalf("inline type-only import missing qualifier: %+v", ts)
	mixed := parseJSImports("src/db.ts", []byte("import { type Pool, Client } from 'pg';"))
	for _, o := range mixed {
		if o.Name == "datastore:postgresql" && o.Properties["import_qualifier"] == "type_only" {
			t.Fatalf("mixed value/type import marked type-only: %+v", mixed)
		}
	}
}

func TestTruncatedJavaScriptDoesNotInferUnverifiedCommonJSImports(t *testing.T) {
	source := "import redis from 'redis'; require('pg');\n" + strings.Repeat(";", DefaultMaxLexicalTokensPerFile) + "\nfunction require(x) { return x; }"
	observations, limited := parseJSImportsBounded("src/cache.js", []byte(source))
	if !limited {
		t.Fatal("expected token limit")
	}
	hasESM := false
	for _, o := range observations {
		if o.Kind == KindCapability && o.Name == "datastore:postgresql" {
			t.Fatalf("inferred shadowed CommonJS capability: %+v", o)
		}
		if o.Kind == KindImport && o.Name == "pg" {
			t.Fatalf("inferred unverified require import: %+v", o)
		}
		if o.Kind == KindCapability && o.Name == "cache:redis" {
			hasESM = true
		}
	}
	if !hasESM {
		t.Fatalf("lost retained ESM evidence: %+v", observations)
	}
}

func TestJSImportPackageMatchingDoesNotNormalizeSourceSpecifiers(t *testing.T) {
	tests := []struct {
		name, source string
		want         string
	}{
		{"exact package", `import pg from "pg";`, "datastore:postgresql"},
		{"package subpath", `require("pg/lib/client");`, "datastore:postgresql"},
		{"scoped package subpath", `import s3 from "@aws-sdk/client-s3/dist/client";`, "storage:object"},
		{"case sensitive", `require("PG");`, ""},
		{"version-like at suffix", `require("redis@fake");`, ""},
		{"node builtin scheme", `import fs from "node:fs";`, ""},
		{"node builtin bare", `import path from "path";`, ""},
		{"malformed scoped package suffix", `import x from "@aws-sdk/client@fake";`, ""},
		{"uppercase scoped package name", `import x from "@aws-sdk/UPPER";`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJSImports("src/app.js", []byte(tt.source))
			found := false
			for _, observation := range got {
				if observation.Kind == KindCapability && observation.Name == tt.want && tt.want != "" {
					found = true
				}
			}
			if tt.want != "" && !found {
				t.Fatalf("missing %s from %q: %+v", tt.want, tt.source, got)
			}
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("unexpected source capability for %q: %+v", tt.source, got)
			}
		})
	}
}

func TestJSImportObservationClonesShortSpecifierFromLargeSource(t *testing.T) {
	source := strings.Repeat(" ", 1<<20) + `import pg from "pg";`
	tokens, limited := lexSource(source, "js")
	if limited {
		t.Fatal("short import in a large source unexpectedly hit token limit")
	}
	observations := parseJSImportsTokens("large.js", tokens, true)
	if len(observations) != 1 || observations[0].Properties["import"] != "pg" {
		t.Fatalf("unexpected import evidence: %+v", observations)
	}
	retained := observations[0].Properties["import"]
	sourceStart := uintptr(unsafe.Pointer(unsafe.StringData(source)))
	retainedStart := uintptr(unsafe.Pointer(unsafe.StringData(retained)))
	if retainedStart >= sourceStart && retainedStart < sourceStart+uintptr(len(source)) {
		t.Fatal("short specifier retained the large source string backing storage")
	}
}

func TestJVMAndDotnetImportObservationsCloneNamespaces(t *testing.T) {
	for _, tt := range []struct {
		name, lang, source, capability string
	}{
		{"java", "java", strings.Repeat(" ", 1<<20) + "import org.postgresql.Driver;", "datastore:postgresql"},
		{"csharp", "cs", strings.Repeat(" ", 1<<20) + "using Npgsql;", "datastore:postgresql"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tokens, limited := lexSource(tt.source, tt.lang)
			if limited {
				t.Fatal("short namespace in large source unexpectedly hit token limit")
			}
			var observations []Observation
			if tt.lang == "java" {
				observations = parseJVMImportsTokens("large.java", tt.lang, tokens)
			} else {
				observations = parseDotnetImportsTokens("large.cs", false, tokens)
			}
			if len(observations) != 1 || observations[0].Name != tt.capability {
				t.Fatalf("unexpected import evidence: %+v", observations)
			}
			retained := observations[0].Properties["import"]
			sourceStart := uintptr(unsafe.Pointer(unsafe.StringData(tt.source)))
			retainedStart := uintptr(unsafe.Pointer(unsafe.StringData(retained)))
			if retainedStart >= sourceStart && retainedStart < sourceStart+uintptr(len(tt.source)) {
				t.Fatal("namespace evidence retained the large source backing storage")
			}
		})
	}
}

func TestJSImportLongSubpathRetainsOnlyPackageRoot(t *testing.T) {
	source := `import client from "@aws-sdk/client-s3/` + strings.Repeat("x", 1<<20) + `";`
	tokens, limited := lexSource(source, "js")
	if limited {
		t.Fatal("one long string token unexpectedly hit token limit")
	}
	observations := parseJSImportsTokens("large.js", tokens, true)
	if len(observations) == 0 {
		t.Fatal("long valid subpath should corroborate the known package root")
	}
	for _, observation := range observations {
		if observation.Properties["import"] != "@aws-sdk/client-s3" || observation.Properties["import_representation"] != "package_root" {
			t.Fatalf("long subpath should retain only validated package root: %+v", observation)
		}
		retained := observation.Properties["import"]
		sourceStart := uintptr(unsafe.Pointer(unsafe.StringData(source)))
		retainedStart := uintptr(unsafe.Pointer(unsafe.StringData(retained)))
		if retainedStart >= sourceStart && retainedStart < sourceStart+uintptr(len(source)) {
			t.Fatal("package-root evidence retained the complete large source backing storage")
		}
	}
}
