package cli

import (
	"bytes"
	"fmt"
	"testing"
)

// BenchmarkCountLines measures standalone-file metadata independently of file
// reading and classification. Its generated payloads stay within the 1 MiB
// inspection boundary; these timings do not describe repository scanning.
func BenchmarkCountLines(b *testing.B) {
	for _, ending := range []struct {
		name string
		data string
	}{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
		for _, size := range []int{16 << 10, 128 << 10, 1 << 20} {
			b.Run(fmt.Sprintf("%s/%d", ending.name, size), func(b *testing.B) {
				line := []byte("public class Example {}" + ending.data)
				count := size / len(line)
				content := bytes.Repeat(line, count)
				b.SetBytes(int64(len(content)))
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					lines, sloc := countLines(content)
					if lines != count || sloc != count {
						b.Fatalf("got %d lines/%d sloc, want %d each", lines, sloc, count)
					}
				}
			})
		}
	}
}
