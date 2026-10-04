package projects

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkGradleLiteralMembership(b *testing.B) {
	for _, count := range []int{4, 256} {
		b.Run(fmt.Sprintf("members_%d", count), func(b *testing.B) {
			var source strings.Builder
			source.WriteString("rootProject.name = 'benchmark-suite'\n")
			for i := 0; i < count; i++ {
				fmt.Fprintf(&source, "include(':service%d')\n", i)
			}
			content := []byte(source.String())
			document := ParseJVM("settings.gradle.kts", content)
			if len(document.References) != count || len(document.Diagnostics) != 0 {
				b.Fatalf("benchmark fixture did not produce %d literal members: %+v", count, document)
			}
			for i, reference := range document.References {
				if reference.Target != fmt.Sprintf("service%d", i) || reference.State != "conditional" {
					b.Fatalf("benchmark fixture has a wrong member: %+v", reference)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(content)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(ParseJVM("settings.gradle.kts", content).References) != count {
					b.Fatal("fixture changed during benchmark")
				}
			}
		})
	}
}
