package deployables

import (
	"math/rand"
	"testing"
)

func TestAspireBindingRangesMatchLinearReference(t *testing.T) {
	rng := rand.New(rand.NewSource(130194))
	texts := []string{"using", "global", "namespace", "Projects", "DistributedApplication", "Alias", ".", "=", ";", "{", "}"}
	for trial := 0; trial < 500; trial++ {
		tokens := make([]csToken, rng.Intn(96))
		for i := range tokens {
			tokens[i] = csToken{kind: "ident", text: texts[rng.Intn(len(texts))]}
		}
		indexed := newAspireBindingRanges(tokens)
		for start := range tokens {
			if got, want := indexed.containsProjectsBeforeSemicolon(start), referenceContainsBefore(tokens, start, "Projects", ";"); got != want {
				t.Fatalf("trial %d start %d Projects before ; = %v, want %v; tokens=%v", trial, start, got, want, tokenTexts(tokens))
			}
			if got, want := indexed.containsDistributedApplicationBeforeSemicolon(start), referenceContainsBefore(tokens, start, "DistributedApplication", ";"); got != want {
				t.Fatalf("trial %d start %d DistributedApplication before ; = %v, want %v; tokens=%v", trial, start, got, want, tokenTexts(tokens))
			}
			if got, want := indexed.containsProjectsBeforeNamespaceBoundary(start), referenceContainsBefore(tokens, start, "Projects", "{", ";"); got != want {
				t.Fatalf("trial %d start %d Projects before namespace boundary = %v, want %v; tokens=%v", trial, start, got, want, tokenTexts(tokens))
			}
		}
		if indexed.containsProjectsBeforeSemicolon(-1) || indexed.containsProjectsBeforeSemicolon(len(tokens)) {
			t.Fatalf("out-of-range start matched in trial %d", trial)
		}
	}
}

func TestAspireBindingRangesRespectLexedBoundaries(t *testing.T) {
	source := "using A = Prefix.Projects; using B = Prefix.Other; namespace Prefix.Projects { class C {} } namespace Other;"
	tokens, err := lexAspireCSharp(source)
	if err != nil {
		t.Fatal(err)
	}
	ranges := newAspireBindingRanges(tokens)
	usingA := findTokenText(tokens, "using", 0)
	usingB := findTokenText(tokens, "using", usingA+1)
	namespaceBlock := findTokenText(tokens, "namespace", 0)
	namespaceFileScoped := findTokenText(tokens, "namespace", namespaceBlock+1)
	if usingA < 0 || usingB < 0 || namespaceBlock < 0 || namespaceFileScoped < 0 {
		t.Fatal("expected all boundary-control keywords in lexed source")
	}
	if !ranges.containsProjectsBeforeSemicolon(usingA) {
		t.Fatal("qualified Projects before using semicolon was missed")
	}
	if ranges.containsProjectsBeforeSemicolon(usingB) {
		t.Fatal("Projects after using semicolon crossed into the preceding using range")
	}
	if !ranges.containsProjectsBeforeNamespaceBoundary(namespaceBlock) {
		t.Fatal("Projects before namespace opening brace was missed")
	}
	if ranges.containsProjectsBeforeNamespaceBoundary(namespaceFileScoped) {
		t.Fatal("Projects after file-scoped namespace semicolon crossed the boundary")
	}
}

func referenceContainsBefore(tokens []csToken, start int, name string, delimiters ...string) bool {
	if start < 0 || start >= len(tokens) {
		return false
	}
	for i := start + 1; i < len(tokens); i++ {
		for _, delimiter := range delimiters {
			if tokens[i].text == delimiter {
				return false
			}
		}
		if tokens[i].text == name {
			return true
		}
	}
	return false
}

func tokenTexts(tokens []csToken) []string {
	texts := make([]string, len(tokens))
	for i, token := range tokens {
		texts[i] = token.text
	}
	return texts
}

func findTokenText(tokens []csToken, text string, from int) int {
	for i := from; i < len(tokens); i++ {
		if tokens[i].text == text {
			return i
		}
	}
	return -1
}
