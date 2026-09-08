package enry

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/go-enry/go-enry/v2/data"
	"github.com/go-enry/go-enry/v2/internal/tokenizer"
)

// The model is exported from the pinned official Linguist gem. Inference is a
// port of Linguist 9.7.0 Classifier#classify: log TF * inverse class frequency,
// L2 normalization, then normalized centroid similarity. Runtime is pure Go.
//
//go:embed data/centroid_model.bin
var centroidModelBinary []byte

const centroidModelMagic = "AURACENT"
const centroidModelFormatVersion uint32 = 1

type centroidModel struct {
	Vocabulary map[string]int
	ICF        []float64
	Centroids  map[string]map[int]float64
}

type centroidModelMetadata struct {
	formatVersion uint32
	sourceSHA256  string
	binarySHA256  string
	vocabulary    uint32
	languages     uint32
	entries       uint32
}

var generatedCentroidModelMetadata = centroidModelMetadata{
	formatVersion: data.CentroidModelFormatVersion,
	sourceSHA256:  data.CentroidModelSourceSHA256,
	binarySHA256:  data.CentroidModelBinarySHA256,
	vocabulary:    data.CentroidModelVocabularyCount,
	languages:     data.CentroidModelLanguageCount,
	entries:       data.CentroidModelEntryCount,
}

var centroidOnce sync.Once
var currentCentroids centroidModel

func loadCentroids() {
	if err := verifyCentroidModelSHA256(centroidModelBinary, generatedCentroidModelMetadata.binarySHA256); err != nil {
		panic(err)
	}
	model, err := decodeCentroidModel(centroidModelBinary, generatedCentroidModelMetadata)
	if err != nil {
		panic(err)
	}
	currentCentroids = model
}

func verifyCentroidModelSHA256(encoded []byte, expectedHex string) error {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("invalid generated centroid binary SHA-256")
	}
	actual := sha256.Sum256(encoded)
	if !bytes.Equal(actual[:], expected) {
		return fmt.Errorf("centroid binary SHA-256 mismatch")
	}
	return nil
}

type centroidCursor struct {
	encoded []byte
	offset  int
}

func (cursor *centroidCursor) remaining() int {
	return len(cursor.encoded) - cursor.offset
}

func (cursor *centroidCursor) take(size uint32) ([]byte, error) {
	if uint64(size) > uint64(cursor.remaining()) {
		return nil, fmt.Errorf("truncated centroid model")
	}
	start := cursor.offset
	cursor.offset += int(size)
	return cursor.encoded[start:cursor.offset], nil
}

func (cursor *centroidCursor) uint32() (uint32, error) {
	encoded, err := cursor.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(encoded), nil
}

func (cursor *centroidCursor) uint64() (uint64, error) {
	encoded, err := cursor.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(encoded), nil
}

func (cursor *centroidCursor) string() (string, error) {
	size, err := cursor.uint32()
	if err != nil {
		return "", err
	}
	encoded, err := cursor.take(size)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(encoded) {
		return "", fmt.Errorf("invalid UTF-8 in centroid model")
	}
	return string(encoded), nil
}

func boundedCentroidCount(count uint32, encodedSize, minimumRecordSize int, description string) (int, error) {
	if minimumRecordSize <= 0 || uint64(count) > uint64(encodedSize/minimumRecordSize) {
		return 0, fmt.Errorf("invalid centroid %s count", description)
	}
	return int(count), nil
}

