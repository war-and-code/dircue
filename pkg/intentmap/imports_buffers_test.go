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

func TestDenseTokenGrowthRetainsOnlyTheReusableWindow(t *testing.T) {
	source := strings.Repeat("word;", (48<<10)/5)
	tokens, limited := lexSource(source, "java")
	if !limited || len(tokens) != DefaultMaxLexicalTokensPerFile || cap(tokens) != DefaultMaxLexicalTokensPerFile {
		t.Fatalf("dense token window: len=%d cap=%d limited=%t", len(tokens), cap(tokens), limited)
	}
	backing := &tokens[0]
	clear(tokens)
	reused, limited := lexSourceInto(source, "java", tokens[:0])
	if !limited || len(reused) != len(tokens) || &reused[0] != backing {
		t.Fatal("dense token window was reallocated or its cutoff changed")
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
			observed, limited := parseJVMImportsBounded("Api.java", source)
			if len(observed) != 1 || observed[0].Name != "datastore:postgresql" || limited != (size == 48<<10) {
				b.Fatalf("unexpected import evidence: observations=%+v limited=%t", observed, limited)
			}
			// A truncated stream does not inspect all offered bytes. Avoid
			// presenting that input size as scanned-byte throughput.
			if !limited {
				b.SetBytes(int64(len(source)))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				parseJVMImportsBounded("Api.java", source)
			}
			b.ReportMetric(float64(len(source)), "input-B/op")
		})
	}
}

// Keep a literal oracle: lexSource and lexSourceInto share their scanner, so
// comparing only those functions cannot catch a common lexical regression.
func TestReusableTokensMatchLiteralOracle(t *testing.T) {
	cases := []struct {
		lang, source string
		want         []sourceToken
	}{
		{"java", "/* hidden import */\nimport org.postgresql.Driver;\n{ \"opaque\" }", []sourceToken{
			{"import", 'i', 2, 0}, {"org", 'i', 2, 0}, {".", 'p', 2, 0},
			{"postgresql", 'i', 2, 0}, {".", 'p', 2, 0}, {"Driver", 'i', 2, 0},
			{";", 'p', 2, 0}, {"{", 'p', 3, 0}, {"opaque", 's', 3, 1}, {"}", 'p', 3, 0},
		}},
		{"kotlin", "/* outer\n/* inner */ */\nimport pg as db", []sourceToken{
			{"import", 'i', 3, 0}, {"pg", 'i', 3, 0}, {"as", 'i', 3, 0}, {"db", 'i', 3, 0},
		}},
		{"cs", "using X = Npgsql;\n{ @\"a\"\"b\" }", []sourceToken{
			{"using", 'i', 1, 0}, {"X", 'i', 1, 0}, {"=", 'p', 1, 0}, {"Npgsql", 'i', 1, 0},
			{";", 'p', 1, 0}, {"{", 'p', 2, 0}, {"a\"\"b", 's', 2, 1}, {"}", 'p', 2, 0},
		}},
		{"vb", "REM Imports Fake\nImports Npgsql\n' hidden\n\"opaque\" : REM hidden\nImports Redis", []sourceToken{
			{"Imports", 'i', 2, 0}, {"Npgsql", 'i', 2, 0}, {"opaque", 's', 4, 0},
			{":", 'p', 4, 0}, {"Imports", 'i', 5, 0}, {"Redis", 'i', 5, 0},
		}},
		{"js", "const r = /import fake/;\nrequire('pg');", []sourceToken{
			{"const", 'i', 1, 0}, {"r", 'i', 1, 0}, {"=", 'p', 1, 0}, {";", 'p', 1, 0},
			{"require", 'i', 2, 0}, {"(", 'p', 2, 0}, {"pg", 's', 2, 0},
			{")", 'p', 2, 0}, {";", 'p', 2, 0},
		}},
	}
	reused := make([]sourceToken, 0, 32)
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			for _, storage := range [][]sourceToken{nil, reused} {
				got, limited := lexSourceInto(tc.source, tc.lang, storage)
				if limited || !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("literal token oracle mismatch: limited=%t\ngot:  %+v\nwant: %+v", limited, got, tc.want)
				}
				clear(got)
				reused = got[:0]
			}
		})
	}
}

func TestReusableTokenLimitExactBoundary(t *testing.T) {
	reused := make([]sourceToken, 0, DefaultMaxLexicalTokensPerFile)
	for _, tc := range []struct {
		name, source string
		wantCount    int
		wantLimited  bool
	}{
		{"exact", strings.Repeat(";", DefaultMaxLexicalTokensPerFile), DefaultMaxLexicalTokensPerFile, false},
		{"ignored suffix", strings.Repeat(";", DefaultMaxLexicalTokensPerFile) + " \n/* ignored */", DefaultMaxLexicalTokensPerFile, false},
		{"extra token", strings.Repeat(";", DefaultMaxLexicalTokensPerFile+1), DefaultMaxLexicalTokensPerFile, true},
		{"empty after dense", "", 0, false},
		{"small after dense", "import pg;", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, storage := range [][]sourceToken{nil, reused} {
				got, limited := lexSourceInto(tc.source, "java", storage)
				if len(got) != tc.wantCount || limited != tc.wantLimited {
					t.Fatalf("token boundary: len=%d limited=%t; want len=%d limited=%t", len(got), limited, tc.wantCount, tc.wantLimited)
				}
				clear(got)
				reused = got[:0]
			}
		})
	}
}
