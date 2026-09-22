package scanner

import (
	"strings"
	"testing"
)

// FuzzParseGitAttributesBounded stresses the .gitattributes parser used on
// every scan. The seed corpus covers the syntactic features flagged by the
// r100/04-worker-and-fuzz.md review: macros, negation, `-attr` and `!attr`
// forms, C-quoted patterns, huge single lines, oversized rule counts, and
// invalid UTF-8. Each iteration asserts non-panic invariants and behavioral
// contracts, not just "does not crash".
func FuzzParseGitAttributesBounded(f *testing.F) {
	f.Add([]byte("# ordinary line\n*.go text\n"))
	f.Add([]byte("[attr]binary -text -diff\n*.bin binary\n"))
	f.Add([]byte("!ignored *.txt text\n"))
	f.Add([]byte("foo -text\nbar !text\n"))
	f.Add([]byte("\"quoted path\" text\n"))
	f.Add([]byte("\"unterminated"))
	f.Add([]byte(strings.Repeat("a", 1<<16) + " text\n"))
	f.Add([]byte(strings.Repeat("* text\n", 12000))) // >10k rules
	f.Add([]byte("*.go text\n\x80\x81\n*.md text\n"))
	f.Add([]byte("*.md linguist-language=Go\n"))
	f.Add([]byte("*.md linguist-language=NoSuchLang\n"))
	f.Add([]byte("*.dat -linguist-detectable\n"))
	f.Add([]byte("*.gen linguist-generated\n"))
	f.Add([]byte("dir/ text\n"))
	f.Add([]byte("/rooted text\n"))
	f.Add([]byte("**/*.go text\n"))
	f.Add([]byte("[abc def\n"))
	f.Add([]byte("*.log filter=lfs\n"))
	// Git 2.42.1 accepts an empty attribute list as a silent no-op. Keep the
	// Kubernetes-style case in the replay corpus; specification reference:
	// https://git-scm.com/docs/gitattributes/2.42.0#_description.
	f.Add([]byte("**/generated.proto\n"))

	f.Fuzz(func(t *testing.T, content []byte) {
		const limit = 500 // exercise the exceeded return; smaller than production
		rules1, warnings1, exceeded1 := parseGitAttributesBoundedFrom("dir/.gitattributes", "dir/.gitattributes", content, limit)
		if len(rules1) > limit {
			t.Fatalf("bound violated: %d rules with limit %d", len(rules1), limit)
		}
		if exceeded1 && len(rules1) != limit {
			t.Fatalf("exceeded reported but rules=%d != limit=%d", len(rules1), limit)
		}
		// Determinism: same input twice must produce identical outputs.
		rules2, warnings2, exceeded2 := parseGitAttributesBoundedFrom("dir/.gitattributes", "dir/.gitattributes", content, limit)
		if len(rules1) != len(rules2) || len(warnings1) != len(warnings2) || exceeded1 != exceeded2 {
			t.Fatalf("nondeterministic outputs: rules=(%d,%d) warnings=(%d,%d) exceeded=(%v,%v)", len(rules1), len(rules2), len(warnings1), len(warnings2), exceeded1, exceeded2)
		}
		for i := range rules1 {
			if rules1[i].macro != rules2[i].macro || rules1[i].scope != rules2[i].scope || rules1[i].basename != rules2[i].basename || len(rules1[i].values) != len(rules2[i].values) {
				t.Fatalf("nondeterministic rule %d", i)
			}
		}
		// Warning provenance stays confined to the declared source and matches
		// the file path exactly; the parser must never rewrite it to something
		// under the ../ prefix or an absolute path.
		for _, w := range warnings1 {
			if w.Path != "dir/.gitattributes" {
				t.Fatalf("warning path escaped source: %q", w.Path)
			}
			// The message may echo raw input bytes verbatim (including NUL or
			// invalid UTF-8); downstream JSON encoding substitutes them. We do
			// not require the message to be valid UTF-8 here, only that it stays
			// under the parser's declared line-buffer bound. A single warning is
			// derived from a single line, and the scanner buffer caps every line
			// at maxAttributesBytes (1 MiB) plus a short "line N: " prefix.
			if int64(len(w.Message)) > maxAttributesBytes+256 {
				t.Fatalf("warning message exceeds line-buffer bound: %d bytes", len(w.Message))
			}
		}
		// Rule scope must not contain traversal segments.
		for i, r := range rules1 {
			if strings.Contains(r.scope, "..") {
				t.Fatalf("rule %d scope escaped: %q", i, r.scope)
			}
		}
	})
}
