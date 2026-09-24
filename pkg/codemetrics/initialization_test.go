package codemetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/war-and-code/dircue/pkg/codemetrics/internal/sccprocessor"
)

// Independent processes keep upstream globals isolated: eager construction and
// on-demand construction must produce identical counts for every mapped language.
func TestLazyInitializationParity(t *testing.T) {
	eager := initializationSubprocess(t, "eager")
	lazy := initializationSubprocess(t, "lazy")
	if !reflect.DeepEqual(eager, lazy) {
		for name, want := range eager {
			if got := lazy[name]; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: lazy %+v, eager %+v", name, got, want)
			}
		}
		if len(eager) != len(lazy) {
			t.Errorf("lazy covered %d languages, eager covered %d", len(lazy), len(eager))
		}
	}
	if len(lazy) != len(grammarNames) {
		t.Fatalf("covered %d mappings, want %d", len(lazy), len(grammarNames))
	}
}

func TestLazyInitializationLoadsOnlyUsedGrammars(t *testing.T) {
	initializationSubprocess(t, "inventory")
}

func initializationSubprocess(t *testing.T, mode string) map[string][]Counts {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInitializationHelper$")
	command.Env = append(os.Environ(), "DIRCUE_TEST_SCC_INITIALIZATION="+mode)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s helper: %v\n%s", mode, err, output)
	}
	const marker = "DIRCUE_COUNTS:"
	for _, line := range strings.Split(string(output), "\n") {
		if payload, ok := strings.CutPrefix(line, marker); ok {
			var counts map[string][]Counts
			if err := json.Unmarshal([]byte(payload), &counts); err != nil {
				t.Fatal(err)
			}
			return counts
		}
	}
	t.Fatalf("%s helper produced no counts: %s", mode, output)
	return nil
}

func TestInitializationHelper(t *testing.T) {
	mode := os.Getenv("DIRCUE_TEST_SCC_INITIALIZATION")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	if len(processor.LanguageFeatures) != 0 {
		t.Fatal("feature construction happened before counting")
	}
	results := make(map[string][]Counts)
	if mode == "inventory" {
		Grammar("x.java", "Java")
		if len(processor.LanguageFeatures) != 0 {
			t.Fatal("grammar resolution constructed features")
		}
		for i, language := range []string{"Java", "C#", "Java"} {
			got, err := Count(context.Background(), "source", language, []byte("// comment\nif (x) {}\n"))
			if err != nil {
				t.Fatal(err)
			}
			results[language] = append(results[language], got)
			if want := min(i+1, 2); len(processor.LanguageFeatures) != want {
				t.Fatalf("after %s: loaded %d grammars, want %d", language, len(processor.LanguageFeatures), want)
			}
		}
	} else {
		if mode == "eager" {
			processor.ProcessConstants()
		} else if mode != "lazy" {
			t.Fatalf("unknown mode %q", mode)
		}
		for language, grammar := range grammarNames {
			for _, content := range initializationInputs(processor.LanguageDatabase()[grammar]) {
				var counts Counts
				if mode == "lazy" {
					var err error
					counts, err = Count(context.Background(), "source", language, content)
					if err != nil {
						t.Fatalf("%s: %v", language, err)
					}
				} else {
					job := processor.FileJob{Filename: "source", Language: grammar, Content: content, Bytes: int64(len(content))}
					processor.CountStats(&job)
					counts = Counts{Grammar: grammar, Lines: job.Lines, Code: job.Code, Comment: job.Comment, Blank: job.Blank, Complexity: job.Complexity}
				}
				results[language] = append(results[language], counts)
			}
		}
		if mode == "lazy" {
			unique := make(map[string]bool)
			for _, grammar := range grammarNames {
				unique[grammar] = true
			}
			if len(processor.LanguageFeatures) != len(unique) {
				t.Fatalf("loaded %d grammars, want %d", len(processor.LanguageFeatures), len(unique))
			}
		}
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("DIRCUE_COUNTS:%s\n", encoded)
}

func initializationInputs(language processor.Language) [][]byte {
	inputs := [][]byte{nil, []byte("\n\r\n"), []byte("value = 1\n// comment?\nif (value) {}\n")}
	for _, marker := range language.LineComment {
		inputs = append(inputs, []byte(marker+" comment\n\nvalue\n"))
	}
	for _, pair := range language.MultiLine {
		inputs = append(inputs, []byte(pair[0]+" first\nsecond\n"+pair[1]+"\nvalue\n"))
	}
	for _, quote := range language.Quotes {
		inputs = append(inputs, []byte(quote.Start+"text // not a comment /* */"+quote.End+"\n"))
	}
	for _, token := range language.ComplexityChecks {
		inputs = append(inputs, []byte(token+" value\n"))
	}
	return inputs
}
