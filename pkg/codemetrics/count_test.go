package codemetrics

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/boyter/scc/v4/processor"
)

func TestGrammars(t *testing.T) {
	for name, grammar := range grammarNames {
		if _, ok := processor.LanguageDatabase()[grammar]; !ok {
			t.Errorf("%q maps to unknown grammar %q", name, grammar)
		}
	}
	for _, tt := range []struct{ filename, language, want string }{
		{"x.cs", "C#", "C#"}, {"x.java", "Java", "Java"},
		{"x.m", "Objective-C", "Objective C"}, {"x.m", "MATLAB", "MATLAB"},
		{"x.tsx", "TSX", "TypeScript"}, {"x.cs", "Python", "Python"},
		{"x.cs", "not-a-language", ""}, {"x.cs", "", ""},
		{"x.vb", "Visual Basic .NET", "Visual Basic"},
	} {
		got, ok := Grammar(tt.filename, tt.language)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("%s %s: %q %v", tt.filename, tt.language, got, ok)
		}
	}
	if processor.Version != EngineVersion {
		t.Fatalf("engine version %s differs from %s", processor.Version, EngineVersion)
	}
}

func TestCountReference(t *testing.T) {
	fixtures := []struct{ name, language, content string }{
		{"record.cs", "C#", "// heading\n\nnamespace Example;\npublic record Person(string Name, int Age);\nif (person.Age != 0) { Console.WriteLine(person.Name); }\n"},
		{"verbatim.cs", "C#", "var text = @\"first\n// string, not comment\nlast\";\n/* actual comment */\n"},
		{"raw.cs", "C#", "var text = \"\"\"\n// string, not comment\n/* also string */\n\"\"\";\n// actual comment\n"},
		{"textblock.java", "Java", "class Example {\n  String text = \"\"\"\n  // string, not comment\n  /* also string */\n  \"\"\";\n}\n"},
		{"switch.java", "Java", "// heading\nrecord Point(int x, int y) {}\nclass Example {\n  int f(Object x) {\n    return switch (x) { case String s -> s.length(); default -> 0; };\n  }\n}\n"},
		{"plain.go", "Go", "package main\r\n\r\n// comment\r\nfunc main() {}"},
		{"indent.py", "Python", "# heading\n\ndef f(x):\n    if x:\n        return x\n"},
		{"strings.tsx", "TSX", "// heading\nconst view = <div>{thing?.name ?? `/*text*/`}</div>;\n"},
		{"empty.java", "Java", ""},
		{"bom.cs", "C#", "\xef\xbb\xbf// comment\nclass X {}\n"},
		{"unfinished.cs", "C#", "var text = \"unclosed\n/* text\n"},
	}
	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte(tt.content)
			before := bytes.Clone(content)
			got, err := Count(context.Background(), tt.name, tt.language, content)
			if err != nil {
				t.Fatal(err)
			}
			grammar, _ := Grammar(tt.name, tt.language)
			// Direct upstream use has no adapter callback, validation, or result conversion.
			job := processor.FileJob{Language: grammar, Filename: tt.name, Content: content, Bytes: int64(len(content))}
			processor.CountStats(&job)
			want := Counts{Grammar: grammar, Lines: job.Lines, Code: job.Code, Comment: job.Comment, Blank: job.Blank, Complexity: job.Complexity}
			if got != want {
				t.Errorf("got %+v, upstream %+v", got, want)
			}
			if got.Lines != got.Code+got.Comment+got.Blank {
				t.Errorf("inconsistent lines: %+v", got)
			}
			if !bytes.Equal(content, before) {
				t.Fatal("counter changed source bytes")
			}
		})
	}
}

func TestCountKnownResults(t *testing.T) {
	for _, language := range []string{"C#", "Java"} {
		got, err := Count(context.Background(), "example", language, []byte("// comment\n\nif (x) { call(); }\n"))
		want := Counts{Grammar: language, Lines: 3, Code: 1, Comment: 1, Blank: 1, Complexity: 1}
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", language, got, err, want)
		}
	}
}

func TestCountRejectsIncompleteOrUnsupportedInputs(t *testing.T) {
	for _, tt := range []struct {
		language string
		content  []byte
		want     error
	}{
		{"Unknown", []byte("class X {}"), ErrUnsupported},
		{"C#", []byte{0xff, 0xfe, 'x', 0}, ErrEncoding},
		{"Java", []byte("class X {}\x00"), ErrBinary},
		{"Java", []byte(strings.Repeat(" ", 12000) + "\x00"), ErrBinary},
	} {
		got, err := Count(context.Background(), "x.cs", tt.language, tt.content)
		if !errors.Is(err, tt.want) || got != (Counts{}) {
			t.Errorf("got %+v, %v; want %v", got, err, tt.want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Count(ctx, "x.cs", "C#", []byte("class X {}")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// Cancel deterministically from the counter's first line callback, without timing assumptions.
type lineCancelContext struct {
	context.Context
	calls int
}

func (c *lineCancelContext) Err() error {
	c.calls++
	if c.calls >= 3 {
		return context.Canceled
	}
	return nil
}
func TestCountCancellationDiscardsPartialResult(t *testing.T) {
	ctx := &lineCancelContext{Context: context.Background()}
	got, err := Count(ctx, "x.java", "Java", []byte(strings.Repeat("class X {}\n", 1000)))
	if !errors.Is(err, context.Canceled) || got != (Counts{}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCountConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, language := range []string{"C#", "Java", "Go", "Python"} {
				got, err := Count(context.Background(), "sample", language, []byte("\n"))
				want := Counts{Grammar: language, Lines: 1, Blank: 1}
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("got %+v, %v", got, err)
				}
			}
		}()
	}
	wg.Wait()
}
