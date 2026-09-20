package scanner

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A terminal error belongs to a byte position, independent of the caller's
// buffer size. This also tests readers returning final bytes and an error together.
type boundedTestReader struct {
	data       []byte
	position   int
	chunk      int
	terminal   error
	withData   bool
	emptyEvery int
	calls      int
}

func (r *boundedTestReader) Read(p []byte) (int, error) {
	r.calls++
	if r.emptyEvery > 0 && r.calls%r.emptyEvery == 0 {
		return 0, nil
	}
	if r.position == len(r.data) {
		return 0, r.terminal
	}
	if r.chunk > 0 && len(p) > r.chunk {
		p = p[:r.chunk]
	}
	n := copy(p, r.data[r.position:])
	r.position += n
	if r.withData && r.position == len(r.data) {
		return n, r.terminal
	}
	return n, nil
}

func compareBoundedRead(t *testing.T, input []byte, limit, hint int64, chunk int, terminal error, withData bool, emptyEvery int) {
	t.Helper()
	candidate := &boundedTestReader{data: input, chunk: chunk, terminal: terminal, withData: withData, emptyEvery: emptyEvery}
	baseline := *candidate
	want, wantErr := io.ReadAll(io.LimitReader(&baseline, limit))
	got, gotErr := readAllBounded(candidate, limit, hint)
	if !reflect.DeepEqual(got, want) || gotErr != wantErr || candidate.position != baseline.position {
		t.Fatalf("size=%d limit=%d hint=%d chunk=%d terminal=%v together=%t: bytes=%d/%d error=%v/%v consumed=%d/%d", len(input), limit, hint, chunk, terminal, withData, len(got), len(want), gotErr, wantErr, candidate.position, baseline.position)
	}
}

func TestReadAllBoundedMatchesStandardReader(t *testing.T) {
	failure := errors.New("read failed")
	for _, size := range []int{0, 1, 255, 256, 511, 512, 513, 65535, 65536, 65537, 524287, 524288, 524289, 1048575, 1048576, 1048577} {
		input := bytes.Repeat([]byte("x"), size)
		limits := []int64{-1, 0, 1, int64(size), int64(size) + 1}
		for _, limit := range limits {
			for _, hint := range []int64{-1, 0, 1, int64(size) / 2, int64(size), int64(size) + 1, math.MaxInt64} {
				for _, terminal := range []error{io.EOF, io.ErrUnexpectedEOF, failure} {
					for _, together := range []bool{false, true} {
						compareBoundedRead(t, input, limit, hint, 4093, terminal, together, 0)
					}
				}
			}
		}
	}
}

func TestReadAllBoundedGeneratedShortReads(t *testing.T) {
	random := rand.New(rand.NewSource(739102))
	failure := errors.New("injected error")
	for range 1000 {
		size := random.Intn(8192)
		data := make([]byte, size)
		_, _ = random.Read(data)
		terminal := []error{io.EOF, io.ErrUnexpectedEOF, failure}[random.Intn(3)]
		compareBoundedRead(t, data, int64(random.Intn(10000)), int64(random.Intn(10000)-20), random.Intn(257)+1, terminal, random.Intn(2) == 0, 3)
	}
}

func TestReadAllBoundedDoesNotReadPastLimit(t *testing.T) {
	failure := errors.New("past limit")
	for _, hint := range []int64{1, 2, 4, math.MaxInt64} {
		r := &boundedTestReader{data: []byte("ab"), terminal: failure}
		got, err := readAllBounded(r, 2, hint)
		if string(got) != "ab" || err != nil || r.position != 2 {
			t.Fatalf("hint=%d: %q %v position=%d", hint, got, err, r.position)
		}
	}
}

func TestReadAllBoundedHintDoesNotAllocateUnboundedMemory(t *testing.T) {
	got, err := readAllBounded(bytes.NewReader(nil), math.MaxInt64, math.MaxInt64)
	if err != nil || len(got) != 0 || cap(got) > int(maxAttributesBytes+1) {
		t.Fatalf("length=%d capacity=%d error=%v", len(got), cap(got), err)
	}
	got, err = readAllBounded(bytes.NewReader([]byte("x")), 1, math.MaxInt64)
	if err != nil || string(got) != "x" || cap(got) != 1 {
		t.Fatalf("one-byte limit: %q capacity=%d error=%v", got, cap(got), err)
	}
}

func TestReadBoundedSizeObservesActualFileAndLimit(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(name, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, tc := range []struct {
		limit int64
		data  string
		large bool
	}{{0, "a", true}, {3, "abcd", true}, {6, "abcdef", false}, {7, "abcdef", false}} {
		data, large, size, err := readBoundedSize(root, "file.txt", tc.limit)
		if err != nil || string(data) != tc.data || large != tc.large || size != 6 {
			t.Fatalf("limit=%d data=%q large=%t size=%d error=%v", tc.limit, data, large, size, err)
		}
	}
}

func TestReadAllBoundedOpenedFileChangesAfterStat(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{{"grow", "x", "abcdef"}, {"shrink", "abcdef", "x"}, {"empty-to-data", "", "abc"}, {"data-to-empty", "abc", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "changing.txt")
			if err := os.WriteFile(name, []byte(tc.before), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte(tc.after), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readAllBounded(file, 4, info.Size())
			want := tc.after[:min(len(tc.after), 4)]
			if err != nil || string(got) != want {
				t.Fatalf("%q, %v; want %q", got, err, want)
			}
		})
	}
}

func BenchmarkReadAllBounded(b *testing.B) {
	for _, size := range []int{0, 64, 4096, 65536, 524288, 1048577, 2097152} {
		data := bytes.Repeat([]byte("x"), size)
		for _, hint := range []struct {
			name  string
			value int64
		}{{"accurate", int64(size)}, {"short", 1}, {"unknown", -1}, {"huge", math.MaxInt64}} {
			for _, optimized := range []bool{false, true} {
				name := "standard"
				if optimized {
					name = "hinted"
				}
				b.Run(fmt.Sprintf("%d/%s/%s", size, hint.name, name), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(size))
					for b.Loop() {
						reader := bytes.NewReader(data)
						var got []byte
						var err error
						if optimized {
							got, err = readAllBounded(reader, int64(size)+1, hint.value)
						} else {
							got, err = io.ReadAll(io.LimitReader(reader, int64(size)+1))
						}
						if err != nil || !bytes.Equal(got, data) {
							b.Fatal("read changed", err)
						}
					}
				})
			}
		}
	}
}

func TestReadAllBoundedCapacityWithLargerPolicyLimit(t *testing.T) {
	const limit = int64(16<<20) + 1
	failure := errors.New("terminal failure")
	for _, size := range []int{(1 << 20) + 2, 1153433, (2 << 20) + 3, 2202009, (4 << 20) + 5, (8 << 20) + 9} {
		input := bytes.Repeat([]byte("x"), size)
		got, err := readAllBounded(bytes.NewReader(input), limit, int64(size))
		if err != nil || !bytes.Equal(got, input) || cap(got) != size {
			t.Fatalf("accurate hint size=%d: length=%d capacity=%d error=%v", size, len(got), cap(got), err)
		}
		// A stale hint still affects allocation only: it cannot hide bytes or
		// terminal errors, or reserve its unbounded declared size.
		for _, hint := range []int64{1, int64(size) / 2, int64(size) - 1, math.MaxInt64} {
			for _, terminal := range []error{io.EOF, failure} {
				compareBoundedRead(t, input, limit, hint, 65521, terminal, true, 0)
			}
		}
	}
}
