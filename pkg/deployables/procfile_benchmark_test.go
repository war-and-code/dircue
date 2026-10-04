package deployables

import (
	"fmt"
	"strings"
	"testing"
)

// The fixtures assert a known answer before timing; benchmarks do not replace
// the map-level attribution tests or the parser's negative cases.
func BenchmarkProcfileParseAndResolve(b *testing.B) {
	for _, count := range []int{4, procfileMaxLines} {
		b.Run(fmt.Sprintf("processes_%d", count), func(b *testing.B) {
			var source strings.Builder
			inventory := newProcfileTargetInventory()
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, "process%d: node service%d.js --port $PORT\n", i, i)
				inventory.add(fmt.Sprintf("service%d.js", i))
			}
			content := []byte(source.String())
			definitions, recognized, err := parseProcfile("Procfile", content)
			if err != nil || !recognized || len(definitions) != count || resolveProcfileTargets(definitions, inventory) != 0 {
				b.Fatalf("benchmark fixture did not produce %d resolved declarations: %v", count, err)
			}
			for i, definition := range definitions {
				if definition.References[0].Value != fmt.Sprintf("service%d.js", i) || definition.References[0].Qualification != "local" {
					b.Fatalf("benchmark fixture has a wrong target: %+v", definition)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(content)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				definitions, _, err := parseProcfile("Procfile", content)
				if err != nil || resolveProcfileTargets(definitions, inventory) != 0 {
					b.Fatal("fixture changed during benchmark", err)
				}
			}
		})
	}
}
