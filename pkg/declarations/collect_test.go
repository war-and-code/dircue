package declarations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"dircue/pkg/projects"
)

func candidateFor(name, content string) *Candidate {
	return &Candidate{Path: name, Size: int64(len(content)), Read: func(context.Context, int64) ([]byte, int64, error) { return []byte(content), int64(len(content)), nil }}
}

func TestCollectorOutputBoundIncludesDiagnosticsAndEnvelope(t *testing.T) {
	c := New("directory", "", 0)
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("%02d.csproj", i)
		d := &projects.Document{}
		for k := 0; k < 32; k++ {
			d.Diagnostics = append(d.Diagnostics, projects.Diagnostic{Code: fmt.Sprintf("code-%d", k)})
		}
		for j := 0; j < 120; j++ {
			d.Requirements = append(d.Requirements, projects.Requirement{Kind: "target-framework", Value: strings.Repeat("a", 8100), State: "declared", Evidence: name})
		}
		c.Add(name, &Candidate{Path: name, Size: 1000000, Legacy: d})
	}
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > MaxOutputBytes {
		t.Fatalf("output=%d limit=%d error=%v", len(data), MaxOutputBytes, err)
	}
	if r.Status != "partial" || r.Coverage.OmittedFiles == 0 || len(r.Projects) == 0 {
		t.Fatalf("missing useful bounded partial report: %+v", r.Coverage)
	}
	retained := 0
	for _, p := range r.Projects {
		retained += len(p.Requirements) + len(p.References) + len(p.Interfaces)
	}
	if retained != r.Coverage.RetainedObservations {
		t.Fatal("trimming left stale retained-observation count")
	}
}

