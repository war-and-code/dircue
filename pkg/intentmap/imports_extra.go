package intentmap

import (
	"path"
	"strings"
	"unicode"
)

// sourceToken is a small lexical token with its source line and lexical brace
// depth. Strings remain opaque tokens so import parsers can accept a genuine
// module specifier without scanning text inside unrelated literals.
type sourceToken struct {
	text  string
	kind  byte // i identifier, s string, p punctuation
	line  int
	depth int
}

// lexSource masks comments and emits strings as opaque tokens. It is a bounded
// lexer, not a language parser: malformed or unsupported constructs simply do
// not produce import evidence.
func lexSource(src string, lang string) ([]sourceToken, bool) {
	capacity := min(DefaultMaxLexicalTokensPerFile, len(src))
	toks := make([]sourceToken, 0, capacity)
	line, depth := 1, 0
	vbStatementStart := true
	overflow := false
	appendToken := func(t sourceToken) bool {
		if len(toks) >= DefaultMaxLexicalTokensPerFile {
			overflow = true
			return false
		}
		toks = append(toks, t)
		return true
	}
	space := func(c byte) {
		if c == '\n' {
			line++
		}
	}
scanLoop:
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\n' {
			line++
			vbStatementStart = true
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' {
			i++
			continue
		}
		if lang == "vb" && vbStatementStart && i+3 <= len(src) && strings.EqualFold(src[i:i+3], "REM") && (i+3 == len(src) || src[i+3] == ' ' || src[i+3] == '\t') {
			for i < len(src) && src[i] != '\n' {
				space(src[i])
				i++
			}
			continue
		}
		// VB uses apostrophe comments and doubled quotes in string literals.
		if lang == "vb" && c == '\'' {
			for i < len(src) && src[i] != '\n' {
				space(src[i])
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				space(src[i])
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			space('/')
			space('*')
			i += 2
			nesting := 1
			for i < len(src) && nesting > 0 {
				if lang == "kotlin" && i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
					space('/')
					space('*')
					i += 2
					nesting++
					continue
				}
				if i+1 < len(src) && src[i] == '*' && src[i+1] == '/' {
					space('*')
					space('/')
					i += 2
					nesting--
					continue
				}
				space(src[i])
				i++
			}
			continue
		}
		// A JavaScript regular-expression literal is opaque too. Recognize it
		// only where an expression may begin; division after a value stays
		// punctuation. This intentionally favors omission over parsing regex
		// contents as module syntax.
		if lang == "js" && c == '/' && jsRegexMayStart(toks) {
			j := i + 1
			inClass, closed := false, false
			for j < len(src) && src[j] != '\n' {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '[' {
					inClass = true
				} else if src[j] == ']' {
					inClass = false
				} else if src[j] == '/' && !inClass {
					j++
					for j < len(src) && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z')) {
						j++
					}
					closed = true
					break
				}
				j++
			}
			if !closed {
				// An ambiguous or malformed regexp may contain arbitrary text;
				// mask the remainder of the line so it cannot yield evidence.
				for i < len(src) && src[i] != '\n' {
					space(src[i])
					i++
				}
				continue
			}
			for i < j {
				space(src[i])
				i++
			}
			continue
		}

		// C# verbatim/interpolated-verbatim strings: @"..." / $@"..." / @$"...".
		strStart := i
		startLine := line
		qpos := i
		if lang == "cs" && (src[i] == '@' || src[i] == '$') {
			j := i
			for j < len(src) && (src[j] == '@' || src[j] == '$') {
				j++
			}
			if j < len(src) && src[j] == '"' {
				qpos = j
			}
		}
		if src[qpos] == '"' || src[qpos] == '\'' || src[qpos] == '`' {
			q := src[qpos]
			start := qpos
			delim := 1
			quoteRun := 1
			if q == '"' {
				for start+quoteRun < len(src) && src[start+quoteRun] == '"' {
					quoteRun++
				}
			}
			raw := q == '`'
			triple := q == '"' && (lang == "java" || lang == "kotlin") && quoteRun >= 3
			if triple {
				delim = 3
			}
			if lang == "cs" && q == '"' && quoteRun >= 3 {
				delim = quoteRun
				raw = true
				triple = true
			}
			if triple {
				if lang != "cs" {
					delim = 3
				}
				qpos = start + delim - 1
			}
			contentStart := start + delim
			verbatim := lang == "cs" && strings.Contains(src[strStart:qpos+1], "@")
			j := contentStart
			closed := false
			invalidLineString := false
			for j < len(src) {
				if !triple && q != '`' && !verbatim && src[j] == '\n' {
					invalidLineString = true
					break
				}
				if triple && src[j] == '"' {
					run := 1
					for j+run < len(src) && src[j+run] == '"' {
						run++
					}
					if run >= delim {
						closed = true
						break
					}
					j += run
					continue
				}
				if !triple && src[j] == q {
					if (verbatim || lang == "vb") && q == '"' && j+1 < len(src) && src[j+1] == '"' {
						j += 2
						continue
					}
					if raw || !escapedAt(src, j) {
						closed = true
						break
					}
				}
				j++
			}
			val := src[contentStart:j]
			if closed {
				if triple {
					run := 1
					for j+run < len(src) && src[j+run] == '"' {
						run++
					}
					j += run
				} else {
					j++
				}
			}
			if invalidLineString {
				j = len(src)
			}
			for k := strStart; k < j; k++ {
				space(src[k])
			}
			if closed {
				if !appendToken(sourceToken{text: val, kind: 's', line: startLine, depth: depth}) {
					break scanLoop
				}
				vbStatementStart = false
			}
			i = j
			continue
		}

		if c == '_' || c == '$' || unicode.IsLetter(rune(c)) {
			start := i
			i++
			for i < len(src) && (src[i] == '_' || src[i] == '$' || unicode.IsLetter(rune(src[i])) || unicode.IsDigit(rune(src[i]))) {
				i++
			}
			v := src[start:i]
			if !appendToken(sourceToken{text: v, kind: 'i', line: line, depth: depth}) {
				break scanLoop
			}
			vbStatementStart = false
			continue
		}
		if c == '}' && depth > 0 {
			depth--
		}
		if !appendToken(sourceToken{text: src[i : i+1], kind: 'p', line: line, depth: depth}) {
			break scanLoop
		}
		if lang == "vb" {
			vbStatementStart = c == ':'
		}
		if c == '{' {
			depth++
		}
		i++
	}
	return toks, overflow
}

