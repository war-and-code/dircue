package treehash

import "strings"

// This file ports the parts of Git's wildmatch.c and dir.c that decide whether
// an ignore or attributes pattern matches a path. Generic glob libraries differ
// from Git in '**', bracket, escape and basename rules, and any difference
// changes the tree ID, so the behavior is reproduced here directly.

type wildResult int

const (
	wmMatch wildResult = iota
	wmNoMatch
	wmAbortAll
	wmAbortToStarStar
)

// wildmatch reports whether text matches pattern. With pathname set, '*' and
// '?' do not match '/', and "**" follows Git's directory rules.
func wildmatch(pattern, text string, pathname bool) bool {
	return dowild(pattern, text, pathname) == wmMatch
}

func isGlobSpecial(c byte) bool {
	return c == '*' || c == '?' || c == '[' || c == '\\'
}

func dowild(p, text string, pathname bool) wildResult {
	pi, ti := 0, 0
	for ; pi < len(p); pi, ti = pi+1, ti+1 {
		pch := p[pi]
		var tch byte
		if ti < len(text) {
			tch = text[ti]
		} else if pch != '*' {
			return wmAbortAll
		}
		switch pch {
		case '\\':
			pi++
			if pi >= len(p) {
				return wmNoMatch
			}
			if tch != p[pi] {
				return wmNoMatch
			}
			continue
		default:
			if tch != pch {
				return wmNoMatch
			}
			continue
		case '?':
			if pathname && tch == '/' {
				return wmNoMatch
			}
			continue
		case '*':
			var matchSlash bool
			pi++
			if pi < len(p) && p[pi] == '*' {
				prev := pi - 2
				for pi < len(p) && p[pi] == '*' {
					pi++
				}
				if (prev < 0 || p[prev] == '/') &&
					(pi >= len(p) || p[pi] == '/' || (p[pi] == '\\' && pi+1 < len(p) && p[pi+1] == '/')) {
					if pi < len(p) && p[pi] == '/' && dowild(p[pi+1:], text[ti:], pathname) == wmMatch {
						return wmMatch
					}
					matchSlash = true
				} else {
					matchSlash = !pathname
				}
			} else {
				matchSlash = !pathname
			}
			if pi >= len(p) {
				if !matchSlash && strings.IndexByte(text[ti:], '/') >= 0 {
					return wmNoMatch
				}
				return wmMatch
			}
			if !matchSlash && p[pi] == '/' {
				slash := strings.IndexByte(text[ti:], '/')
				if slash < 0 {
					return wmNoMatch
				}
				ti += slash
				// The loop's increment consumes the slash in both strings.
				continue
			}
			for {
				if ti >= len(text) {
					break
				}
				if !isGlobSpecial(p[pi]) {
					want := p[pi]
					for ti < len(text) && (matchSlash || text[ti] != '/') {
						if text[ti] == want {
							break
						}
						ti++
					}
					if ti >= len(text) || text[ti] != want {
						if matchSlash {
							return wmAbortAll
						}
						return wmAbortToStarStar
					}
				}
				matched := dowild(p[pi:], text[ti:], pathname)
				if matched != wmNoMatch {
					if !matchSlash || matched != wmAbortToStarStar {
						return matched
					}
				} else if !matchSlash && text[ti] == '/' {
					return wmAbortToStarStar
				}
				ti++
			}
			return wmAbortAll
		case '[':
			pi++
			if pi >= len(p) {
				return wmAbortAll
			}
			pch = p[pi]
			if pch == '^' {
				pch = '!'
			}
			negated := pch == '!'
			if negated {
				pi++
				if pi >= len(p) {
					return wmAbortAll
				}
				pch = p[pi]
			}
			var prev byte
			matched := false
			for {
				if pch == '\\' {
					pi++
					if pi >= len(p) {
						return wmAbortAll
					}
					pch = p[pi]
					if tch == pch {
						matched = true
					}
				} else if pch == '-' && prev != 0 && pi+1 < len(p) && p[pi+1] != ']' {
					pi++
					pch = p[pi]
					if pch == '\\' {
						pi++
						if pi >= len(p) {
							return wmAbortAll
						}
						pch = p[pi]
					}
					if tch <= pch && tch >= prev {
						matched = true
					}
					pch = 0
				} else if pch == '[' && pi+1 < len(p) && p[pi+1] == ':' {
					start := pi + 2
					end := start
					for end < len(p) && p[end] != ']' {
						end++
					}
					if end >= len(p) {
						return wmAbortAll
					}
					if end-start < 1 || p[end-1] != ':' {
						// Not a character class; treat '[' literally.
						if tch == '[' {
							matched = true
						}
						pch = '['
					} else {
						ok, valid := charClass(p[start:end-1], tch)
						if !valid {
							return wmAbortAll
						}
						if ok {
							matched = true
						}
						pi = end
						pch = 0
					}
				} else if tch == pch {
					matched = true
				}
				prev = pch
				pi++
				if pi >= len(p) {
					return wmAbortAll
				}
				pch = p[pi]
				if pch == ']' {
					break
				}
			}
			if matched == negated || (pathname && tch == '/') {
				return wmNoMatch
			}
			continue
		}
	}
	if ti < len(text) {
		return wmNoMatch
	}
	return wmMatch
}

func charClass(name string, c byte) (bool, bool) {
	isUpper := c >= 'A' && c <= 'Z'
	isLower := c >= 'a' && c <= 'z'
	isDigit := c >= '0' && c <= '9'
	switch name {
	case "alnum":
		return isUpper || isLower || isDigit, true
	case "alpha":
		return isUpper || isLower, true
	case "blank":
		return c == ' ' || c == '\t', true
	case "cntrl":
		return c < 32 || c == 127, true
	case "digit":
		return isDigit, true
	case "graph":
		return c > 32 && c < 127, true
	case "lower":
		return isLower, true
	case "print":
		return c >= 32 && c < 127, true
	case "punct":
		return c > 32 && c < 127 && !isUpper && !isLower && !isDigit, true
	case "space":
		return c == ' ' || (c >= '\t' && c <= '\r'), true
	case "upper":
		return isUpper, true
	case "xdigit":
		return isDigit || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'), true
	}
	return false, false
}

// gitPattern is a parsed ignore or attributes pattern (Git's
// parse_path_pattern) together with the directory that defines it.
type gitPattern struct {
	pattern  string
	base     string // root-relative directory, "" for the root
	negative bool
	mustDir  bool
	noDir    bool // no slash: match the basename only
}

func parsePathPattern(p, base string, allowNegative bool) (gitPattern, bool) {
	g := gitPattern{base: base}
	if strings.HasPrefix(p, "!") {
		if !allowNegative {
			return g, false
		}
		g.negative = true
		p = p[1:]
	}
	if strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
		g.mustDir = true
	}
	g.noDir = !strings.Contains(p, "/")
	g.pattern = p
	return g, true
}

// matches implements Git's match_basename / match_pathname for a
// root-relative path. Callers apply a pattern only to paths inside its base
// directory, as Git does through its per-directory stacks.
func (g gitPattern) matches(rel string, isDir bool) bool {
	if g.mustDir && !isDir {
		return false
	}
	if g.noDir {
		base := rel
		if i := strings.LastIndexByte(rel, '/'); i >= 0 {
			base = rel[i+1:]
		}
		return wildmatch(g.pattern, base, false)
	}
	pattern := strings.TrimPrefix(g.pattern, "/")
	name := rel
	if g.base != "" {
		if !strings.HasPrefix(rel, g.base+"/") {
			return false
		}
		name = rel[len(g.base)+1:]
	}
	return wildmatch(pattern, name, true)
}
