package enry

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/go-enry/go-enry/v2/internal/tokenizer"
)

type wireTestEntry struct {
	index uint32
	bits  uint64
}

type wireTestLanguage struct {
	name    string
	entries []wireTestEntry
}

func appendWireUint32(encoded []byte, value uint32) []byte {
	return binary.LittleEndian.AppendUint32(encoded, value)
}

func appendWireUint64(encoded []byte, value uint64) []byte {
	return binary.LittleEndian.AppendUint64(encoded, value)
}

func appendWireString(encoded []byte, value string) []byte {
	encoded = appendWireUint32(encoded, uint32(len(value)))
	return append(encoded, value...)
}

func testCentroidWire(tokens []string, icf []uint64, languages []wireTestLanguage) ([]byte, centroidModelMetadata) {
	source := sha256.Sum256([]byte("test source"))
	var entries uint32
	for _, language := range languages {
		entries += uint32(len(language.entries))
	}
	encoded := append([]byte{}, centroidModelMagic...)
	encoded = appendWireUint32(encoded, 1)
	encoded = append(encoded, source[:]...)
	encoded = appendWireUint32(encoded, uint32(len(tokens)))
	encoded = appendWireUint32(encoded, uint32(len(languages)))
	encoded = appendWireUint32(encoded, entries)
	for _, token := range tokens {
		encoded = appendWireString(encoded, token)
	}
	for _, bits := range icf {
		encoded = appendWireUint64(encoded, bits)
	}
	for _, language := range languages {
		encoded = appendWireString(encoded, language.name)
		encoded = appendWireUint32(encoded, uint32(len(language.entries)))
		for _, entry := range language.entries {
			encoded = appendWireUint32(encoded, entry.index)
			encoded = appendWireUint64(encoded, entry.bits)
		}
	}
	digest := sha256.Sum256(encoded)
	return encoded, centroidModelMetadata{
		formatVersion: 1,
		sourceSHA256:  hex.EncodeToString(source[:]),
		binarySHA256:  hex.EncodeToString(digest[:]),
		vocabulary:    uint32(len(tokens)),
		languages:     uint32(len(languages)),
		entries:       entries,
	}
}

func validTestCentroidWire() ([]byte, centroidModelMetadata) {
	return testCentroidWire(
		[]string{"a", "b"},
		[]uint64{math.Float64bits(1), math.Float64bits(2)},
		[]wireTestLanguage{
			{"A", []wireTestEntry{{0, math.Float64bits(.5)}, {1, math.Float64bits(.25)}}},
			{"B", []wireTestEntry{{1, math.Float64bits(.75)}}},
		},
	)
}

