package structure

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type functionBenchmarkInput struct {
	response  []byte
	content   []byte
	submitted File
}

func loadFunctionBenchmark(b *testing.B, count int, functions bool) functionBenchmarkInput {
	b.Helper()
	root := filepath.Join("..", "..", "tests", "functions", "decode-performance", "inputs")
	variant := "default"
	if functions {
		variant = "functions"
	}
	file, err := os.Open(filepath.Join(root, fmt.Sprintf("%d-%s.json.gz", count, variant)))
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		b.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		b.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("%d.py", count)))
	if err != nil {
		b.Fatal(err)
	}
	input := functionBenchmarkInput{data, content, File{Path: "many.py", Language: "Python", SourceBytes: int64(len(content))}}
	if _, err := decodeResponse(data, input.submitted, content, functions); err != nil {
		b.Fatal(err)
	}
	return input
}

func BenchmarkFunctionResponse(b *testing.B) {
	for _, count := range []int{1, 10, 50, 100, 128, 500, 1000} {
		for _, functions := range []bool{false, true} {
			b.Run(fmt.Sprintf("spaces=%d/functions=%t", count, functions), func(b *testing.B) {
				input := loadFunctionBenchmark(b, count, functions)
				b.ReportAllocs()
				b.SetBytes(int64(len(input.response)))
				b.ResetTimer()
				for b.Loop() {
					if _, err := decodeResponse(input.response, input.submitted, input.content, functions); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkFunctionStages(b *testing.B) {
	input := loadFunctionBenchmark(b, 128, true)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input.response, &object); err != nil {
		b.Fatal(err)
	}
	var observations map[string]uint64
	if err := json.Unmarshal(object["observations"], &observations); err != nil {
		b.Fatal(err)
	}
	b.Run("strict-whole-response", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if !strictFunctionResponse(input.response) {
				b.Fatal("strict response rejected")
			}
		}
	})
	b.Run("standalone-function-block", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, ok := decodeFunctions(object["functions"], input.content, false, observations["syntax_nodes"]); !ok {
				b.Fatal("function block rejected")
			}
		}
	})
}