func decodeCentroidModel(encoded []byte, metadata centroidModelMetadata) (centroidModel, error) {
	cursor := centroidCursor{encoded: encoded}
	magic, err := cursor.take(uint32(len(centroidModelMagic)))
	if err != nil || string(magic) != centroidModelMagic {
		return centroidModel{}, fmt.Errorf("invalid centroid model magic")
	}
	version, err := cursor.uint32()
	if err != nil || version != centroidModelFormatVersion || version != metadata.formatVersion {
		return centroidModel{}, fmt.Errorf("invalid centroid model format version")
	}
	sourceSHA, err := cursor.take(sha256.Size)
	if err != nil {
		return centroidModel{}, err
	}
	expectedSourceSHA, err := hex.DecodeString(metadata.sourceSHA256)
	if err != nil || len(expectedSourceSHA) != sha256.Size {
		return centroidModel{}, fmt.Errorf("invalid generated centroid source SHA-256")
	}
	if !bytes.Equal(sourceSHA, expectedSourceSHA) {
		return centroidModel{}, fmt.Errorf("centroid source SHA-256 mismatch")
	}
	vocabularyCount, err := cursor.uint32()
	if err != nil {
		return centroidModel{}, err
	}
	languageCount, err := cursor.uint32()
	if err != nil {
		return centroidModel{}, err
	}
	entryCount, err := cursor.uint32()
	if err != nil {
		return centroidModel{}, err
	}
	if vocabularyCount != metadata.vocabulary || languageCount != metadata.languages || entryCount != metadata.entries {
		return centroidModel{}, fmt.Errorf("centroid model counts do not match generated metadata")
	}
	vocabularySize, err := boundedCentroidCount(vocabularyCount, len(encoded), 12, "vocabulary")
	if err != nil {
		return centroidModel{}, err
	}
	languageSize, err := boundedCentroidCount(languageCount, len(encoded), 8, "language")
	if err != nil {
		return centroidModel{}, err
	}
	if _, err := boundedCentroidCount(entryCount, len(encoded), 12, "entry"); err != nil {
		return centroidModel{}, err
	}

	model := centroidModel{
		Vocabulary: make(map[string]int, vocabularySize),
		ICF:        make([]float64, vocabularySize),
		Centroids:  make(map[string]map[int]float64, languageSize),
	}
	for index := 0; index < vocabularySize; index++ {
		token, err := cursor.string()
		if err != nil {
			return centroidModel{}, err
		}
		if _, exists := model.Vocabulary[token]; exists {
			return centroidModel{}, fmt.Errorf("duplicate centroid vocabulary token")
		}
		model.Vocabulary[token] = index
	}
	for index := range model.ICF {
		bits, err := cursor.uint64()
		if err != nil {
			return centroidModel{}, err
		}
		value := math.Float64frombits(bits)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return centroidModel{}, fmt.Errorf("non-finite centroid ICF value")
		}
		model.ICF[index] = value
	}
	previousLanguage := ""
	var decodedEntries uint64
	for languageIndex := 0; languageIndex < languageSize; languageIndex++ {
		language, err := cursor.string()
		if err != nil {
			return centroidModel{}, err
		}
		if languageIndex > 0 && language <= previousLanguage {
			return centroidModel{}, fmt.Errorf("centroid languages are not strictly increasing")
		}
		previousLanguage = language
		count, err := cursor.uint32()
		if err != nil {
			return centroidModel{}, err
		}
		centroidSize, err := boundedCentroidCount(count, cursor.remaining(), 12, "language entry")
		if err != nil {
			return centroidModel{}, err
		}
		decodedEntries += uint64(count)
		if decodedEntries > uint64(entryCount) {
			return centroidModel{}, fmt.Errorf("centroid entry count exceeds header")
		}
		centroid := make(map[int]float64, centroidSize)
		var previousIndex uint32
		for entryIndex := 0; entryIndex < centroidSize; entryIndex++ {
			index, err := cursor.uint32()
			if err != nil {
				return centroidModel{}, err
			}
			if index >= vocabularyCount || entryIndex > 0 && index <= previousIndex {
				return centroidModel{}, fmt.Errorf("invalid centroid vocabulary index order")
			}
			previousIndex = index
			bits, err := cursor.uint64()
			if err != nil {
				return centroidModel{}, err
			}
			value := math.Float64frombits(bits)
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return centroidModel{}, fmt.Errorf("non-finite centroid value")
			}
			centroid[int(index)] = value
		}
		model.Centroids[language] = centroid
	}
	if decodedEntries != uint64(entryCount) {
		return centroidModel{}, fmt.Errorf("centroid entry count does not match header")
	}
	if cursor.remaining() != 0 {
		return centroidModel{}, fmt.Errorf("trailing centroid model bytes")
	}
	return model, nil
}

type centroidClassifier struct{}

func (*centroidClassifier) classify(content []byte, candidates map[string]float64) []string {
	if len(candidates) == 0 {
		return nil
	}
	centroidOnce.Do(loadCentroids)
	// Ruby caps inference at 50 KiB, while training sees complete samples.
	if len(content) > 50*1024 {
		content = content[:50*1024]
	}
	counts := map[int]float64{}
	for _, token := range tokenizer.LinguistTokenize(content) {
		if index, ok := currentCentroids.Vocabulary[token]; ok {
			counts[index]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	// Stable token order avoids map-iteration-dependent floating point noise.
	indices := make([]int, 0, len(counts))
	for index := range counts {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	norm := 0.0
	for _, index := range indices {
		value := (1 + math.Log(counts[index])) * currentCentroids.ICF[index]
		counts[index] = value
		norm += value * value
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return nil
	}
	scored := make([]*scoredLanguage, 0, len(candidates))
	for language := range candidates {
		if alias, ok := GetLanguageByAlias(language); ok {
			language = alias
		}
		fsName := language
		if info, err := GetLanguageInfo(language); err == nil && info.FSName != "" {
			fsName = info.FSName
		}
		centroid := currentCentroids.Centroids[fsName]
		score := 0.0
		for _, index := range indices {
			score += (counts[index] / norm) * centroid[index]
		}
		if score > 0 {
			scored = append(scored, &scoredLanguage{language: language, score: score})
		}
	}
	// Ruby preserves candidate order for ties; generated candidates are sorted
	// by canonical name, so make that order explicit before its stable score sort.
	sort.Slice(scored, func(i, j int) bool { return scored[i].language < scored[j].language })
	return sortLanguagesByScore(scored)
}