func TestGeneratedCentroidModel(t *testing.T) {
	if err := verifyCentroidModelSHA256(centroidModelBinary, generatedCentroidModelMetadata.binarySHA256); err != nil {
		t.Fatal(err)
	}
	model, err := decodeCentroidModel(centroidModelBinary, generatedCentroidModelMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if got := uint32(len(model.Vocabulary)); got != generatedCentroidModelMetadata.vocabulary {
		t.Fatalf("vocabulary count %d, want %d", got, generatedCentroidModelMetadata.vocabulary)
	}
	if got := uint32(len(model.ICF)); got != generatedCentroidModelMetadata.vocabulary {
		t.Fatalf("ICF count %d, want %d", got, generatedCentroidModelMetadata.vocabulary)
	}
	if got := uint32(len(model.Centroids)); got != generatedCentroidModelMetadata.languages {
		t.Fatalf("language count %d, want %d", got, generatedCentroidModelMetadata.languages)
	}
	var entries uint32
	for _, centroid := range model.Centroids {
		entries += uint32(len(centroid))
	}
	if entries != generatedCentroidModelMetadata.entries {
		t.Fatalf("entry count %d, want %d", entries, generatedCentroidModelMetadata.entries)
	}
}

func modelScoreBits(model centroidModel, content []byte, candidates []string) map[string]uint64 {
	counts := map[int]float64{}
	for _, token := range tokenizer.LinguistTokenize(content) {
		if index, ok := model.Vocabulary[token]; ok {
			counts[index]++
		}
	}
	indices := make([]int, 0, len(counts))
	for index := range counts {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	norm := 0.0
	for _, index := range indices {
		value := (1 + math.Log(counts[index])) * model.ICF[index]
		counts[index] = value
		norm += value * value
	}
	norm = math.Sqrt(norm)
	scores := make(map[string]uint64, len(candidates))
	for _, language := range candidates {
		score := 0.0
		for _, index := range indices {
			score += (counts[index] / norm) * model.Centroids[language][index]
		}
		scores[language] = math.Float64bits(score)
	}
	return scores
}

func TestCentroidModelScoreBitsFixture(t *testing.T) {
	model, err := decodeCentroidModel(centroidModelBinary, generatedCentroidModelMetadata)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("namespace Example { public record Person(string Name, int Age); static void Main() { Console.WriteLine(42); } }")
	want := map[string]uint64{
		"C#":         0x3fd8779783b1b48d,
		"Java":       0x3fcefb6c6a7e916f,
		"Smalltalk":  0x3faa5e62dfbc55fd,
		"TypeScript": 0x3fc79cc382527788,
	}
	got := modelScoreBits(model, content, []string{"C#", "Java", "Smalltalk", "TypeScript"})
	for language, bits := range want {
		if got[language] != bits {
			t.Errorf("%s score bits %#x, want %#x", language, got[language], bits)
		}
	}
}

func TestCentroidModelDigestRejectsCorruption(t *testing.T) {
	encoded, metadata := validTestCentroidWire()
	if err := verifyCentroidModelSHA256(encoded, metadata.binarySHA256); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte{}, encoded...)
	corrupt[len(corrupt)-1] ^= 1
	if err := verifyCentroidModelSHA256(corrupt, metadata.binarySHA256); err == nil {
		t.Fatal("corrupt binary passed SHA-256 verification")
	}
	for _, invalid := range []string{"", "xyz", strings.Repeat("0", 62)} {
		if err := verifyCentroidModelSHA256(encoded, invalid); err == nil {
			t.Fatalf("invalid generated digest %q was accepted", invalid)
		}
	}
}

func TestCentroidModelMalformedInputs(t *testing.T) {
	valid, metadata := validTestCentroidWire()
	clone := func() []byte { return append([]byte{}, valid...) }
	tests := []struct {
		name string
		make func() ([]byte, centroidModelMetadata)
		want string
	}{
		{"empty", func() ([]byte, centroidModelMetadata) { return nil, metadata }, "magic"},
		{"magic", func() ([]byte, centroidModelMetadata) { b := clone(); b[0] ^= 1; return b, metadata }, "magic"},
		{"version", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[8:12], 2)
			return b, metadata
		}, "version"},
		{"metadata cannot bless unknown version", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[8:12], 2)
			m := metadata
			m.formatVersion = 2
			return b, m
		}, "version"},
		{"source", func() ([]byte, centroidModelMetadata) { b := clone(); b[12] ^= 1; return b, metadata }, "source SHA-256"},
		{"generated source", func() ([]byte, centroidModelMetadata) { m := metadata; m.sourceSHA256 = "bad"; return clone(), m }, "generated centroid source"},
		{"metadata count", func() ([]byte, centroidModelMetadata) { m := metadata; m.vocabulary++; return clone(), m }, "generated metadata"},
		{"count bound", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[44:48], math.MaxUint32)
			m := metadata
			m.vocabulary = math.MaxUint32
			return b, m
		}, "vocabulary count"},
		{"truncated", func() ([]byte, centroidModelMetadata) { return clone()[:len(valid)-1], metadata }, "count"},
		{"trailing", func() ([]byte, centroidModelMetadata) { return append(clone(), 0), metadata }, "trailing"},
		{"duplicate token", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a", "a"}, []uint64{math.Float64bits(1), math.Float64bits(2)}, []wireTestLanguage{})
		}, "duplicate"},
		{"invalid UTF-8", func() ([]byte, centroidModelMetadata) { b := clone(); b[60] = 0xff; return b, metadata }, "UTF-8"},
		{"nonfinite ICF", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(math.Inf(1))}, nil)
		}, "non-finite"},
		{"language order", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(1)}, []wireTestLanguage{{"B", nil}, {"A", nil}})
		}, "strictly increasing"},
		{"duplicate language", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(1)}, []wireTestLanguage{{"A", nil}, {"A", nil}})
		}, "strictly increasing"},
		{"out of range index", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(1)}, []wireTestLanguage{{"A", []wireTestEntry{{1, math.Float64bits(1)}}}})
		}, "index order"},
		{"duplicate index", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(1)}, []wireTestLanguage{{"A", []wireTestEntry{{0, math.Float64bits(1)}, {0, math.Float64bits(1)}}}})
		}, "index order"},
		{"descending index", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a", "b"}, []uint64{math.Float64bits(1), math.Float64bits(1)}, []wireTestLanguage{{"A", []wireTestEntry{{1, math.Float64bits(1)}, {0, math.Float64bits(1)}}}})
		}, "index order"},
		{"nonfinite centroid", func() ([]byte, centroidModelMetadata) {
			return testCentroidWire([]string{"a"}, []uint64{math.Float64bits(1)}, []wireTestLanguage{{"A", []wireTestEntry{{0, math.Float64bits(math.NaN())}}}})
		}, "non-finite"},
		{"entry total low", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[52:56], 2)
			m := metadata
			m.entries = 2
			return b, m
		}, "exceeds header"},
		{"entry total high", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[52:56], 4)
			m := metadata
			m.entries = 4
			return b, m
		}, "does not match"},
		{"string length", func() ([]byte, centroidModelMetadata) {
			b := clone()
			binary.LittleEndian.PutUint32(b[56:60], math.MaxUint32)
			return b, metadata
		}, "truncated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, expected := test.make()
			_, err := decodeCentroidModel(encoded, expected)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCentroidModelRejectsEveryTruncation(t *testing.T) {
	encoded, metadata := validTestCentroidWire()
	for size := range encoded {
		if _, err := decodeCentroidModel(encoded[:size], metadata); err == nil {
			t.Fatalf("accepted truncation at byte %d of %d", size, len(encoded))
		}
	}
}
