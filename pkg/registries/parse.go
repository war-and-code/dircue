package registries

import (
	"bytes"
	"unicode/utf8"
)

// Parse observes supported declarations in one complete configuration. It does
// not retain input bytes or expose parser errors, credentials or raw excerpts.
func Parse(filename string, content []byte) (Configuration, error) {
	eco, ok := MatchPath(filename)
	if !ok || !safePath(filename) {
		return Configuration{}, ErrCandidate
	}
	c := configuration(filename, eco)
	if int64(len(content)) > MaxFileBytes {
		c.fail("file_size_limit", "not_read")
		return c, nil
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		c.fail("unsupported_encoding", "unsupported")
		return c, nil
	}
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	// XML declarations can be validated before any endpoint or label sanitizer
	// runs, so prepare the shared expressions on the registry opt-in path.
	preparePatterns()
	switch eco {
	case "nuget":
		parseXML(&c, content)
	case "npm":
		parseNPM(&c, content)
	case "maven":
		parseMavenSettings(&c, content)
	case "cargo":
		parseCargoConfig(&c, content)
	}
	return c, nil
}
