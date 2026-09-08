package tokenizer

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
)

// This oracle uses the unchanged longest-match regexp and ordered capture
// groups. It checks the fast path's consumed extent and lexical action, not
// merely the emitted token (which is truncated and could conceal overreading).
func checkIdentifierDecision(t *testing.T, content []byte) {
	t.Helper()
	size := asciiIdentifierLength(content)
	wantGuard := len(content) > 0 && bytes.ContainsRune([]byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_"), rune(content[0]))
	if (size > 0) != wantGuard {
		t.Fatalf("guard for %q: size=%d, want guard=%t", content, size, wantGuard)
	}
	if size == 0 {
		return
	}
	for _, lexer := range []compiledLexer{ordinaryLexer, beginningLexer} {
		match := lexer.expression.FindSubmatchIndex(content)
		if match == nil || match[0] != 0 || match[1] != size {
			t.Fatalf("extent for %q: fast=%d, reference=%v", content, size, match)
		}
		selected := -1
		for i, group := range lexer.groups {
			if match[group*2] >= 0 {
				selected = i
				break
			}
		}
		if selected < 0 || lexer.rules[selected].pattern != `[.@#$]?[A-Za-z0-9_]+` || lexer.rules[selected].action != "feed" {
			t.Fatalf("reference selected a competing rule for %q: %d", content, selected)
		}
	}
}

func TestASCIIIdentifierLexicalBoundaries(t *testing.T) {
	checkIdentifierDecision(t, nil)
	// Every possible first byte and next byte, with a later identifier that
	// must remain untouched when the next byte terminates this identifier.
	for first := 0; first < 256; first++ {
		for next := 0; next < 256; next++ {
			checkIdentifierDecision(t, []byte{byte(first), byte(next), 'z', '9', '_'})
		}
	}
	for _, length := range []int{1, 15, 16, 17, 31, 255, 100000, 100001} {
		for _, ending := range [][]byte{nil, {'\n'}, {'\r', '\n'}, {0}, {0xff}, []byte("é"), []byte("/*comment*/next")} {
			content := append(bytes.Repeat([]byte{'_'}, length), ending...)
			checkIdentifierDecision(t, content)
		}
	}
}

func TestIdentifierFastPathOrderedTokens(t *testing.T) {
	inputs := [][]byte{
		nil, {}, []byte("a"), []byte("_"), []byte("a012_Z"),
		[]byte("0xdeadbeef 123abc 1e+2 5.2F .name @name #name $name"),
		[]byte("identifier\n#! /usr/bin/env python\nnext"),
		[]byte("identifier\n // comment\nnext\n-- comment\nfinal"),
		[]byte("before/* blocked identifier */after\n\"quoted identifier\" tail"),
		[]byte("before\"escaped\\\" identifier\"after\n'''block'''tail"),
		[]byte("before\"unterminated\x00after"),
		[]byte("\xef\xbb\xbfalpha é_beta \xffname\x00last"),
		[]byte("a\n.ig\nignored identifiers\n..\nlast"),
		[]byte("a\n#!/usr/bin/env KEY=value python -x\nlast"),
	}
	for _, boundary := range []int{15, 16, 17, 99999, 100000, 100001} {
		inputs = append(inputs, bytes.Repeat([]byte{'a'}, boundary))
		input := bytes.Repeat([]byte{'a'}, boundary)
		inputs = append(inputs, append(input, []byte("\n#! /bin/sh\nnext")...))
	}
	for _, input := range inputs {
		if got, want := LinguistTokenize(input), referenceLinguistTokenize(input); !reflect.DeepEqual(got, want) {
			t.Fatalf("ordered tokens differ for %d bytes (%q): got %q, want %q", len(input), input[:min(len(input), 80)], got, want)
		}
	}
}

func TestIdentifierFastPathDeterministicRandom(t *testing.T) {
	random := rand.New(rand.NewSource(97001))
	alphabet := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789 \t\r\n#@.$'\"/\\*-+(){}[];\x00\xff\xc3\xa9")
	for trial := 0; trial < 10000; trial++ {
		content := make([]byte, random.Intn(256))
		for i := range content {
			content[i] = alphabet[random.Intn(len(alphabet))]
		}
		checkIdentifierDecision(t, content)
		if got, want := LinguistTokenize(content), referenceLinguistTokenize(content); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed=97001 trial=%d content=%q: got %q, want %q", trial, content, got, want)
		}
	}
}
