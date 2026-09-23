package treehash

import "strings"

// Attribute states follow Git's attr.c: an attribute is set (true), unset
// ("-name", false), explicitly unspecified ("!name"), or has a string value.
type attrState struct {
	decided bool
	set     bool   // name
	unset   bool   // -name
	value   string // name=value
}

type attrAssignment struct {
	name  string
	state attrState
}

type attrRule struct {
	pattern     *gitPattern // nil for a macro definition
	macro       string
	assignments []attrAssignment
}

// attrFile holds the rules of one attributes source in file order.
type attrFile struct {
	rules []attrRule
}

// relevantAttributes are the only attributes that can change object content in
// Git's clean ("add") direction, plus the legacy crlf attribute.
var relevantAttributes = map[string]bool{
	"text": true, "eol": true, "crlf": true, "filter": true, "ident": true,
	"working-tree-encoding": true,
}

// builtinMacros are Git's predefined macro attributes.
var builtinMacros = map[string][]attrAssignment{
	"binary": {
		{name: "diff", state: attrState{decided: true, unset: true}},
		{name: "merge", state: attrState{decided: true, unset: true}},
		{name: "text", state: attrState{decided: true, unset: true}},
	},
}

// parseAttributes parses one .gitattributes-style file. domain is the
// directory (as path components) that contains the file; allowMacro is true
// only for the top-level file and $GIT_DIR/info/attributes, as in Git.
func parseAttributes(content []byte, base string, allowMacro bool) attrFile {
	var out attrFile
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		pattern, rest, ok := splitAttributePattern(trimmed)
		if !ok || pattern == "" {
			continue
		}
		rule := attrRule{}
		if strings.HasPrefix(pattern, "[attr]") {
			if !allowMacro {
				continue
			}
			rule.macro = strings.TrimPrefix(pattern, "[attr]")
			if rule.macro == "" {
				continue
			}
		} else {
			// Git rejects negative patterns in attribute files.
			p, ok := parsePathPattern(pattern, base, false)
			if !ok || p.pattern == "" {
				continue
			}
			rule.pattern = &p
		}
		for _, field := range strings.Fields(rest) {
			a, ok := parseAssignment(field)
			if ok {
				rule.assignments = append(rule.assignments, a)
			}
		}
		out.rules = append(out.rules, rule)
	}
	return out
}

// splitAttributePattern splits the leading pattern from the attribute list.
// A pattern starting with '"' is C-unquoted as Git's unquote_c_style does; if
// unquoting fails, Git falls back to the raw token, quotes included.
func splitAttributePattern(line string) (string, string, bool) {
	if strings.HasPrefix(line, "\"") {
		if pattern, rest, ok := unquoteC(line); ok {
			return pattern, rest, true
		}
	}
	end := strings.IndexAny(line, " \t\r\n")
	if end < 0 {
		return line, "", true
	}
	return line[:end], line[end:], true
}

// unquoteC ports Git's unquote_c_style for a string starting with '"'.
func unquoteC(s string) (string, string, bool) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			return b.String(), s[i+1:], true
		case '\\':
			i++
			if i >= len(s) {
				return "", "", false
			}
			switch e := s[i]; e {
			case '"', '\\':
				b.WriteByte(e)
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'v':
				b.WriteByte('\v')
			case '0', '1', '2', '3':
				if i+2 >= len(s) || s[i+1] < '0' || s[i+1] > '7' || s[i+2] < '0' || s[i+2] > '7' {
					return "", "", false
				}
				b.WriteByte((e-'0')<<6 | (s[i+1]-'0')<<3 | (s[i+2] - '0'))
				i += 2
			default:
				return "", "", false
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", "", false
}

func parseAssignment(field string) (attrAssignment, bool) {
	switch {
	case strings.HasPrefix(field, "-"):
		return attrAssignment{name: field[1:], state: attrState{decided: true, unset: true}}, field != "-"
	case strings.HasPrefix(field, "!"):
		return attrAssignment{name: field[1:], state: attrState{decided: true}}, field != "!"
	}
	if name, value, ok := strings.Cut(field, "="); ok {
		return attrAssignment{name: name, state: attrState{decided: true, value: value}}, name != ""
	}
	return attrAssignment{name: field, state: attrState{decided: true, set: true}}, true
}

// attrStack is the ordered set of attribute files in increasing priority:
// ancestors' .gitattributes from the root down, then $GIT_DIR/info/attributes
// (highest). Macros may come from the root file or info/attributes.
type attrStack struct {
	files  []attrFile
	macros map[string][]attrAssignment
}

func newAttrStack(files []attrFile) *attrStack {
	s := &attrStack{files: files, macros: map[string][]attrAssignment{}}
	for name, values := range builtinMacros {
		s.macros[name] = values
	}
	// Later macro definitions override earlier ones, as in Git.
	for _, f := range files {
		for _, r := range f.rules {
			if r.macro != "" {
				s.macros[r.macro] = r.assignments
			}
		}
	}
	return s
}

// resolve returns the decided states of the relevant attributes for path,
// following Git's fill/macroexpand order: highest-priority rule first, and
// within a rule the last assignment first; an attribute is decided once.
func (s *attrStack) resolve(rel string) map[string]attrState {
	result := map[string]attrState{}
	var fill func(assignments []attrAssignment, depth int)
	fill = func(assignments []attrAssignment, depth int) {
		if depth > 32 {
			return
		}
		for i := len(assignments) - 1; i >= 0; i-- {
			a := assignments[i]
			if _, done := result[a.name]; done {
				continue
			}
			result[a.name] = a.state
			if macro, ok := s.macros[a.name]; ok && a.state.set {
				fill(macro, depth+1)
			}
		}
	}
	for fi := len(s.files) - 1; fi >= 0; fi-- {
		rules := s.files[fi].rules
		for ri := len(rules) - 1; ri >= 0; ri-- {
			r := rules[ri]
			if r.pattern == nil || !r.pattern.matches(rel, false) {
				continue
			}
			fill(r.assignments, 0)
		}
	}
	for name := range result {
		if !relevantAttributes[name] {
			delete(result, name)
		}
	}
	return result
}

// cleanAction derives Git's clean-direction CRLF action from resolved
// attributes, with core.autocrlf treated as false (user configuration is not
// repository content and is deliberately not consulted).
func cleanAction(attrs map[string]attrState) crlfAction {
	action, defined := crlfFromState(attrs["text"])
	if !defined {
		// The legacy "crlf" attribute is consulted only when "text" is unspecified.
		action, defined = crlfFromState(attrs["crlf"])
	}
	if defined {
		return action
	}
	// "eol" alone implies text.
	if e := attrs["eol"]; e.decided && (e.value == "lf" || e.value == "crlf") {
		return crlfText
	}
	return crlfBinary
}

func crlfFromState(s attrState) (crlfAction, bool) {
	switch {
	case !s.decided:
		return crlfBinary, false
	case s.set:
		return crlfText, true
	case s.unset:
		return crlfBinary, true
	case s.value == "auto":
		return crlfAuto, true
	case s.value == "input":
		// Git's CRLF_TEXT_INPUT: normalize on the way in, like text.
		return crlfText, true
	case s.value != "":
		// Unknown values leave the attribute undefined, as in Git.
		return crlfBinary, false
	}
	// Explicitly unspecified (!text).
	return crlfBinary, false
}
