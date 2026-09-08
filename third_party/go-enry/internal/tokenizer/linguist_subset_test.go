package tokenizer

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

type lexicalDecision struct {
	ruleID int
	size   int
}

func decision(t *testing.T, lexer *compiledLexer, content []byte) lexicalDecision {
	t.Helper()
	match := lexer.expression.FindSubmatchIndex(content)
	if match == nil {
		return lexicalDecision{-1, 0}
	}
	if match[0] != 0 {
		t.Fatal("lexer match was not anchored")
	}
	for _, group := range lexer.groups {
		if match[group*2] >= 0 {
			name := lexer.expression.SubexpNames()[group]
			if len(name) < 2 || name[0] != 'r' {
				t.Fatalf("unexpected lexical rule capture %q", name)
			}
			ruleID, err := strconv.Atoi(name[1:])
			if err != nil {
				t.Fatalf("unexpected lexical rule capture %q: %v", name, err)
			}
			return lexicalDecision{ruleID, match[1]}
		}
	}
	t.Fatal("matched expression without a selected lexical rule")
	return lexicalDecision{}
}

func checkSubsetDecision(t *testing.T, full *compiledLexer, content []byte) {
	t.Helper()
	subset := full
	if len(content) > 0 {
		subset = full.forInitialByte(content[0])
	}
	if got, want := decision(t, subset, content), decision(t, full, content); got != want {
		t.Fatalf("subset decision for %q: got %+v, want %+v", content, got, want)
	}
}

func TestASCIISubsetsCurrentRules(t *testing.T) {
	for _, lexer := range []*compiledLexer{&ordinaryLexer, &beginningLexer} {
		for first := 0; first < 256; first++ {
			for next := 0; next < 256; next++ {
				checkSubsetDecision(t, lexer, []byte{byte(first), byte(next), 'a', '0', '\n'})
			}
			for _, ending := range []string{"", "abc", "23e+5", "0x123", "\n", " comment\n", "*comment*/", "#!/bin/sh\n", "é", "\x00\xff"} {
				checkSubsetDecision(t, lexer, append([]byte{byte(first)}, []byte(ending)...))
			}
			if first >= 128 && lexer.forInitialByte(byte(first)) != lexer {
				t.Fatalf("non-ASCII byte %d did not retain the full lexer", first)
			}
		}
		if lexer.forInitialByte('0') != lexer.forInitialByte('1') {
			t.Error("identical numeric rule masks were not interned")
		}
		if lexer.forInitialByte(' ') != lexer.forInitialByte('\t') {
			t.Error("identical whitespace rule masks were not interned")
		}
	}
}

func TestASCIISubsetsFutureRegexConstructs(t *testing.T) {
	// Alternations, nullable repeats, captures and zero-width constraints must
	// not remove an eligible rule. Unicode folding can reach ASCII.
	patterns := []string{
		`(?i:K)`, `(?i:s)`, `(?i)abc`, `(?:a?)*b`, `(?:ab|a)c?`,
		`(a)(b)?`, `(?P<inner>x)y?`, `\bword`, `\Bword`, `^abc$`,
		`(?m:^a$)`, `(?:\A|\b)x`, `a{0,3}b`, `[^\na]`, `\p{Greek}`,
		`(?s:.)`, `.`, `(?:x|)`, `a*`, `(?:)`, `\z`, `[a&&b]`,
		`a+?`, `(?:a?|b?)*c`, `[^\x00-\x{10FFFF}]`,
	}
	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			// Competing rules before/after the candidate expose selection-order
			// mistakes, including ties and empty matches when no rune matches.
			lexer := compileLexer([]lexicalRule{
				{`abc|word|xy`, "feed", "", ""},
				{pattern, "static", "selected", ""},
				{`abc|a|b|x|K|S`, "feed", "", ""},
				{`(?s:.)`, "skip", "", ""},
			})
			checkSubsetDecision(t, &lexer, nil)
			for first := 0; first < 256; first++ {
				for _, tail := range []string{"", "abc", "word", "b", "bc", "xy", "\n", "\x00", "é"} {
					checkSubsetDecision(t, &lexer, append([]byte{byte(first)}, []byte(tail)...))
				}
			}
			for _, input := range []string{"K", "ſ", "Σ", "é", "\xff", "\xc3", "ab\n", "word\n"} {
				checkSubsetDecision(t, &lexer, []byte(input))
			}
		})
	}
	// A nullable-only grammar is not used by the tokenizer, but future analysis
	// must retain its zero-length match and original first-rule tie winner.
	nullable := compileLexer([]lexicalRule{{`(?:)`, "skip", "", ""}, {`a*`, "feed", "", ""}})
	for _, input := range []string{"", "z", "aaa", "\n"} {
		checkSubsetDecision(t, &nullable, []byte(input))
	}
	// No candidate mask should weaken full-lexer fallback or its no-match result.
	narrow := compileLexer([]lexicalRule{{`abc`, "feed", "", ""}})
	for _, input := range []string{"", "abc", "xyz"} {
		checkSubsetDecision(t, &narrow, []byte(input))
	}
}

func TestASCIISubsetsAbove64Rules(t *testing.T) {
	var rules []lexicalRule
	for i := 0; i < 90; i++ {
		rules = append(rules, lexicalRule{fmt.Sprintf("a%d", i), "feed", "", ""})
	}
	rules = append(rules, lexicalRule{`z+`, "static", "last", ""}, lexicalRule{`(?s:.)`, "skip", "", ""})
	lexer := compileLexer(rules)
	for i := 0; i < 90; i++ {
		checkSubsetDecision(t, &lexer, []byte(fmt.Sprintf("a%d", i)))
	}
	for _, input := range []string{"zzz", "a89x", "!", "\xff"} {
		checkSubsetDecision(t, &lexer, []byte(input))
	}
	if got := decision(t, lexer.forInitialByte('z'), []byte("zzz")); got.ruleID != 90 {
		t.Fatalf("rule beyond 64 lost its original identity: %+v", got)
	}
}

func TestASCIISubsetsOrderedTokens(t *testing.T) {
	random := rand.New(rand.NewSource(97002))
	alphabet := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789 \t\r\n#@.$'\"/\\*-+(){}[];\x00\xff\xc3\xa9")
	for trial := 0; trial < 10000; trial++ {
		content := make([]byte, random.Intn(256))
		for i := range content {
			content[i] = alphabet[random.Intn(len(alphabet))]
		}
		for _, lexer := range []*compiledLexer{&ordinaryLexer, &beginningLexer} {
			checkSubsetDecision(t, lexer, content)
		}
		if got, want := LinguistTokenize(content), referenceLinguistTokenize(content); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed=97002 trial=%d content=%q: got %q, want %q", trial, content, got, want)
		}
	}
	for _, boundary := range []int{15, 16, 17, 99999, 100000, 100001} {
		content := append(bytes.Repeat([]byte(" "), boundary), []byte("\n// comment\nnext")...)
		if got, want := LinguistTokenize(content), referenceLinguistTokenize(content); !reflect.DeepEqual(got, want) {
			t.Fatalf("input cap or whitespace state differs at %d", boundary)
		}
	}
}
