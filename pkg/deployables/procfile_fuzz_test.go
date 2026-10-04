package deployables

import (
	"reflect"
	"strings"
	"testing"
)

// A fixed filename keeps mutations in the Procfile grammar instead of mostly
// exercising the unsupported-filename fast path.
func FuzzProcfileParserAndSelectedTargetBinding(f *testing.F) {
	for _, source := range []string{
		"web: gunicorn api.app:app\nworker: python -m tasks\n",
		"node: node src/server.js\nnode: node other.js\n",
		"web: gunicorn api.app:app -cconfig.py\n",
		"node: node\u00a0src/server.js\n",
		"node: node src/server.js && curl https://example.invalid\n",
		strings.Repeat("x", procfileLineBytes+1) + "\nworker: python tasks.py\n",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > int(DefaultFileBytes) {
			return
		}
		first, found, err := parseProcfile("Procfile", []byte(source))
		second, repeatedFound, repeatedErr := parseProcfile("Procfile", []byte(source))
		if found != repeatedFound || (err == nil) != (repeatedErr == nil) || !reflect.DeepEqual(first, second) {
			t.Fatal("Procfile parsing is not deterministic")
		}
		if len(first) > procfileMaxLines || found != (len(first) > 0) {
			t.Fatalf("invalid retained process count: %d, recognized=%v", len(first), found)
		}
		inventory := newProcfileTargetInventory()
		for _, selected := range []string{"api/app.py", "tasks.py", "src/server.js", "other.js"} {
			inventory.add(selected)
		}
		resolveProcfileTargets(first, inventory)
		resolveProcfileTargets(second, inventory)
		if !reflect.DeepEqual(first, second) {
			t.Fatal("Procfile target binding is not deterministic")
		}
		seen := make(map[string]bool)
		for _, definition := range first {
			if seen[definition.Name] || !safeProcfileName(definition.Name) {
				t.Fatalf("invalid or repeated process name: %q", definition.Name)
			}
			seen[definition.Name] = true
			for _, evidence := range definition.Evidence {
				assertFuzzEvidenceLine(t, evidence, strings.Count(source, "\n")+1)
			}
			for _, reference := range definition.References {
				if reference.Qualification == "local" && !inventory.paths[reference.SourcePath] {
					t.Fatalf("local reference has no selected target: %+v", reference)
				}
				assertFuzzEvidenceLine(t, reference.Evidence, strings.Count(source, "\n")+1)
			}
		}
	})
}
