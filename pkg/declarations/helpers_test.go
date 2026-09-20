package declarations

import (
	"strings"
	"testing"
)

func TestLocalTargetConfinesToInventory(t *testing.T) {
	for _, tt := range []struct {
		manifest, value, want string
		ok                    bool
	}{
		{"a/go.mod", "../b", "b/go.mod", true},
		{"go.mod", "../outside", "", false},
		{"a/go.mod", "../../outside", "", false},
		{"a/go.mod", "/etc", "", false},
		{"a/go.mod", `C:\secret`, "", false},
		{"a/go.mod", `\\host\secret`, "", false},
		{"a/go.mod", "https://user:secret@example.invalid/x", "", false},
		{"a/go.mod", "line\nbreak", "", false},
		{"a/go.mod", `..\b`, "b/go.mod", true},
	} {
		got, ok := LocalTarget(tt.manifest, tt.value, "go.mod")
		if got != tt.want || ok != tt.ok {
			t.Errorf("%q %q = %q,%v", tt.manifest, tt.value, got, ok)
		}
	}
}
func TestWorkspaceGlobSubset(t *testing.T) {
	for _, tt := range []struct {
		p, s           string
		match, invalid bool
	}{
		{"packages/*", "packages/a", true, false}, {"packages/*", "packages/a/b", false, false},
		{"packages/**", "packages/a/b", true, false}, {"**/lib", "lib", true, false},
		{"**/lib", "a/lib", true, false}, {"lib/**/src", "lib/src", true, false},
		{"lib/**/src", "lib/a/b/src", true, false}, {"lib/[ab]", "lib/a", true, false},
		{"lib/{a,b}", "lib/a", false, true}, {"../lib", "../lib", false, true},
		{"lib/[", "lib/a", false, true}, {"lib/a**", "lib/abc", false, true},
	} {
		v, e := MatchPattern(tt.p, tt.s)
		if v != tt.match || (e != nil) != tt.invalid {
			t.Errorf("%q %q = %v,%v", tt.p, tt.s, v, e)
		}
	}
}
func TestJSONAmbiguityAndBounds(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`, `{} {}`, `null`, `[]`, `{"x":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`, `{"x":"` + strings.Repeat("x", 65537) + `"}`} {
		if _, e := ValidateJSON([]byte(input)); e == nil {
			t.Errorf("accepted invalid/oversized JSON (length %d)", len(input))
		}
	}
	if _, e := ValidateJSON([]byte(`{"a":[true,null,1,{"x":"ok"}]}`)); e != nil {
		t.Fatal(e)
	}
}
func TestTOMLAmbiguityAndBounds(t *testing.T) {
	for _, input := range []string{"x=1\nx=2", strings.Repeat("a.", 70) + "x=1", "x='" + strings.Repeat("x", 65537) + "'"} {
		if _, e := ValidateTOML([]byte(input)); e == nil {
			t.Errorf("accepted invalid/oversized TOML (length %d)", len(input))
		}
	}
}
func TestObservationBudgetIsExplicit(t *testing.T) {
	d := NewDocument("go.mod", "go")
	for i := 0; i < MaxObservationsPerManifest; i++ {
		if !AddRequirement(d, Requirement{Kind: "x", Value: "y"}) {
			t.Fatalf("premature limit %d", i)
		}
	}
	if AddReference(d, Reference{Kind: "z"}) {
		t.Fatal("exceeded per-manifest budget")
	}
	if len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "declaration-limit" {
		t.Fatal(d.Diagnostics)
	}
	if AddInterface(d, Interface{Name: "f"}) {
		t.Fatal("continued after limit")
	}
}

func TestTOMLPreflightDottedKeysAndQuotedDelimiters(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("a.", 100000) + "x=1",
		"[" + strings.Repeat("a.", 100000) + "x]\ny=1",
		"x={" + strings.Repeat("a.", 100000) + "y=1}",
		"[" + strings.Repeat("a.", 40) + "x]\n" + strings.Repeat("b.", 40) + "y=1",
	} {
		if _, e := ValidateTOML([]byte(input)); e == nil {
			t.Fatal("accepted excessive key depth")
		}
	}
	for _, input := range []string{`"a.b.c"="[not.an.array]"`, "[project]\nname='demo'\n", "x=[{a=1}, {b=[2]}]"} {
		if _, e := ValidateTOML([]byte(input)); e != nil {
			t.Fatalf("rejected valid delimiters: %q", input)
		}
	}
}
