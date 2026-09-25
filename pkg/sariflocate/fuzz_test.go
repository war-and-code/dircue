package sariflocate

import (
	"path"
	"strings"
	"testing"
)

// A URI admitted as a map-relative SARIF location must stay within the root.
func FuzzResolveURIConfinesPaths(f *testing.F) {
	for _, seed := range []string{
		"src/main.go", "../secret", "%2e%2e/secret", "a/%2e%2e/b.go",
		"file:///source/src/main.go", "file:///outside/private", "C:%5csecret", "%00",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, uri string) {
		rel, status := resolveURI(uri, "", nil, "/source", true)
		if status != "" {
			return
		}
		if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") || path.IsAbs(rel) || strings.ContainsRune(rel, '\x00') {
			t.Fatalf("URI %q escaped root as %q", uri, rel)
		}
	})
}
