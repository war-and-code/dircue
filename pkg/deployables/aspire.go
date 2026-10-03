package deployables

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// This lexer intentionally recognizes only enough C# to bind the common
// top-level Aspire declaration shape. It is not a C# parser or evaluator.
type csToken struct {
	kind        string
	text        string
	line        int
	conditional bool
}

const maxAspireCSharpTokens = 32768
const maxAspireSourceReferences = 2048
const maxAspireInterpolationDepth = 64

// AspireBounds exposes the finite parser limits for deterministic map settings.
func AspireBounds() (tokens, interpolationDepth, sourceReferences int) {
	return maxAspireCSharpTokens, maxAspireInterpolationDepth, maxAspireSourceReferences
}

func parseAspireAppHost(name string, content []byte) ([]Definition, bool, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, fmt.Errorf("Aspire AppHost Program.cs contains binary data")
	}
	source := strings.TrimPrefix(string(content), "\uFEFF")
	tokens, err := lexAspireCSharp(source)
	if err != nil {
		return nil, false, err
	}
	if hasCustomProjectsBinding(tokens) || hasCustomDistributedApplicationBinding(tokens) {
		return nil, false, nil
	}
	var statements [][]csToken
	var current []csToken
	brace, paren, bracket := 0, 0, 0
	for _, t := range tokens {
		if t.text == "{" {
			brace++
		}
		if t.text == "}" {
			brace--
			if brace == 0 {
				continue
			}
		}
		if brace < 0 {
			return nil, false, fmt.Errorf("malformed C# braces")
		}
		if brace == 0 {
			switch t.text {
			case "(":
				paren++
			case ")":
				paren--
			case "[":
				bracket++
			case "]":
				bracket--
			}
			if paren < 0 || bracket < 0 {
				return nil, false, fmt.Errorf("malformed C# delimiters")
			}
			if t.text == ";" && paren == 0 && bracket == 0 {
				if len(current) > 0 {
					statements = append(statements, current)
					current = nil
				}
				continue
			}
			current = append(current, t)
		}
	}
	if brace != 0 || paren != 0 || bracket != 0 {
		return nil, false, fmt.Errorf("malformed C# delimiters")
	}
	var builderAt = -1
	var refs []Reference
	validCreation := func(s []csToken) int {
		// var builder = DistributedApplication.CreateBuilder(...)
		if len(s) < 8 || s[0].text != "var" || s[1].text != "builder" || s[2].text != "=" ||
			s[3].text != "DistributedApplication" || s[4].text != "." || s[5].text != "CreateBuilder" || s[6].text != "(" {
			return -1
		}
		if matchingParen(s, 6) != len(s)-1 {
			return -1
		}
		for _, t := range s {
			if t.conditional {
				return -1
			}
		}
		return s[0].line
	}
	for si, s := range statements {
		if line := validCreation(s); line > 0 {
			if builderAt >= 0 {
				builderAt = -2
				continue
			}
			builderAt = si
		}
	}
	if builderAt < 0 {
		return nil, false, nil
	}
	creationLine := validCreation(statements[builderAt])
	for si, s := range statements {
		if si <= builderAt {
			continue
		}
		callIndex := findAspireAddProject(s)
		if callIndex >= 0 {
			if containsConditional(s) || !directAspireStatement(s, callIndex) {
				continue
			}
			ident := s[callIndex+4].text
			open := callIndex + 6
			close := matchingParen(s, open)
			if close < 0 || open+1 >= close || s[open+1].kind != "string" || open+2 < close && s[open+2].text != "," {
				continue
			}
			if len(refs) >= maxAspireSourceReferences {
				return nil, false, fmt.Errorf("Aspire static project call observation limit exceeded")
			}
			refs = append(refs, Reference{Kind: "aspire_project", Value: bounded(ident), Qualification: "declared", Evidence: Evidence{Field: "AddProject", Value: bounded(ident), Line: s[callIndex].line, Basis: "aspire-csharp-top-level-static"}})
			continue
		}
		// Any other top-level use or reassignment of builder could shadow or
		// replace the object we observed. Stop attributing calls after it.
		if reassignsAspireBuilder(s) {
			refs = nil
			break
		}
	}
	if len(refs) == 0 {
		return nil, false, nil
	}
	appHostName := path.Base(path.Dir(name))
	d := Definition{Kind: "service", Provider: "aspire-apphost", Name: bounded(appHostName), Coverage: "qualified", Evidence: []Evidence{{Field: "aspire-apphost", Value: bounded(appHostName), Line: creationLine, Basis: "aspire-apphost-top-level-builder"}}, References: refs}
	return []Definition{d}, true, nil
}