func jsRegexMayStart(t []sourceToken) bool {
	if len(t) == 0 {
		return true
	}
	x := t[len(t)-1]
	if x.kind == 'i' || x.kind == 's' || x.text == ")" || x.text == "]" || x.text == "}" || x.text == "++" || x.text == "--" {
		return x.kind == 'i' && (x.text == "return" || x.text == "throw" || x.text == "case" || x.text == "delete" || x.text == "void" || x.text == "typeof" || x.text == "instanceof" || x.text == "in" || x.text == "of" || x.text == "yield" || x.text == "await")
	}
	switch x.text {
	case "(", "=", "=>", ":", ",", "!", "?", "&", "|", "+", "-", "*", "%", "{", ";":
		return true
	default:
		return false
	}
}

func escapedAt(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

func parseJVMImports(name string, content []byte) []Observation {
	out, _ := parseJVMImportsBounded(name, content)
	return out
}

func parseJVMImportsBounded(name string, content []byte) ([]Observation, bool) {
	lang := "java"
	if strings.HasSuffix(strings.ToLower(name), ".kt") {
		lang = "kotlin"
	}
	toks, limited := lexSource(string(content), lang)
	return parseJVMImportsTokens(name, lang, toks), limited
}

func parseJVMImportsTokens(name, lang string, toks []sourceToken) []Observation {
	out := []Observation{}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.depth != 0 || t.text != "import" || t.kind != 'i' {
			continue
		}
		j := i + 1
		isStatic := false
		if j < len(toks) && toks[j].text == "static" {
			isStatic = true
			j++
		}
		var b strings.Builder
		expectName := true
		sawSemicolon := false
		alias := false
		for j < len(toks) {
			x := toks[j]
			if x.text == ";" {
				sawSemicolon = true
				break
			}
			if x.text == "as" && lang == "kotlin" && j+1 < len(toks) && toks[j+1].kind == 'i' {
				alias = true
				j += 2
				break
			}
			if expectName && x.kind == 'i' {
				b.WriteString(x.text)
				expectName = false
				j++
				continue
			}
			if !expectName && x.text == "." {
				b.WriteByte('.')
				expectName = true
				j++
				continue
			}
			if expectName && b.Len() > 0 && x.text == "*" {
				b.WriteByte('*')
				expectName = false
				j++
				continue
			}
			break
		}
		endLine := t.line
		if j > i+1 && j-1 < len(toks) {
			endLine = toks[j-1].line
		}
		pkg := strings.TrimSuffix(b.String(), ".*")
		if pkg == "" || expectName || (lang == "java" && !sawSemicolon) {
			continue
		}
		for _, c := range capabilitiesFor("maven-import", pkg) {
			props := map[string]string{"import": pkg, "evidence_scope": importEvidenceScope(name)}
			if isStatic {
				props["import_static"] = "true"
			}
			if alias {
				props["import_alias"] = "true"
			}
			out = append(out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: t.line, EndLine: endLine, Properties: props})
		}
		i = j - 1
	}
	return out
}

