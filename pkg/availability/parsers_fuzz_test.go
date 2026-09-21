package availability

import (
	"crypto/sha1"
	"encoding/binary"
	"reflect"
	"testing"
)

const fuzzInputLimit = 128 << 10

func FuzzAvailabilityParsers(f *testing.F) {
	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nsize 123\n")
	gitmodules := []byte("[submodule \"lib\"]\npath = vendor/lib\n")
	config := []byte("[core]\nsparseCheckout = true\n")
	emptyIndex := fuzzEmptyIndexSeed()
	index := fuzzSparseIndexSeed()
	for _, seed := range []struct {
		mode byte
		data []byte
	}{
		{0, pointer}, {0, pointer[:24]},
		{1, gitmodules}, {1, gitmodules[:18]},
		{2, config}, {2, config[:13]},
		{3, emptyIndex}, {3, index}, {3, index[:24]},
	} {
		f.Add(append([]byte{seed.mode}, seed.data...))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) == 0 {
			return
		}
		mode := input[0] % 4
		data := input[1:]
		if len(data) > fuzzInputLimit {
			data = data[:fuzzInputLimit]
		}
		fullSize := int64(len(data))
		switch mode {
		case 0:
			first := InspectLFSPointer(data, fullSize)
			second := InspectLFSPointer(data, fullSize)
			if first != second {
				t.Fatal("LFS pointer inspection is nondeterministic")
			}
			truncated := InspectLFSPointer(data, fullSize+1)
			if truncated.Kind == "valid_pointer" {
				t.Fatal("truncated LFS content produced complete pointer metadata")
			}
		case 1:
			first := ParseGitmodules(".gitmodules", data, fullSize, fuzzInputLimit)
			second := ParseGitmodules(".gitmodules", data, fullSize, fuzzInputLimit)
			assertGitmodulesParseBounded(t, first)
			if !reflect.DeepEqual(first, second) {
				t.Fatal(".gitmodules parsing is nondeterministic")
			}
			truncated := ParseGitmodules(".gitmodules", data, fullSize+1, fuzzInputLimit)
			assertGitmodulesParseBounded(t, truncated)
			if truncated.Complete || len(truncated.Declarations) != 0 {
				t.Fatal("truncated .gitmodules content produced declaration claims")
			}
		case 2:
			first := InspectSparseConfig(".git/config", data, fullSize, fuzzInputLimit)
			second := InspectSparseConfig(".git/config", data, fullSize, fuzzInputLimit)
			assertSparseParseBounded(t, first)
			if !reflect.DeepEqual(first, second) {
				t.Fatal("sparse configuration parsing is nondeterministic")
			}
			truncated := InspectSparseConfig(".git/config", data, fullSize+1, fuzzInputLimit)
			assertSparseParseBounded(t, truncated)
			if truncated.Complete || len(truncated.Indications) != 0 {
				t.Fatal("truncated sparse configuration produced indication claims")
			}
		case 3:
			assertIndexParse(t, data)
			// Preserve the raw checksum-rejection lane above, and add a lane that
			// repairs SHA-1 after mutations so structured changes can reach entry
			// and extension invariants.
			if len(data) >= 12+sha1.Size {
				rehashed := append([]byte(nil), data...)
				sum := sha1.Sum(rehashed[:len(rehashed)-sha1.Size])
				copy(rehashed[len(rehashed)-sha1.Size:], sum[:])
				assertIndexParse(t, rehashed)
			}
		}
	})
}

func assertIndexParse(t *testing.T, data []byte) {
	t.Helper()
	fullSize := int64(len(data))
	first := InspectGitIndex(".git/index", data, fullSize, fuzzInputLimit)
	second := InspectGitIndex(".git/index", data, fullSize, fuzzInputLimit)
	assertSparseParseBounded(t, first)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("Git index parsing is nondeterministic")
	}
	truncated := InspectGitIndex(".git/index", data, fullSize+1, fuzzInputLimit)
	assertSparseParseBounded(t, truncated)
	if truncated.Complete || len(truncated.Indications) != 0 {
		t.Fatal("truncated Git index produced sparse indication claims")
	}
}

func assertGitmodulesParseBounded(t *testing.T, parsed GitmodulesParse) {
	t.Helper()
	if len(parsed.Declarations) > DefaultEvidenceLimit || len(parsed.Diagnostics) > DefaultDiagnosticLimit || parsed.OmittedDeclarations < 0 || parsed.OmittedDiagnostics < 0 {
		t.Fatalf("unbounded .gitmodules parse: declarations=%d diagnostics=%d", len(parsed.Declarations), len(parsed.Diagnostics))
	}
}

func assertSparseParseBounded(t *testing.T, parsed SparseMetadataParse) {
	t.Helper()
	if len(parsed.Indications) > DefaultEvidenceLimit || len(parsed.Diagnostics) > DefaultDiagnosticLimit || parsed.OmittedIndications < 0 || parsed.OmittedDiagnostics < 0 {
		t.Fatalf("unbounded sparse metadata parse: indications=%d diagnostics=%d", len(parsed.Indications), len(parsed.Diagnostics))
	}
}

func fuzzEmptyIndexSeed() []byte {
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[8:12], 0)
	sum := sha1.Sum(data)
	return append(data, sum[:]...)
}

func fuzzSparseIndexSeed() []byte {
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[8:12], 1)
	name := "cold/file.bin"
	entry := make([]byte, 62)
	binary.BigEndian.PutUint32(entry[24:28], 0100644)
	binary.BigEndian.PutUint16(entry[60:62], uint16(len(name))|0x4000)
	data = append(data, entry...)
	data = append(data, 0x40, 0x00)
	data = append(data, name...)
	data = append(data, 0)
	for (len(data)-12)%8 != 0 {
		data = append(data, 0)
	}
	data = append(data, "sdir"...)
	data = append(data, 0, 0, 0, 0)
	sum := sha1.Sum(data)
	return append(data, sum[:]...)
}
