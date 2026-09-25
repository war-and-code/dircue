package scanner

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"testing"

	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/objfile"
)

func TestReadAllBoundedCompressedGitStreams(t *testing.T) {
	cases := 0
	for _, size := range []int{0, 1, 2, 511, 512, 513, 4096, 16384, 32768, 65536, 65537, 131072} {
		content := make([]byte, size)
		for i := range content {
			content[i] = byte((i * 71) ^ (i >> 7))
		}
		var buf bytes.Buffer
		writer := zlib.NewWriter(&buf)
		if _, err := fmt.Fprintf(writer, "blob %d\x00", size); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		compressed := buf.Bytes()
		variants := [][]byte{compressed}
		for tail := 1; tail <= 8 && tail < len(compressed); tail++ {
			variants = append(variants, compressed[:len(compressed)-tail])
		}
		for tail := 1; tail <= 4 && tail < len(compressed); tail++ {
			damaged := append([]byte(nil), compressed...)
			damaged[len(damaged)-tail] ^= 255
			variants = append(variants, damaged)
		}
		for variant, payload := range variants {
			for _, limit := range []int64{-1, 0, 1, int64(size - 1), int64(size), int64(size + 1), int64(size + 512)} {
				for _, hint := range []int64{0, 1, int64(size), int64(size + 1), 1 << 40} {
					baseline, err := objfile.NewReader(bytes.NewReader(payload))
					if err != nil {
						continue
					}
					_, _, err = baseline.Header()
					if err != nil {
						baseline.Close()
						continue
					}
					candidate, err := objfile.NewReader(bytes.NewReader(payload))
					if err != nil {
						t.Fatal(err)
					}
					_, _, err = candidate.Header()
					if err != nil {
						t.Fatal(err)
					}
					want, werr := io.ReadAll(io.LimitReader(baseline, limit))
					got, gerr := readAllBounded(candidate, limit, hint)
					baseline.Close()
					candidate.Close()
					cases++
					if !bytes.Equal(got, want) || fmt.Sprint(gerr) != fmt.Sprint(werr) {
						t.Fatalf("size=%d variant=%d limit=%d hint=%d: bytes %d/%d errors %v/%v", size, variant, limit, hint, len(got), len(want), gerr, werr)
					}
				}
			}
		}
	}
	if cases < 4000 {
		t.Fatalf("insufficient admitted compressed cases: %d", cases)
	}
	t.Logf("%d compressed Git read comparisons", cases)
}
