package environments

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// FuzzParseGlobalJSON stresses the JSONC-aware global.json parser: BOM,
// comments, trailing commas, deep nesting, duplicate keys, alternative casing,
// oversized numbers, and unpaired unicode escapes. The parser must never
// panic, and must obey the retained-value contract: when the parse succeeds
// with State=="declared", the returned SDKVersion/RollForward/AllowPrerelease
// bytes stay within the wire limits.
func FuzzParseGlobalJSON(f *testing.F) {
	f.Add([]byte(`{"sdk":{"version":"8.0.100"}}`))
	f.Add([]byte(`{"sdk":{"version":"8.0.100","rollForward":"latestFeature","allowPrerelease":true}}`))
	f.Add([]byte("// pinned\n{\"sdk\":{\"version\":\"8.0.100\"}}"))
	f.Add([]byte("{/* c */\"sdk\":{\"version\":\"8.0.100\"}}"))
	f.Add([]byte(`{"sdk":{"version":"8.0.100",}}`))
	f.Add([]byte("\xef\xbb\xbf{\"sdk\":{\"version\":\"8.0.100\"}}"))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"sdk":null}`))
	f.Add([]byte(`{"SDK":{"version":"8.0.100"}}`))
	f.Add([]byte(`{"sdk":{"version":">=8"}}`))
	f.Add([]byte(`{"sdk":{"paths":[".dotnet","$host$"]}}`))
	f.Add([]byte(`{"sdk":{"futurePolicy":true}}`))
	f.Add([]byte(strings.Repeat("[", 130) + "0" + strings.Repeat("]", 130)))
	f.Add([]byte(`{"sdk":{"version":"8.0.100","version":"9.0.100"}}`))
	f.Add([]byte(`{"sdk":{"version":"8.0.100-preview.1"}}`))
	f.Add([]byte(`{"sdk":{"version":"8.0.100-preview."}}`))
	f.Add([]byte(`{"sdk":{}} /*`)) // unterminated block comment
	f.Add([]byte("{\"sdk\":{\"version\":\"8.0.100\"\x80}}"))

	f.Fuzz(func(t *testing.T, content []byte) {
		if int64(len(content)) > DefaultMaxGlobalJSONBytes {
			return
		}
		r1 := &Report{Status: "complete", Boundaries: []Boundary{}, Diagnostics: []Diagnostic{}}
		s1 := &Selection{GlobalJSON: "global.json"}
		parseGlobal(r1, s1, append([]byte(nil), content...))
		r2 := &Report{Status: "complete", Boundaries: []Boundary{}, Diagnostics: []Diagnostic{}}
		s2 := &Selection{GlobalJSON: "global.json"}
		parseGlobal(r2, s2, append([]byte(nil), content...))
		if !reflect.DeepEqual(r1.Status, r2.Status) || !reflect.DeepEqual(s1, s2) || !reflect.DeepEqual(r1.Diagnostics, r2.Diagnostics) || !reflect.DeepEqual(r1.Boundaries, r2.Boundaries) {
			t.Fatalf("nondeterministic parseGlobal:\n%+v\n%+v", r1, r2)
		}
		// Wire-string invariants: any retained selection scalar must obey the
		// serialization limit that ValidateReport enforces.
		if len(s1.SDKVersion) > 8192 || len(s1.RollForward) > 8192 || len(s1.GlobalJSON) > 8192 {
			t.Fatalf("selection wire limits violated: %+v", s1)
		}
		// A "declared" state must carry at least one policy scalar, and an
		// "unconstrained" state must carry none: those are the same contracts
		// ValidateReport polices on decoded reports.
		switch s1.State {
		case "declared":
			if s1.SDKVersion == "" && s1.RollForward == "" && s1.AllowPrerelease == nil {
				t.Fatalf("declared selection has no policy: %+v", s1)
			}
		case "unconstrained":
			if s1.SDKVersion != "" || s1.RollForward != "" || s1.AllowPrerelease != nil {
				t.Fatalf("unconstrained selection carries a policy: %+v", s1)
			}
		case "unresolved":
			// unresolved is always valid
		default:
			t.Fatalf("unknown selection state %q", s1.State)
		}
		// Never both "declared" and "unresolved" at once.
		if r1.Status == "complete" && s1.State == "unresolved" {
			t.Fatalf("status complete with unresolved selection: %+v %+v", r1, s1)
		}
	})
}

// FuzzPythonRange checks the pyproject requires-python range parser. It must
// be deterministic, must reject anything outside the supported subset, and
// when a range is accepted every returned bound must have a numeric version
// small enough to compare without overflow.
func FuzzPythonRange(f *testing.F) {
	f.Add(">=3.11")
	f.Add(">=3.11,<4")
	f.Add(">=3.9,<3.12")
	f.Add("==3.10")
	f.Add(">= 3.11 , < 4 ")
	f.Add("~=3.12")
	f.Add("")
	f.Add(",")
	f.Add(">=")
	f.Add(">=3.")
	f.Add(">=3.11.4.9.2.7")
	f.Add(">=18446744073709551616")
	f.Add(strings.Repeat(">=1,", 50))

	f.Fuzz(func(t *testing.T, value string) {
		low1, high1, ok1 := pythonRange(value)
		low2, high2, ok2 := pythonRange(value)
		if ok1 != ok2 || !reflect.DeepEqual(low1, low2) || !reflect.DeepEqual(high1, high2) {
			t.Fatalf("nondeterministic pythonRange(%q)", value)
		}
		if !ok1 {
			return
		}
		// When accepted, every returned bound version must fit in the numeric
		// version representation the module uses elsewhere.
		for _, b := range []*pythonBound{low1, high1} {
			if b == nil {
				continue
			}
			if len(b.version) > 16 {
				t.Fatalf("bound too deep: %+v", b)
			}
			for _, part := range b.version {
				if part < 0 {
					t.Fatalf("negative version part: %+v", b)
				}
			}
		}
	})
}

// FuzzAnalyzeGlobalJSON hits the full Analyze path with hostile inventories
// and file contents to make sure the whole pipeline stays panic-free and
// returns a report that ValidateReport accepts.
func FuzzAnalyzeGlobalJSON(f *testing.F) {
	seeds := []string{
		`{"sdk":{"version":"8.0.100"}}`,
		`{"sdk":{"version":"8.0.100",}}`,
		"// pinned\n{\"sdk\":{\"version\":\"8.0.100\"}}",
		`{"sdk":{"version":">=8"}}`,
		`{"sdk":null}`,
		`null`,
		`{}`,
		`{"sdk":{"paths":[".dotnet","$host$"]}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if int64(len(content)) > DefaultMaxGlobalJSONBytes {
			return
		}
		in := envInput(map[string]string{"global.json": string(content)}, []Invocation{{"p", "."}})
		r, err := Analyze(context.Background(), in, Limits{})
		if err != nil {
			t.Fatalf("Analyze returned err on bounded input: %v", err)
		}
		if r == nil {
			t.Fatal("Analyze returned nil report")
		}
		if err := ValidateReport(r); err != nil {
			t.Fatalf("ValidateReport rejected produced report: %v", err)
		}
	})
}
