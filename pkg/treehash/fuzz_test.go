package treehash

import "testing"

// FuzzWildmatch checks that pattern matching terminates without panicking for
// any pattern and path, and is deterministic. Differential correctness against
// Git is covered by TestComputeMatchesGitOnRandomTrees.
func FuzzWildmatch(f *testing.F) {
	for _, seed := range [][2]string{
		{"**/deep", "a/b/deep"}, {"[!a]*.txt", "b.txt"}, {"a/**/b", "a/x/y/b"}, {"[[:digit:]]*", "1x"},
		{"\\#h", "#h"}, {"*[", "x["}, {"[]a]*", "]b"}, {"x.???", "x.abc"}, {"**", "a/b"}, {"[a-", "a"},
	} {
		f.Add(seed[0], seed[1], true)
		f.Add(seed[0], seed[1], false)
	}
	f.Fuzz(func(t *testing.T, pattern, text string, pathname bool) {
		if len(pattern) > 256 || len(text) > 1024 {
			return
		}
		if wildmatch(pattern, text, pathname) != wildmatch(pattern, text, pathname) {
			t.Fatal("wildmatch is not deterministic")
		}
	})
}

// FuzzParseRules checks that ignore and attribute parsing never panics and
// that resolution of the parsed rules terminates.
func FuzzParseRules(f *testing.F) {
	f.Add([]byte("*.txt text eol=crlf\n[attr]m text\n\"a\\040b\" -text\n"), "a/b.txt")
	f.Add([]byte("!keep\nbuild/\ntrailing\\ \n\xef\xbb\xbf#c\n"), "build/x")
	f.Fuzz(func(t *testing.T, content []byte, rel string) {
		if len(content) > 4096 || len(rel) > 512 {
			return
		}
		attrs := parseAttributes(content, "", true)
		_ = cleanAction(newAttrStack([]attrFile{attrs}).resolve(rel))
		ign := &ignoreStack{patterns: parseIgnore(content, "")}
		_ = ign.excluded(rel, false)
		_ = ign.excluded(rel, true)
	})
}