func csNamespaceScopes(t []sourceToken) []bool {
	scopes := make([]bool, len(t))
	stack := []bool{}
	pending := false
	for i, x := range t {
		if x.text == "}" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && stack[len(stack)-1] {
			scopes[i] = true
		}
		if x.kind == 'i' && x.text == "namespace" {
			pending = true
		}
		if x.text == ";" && pending {
			pending = false
		}
		if x.text == "{" {
			stack = append(stack, pending)
			pending = false
		}
	}
	return scopes
}

func parseDotnetImports(name string, content []byte) []Observation {
	out, _ := parseDotnetImportsBounded(name, content)
	return out
}

func parseDotnetImportsBounded(name string, content []byte) ([]Observation, bool) {
	vb := strings.HasSuffix(strings.ToLower(name), ".vb")
	lang := "cs"
	if vb {
		lang = "vb"
	}
	toks, limited := lexSource(string(content), lang)
	return parseDotnetImportsTokens(name, vb, toks), limited
}

func parseDotnetImportsTokens(name string, vb bool, toks []sourceToken) []Observation {
	out := []Observation{}
	namespaceScope := csNamespaceScopes(toks)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t.depth != 0 && (vb || !namespaceScope[i])) || t.kind != 'i' {
			continue
		}
		j := i
		alias := false
		isStatic := false
		if vb {
			if !strings.EqualFold(t.text, "Imports") {
				continue
			}
			j++
		} else {
			if t.text != "using" && !(t.text == "global" && i+1 < len(toks) && toks[i+1].text == "using") {
				continue
			}
			if t.text == "global" {
				j++
			}
			j++
			if j < len(toks) && toks[j].text == "static" {
				isStatic = true
				j++
			}
		}
		if j < len(toks) && toks[j].kind == 'i' && j+1 < len(toks) && toks[j+1].text == "=" {
			alias = true
			j += 2
		}
		var b strings.Builder
		needID := true
		for j < len(toks) && j < i+64 {
			if vb && toks[j].line != t.line {
				break
			}
			if !vb && toks[j].line > t.line+4 {
				break
			}
			if !vb && toks[j].line > t.line && !needID {
				break
			}
			x := toks[j]
			if x.text == ";" {
				break
			}
			if x.kind == 'i' {
				b.WriteString(x.text)
				needID = false
				j++
				continue
			}
			if x.text == "." && !needID {
				b.WriteByte('.')
				needID = true
				j++
				continue
			}
			break
		}
		endLine := t.line
		if j > i && j-1 < len(toks) {
			endLine = toks[j-1].line
		}
		ns := b.String()
		if ns == "" || needID {
			continue
		}
		kind := "nuget-import"
		if vb {
			kind = "nuget-import-vb"
		}
		for _, c := range capabilitiesFor(kind, ns) {
			props := map[string]string{"import": ns, "evidence_scope": importEvidenceScope(name)}
			if alias {
				props["import_alias"] = "true"
			}
			if isStatic {
				props["import_static"] = "true"
			}
			out = append(out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: t.line, EndLine: endLine, Properties: props})
		}
		i = j - 1
	}
	return out
}

func parseJSImports(name string, content []byte) []Observation {
	out, _ := parseJSImportsBounded(name, content)
	return out
}

func parseJSImportsBounded(name string, content []byte) ([]Observation, bool) {
	toks, limited := lexSource(string(content), "js")
	return parseJSImportsTokens(name, toks, !limited), limited
}

