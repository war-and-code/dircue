package deployables

import (
	"fmt"
	"testing"
)

func BenchmarkAspireBindingRangesWorstCaseKeywordArray(b *testing.B) {
	for _, size := range []int{1024, 8192, maxAspireCSharpTokens} {
		b.Run(fmt.Sprintf("tokens_%d", size), func(b *testing.B) {
			tokens := make([]csToken, size)
			for i := range tokens {
				if i%2 == 0 {
					tokens[i] = csToken{kind: "ident", text: "using"}
				} else {
					tokens[i] = csToken{kind: "ident", text: "Alias"}
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				ranges := newAspireBindingRanges(tokens)
				for i := 0; i < len(tokens); i += 2 {
					if ranges.containsProjectsBeforeSemicolon(i) || ranges.containsDistributedApplicationBeforeSemicolon(i) {
						b.Fatal("keyword-only input unexpectedly matched an Aspire name")
					}
				}
			}
		})
	}
}

func BenchmarkAspireBindingRangesPositiveAndBoundaryControls(b *testing.B) {
	tokens := []csToken{
		{kind: "ident", text: "using"},
		{kind: "ident", text: "Alias"},
		{kind: "ident", text: "="},
		{kind: "ident", text: "Projects"},
		{kind: "ident", text: ";"},
		{kind: "ident", text: "using"},
		{kind: "ident", text: "Alias"},
		{kind: "ident", text: "="},
		{kind: "ident", text: "Other"},
		{kind: "ident", text: ";"},
		{kind: "ident", text: "namespace"},
		{kind: "ident", text: "Projects"},
		{kind: "ident", text: "{"},
		{kind: "ident", text: "namespace"},
		{kind: "ident", text: "Other"},
		{kind: "ident", text: ";"},
	}
	ranges := newAspireBindingRanges(tokens)
	if !ranges.containsProjectsBeforeSemicolon(0) || ranges.containsProjectsBeforeSemicolon(5) ||
		!ranges.containsProjectsBeforeNamespaceBoundary(10) || ranges.containsProjectsBeforeNamespaceBoundary(13) {
		b.Fatal("positive or boundary control failed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if !ranges.containsProjectsBeforeSemicolon(0) || !ranges.containsProjectsBeforeNamespaceBoundary(10) {
			b.Fatal("positive control changed during benchmark")
		}
	}
}
