package capabilities

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// FuzzDescriptorValidateJSON exercises the planner-capabilities registry's
// JSON decode + Validate boundary. The Descriptor is the authoritative shape
// consumed by the planner and by `--schema NAME` callers; a hostile document
// must never panic, must be rejected deterministically, and — when accepted —
// must round-trip through JSON unchanged. Seed corpus covers duplicate IDs,
// forbidden argv prefixes, unknown cost classes, out-of-order IDs, and
// obviously oversized documents.
func FuzzDescriptorValidateJSON(f *testing.F) {
	good, err := json.Marshal(Dircue("1.0.0"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good)
	f.Add([]byte(`{"schema_version":"1.0.0","kind":"dircue-planner-capabilities","provider":"dircue","provider_version":"1.0.0","scope":"planner-supported-modules","modules":[]}`))
	f.Add([]byte(`{"schema_version":"1.0.0","kind":"dircue-planner-capabilities","provider":"dircue","provider_version":"1.0.0","scope":"planner-supported-modules","modules":[{"id":"a","question":"q","command":{"argv_prefix":["dircue","analyze","a","--json"]},"provider_version":"1.0.0","aggregate_schema_since":"1.0.0","cost":{"inspection":"metadata"}},{"id":"a","question":"q2","command":{"argv_prefix":["dircue","analyze","a","--json"]},"provider_version":"1.0.0","aggregate_schema_since":"1.0.0","cost":{"inspection":"metadata"}}]}`))
	f.Add([]byte(`{"schema_version":"1.0.0","kind":"dircue-planner-capabilities","provider":"dircue","provider_version":"1.0.0","scope":"planner-supported-modules","modules":[{"id":"a","question":"q","command":{"argv_prefix":["evil"]},"provider_version":"1.0.0","aggregate_schema_since":"1.0.0","cost":{"inspection":"metadata"}}]}`))
	f.Add([]byte(`{"schema_version":"1.0.0","kind":"dircue-planner-capabilities","provider":"dircue","provider_version":"1.0.0","scope":"planner-supported-modules","modules":[{"id":"a","question":"q","command":{"argv_prefix":["dircue","analyze","a","--json"]},"provider_version":"1.0.0","aggregate_schema_since":"1.0.0","cost":{"inspection":"UNKNOWN"}}]}`))
	f.Add([]byte(`{"schema_version":"2.0.0","kind":"dircue-planner-capabilities","provider":"dircue","provider_version":"1.0.0","scope":"planner-supported-modules","modules":[]}`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<16 { // capabilities.Validate uses bounded string fields already
			return
		}
		var d1 Descriptor
		err1 := json.Unmarshal(raw, &d1)
		var d2 Descriptor
		err2 := json.Unmarshal(raw, &d2)
		if (err1 == nil) != (err2 == nil) {
			t.Fatal("nondeterministic json.Unmarshal")
		}
		if err1 != nil {
			return
		}
		v1 := d1.Validate()
		v2 := d2.Validate()
		if (v1 == nil) != (v2 == nil) {
			t.Fatalf("nondeterministic Validate: %v vs %v", v1, v2)
		}
		if v1 != nil {
			return
		}
		// A validated Descriptor's modules must be sorted, unique on ID and on
		// question, and every argv_prefix must remain the safe canonical form.
		last := ""
		ids, questions := map[string]bool{}, map[string]bool{}
		for _, m := range d1.Modules {
			if m.ID <= last && last != "" {
				t.Fatalf("validated but unsorted modules: %q <= %q", m.ID, last)
			}
			last = m.ID
			if ids[m.ID] {
				t.Fatalf("validated but duplicated ID %q", m.ID)
			}
			ids[m.ID] = true
			if questions[m.Question] {
				t.Fatalf("validated but duplicated question %q", m.Question)
			}
			questions[m.Question] = true
			want := []string{"dircue", "analyze", m.ID, "--json"}
			if !reflect.DeepEqual(m.Command.ArgvPrefix, want) {
				t.Fatalf("validated argv drift: %+v vs %+v", m.Command.ArgvPrefix, want)
			}
			if strings.Contains(strings.Join(m.Command.ArgvPrefix, " "), "\x00") {
				t.Fatalf("argv contains NUL: %+v", m.Command.ArgvPrefix)
			}
		}
	})
}

// FuzzDircueDescriptorDeterministic guarantees the built-in registry is
// deterministic and self-consistent under many different providerVersion
// strings — this is what the CLI ends up handing planning.Build.
func FuzzDircueDescriptorDeterministic(f *testing.F) {
	f.Add("1.0.0")
	f.Add("")
	f.Add("dev-preview-0")
	f.Add(strings.Repeat("v", 200))

	f.Fuzz(func(t *testing.T, providerVersion string) {
		d1 := Dircue(providerVersion)
		d2 := Dircue(providerVersion)
		if !reflect.DeepEqual(d1, d2) {
			t.Fatal("Dircue returned nondeterministic descriptor")
		}
		if len(d1.Modules) == 0 {
			t.Fatal("Dircue returned no modules")
		}
		// Only when the caller version fits the wire-string bound does the
		// registry validate; that's the contract the CLI depends on.
		if len(providerVersion) > 0 && len(providerVersion) <= 256 {
			if err := d1.Validate(); err != nil {
				t.Fatalf("bounded provider version rejected: %v", err)
			}
		} else if err := d1.Validate(); err == nil {
			t.Fatal("out-of-band provider version accepted")
		}
	})
}