func parseJSImportsTokens(name string, toks []sourceToken, allowCommonJS bool) []Observation {
	out := []Observation{}
	// A truncated token stream cannot rule out a later binding that shadows require.
	shadowed := !allowCommonJS || jsRequireShadowed(toks)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.depth != 0 || t.kind != 'i' {
			continue
		}
		if t.text == "import" {
			if i+1 >= len(toks) || toks[i+1].text == "(" || toks[i+1].text == "." {
				continue
			}
			typeOnly := tsImportTypeOnly(toks, i)
			j := i + 1
			spec := ""
			if toks[j].kind == 's' {
				spec = toks[j].text
			} else {
				for j < len(toks) {
					if toks[j].text == ";" {
						break
					}
					if j > i+1 && toks[j].line > t.line+12 {
						break
					}
					if toks[j].text == "import" || toks[j].text == "export" {
						break
					}
					if toks[j].text == "from" && j+1 < len(toks) && toks[j+1].kind == 's' {
						spec = toks[j+1].text
						break
					}
					j++
				}
			}
			if spec != "" {
				endLine := t.line
				if j < len(toks) {
					endLine = toks[j].line
				}
				addJSImport(&out, name, t.line, endLine, spec, typeOnly)
			}
			continue
		}
		if t.text == "require" && !shadowed && (i == 0 || (toks[i-1].text != "." && toks[i-1].text != "?." && toks[i-1].text != "/")) && i+3 < len(toks) && toks[i+1].text == "(" && toks[i+2].kind == 's' && toks[i+3].text == ")" {
			addJSImport(&out, name, t.line, toks[i+2].line, toks[i+2].text, false)
		}
	}
	return out
}
func tsImportTypeOnly(t []sourceToken, i int) bool {
	if i+2 < len(t) && t[i+1].text == "type" && t[i+2].text != "from" && (t[i+2].kind == 'i' || t[i+2].text == "{" || t[i+2].text == "*") {
		return true
	}
	open := -1
	for j := i + 1; j < len(t) && j < i+32; j++ {
		if t[j].text == "from" || t[j].text == ";" {
			break
		}
		if t[j].text == "{" && t[j].depth == 0 {
			open = j
			break
		}
	}
	if open < 0 {
		return false
	}
	groups := [][]sourceToken{{}}
	closed := false
	for j := open + 1; j < len(t); j++ {
		if t[j].text == "}" && t[j].depth == 0 {
			closed = true
			break
		}
		if t[j].text == "," && t[j].depth == 1 {
			groups = append(groups, []sourceToken{})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], t[j])
	}
	if !closed {
		return false
	}
	seen := false
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		seen = true
		if g[0].text != "type" || len(g) < 2 || g[1].kind != 'i' {
			return false
		}
	}
	return seen
}

func addJSImport(out *[]Observation, name string, line, endLine int, p string, typeOnly bool) {
	if strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") || p == "" {
		return
	}
	for _, c := range capabilitiesFor("npm-dependency", p) {
		props := map[string]string{"import": p, "evidence_scope": importEvidenceScope(name)}
		if typeOnly {
			props["import_qualifier"] = "type_only"
		}
		*out = append(*out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: line, EndLine: endLine, Properties: props})
	}
}
func jsRequireShadowed(t []sourceToken) bool {
	for i, x := range t {
		if x.text != "require" || x.kind != 'i' {
			continue
		}
		prev := ""
		if i > 0 {
			prev = t[i-1].text
		}
		if prev == "const" || prev == "let" || prev == "var" || prev == "function" || prev == "class" || prev == "catch" || prev == "import" || prev == "as" || prev == "namespace" {
			return true
		}
		if i+1 < len(t) && t[i+1].text == "=>" {
			return true
		}
		if prev == "(" || prev == "," || prev == "{" || prev == ":" { // Parameter or destructured binding; inspect a bounded local declaration shape.
			close := -1
			for j := i + 1; j < len(t) && j < i+16; j++ {
				if t[j].text == ")" || t[j].text == "}" || t[j].text == "]" {
					close = j
					break
				}
				if t[j].text == "=>" {
					return true
				}
			}
			if close > 0 && close+1 < len(t) && t[close+1].text == "=>" {
				return true
			}
			for j := i - 1; j >= 0 && j > i-16; j-- {
				if t[j].text == "=" || t[j].text == ";" {
					break
				}
				if t[j].text == "const" || t[j].text == "let" || t[j].text == "var" {
					return true
				}
			}
		}
	}
	return false
}

func isExtraImport(name string) bool {
	e := strings.ToLower(path.Ext(name))
	return e == ".java" || e == ".kt" || e == ".cs" || e == ".vb" || e == ".ts" || e == ".tsx" || e == ".js" || e == ".jsx" || e == ".mjs" || e == ".cjs"
}
func testEvidencePath(p string) bool {
	l := strings.ToLower(p)
	b := path.Base(l)
	return strings.HasPrefix(l, "tests/") || strings.HasPrefix(l, "test/") || strings.HasPrefix(l, "src/test/") || strings.HasPrefix(l, "__tests__/") || strings.Contains(l, "/src/test/") || strings.Contains(l, "/tests/") || strings.Contains(l, "/__tests__/") || strings.Contains(l, "/test/") || strings.HasPrefix(b, "test_") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") || strings.HasSuffix(b, "_test.go")
}
func importEvidenceScope(p string) string {
	if testEvidencePath(p) {
		return "test_path_convention"
	}
	return "non_test_path_convention"
}
