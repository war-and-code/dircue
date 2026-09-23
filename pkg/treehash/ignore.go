package treehash

import (
	"bytes"
	"strings"
)

// parseIgnore parses one ignore file (.gitignore or $GIT_DIR/info/exclude)
// for the directory given as path components. It follows Git's dir.c rules:
// a UTF-8 BOM is skipped, '#' starts a comment unless escaped, and trailing
// spaces are dropped unless escaped with a backslash. A carriage return
// before the line feed is dropped, as add_patterns_from_buffer does.
func parseIgnore(content []byte, base string) []gitPattern {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	var out []gitPattern
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = trimTrailingSpaces(line)
		if line == "" {
			continue
		}
		if p, ok := parsePathPattern(line, base, true); ok && p.pattern != "" {
			out = append(out, p)
		}
	}
	return out
}

// trimTrailingSpaces mirrors Git's trim_trailing_spaces: unescaped trailing
// spaces are removed, and an escaped space ends the trimming.
func trimTrailingSpaces(s string) string {
	end := len(s)
	lastSpace := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			if lastSpace < 0 {
				lastSpace = i
			}
		case '\\':
			i++
			lastSpace = -1
		default:
			lastSpace = -1
		}
	}
	if lastSpace >= 0 {
		end = lastSpace
	}
	return s[:end]
}

// ignoreStack holds patterns in increasing priority: info/exclude first, then
// .gitignore files from the root down. Later patterns win.
type ignoreStack struct {
	patterns []gitPattern
}

func (s *ignoreStack) with(more []gitPattern) *ignoreStack {
	if len(more) == 0 {
		return s
	}
	next := make([]gitPattern, 0, len(s.patterns)+len(more))
	next = append(next, s.patterns...)
	next = append(next, more...)
	return &ignoreStack{patterns: next}
}

// excluded reports whether path is ignored: the highest-priority matching
// pattern decides, and a negated pattern re-includes.
func (s *ignoreStack) excluded(rel string, isDir bool) bool {
	for i := len(s.patterns) - 1; i >= 0; i-- {
		if s.patterns[i].matches(rel, isDir) {
			return !s.patterns[i].negative
		}
	}
	return false
}
