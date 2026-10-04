package intentmap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestReusableTokensEqualFreshTokens(t *testing.T) {
	var reused []sourceToken
	for _, lang := range []string{"java", "kotlin", "cs", "vb", "js"} {
		for _, source := range []string{
			"", "import org.postgresql.Driver;\nusing Npgsql;",
			"/* import fake */\nconst s = `text ${`nested`}`; require('pg');",
			"\n\nnamespace App { using Redis = StackExchange.Redis; }",
			"REM Imports Npgsql\nImports StackExchange.Redis",
			strings.Repeat(";", DefaultMaxLexicalTokensPerFile+1),
			"import org.springframework.web.bind.annotation.RestController;",
		} {
			fresh, wantLimited := lexSource(source, lang)
			got, limited := lexSourceInto(source, lang, reused)
			if !reflect.DeepEqual(got, fresh) || limited != wantLimited {
				t.Fatalf("reused lexer differs for %s: tokens %d/%d; limit %t/%t", lang, len(got), len(fresh), limited, wantLimited)
			}
			clear(got)
			reused = got[:0]
		}
	}
}

func TestTokenBufferReleaseClearsSourceReferencesAndCapsRetention(t *testing.T) {
	tokens, _ := lexSource("import org.postgresql.Driver;", "java")
	backing := tokens[:cap(tokens)]
	buffer := &tokenBuffer{tokens: tokens}
	buffer.release()
	for i, token := range backing {
		if token != (sourceToken{}) {
			t.Fatalf("source reference retained at token %d: %+v", i, token)
		}
	}
	if len(buffer.tokens) != 0 {
		t.Fatal("released buffer retained tokens")
	}
	large := &tokenBuffer{tokens: make([]sourceToken, 1, DefaultMaxLexicalTokensPerFile+1)}
	large.tokens[0].text = "source reference"
	largeBacking := large.tokens
	large.release()
	if large.tokens != nil || largeBacking[0] != (sourceToken{}) {
		t.Fatal("oversized buffer retained capacity or source references")
	}
}

func TestImportObservationsSurviveConcurrentBufferReuse(t *testing.T) {
	parsers := []struct {
		name, source string
		parse        func(string, []byte) ([]Observation, bool)
	}{
		{"Api.java", "import org.postgresql.Driver;", parseJVMImportsBounded},
		{"Api.cs", "using StackExchange.Redis;", parseDotnetImportsBounded},
		{"api.js", "import pg from 'pg';", parseJSImportsBounded},
	}
	for _, parser := range parsers {
		t.Run(parser.name, func(t *testing.T) {
			observed, limited := parser.parse(parser.name, []byte(parser.source))
			if limited || len(observed) == 0 {
				t.Fatal("fixture did not produce complete import evidence")
			}
			before, _ := json.Marshal(observed)
			var workers sync.WaitGroup
			for range 8 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					for range 20 {
						for _, next := range parsers {
							next.parse(next.name, []byte(next.source))
						}
					}
				}()
			}
			workers.Wait()
			after, _ := json.Marshal(observed)
			if string(before) != string(after) {
				t.Fatal("retained observations changed during buffer reuse")
			}
		})
	}
}

func FuzzReusableImportTokens(f *testing.F) {
	f.Add(byte(0), "/* comment */ import org.postgresql.Driver;", "")
	f.Add(byte(4), "const x = `a ${`b`}`; require('pg');", "require('redis');")
	f.Add(byte(3), "REM Imports Npgsql\nImports StackExchange.Redis", "\n")
	f.Fuzz(func(t *testing.T, kind byte, first, second string) {
		if len(first)+len(second) > DefaultMaxFileBytes {
			return
		}
		languages := []string{"java", "kotlin", "cs", "vb", "js"}
		var reused []sourceToken
		for i, source := range []string{first, second, first} {
			lang := languages[(int(kind)+i)%len(languages)]
			fresh, wantLimited := lexSource(source, lang)
			got, limited := lexSourceInto(source, lang, reused)
			if !reflect.DeepEqual(fresh, got) || limited != wantLimited {
				t.Fatalf("reused tokens differ for %s", lang)
			}
			clear(got)
			reused = got[:0]
		}
	})
}

func BenchmarkImportObservations(b *testing.B) {
	for _, size := range []int{8 << 10, 48 << 10} {
		source := []byte("import org.postgresql.Driver;\n" + strings.Repeat("class A { int x = 1; }\n", size/23))
		b.Run(fmt.Sprintf("java-%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for range b.N {
				parseJVMImportsBounded("Api.java", source)
			}
		})
	}
}
