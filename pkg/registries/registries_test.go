package registries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, path, content string) Configuration {
	t.Helper()
	c, err := Parse(path, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestNuGetIndependentDeclarations(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?><configuration>
<packageSources><clear/><add key="Public" value="https://api.nuget.org/v3/index.json"/>
<add key="Company feed" value="https://user:credential-sentinel@PACKAGES.example:443/private-secret/index.json?token=query-secret#fragment-secret"/>
<add key="Local" value="C:\private-secret\packages"/><add key="Dynamic" value="https://%PRIVATE_ENV%/index.json"/></packageSources>
<disabledPackageSources><add key="Company feed" value="false"/></disabledPackageSources>
<packageSourceMapping><clear/><packageSource key="Company feed"><package pattern="Company.*"/></packageSource></packageSourceMapping>
<packageSourceCredentials><Company><add key="ClearTextPassword" value="password-sentinel"/></Company></packageSourceCredentials>
<apikeys><add key="https://private-secret.example/path" value="api-key-sentinel"/></apikeys></configuration>`
	c := mustParse(t, "src/NuGet.Config", xml)
	if c.Status != "partial" || c.SyntaxStatus != "complete" || !c.DeclarationCountComplete || c.ObservedDeclarations != 9 || len(c.Declarations) != 9 {
		t.Fatalf("unexpected coverage: %+v", c)
	}
	d := c.Declarations
	if d[0].Operation != "clear" || d[0].Name != nil {
		t.Fatal(d[0])
	}
	if d[1].Endpoint.Origin != "https://api.nuget.org" || d[2].Endpoint.Origin != "https://packages.example:443" || d[2].Name.Value != "Company feed" {
		t.Fatal(d[1:3])
	}
	if *d[5].Disabled || d[5].Endpoint != nil || d[4].Endpoint.Status != "unresolved" || d[3].Endpoint.Status != "local_path" {
		t.Fatal(d[3:6])
	}
	if d[8].Operation != "map" || d[8].Pattern.Value != "Company.*" || d[8].Name.Value != "Company feed" {
		t.Fatal(d[8])
	}
	encoded, _ := json.Marshal(c)
	for _, secret := range []string{"credential-sentinel", "private-secret", "query-secret", "fragment-secret", "PRIVATE_ENV", "password-sentinel", "api-key-sentinel", "userinfo"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}
func TestNPMIndependentDeclarations(t *testing.T) {
	c := mustParse(t, "service/.npmrc", `# ignored
registry=https://user:password-sentinel@REGISTRY.example/base?token=query-secret#fragment-secret
@company:registry="https://feeds.example:8443/private-secret"
@other:registry='https://other.example/feed'
@dynamic:registry=${PRIVATE_ENV}
//registry.example/private-secret/:_authToken=auth-token-sentinel
email=email-sentinel@example.test
registry=https://last.example/path\#fragment ; comment-secret
`)
	if len(c.Declarations) != 5 || c.Declarations[0].Endpoint.Origin != "https://registry.example" || c.Declarations[1].Scope.Value != "@company" || c.Declarations[1].Endpoint.Origin != "https://feeds.example:8443" || c.Declarations[4].Endpoint.Origin != "https://last.example" {
		t.Fatalf("%+v", c)
	}
	encoded, _ := json.Marshal(c)
	for _, secret := range []string{"password-sentinel", "query-secret", "fragment-secret", "private-secret", "PRIVATE_ENV", "auth-token-sentinel", "email-sentinel", "comment-secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}
func TestEndpointAdversarial(t *testing.T) {
	for _, tt := range []struct{ input, status, origin string }{
		{"https://u:p@example.com:00080/private?secret=x#y", "origin", "https://example.com:80"},
		{"http://[2001:db8::1]:8080/secret", "origin", "http://[2001:db8::1]:8080"},
		{"https://127.0.0.1/secret", "origin", "https://127.0.0.1"},
		{"https://a@b@c.example/path", "origin", "https://c.example"},
		{"https://%HOST%/", "unresolved", ""}, {"https://example.com/${SECRET}", "unresolved", ""},
		{`\\server\secret`, "local_path", ""}, {"../private/feed", "local_path", ""},
		{"file:///private/feed", "unsupported", ""}, {"//host/path", "unsupported", ""},
		{"https://host:", "invalid", ""}, {"https://host:65536/", "invalid", ""},
		{"https://0127.0.0.1/", "invalid", ""}, {"https://[fe80::1%25en0]/", "invalid", ""},
		{"https://xn--bcher-kva.example/path", "origin", "https://xn--bcher-kva.example"},
		{"https://bücher.example/path", "invalid", ""}, {"https://host\n/secret", "invalid", ""},
		{"https://host\\@evil.example/", "invalid", ""}, {"https://host/%ZZ", "invalid", ""},
		{"https://user:pass@host:bad/", "invalid", ""}, {"https:opaque-secret", "invalid", ""},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got := endpoint(tt.input)
			if got.Status != tt.status || got.Origin != tt.origin {
				t.Fatalf("got %+v want %s %s", got, tt.status, tt.origin)
			}
		})
	}
}
func TestMalformedXMLTransactional(t *testing.T) {
	valid := `<packageSources><add key="Public" value="https://example.com/private-secret"/></packageSources>`
	for _, input := range []string{
		`<configuration>` + valid + `<broken>`,
		`<configuration>` + valid + `</configuration><configuration/>`,
		`<!DOCTYPE configuration [<!ENTITY leak SYSTEM "file:///private-secret">]><configuration>` + valid + `</configuration>`,
		`<configuration>` + valid + `<config>&private-secret;</config></configuration>`,
		`<configuration>` + valid + `<packageSources><add key="x" key="y" value="private-secret"/></packageSources></configuration>`,
		`<configuration xmlns="private-secret">` + valid + `</configuration>`,
		`<configuration>` + valid + strings.Repeat(`<x>`, 33) + strings.Repeat(`</x>`, 33) + `</configuration>`,
		`<configuration>` + valid + strings.Repeat(`<!--x-->`, MaxXMLTokens) + `</configuration>`,
	} {
		c := mustParse(t, "NuGet.Config", input)
		if c.Status != "partial" || c.DeclarationCountComplete || len(c.Declarations) != 0 || c.ObservedDeclarations != 0 {
			t.Fatalf("nontransactional parse: %+v", c)
		}
		encoded, _ := json.Marshal(c)
		if strings.Contains(string(encoded), "private-secret") {
			t.Fatal("parser exposed input")
		}
	}
}
func TestUnsupportedAndIdentifierQualification(t *testing.T) {
	c := mustParse(t, "nuget.config", `<configuration><packageSources><remove key="Feed"/><add key="secret://host/?token=value" value="https://example.com"/></packageSources><disabledPackageSources><add key="Feed" value="secret-value"/></disabledPackageSources></configuration>`)
	if c.Declarations[0].Semantics != "unsupported" || c.Omissions["unsupported_remove_semantics"] != 1 || c.Declarations[1].Name.Status != "omitted" || c.Declarations[2].Disabled != nil {
		t.Fatalf("%+v", c)
	}
	n := mustParse(t, ".npmrc", "registry[]=https://example.com\n[private-secret]\nregistry=https://other.example\n")
	if n.Status != "partial" || len(n.Declarations) != 0 || n.Omissions["unsupported_npm_array_registry"] != 1 {
		t.Fatal(n)
	}
	dynamic := mustParse(t, "NuGet.Config", `<configuration><disabledPackageSources><add key="Feed" value="%PRIVATE_ENV%"/></disabledPackageSources></configuration>`)
	if dynamic.Omissions["unresolved_disabled_value"] != 1 || dynamic.Declarations[0].Disabled != nil {
		t.Fatal(dynamic)
	}
	encoded, _ := json.Marshal(dynamic)
	if strings.Contains(string(encoded), "PRIVATE_ENV") {
		t.Fatal("unresolved value escaped")
	}
	long := mustParse(t, ".npmrc", "registry=https://example.com\n"+strings.Repeat("x", MaxNPMLineBytes+1))
	if long.SyntaxStatus != "incomplete" || long.DeclarationCountComplete || len(long.Declarations) != 0 {
		t.Fatal(long)
	}
}

func TestNuGetCasingAndNPMQuotedKeys(t *testing.T) {
	valid := mustParse(t, "NuGet.Config", `<Configuration><packageSources><ADD key="Feed" value="https://example.com/path"/></packageSources></Configuration>`)
	if valid.Status != "complete" || len(valid.Declarations) != 1 {
		t.Fatal(valid)
	}
	for _, input := range []string{
		`<configuration><PackageSources><add key="Feed" value="https://example.com"/></PackageSources></configuration>`,
		`<configuration><packageSources><add Key="Feed" value="https://example.com"/></packageSources></configuration>`,
		`<configuration><packageSourceMapping><packageSource key="Feed"><package Pattern="*"/></packageSource></packageSourceMapping></configuration>`,
	} {
		cfg := mustParse(t, "NuGet.Config", input)
		if cfg.SyntaxStatus != "unsupported" || cfg.Omissions["unsupported_xml_case"] != 1 || len(cfg.Declarations) != 0 {
			t.Fatal(cfg)
		}
	}
	quoted := mustParse(t, ".npmrc", "\"registry\"=https://example.com\n")
	if quoted.Omissions["unsupported_npm_key"] != 1 || len(quoted.Declarations) != 0 {
		t.Fatal(quoted)
	}
	for _, p := range []string{"../.npmrc", "/.npmrc", "bad\n/.npmrc", "bad\u202e/.npmrc"} {
		if _, err := Parse(p, nil); err != ErrCandidate {
			t.Fatalf("unsafe path accepted: %q", p)
		}
	}
}

func TestStrictXMLDeclarations(t *testing.T) {
	for _, declaration := range []string{
		`<?xml version="1.0"?>`,
		"\xef\xbb\xbf<?xml version='1.0' encoding='UTF-8' standalone='yes'?>",
		"<?xml\nversion = \"1.0\" standalone = \"no\"?>",
	} {
		cfg := mustParse(t, "NuGet.Config", declaration+`<configuration/>`)
		if cfg.Status != "complete" {
			t.Fatal(cfg)
		}
	}
	for _, declaration := range []string{
		`<?xml version="1.0"?><?xml version="1.0"?>`,
		`<?xml garbage?>`, `<?xml?>`,
		`<!--comment--><?xml version="1.0"?>`,
		` <?xml version="1.0"?>`,
		`<?xml version="1.1"?>`,
		`<?xml encoding="utf-8" version="1.0"?>`,
		`<?xml version="1.0" encoding="utf-16"?>`,
		`<?xml version="1.0" standalone="maybe"?>`,
		`<?xml version="1.0" version="1.0"?>`,
	} {
		cfg := mustParse(t, "NuGet.Config", declaration+`<configuration><packageSources><clear/></packageSources></configuration>`)
		if cfg.Status != "partial" || cfg.DeclarationCountComplete || len(cfg.Declarations) != 0 {
			t.Fatal(cfg)
		}
	}
}

func TestSingleQuotedNumericOverflow(t *testing.T) {
	for _, value := range []string{"'1e400'", "'-1e400'", "'1e-400'", "'123'", "'[]'", "'{}'", "'[1]'", "'true'", "'null'"} {
		cfg := mustParse(t, ".npmrc", "registry="+value+"\n")
		if cfg.Status != "partial" || cfg.Omissions["unsupported_npm_value"] != 1 || len(cfg.Declarations) != 0 {
			t.Fatal(value, cfg)
		}
	}
}
func fixtureCandidate(path, content string, calls *int) Candidate {
	return Candidate{Path: path, Size: int64(len(content)), Read: func(ctx context.Context, limit int64) ([]byte, int64, error) {
		if calls != nil {
			*calls++
		}
		return []byte(content)[:min(int64(len(content)), limit)], int64(len(content)), nil
	}}
}
func newCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := New(Source{Mode: "directory"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestScopeEmptyAndTreeSkipped(t *testing.T) {
	for _, skip := range []bool{false, true} {
		c := newCollector(t)
		if skip {
			if err := c.Omit("tree_size_limit", 1); err != nil {
				t.Fatal(err)
			}
		}
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Scope.SupportedConfigurations, []string{"nuget_config_basename_case_insensitive", "npmrc_basename_exact"}) {
			t.Fatal(r.Scope)
		}
		if len(r.Configurations) != 0 || r.Scope.ExternalConfiguration || r.Scope.EnvironmentExpansion || r.Scope.NetworkAccess {
			t.Fatal(r)
		}
		if skip && (r.Status != "skipped" || r.Coverage.EnumerationComplete) {
			t.Fatal(r)
		}
		if !skip && (r.Status != "complete" || !r.Coverage.EnumerationComplete) {
			t.Fatal(r)
		}
	}
}
func TestDeterministicSelectionAndAncestorCandidates(t *testing.T) {
	build := func(reverse bool) *Report {
		c := newCollector(t)
		calls := 0
		for j := range 70 {
			i := j
			if reverse {
				i = 69 - j
			}
			for _, base := range []string{"NuGet.Config", ".npmrc"} {
				text := "registry=https://example.com/path"
				if base == "NuGet.Config" {
					text = `<configuration><packageSources><clear/></packageSources></configuration>`
				}
				if err := c.Add(fixtureCandidate(fmt.Sprintf("d%02d/%s", i, base), text, &calls)); err != nil {
					t.Fatal(err)
				}
			}
		}
		r, err := c.Finish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if calls != 128 || r.Coverage.CandidateFiles != 140 || r.Omissions["configuration_limit"] != 12 || r.Coverage.AdmittedFiles != 128 {
			t.Fatal(r.Coverage, r.Omissions, calls)
		}
		return r
	}
	if !reflect.DeepEqual(build(false), build(true)) {
		t.Fatal("worker order changes selection")
	}
	c := newCollector(t)
	for _, p := range []string{"NuGet.Config", "src/NuGet.Config", "src/deep/nuget.config", ".npmrc", "src/.npmrc"} {
		content := `<configuration/>`
		if strings.HasSuffix(p, ".npmrc") {
			content = "registry=https://example.com"
		}
		if err := c.Add(fixtureCandidate(p, content, nil)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range r.Configurations {
		if cfg.Path == "src/deep/nuget.config" && !reflect.DeepEqual(cfg.AncestorCandidates, []string{"NuGet.Config", "src/NuGet.Config"}) {
			t.Fatal(cfg)
		}
		if cfg.Ecosystem == "npm" && len(cfg.AncestorCandidates) != 0 {
			t.Fatal("invented npm inheritance")
		}
	}
}
func TestDeclarationBoundsAndCounts(t *testing.T) {
	content := strings.Repeat("registry=https://example.com/path\n", 300)
	c := mustParse(t, ".npmrc", content)
	if c.ObservedDeclarations != 300 || c.OmittedDeclarations != 44 || len(c.Declarations) != 256 || !c.DeclarationCountComplete {
		t.Fatal(c)
	}
	collector := newCollector(t)
	for i := range 5 {
		if err := collector.Add(fixtureCandidate(fmt.Sprintf("d%d/.npmrc", i), content, nil)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := collector.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Coverage.ObservedDeclarations != 1500 || r.Coverage.RetainedDeclarations != 1024 || len(r.Configurations[4].Declarations) != 0 || r.Configurations[4].OmittedDeclarations != 300 {
		t.Fatal(r.Coverage)
	}
}
func TestReadLimitsPrivacyCancellationAndOverflow(t *testing.T) {
	c, err := New(Source{Mode: "directory"}, Options{MaxFileBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err = c.Add(fixtureCandidate(".npmrc", "registry=https://example.com", &calls)); err != nil {
		t.Fatal(err)
	}
	r, err := c.Finish(context.Background())
	if err != nil || calls != 0 || r.Configurations[0].Omissions["file_size_limit"] != 1 {
		t.Fatal(r, err, calls)
	}
	c = newCollector(t)
	if err = c.Add(Candidate{Path: ".npmrc", Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) { return nil, 0, errors.New("password-sentinel") }}); err != nil {
		t.Fatal(err)
	}
	if r, err = c.Finish(context.Background()); r != nil || err != ErrRead || strings.Contains(err.Error(), "sentinel") {
		t.Fatal(r, err)
	}
	c = newCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.Finish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c = newCollector(t)
	ctx, cancel = context.WithCancel(context.Background())
	if err = c.Add(Candidate{Path: ".npmrc", Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) { cancel(); return []byte("x"), 1, nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Finish(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c = newCollector(t)
	if err = c.Omit("tree_size_limit", math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	if err = c.Omit("tree_size_limit", 1); err != ErrOptions {
		t.Fatal(err)
	}
	c.report.Coverage.CandidateFiles = math.MaxInt64
	if err = c.Add(fixtureCandidate(".npmrc", "", nil)); err != ErrOptions {
		t.Fatal(err)
	}
}
func TestChangedAndIncompleteReads(t *testing.T) {
	for _, result := range []struct {
		bytes []byte
		size  int64
	}{{[]byte("x"), 2}, {[]byte("xy"), 2}, {[]byte("xx"), 1}} {
		c := newCollector(t)
		if err := c.Add(Candidate{Path: ".npmrc", Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) { return result.bytes, result.size, nil }}); err != nil {
			t.Fatal(err)
		}
		r, err := c.Finish(context.Background())
		if err != nil || r.Configurations[0].Omissions["incomplete_content"] != 1 {
			t.Fatal(r, err)
		}
	}
	c := newCollector(t)
	if err := c.Add(Candidate{Path: ".npmrc", Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) {
		return make([]byte, MaxFileBytes+2), 1, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if report, err := c.Finish(context.Background()); report != nil || err != ErrRead {
		t.Fatal("oversized callback result accepted", err)
	}
}
func FuzzParseDeterminismAndBounds(f *testing.F) {
	for _, s := range []string{"registry=https://u:p@example.com/path?token=secret", `<configuration><packageSources><clear/></packageSources></configuration>`, "registry=${SECRET}", "\x00\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > int(MaxFileBytes)+1 {
			t.Skip()
		}
		for _, name := range []string{"NuGet.Config", ".npmrc"} {
			a, err := Parse(name, []byte(input))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := Parse(name, []byte(input))
			if !reflect.DeepEqual(a, b) {
				t.Fatal("nondeterministic")
			}
			if len(a.Declarations) > MaxDeclarationsPerConfiguration || a.ObservedDeclarations != int64(len(a.Declarations))+a.OmittedDeclarations {
				t.Fatal("count bound")
			}
			for _, d := range a.Declarations {
				if d.Endpoint != nil && d.Endpoint.Origin != "" {
					u, err := url.Parse(d.Endpoint.Origin)
					if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
						t.Fatal("not an origin")
					}
				}
			}
		}
	})
}