func TestCollectorDeterministicAndSelectedOnly(t *testing.T) {
	files := []string{"go.work", "a/go.mod", "b/go.mod", "README.md"}
	contents := map[string]string{"go.work": "go 1.26\nuse (\n ./a\n ./b\n)\n", "a/go.mod": "module example.invalid/a\ngo 1.26\n", "b/go.mod": "module example.invalid/b\ngo 1.26\n"}
	var previous []byte
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 0, 1}, {1, 3, 2, 0}} {
		c := New("git", strings.Repeat("a", 40), 0)
		for _, i := range order {
			var p *Candidate
			if v, ok := contents[files[i]]; ok {
				p = candidateFor(files[i], v)
			}
			c.Add(files[i], p)
		}
		r, e := c.Finish(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if r.Status != "complete" || r.Coverage.ParsedManifests != 3 {
			t.Fatalf("%+v", r)
		}
		b, _ := json.Marshal(r)
		if previous != nil && string(previous) != string(b) {
			t.Fatal("arrival order changed report")
		}
		previous = b
		again, e := c.Finish(context.Background())
		if e != nil || again != r {
			t.Fatal("finish is not idempotent")
		}
	}
}
func TestCollectorLexicalManifestLimit(t *testing.T) {
	c := New("directory", "", 0)
	reads := 0
	for i := MaxDocuments + 2; i >= 0; i-- {
		name := fmt.Sprintf("p%05d/package.json", i)
		p := candidateFor(name, `{"name":"p"}`)
		original := p.Read
		p.Read = func(ctx context.Context, n int64) ([]byte, int64, error) { reads++; return original(ctx, n) }
		c.Add(name, p)
	}
	r, e := c.Finish(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if reads != MaxDocuments || len(r.Projects) != MaxDocuments || r.Status != "partial" {
		t.Fatalf("reads %d; coverage %+v", reads, r.Coverage)
	}
	if r.Projects[0].ID != "p00000/package.json" || r.Projects[len(r.Projects)-1].ID != fmt.Sprintf("p%05d/package.json", MaxDocuments-1) {
		t.Fatal("wrong retained order")
	}
}
func TestCollectorSkipsOversizedWithoutReading(t *testing.T) {
	c := New("directory", "", 4)
	p := candidateFor("package.json", `{"name":"large"}`)
	p.Read = func(context.Context, int64) ([]byte, int64, error) {
		t.Fatal("read oversized manifest")
		return nil, 0, nil
	}
	c.Add(p.Path, p)
	r, e := c.Finish(context.Background())
	if e != nil || r.Status != "partial" || r.Coverage.ParsedManifests != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestLegacyExpansionWithholdsUnsafeTextWithoutChangingLegacyParser(t *testing.T) {
	content := `<Project><PropertyGroup Condition="'$(Secret)'=='DO_NOT_DISCLOSE'"><TargetFramework>net8.0</TargetFramework></PropertyGroup><Import Project="https://user:DO_NOT_DISCLOSE@example.invalid/config"/><ItemGroup><ProjectReference Include="/Users/private/DO_NOT_DISCLOSE.csproj"/></ItemGroup></Project>`
	c := New("directory", "", 0)
	c.Add("App.csproj", candidateFor("App.csproj", content))
	r, e := c.Finish(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	output, _ := json.Marshal(r)
	if strings.Contains(string(output), "DO_NOT_DISCLOSE") || strings.Contains(string(output), "/Users/private") {
		t.Fatalf("unsafe text escaped: %s", output)
	}
	if !strings.Contains(string(output), "net8.0") {
		t.Fatal("lost safe requirement")
	}
}

func TestCollectorCancellationDoesNotBecomeSuccessOnRetry(t *testing.T) {
	c := New("directory", "", 0)
	c.Add("go.mod", candidateFor("go.mod", "module example.invalid/demo\n"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Finish(ctx); e == nil {
		t.Fatal("canceled finish succeeded")
	}
	if _, e := c.Finish(context.Background()); e == nil {
		t.Fatal("failed collector became success on retry")
	}
}

func TestCollectorDoesNotMergeInvalidUTF8ProjectIdentities(t *testing.T) {
	c := New("directory", "", 0)
	for _, prefix := range []string{"\xff", "\xfe"} {
		name := prefix + "/go.mod"
		p := candidateFor(name, "module example.invalid/demo\n")
		p.Read = func(context.Context, int64) ([]byte, int64, error) {
			t.Fatal("read a manifest whose identity cannot be represented")
			return nil, 0, nil
		}
		c.Add(name, p)
	}
	r, err := c.Finish(context.Background())
	if err != nil || r.Status != "partial" || len(r.Projects) != 0 || r.Coverage.ManifestCandidates != 2 || r.Coverage.OmittedFiles != 2 {
		t.Fatalf("invalid identities were silently replaced: %+v %v", r, err)
	}
}

func TestCollectorReadErrorContinuePolicy(t *testing.T) {
	// Under the continue policy a per-file manifest read failure records a
	// per-path diagnostic and marks the omission attributable while the
	// remaining manifests still parse and appear in the report.
	c := New("directory", "", 0)
	c.SetErrorPolicy("continue")
	bad := &Candidate{Path: "a/go.mod", Size: 4, Read: func(context.Context, int64) ([]byte, int64, error) {
		return nil, 0, errors.New("secret host error")
	}}
	c.Add("a/go.mod", bad)
	c.Add("b/go.mod", candidateFor("b/go.mod", "module example.invalid/b\ngo 1.26\n"))
	r, err := c.Finish(context.Background())
	if err != nil {
		t.Fatalf("continue policy returned error: %v", err)
	}
	if r.Status != "partial" || r.Coverage.OmittedFiles == 0 {
		t.Fatalf("read failure was not recorded as an omission: %+v", r.Coverage)
	}
	if r.Coverage.ParsedManifests == 0 {
		t.Fatal("remaining manifest was not parsed")
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Code == "file-read-error" && d.Path == "a/go.mod" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no file-read-error diagnostic attributed to path: %+v", r.Diagnostics)
	}
	if got := c.ReadErrors(); len(got) != 1 || got[0] != "a/go.mod" {
		t.Fatalf("ReadErrors: %v", got)
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "host error") {
		t.Fatalf("caller I/O error leaked into report: %s", encoded)
	}
	// Default policy still fails hard so existing invocations do not silently
	// degrade to a partial report.
	strict := New("directory", "", 0)
	strict.Add("a/go.mod", bad)
	if _, err := strict.Finish(context.Background()); err == nil {
		t.Fatal("default policy no longer fails hard on manifest read error")
	}
}

func TestCollectorContinueDoesNotSwallowReaderCancellation(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		c := New("directory", "", 0)
		c.SetErrorPolicy("continue")
		candidate := &Candidate{Path: "go.mod", Size: 1, Read: func(context.Context, int64) ([]byte, int64, error) {
			return nil, 0, fmt.Errorf("selected reader stopped: %w", want)
		}}
		c.Add("go.mod", candidate)
		if _, err := c.Finish(context.Background()); !errors.Is(err, want) {
			t.Fatalf("continue swallowed %v: %v", want, err)
		}
	}
}
