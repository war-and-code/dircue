package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// enumFlagsBySemantics enumerates finite-choice flags whose validation
// dictionary MUST appear in the CLI catalog. Adding a new enum flag anywhere
// without updating flagAllowedValues fails TestCatalogCoversAllCommandsAndFlags.
var enumFlagsBySemantics = map[string]struct{}{
	"source":        {},
	"on-error":      {},
	"metrics-scope": {},
	"schema":        {},
	"module":        {},
	"question":      {},
	"input":         {},
	"preset":        {},
}

// TestCatalogCoversAllCommandsAndFlags asserts three invariants that stop a
// whole class of drift bugs: every catalog command carries its applicable
// restrictions (shared source-selection rules count), every leaf command
// carries an output-contract identifier so consumers do not have to guess the
// response shape, and every enum flag advertises the exact accepted
// vocabulary through allowed_values so a new enum flag cannot ship without
// cataloging its choices. Fixing any drift here is the intended outcome;
// loosening the assertion is not.
func TestCatalogCoversAllCommandsAndFlags(t *testing.T) {
	out, stderr, err := invoke("capabilities", "--cli", "--json")
	if err != nil || stderr != "" {
		t.Fatalf("catalog fetch: stderr=%q err=%v", stderr, err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}

	// Every leaf's output_contracts entry must reference an id defined in the
	// top-level output_contracts registry, so contract names stay in one place.
	knownOutputs := make(map[string]struct{}, len(contract.OutputContracts))
	for _, out := range contract.OutputContracts {
		knownOutputs[out.ID] = struct{}{}
	}

	// A leaf is any command whose path is not a strict prefix of another path.
	pathSet := make(map[string]struct{}, len(contract.Commands))
	for _, entry := range contract.Commands {
		pathSet[strings.Join(entry.Path, " ")] = struct{}{}
	}
	isLeaf := func(entry cliCommandContract) bool {
		me := strings.Join(entry.Path, " ")
		for path := range pathSet {
			if path != me && strings.HasPrefix(path, me+" ") {
				return false
			}
		}
		return true
	}

	for _, entry := range contract.Commands {
		path := strings.Join(entry.Path, " ")

		if len(entry.Restrictions) == 0 {
			t.Errorf("%s: restrictions is empty; add a scoped marker in commandRestrictions", path)
		}

		if isLeaf(entry) {
			if len(entry.OutputContracts) == 0 {
				t.Errorf("%s: leaf command missing output_contracts", path)
			}
			for _, id := range entry.OutputContracts {
				if _, ok := knownOutputs[id]; !ok {
					t.Errorf("%s: output_contracts references unknown id %q", path, id)
				}
			}
		}

		for _, flag := range entry.Flags {
			if _, isEnum := enumFlagsBySemantics[flag.Name]; !isEnum {
				continue
			}
			// The help command inherits scan flags for parser compatibility
			// but ignores their values; catalog empties them explicitly to
			// signal the ignore.
			if flag.Inherited && path == "dircue help" {
				continue
			}
			// The bare `analyze` group entry point never validates values
			// itself; the leaf profiler underneath enforces the enum.
			if flag.Inherited && path == "dircue analyze" {
				continue
			}
			if len(flag.AllowedValues) == 0 {
				t.Errorf("%s --%s: enum flag catalogs no allowed_values", path, flag.Name)
			}
		}
	}
}

// TestCatalogEnumRegistryStaysExhaustive keeps enumFlagsBySemantics honest.
// Every enum flag observed in the catalog must be listed here, and every
// listed name must appear in the catalog. New enum flags need one edit in
// each direction, and stale entries fail loudly.
func TestCatalogEnumRegistryStaysExhaustive(t *testing.T) {
	out, _, err := invoke("capabilities", "--cli", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var contract cliContract
	if err := json.Unmarshal([]byte(out), &contract); err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, entry := range contract.Commands {
		for _, flag := range entry.Flags {
			if len(flag.AllowedValues) > 0 {
				seen[flag.Name] = struct{}{}
			}
		}
	}
	for name := range seen {
		if _, ok := enumFlagsBySemantics[name]; !ok {
			t.Errorf("enumFlagsBySemantics missing %q; add it so TestCatalogCoversAllCommandsAndFlags enforces the invariant", name)
		}
	}
	for name := range enumFlagsBySemantics {
		if _, ok := seen[name]; !ok {
			t.Errorf("enumFlagsBySemantics lists %q but no cataloged command advertises it; drop it or add a producer", name)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	t.Logf("cataloged enum flags: %v", names)
}