func findAspireAddProject(s []csToken) int {
	for i := 0; i+7 < len(s); i++ {
		if s[i].text == "builder" && s[i+1].text == "." && s[i+2].text == "AddProject" && s[i+3].text == "<" && s[i+4].text == "Projects" && s[i+5].text == "." && s[i+6].kind == "ident" && s[i+7].text == ">" && i+8 < len(s) && s[i+8].text == "(" {
			return i + 2
		}
	}
	return -1
}

func directAspireStatement(s []csToken, add int) bool {
	start := add - 2 // builder
	if start == 0 {
		return true
	}
	if start < 3 || s[start-1].text != "=" {
		return false
	}
	// A local declaration may have a type; reject control flow, invocation,
	// member access, and all other expression prefixes.
	for _, t := range s[:start-1] {
		if t.text == "=" || t.text == "(" || t.text == "." || t.text == "return" {
			return false
		}
	}
	return true
}

func matchingParen(s []csToken, open int) int {
	if open < 0 || open >= len(s) || s[open].text != "(" {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		if s[i].text == "(" {
			depth++
		}
		if s[i].text == ")" {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
func containsConditional(s []csToken) bool {
	for _, t := range s {
		if t.conditional {
			return true
		}
	}
	return false
}
func hasCustomProjectsBinding(tokens []csToken) bool {
	for i, t := range tokens {
		if t.kind != "ident" {
			continue
		}
		if (t.text == "class" || t.text == "struct" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(tokens) && tokens[i+1].text == "Projects" {
			return true
		}
		if t.text == "namespace" {
			for j := i + 1; j < len(tokens) && tokens[j].text != "{" && tokens[j].text != ";"; j++ {
				if tokens[j].text == "Projects" {
					return true
				}
			}
		}
		if t.text == "using" || t.text == "global" && i+1 < len(tokens) && tokens[i+1].text == "using" {
			start := i
			if t.text == "global" {
				start = i + 1
			}
			for j := start + 1; j < len(tokens) && tokens[j].text != ";"; j++ {
				if tokens[j].text == "Projects" {
					return true
				}
			}
		}
		if t.text == "var" && i+1 < len(tokens) && tokens[i+1].text == "Projects" {
			return true
		}
		if t.text == "Projects" && i+1 < len(tokens) && (tokens[i+1].text == "=" || tokens[i+1].text == "++" || tokens[i+1].text == "--") && (i == 0 || tokens[i-1].text != ".") {
			return true
		}
	}
	return false
}

func hasCustomDistributedApplicationBinding(tokens []csToken) bool {
	for i, t := range tokens {
		if t.kind != "ident" {
			continue
		}
		if (t.text == "class" || t.text == "struct" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(tokens) && tokens[i+1].text == "DistributedApplication" {
			return true
		}
		if t.text == "using" {
			for j := i + 1; j < len(tokens) && tokens[j].text != ";"; j++ {
				if tokens[j].text == "DistributedApplication" {
					return true
				}
			}
		}
	}
	return false
}
func reassignsAspireBuilder(s []csToken) bool {
	for i, t := range s {
		if t.kind != "ident" || t.text != "builder" {
			continue
		}
		if i > 0 && (s[i-1].text == "var" || s[i-1].text == "DistributedApplicationBuilder") {
			return true
		}
		if i+1 < len(s) && (s[i+1].text == "=" || s[i+1].text == "++" || s[i+1].text == "--" || strings.HasSuffix(s[i+1].text, "=")) {
			if i == 0 || s[i-1].text != "." {
				return true
			}
		}
		if i+2 < len(s) && s[i+2].text == "=" && strings.ContainsRune("+-?&|^/%*", rune(s[i+1].text[0])) {
			return true
		}
	}
	return false
}

func lexAspireCSharp(src string) ([]csToken, error) {
	var out []csToken
	line, conditional := 1, 0
	for i := 0; i < len(src); {
		if i == 0 || src[i-1] == '\n' {
			j := i
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == '#' {
				end := strings.IndexByte(src[j:], '\n')
				if end < 0 {
					end = len(src) - j
				}
				directive := strings.TrimSpace(src[j : j+end])
				fields := strings.Fields(strings.TrimPrefix(directive, "#"))
				keyword := ""
				if len(fields) > 0 {
					keyword = fields[0]
				}
				if (strings.HasPrefix(keyword, "if") && keyword != "if") || (strings.HasPrefix(keyword, "end") && keyword != "endif") || (strings.HasPrefix(keyword, "else") && keyword != "else") || (strings.HasPrefix(keyword, "elif") && keyword != "elif") {
					return nil, fmt.Errorf("unsupported C# preprocessor directive")
				}
				switch keyword {
				case "if":
					conditional++
				case "endif":
					conditional--
					if conditional < 0 {
						return nil, fmt.Errorf("unmatched #endif")
					}
				case "else", "elif":
					if conditional == 0 {
						return nil, fmt.Errorf("unmatched C# conditional directive")
					}
				}
				for k := i; k < j+end; k++ {
					if src[k] == '\n' {
						line++
					}
				}
				i = j + end
				continue
			}
		}
		c := src[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if i+1 < len(src) && src[i:i+2] == "//" {
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				break
			}
			i += j
			continue
		}
		if i+1 < len(src) && src[i:i+2] == "/*" {
			start := line
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("unterminated C# comment at line %d", start)
			}
			chunk := src[i : i+2+j+2]
			line += strings.Count(chunk, "\n")
			i += len(chunk)
			continue
		}
		start := i
		tokLine := line
		if c == '\'' {
			end, ok := skipQuoted(src, i, '\'', false)
			if !ok || strings.ContainsAny(src[i:end], "\r\n") {
				return nil, fmt.Errorf("malformed C# character literal")
			}
			out = append(out, csToken{kind: "opaque", line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			i = end
			continue
		}
		if end, kind, value, ok := csharpString(src, i); ok {
			if kind == "limit" {
				return nil, fmt.Errorf("Aspire C# interpolation nesting limit exceeded")
			}
			chunk := src[i:end]
			line += strings.Count(chunk, "\n")
			out = append(out, csToken{kind: kind, text: value, line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			i = end
			continue
		}
		if isCSharpIdentStart(c) {
			i++
			for i < len(src) && isCSharpIdentPart(src[i]) {
				i++
			}
			out = append(out, csToken{kind: "ident", text: src[start:i], line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			continue
		}
		out = append(out, csToken{kind: "punct", text: src[i : i+1], line: tokLine, conditional: conditional > 0})
		if len(out) > maxAspireCSharpTokens {
			return nil, fmt.Errorf("Aspire C# token limit exceeded")
		}
		i++
	}
	if conditional != 0 {
		return nil, fmt.Errorf("unmatched #if")
	}
	return out, nil
}

func csharpString(s string, at int) (int, string, string, bool) {
	return csharpStringDepth(s, at, 0)
}

func csharpStringDepth(s string, at, interpolationDepth int) (int, string, string, bool) {
	i := at
	dollars := 0
	verbatim := false
	for i < len(s) && (s[i] == '$' || s[i] == '@') {
		if s[i] == '$' {
			dollars++
		} else {
			verbatim = true
		}
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return 0, "", "", false
	}
	q := i
	for i < len(s) && s[i] == '"' {
		i++
	}
	quotes := i - q
	if quotes == 1 && dollars == 0 && !verbatim {
		end, ok := skipQuoted(s, q, '"', false)
		if !ok {
			return len(s), "opaque", "", true
		}
		raw := s[q:end]
		val, err := strconv.Unquote(raw)
		if err != nil {
			return end, "opaque", "", true
		}
		return end, "string", val, true
	}
	if quotes == 1 || quotes >= 3 {
		end, ok := skipCSharpBody(s, i, quotes, dollars, verbatim, interpolationDepth)
		if !ok {
			if end < 0 {
				return len(s), "limit", "", true
			}
			return len(s), "opaque", "", true
		}
		if dollars == 0 {
			return end, "string", "", true
		}
		return end, "opaque", "", true
	}
	return at, "", "", false
}

func skipQuoted(s string, start int, quote byte, verbatim bool) (int, bool) {
	for i := start + 1; i < len(s); i++ {
		if verbatim && s[i] == quote && i+1 < len(s) && s[i+1] == quote {
			i++
			continue
		}
		if !verbatim && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == quote {
			return i + 1, true
		}
	}
	return len(s), false
}
func skipCSharpBody(s string, i, quotes, dollars int, verbatim bool, interpolationDepth int) (int, bool) {
	for i < len(s) {
		if dollars > 0 && s[i] == '{' {
			run := 1
			for i+run < len(s) && s[i+run] == '{' {
				run++
			}
			arity := dollars
			if quotes == 1 {
				arity = 1
				if run == 2 {
					i += run
					continue
				}
				if run > 2 {
					end, ok := skipInterpolation(s, i+run, interpolationDepth+1, 1, 0)
					if !ok {
						if end < 0 {
							return -1, false
						}
						return len(s), false
					}
					i = end
					continue
				}
			}
			if run < arity {
				i += run
				continue
			}
			end, ok := skipInterpolation(s, i+arity, interpolationDepth+1, arity, run-arity)
			if !ok {
				if end < 0 {
					return -1, false
				}
				return len(s), false
			}
			i = end
			continue
		}
		if dollars > 0 && quotes == 1 && s[i] == '}' && i+1 < len(s) && s[i+1] == '}' {
			i += 2
			continue
		}
		if s[i] == '"' {
			n := 0
			for i+n < len(s) && s[i+n] == '"' {
				n++
			}
			if verbatim && quotes == 1 && n >= 2 {
				i += 2
				continue
			}
			if (quotes == 1 && n >= 1) || (quotes >= 3 && n >= quotes) {
				return i + quotes, true
			}
			i += n
			continue
		}
		if !verbatim && quotes == 1 && s[i] == '\\' {
			i += 2
			continue
		}
		i++
	}
	return len(s), false
}
func skipInterpolation(s string, i, nestingDepth, arity, exprDepth int) (int, bool) {
	if nestingDepth > maxAspireInterpolationDepth {
		return -1, false
	}
	depth := exprDepth
	for i < len(s) {
		if i+1 < len(s) && s[i:i+2] == "//" {
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return len(s), false
			}
			i += j
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return len(s), false
			}
			i += j + 4
			continue
		}
		if s[i] == '\'' {
			end, ok := skipQuoted(s, i, '\'', false)
			if !ok || strings.ContainsAny(s[i:end], "\r\n") {
				return len(s), false
			}
			i = end
			continue
		}
		if end, kind, _, ok := csharpStringDepth(s, i, nestingDepth); ok {
			if kind == "limit" {
				return -1, false
			}
			i = end
			continue
		}
		if s[i] == '{' {
			run := 1
			for i+run < len(s) && s[i+run] == '{' {
				run++
			}
			depth += run
			i += run
			continue
		}
		if s[i] == '}' {
			run := 1
			for i+run < len(s) && s[i+run] == '}' {
				run++
			}
			if depth > 0 {
				consumed := min(run, depth)
				depth -= consumed
				i += consumed
				run -= consumed
			}
			if depth == 0 && run >= arity {
				return i + arity, true
			}
			if run > 0 {
				i += run
			}
			continue
		}
		i++
	}
	return len(s), false
}
func isCSharpIdentStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
func isCSharpIdentPart(c byte) bool { return isCSharpIdentStart(c) || (c >= '0' && c <= '9') }
