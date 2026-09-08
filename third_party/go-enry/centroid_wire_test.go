package enry

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
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

// This fixture contains the 21 vocabulary entries used by the input below,
// copied from the canonical Linguist JSON identified by source_sha256. Entries
// retain original vocabulary order. Missing centroid weights are zero, as in
// classification. Decimal JSON values round-trip to the original float64 bits.
const centroidScoreFixture = `{"source_sha256":"13a5bb39a79e60c5bdb08f49c069445e664b475e82a9a65b018590293cd75743",
"candidates":["C#","Java","Smalltalk","TypeScript"],
"entries":[
{"token":"(","index":734,"icf":1.1925699712853226,"centroids":[0.1663035129670237,0.11296491259271876,0.05384967233068738,0.19757367679559257]},
{"token":"()","index":740,"icf":1.7399784802440443,"centroids":[0.0845686076784111,0.12020482007799052,0,0.11321276507580416]},
{"token":")","index":746,"icf":1.1878268793893099,"centroids":[0.16561270330580535,0.1090393355893858,0.06285904701915151,0.1962038860653798]},
{"token":",","index":770,"icf":1.1925699712853226,"centroids":[0.08009195906126025,0.10112019516676803,0.08097321877878998,0.12012162575571964]},
{"token":".WriteLine","index":1498,"icf":6.030437921392435,"centroids":[0.209959077137826,0,0,0]},
{"token":";","index":4176,"icf":1.5553764207513643,"centroids":[0.25543692734187445,0.15166985678217107,0.15123180780370493,0.22604635371721749]},
{"token":"Age","index":4813,"icf":6.253581472706645,"centroids":[0,0,0,0]},
{"token":"Console","index":5754,"icf":5.154969184038536,"centroids":[0.1050456210501385,0,0,0]},
{"token":"Example","index":6526,"icf":4.504381617897386,"centroids":[0,0,0,0]},
{"token":"Main","index":8401,"icf":4.2058886293413895,"centroids":[0.06164243379598141,0,0,0]},
{"token":"Name","index":8750,"icf":3.496741107435003,"centroids":[0.01555163973229054,0.020798917576237502,0,0]},
{"token":"Person","index":9402,"icf":4.80666248977032,"centroids":[0,0.019983350650829377,0,0]},
{"token":"int","index":17563,"icf":2.5584714688420727,"centroids":[0.010902816374432828,0.11823189243890256,0,0]},
{"token":"namespace","index":19330,"icf":3.6508917872622613,"centroids":[0.15895243117508528,0.007734459613083652,0,0]},
{"token":"public","index":20838,"icf":3.3632097148104805,"centroids":[0.16522612499401826,0.23773573131124984,0,0.06704716588997496]},
{"token":"record","index":21158,"icf":4.2058886293413895,"centroids":[0,0.004919085933997946,0,0]},
{"token":"static","index":22806,"icf":3.257849199152654,"centroids":[0.07461216141856918,0.20658432576269184,0,0]},
{"token":"string","index":22912,"icf":2.307157040561167,"centroids":[0.08554035074669435,0,0.003716273226219492,0.0940874044861261]},
{"token":"void","index":24405,"icf":3.1180872567774953,"centroids":[0.10036453423334313,0.10880266033024878,0,0.04890009421526387]},
{"token":"{","index":24891,"icf":1.3957089331627996,"centroids":[0.16711624121020194,0.11683512696351654,0.02657049661350504,0.16056361711424463]},
{"token":"}","index":24903,"icf":1.376477571234912,"centroids":[0.16481355986857105,0.11522526507961041,0.026204383862603542,0.16542621223561704]}
]}`

func TestCentroidModelScoreBitsFixture(t *testing.T) {
	model, err := decodeCentroidModel(centroidModelBinary, generatedCentroidModelMetadata)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SourceSHA256 string `json:"source_sha256"`
		Candidates   []string
		Entries      []struct {
			Token     string
			Index     int
			ICF       float64
			Centroids []float64
		}
	}
	if err := json.Unmarshal([]byte(centroidScoreFixture), &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SourceSHA256 != generatedCentroidModelMetadata.sourceSHA256 {
		t.Fatal("score fixture belongs to a different canonical model")
	}
	reference := centroidModel{
		Vocabulary: make(map[string]int),
		ICF:        make([]float64, len(fixture.Entries)),
		Centroids:  make(map[string]map[int]float64),
	}
	for _, language := range fixture.Candidates {
		reference.Centroids[language] = make(map[int]float64)
	}
	for index, entry := range fixture.Entries {
		if index > 0 && fixture.Entries[index-1].Index >= entry.Index {
			t.Fatal("fixture must preserve original vocabulary order")
		}
		if original, ok := model.Vocabulary[entry.Token]; !ok || original != entry.Index {
			t.Fatalf("vocabulary entry changed for %q", entry.Token)
		}
		if math.Float64bits(model.ICF[entry.Index]) != math.Float64bits(entry.ICF) {
			t.Fatalf("ICF bits changed for %q", entry.Token)
		}
		if len(entry.Centroids) != len(fixture.Candidates) {
			t.Fatal("fixture centroid count differs from its candidates")
		}
		reference.Vocabulary[entry.Token] = index
		reference.ICF[index] = entry.ICF
		for candidate, language := range fixture.Candidates {
			weight := entry.Centroids[candidate]
			if math.Float64bits(model.Centroids[language][entry.Index]) != math.Float64bits(weight) {
				t.Fatalf("centroid bits changed for %s/%q", language, entry.Token)
			}
			reference.Centroids[language][index] = weight
		}
	}
	content := []byte("namespace Example { public record Person(string Name, int Age); static void Main() { Console.WriteLine(42); } }")
	// math.Log and floating-point arithmetic can round differently by architecture.
	// Compare both representations on this host without relaxing bit equality.
	want := modelScoreBits(reference, content, fixture.Candidates)
	got := modelScoreBits(model, content, fixture.Candidates)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded model score bits %#v, want JSON fixture %#v", got, want)
	}
	ranking := GetLanguagesByClassifier("Example.cs", content, fixture.Candidates)
	if want := []string{"C#", "Java", "TypeScript", "Smalltalk"}; !reflect.DeepEqual(ranking, want) {
		t.Errorf("classifier ranking %v, want %v", ranking, want)
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
