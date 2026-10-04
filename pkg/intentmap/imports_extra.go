package intentmap

import (
	"path"
	"strings"
	"sync"
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
	return lexSourceInto(src, lang, nil)
}

// Each lease belongs to one parser call. Clearing before return prevents the
// reusable array from retaining source contents after the observations finish.
type tokenBuffer struct {
	tokens []sourceToken
}

var importTokenBuffers = sync.Pool{New: func() any { return new(tokenBuffer) }}

func (b *tokenBuffer) release() {
	clear(b.tokens)
	if cap(b.tokens) > DefaultMaxLexicalTokensPerFile {
		// Append may grow past the lexical limit; do not retain that capacity.
		b.tokens = nil
	} else {
		b.tokens = b.tokens[:0]
	}
	importTokenBuffers.Put(b)
}

func lexSourceInto(src string, lang string, toks []sourceToken) ([]sourceToken, bool) {
	if toks == nil {
		// Reserve for typical token density, preserving the retained-token cap.
		toks = make([]sourceToken, 0, min(DefaultMaxLexicalTokensPerFile, len(src)/4+1))
	} else {
		toks = toks[:0]
	}
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
			if q == '`' {
				j = findTemplateLiteralEnd(src, start)
				closed = j < len(src) && src[j] == '`'
			} else {
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

// findTemplateLiteralEnd finds the matching backtick while accounting for
// nested template literals inside ${...}. The lexer treats all template
// content as opaque; this boundary prevents raw text after a nested template
// from being mistaken for JavaScript source.
func findTemplateLiteralEnd(src string, start int) int {
	return findTemplateLiteralEndDepth(src, start, 0)
}

func findTemplateLiteralEndDepth(src string, start, nesting int) int {
	if nesting >= 128 {
		return len(src)
	}
	type expression struct {
		braces int
		start  int
	}
	expressions := []expression{}
	quote := byte(0)
	for i := start + 1; i < len(src); {
		c := src[i]
		if c == '\\' {
			i += 2
			continue
		}
		if len(expressions) == 0 {
			if c == '`' {
				return i
			}
			if c == '$' && i+1 < len(src) && src[i+1] == '{' {
				expressions = append(expressions, expression{start: i + 2})
				i += 2
				continue
			}
			i++
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
			continue
		}
		if c == '/' && jsTemplateRegexMayStart(src, expressions[len(expressions)-1].start, i) {
			end, ok := findJSRegexEnd(src, i)
			if !ok {
				return len(src)
			}
			i = end + 1
			for i < len(src) && ((src[i] >= 'a' && src[i] <= 'z') || (src[i] >= 'A' && src[i] <= 'Z')) {
				i++
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			i++
			continue
		}
		if c == '`' {
			nestedEnd := findTemplateLiteralEndDepth(src, i, nesting+1)
			if nestedEnd >= len(src) {
				return len(src)
			}
			i = nestedEnd + 1
			continue
		}
		if c == '{' {
			expressions[len(expressions)-1].braces++
		} else if c == '}' {
			if expressions[len(expressions)-1].braces == 0 {
				expressions = expressions[:len(expressions)-1]
			} else {
				expressions[len(expressions)-1].braces--
			}
		}
		i++
	}
	return len(src)
}

func jsTemplateRegexMayStart(src string, start, slash int) bool {
	i := slash - 1
	for i >= start && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r' || src[i] == '\n') {
		i--
	}
	if i < start {
		return true
	}
	switch src[i] {
	case '(', '[', '{', '=', ':', ',', ';', '!', '?', '&', '|', '+', '-', '*', '%', '^', '~', '<', '>':
		return true
	}
	end := i + 1
	for i >= start && ((src[i] >= 'a' && src[i] <= 'z') || (src[i] >= 'A' && src[i] <= 'Z')) {
		i--
	}
	switch src[i+1 : end] {
	case "return", "throw", "case", "delete", "void", "typeof", "instanceof", "in", "of", "yield", "await":
		return true
	}
	return false
}

func findJSRegexEnd(src string, start int) (int, bool) {
	inClass := false
	for i := start + 1; i < len(src) && src[i] != '\n'; i++ {
		if src[i] == '\\' {
			i++
			continue
		}
		if src[i] == '[' {
			inClass = true
		} else if src[i] == ']' {
			inClass = false
		} else if src[i] == '/' && !inClass {
			return i, true
		}
	}
	return len(src), false
}

func jsRegexMayStart(t []sourceToken) bool {
	if len(t) == 0 {
		return true
	}
	x := t[len(t)-1]
	if x.text == ")" {
		return jsRegexAfterControlHeader(t)
	}
	if x.text == "}" {
		return jsRegexAfterControlBlock(t)
	}
	if x.kind == 'i' || x.kind == 's' || x.text == "]" || x.text == "++" || x.text == "--" {
		return x.kind == 'i' && (x.text == "return" || x.text == "throw" || x.text == "case" || x.text == "delete" || x.text == "void" || x.text == "typeof" || x.text == "instanceof" || x.text == "in" || x.text == "of" || x.text == "yield" || x.text == "await" || x.text == "else" || x.text == "do")
	}
	switch x.text {
	case "(", "[", "=", "=>", ":", ",", "!", "?", "&", "|", "+", "-", "*", "%", "{", ";":
		return true
	default:
		return false
	}
}

// A control-statement header closes with `)`, after which an expression
// statement may begin (and therefore a regexp literal may follow). A call or
// grouping expression ending in `)` instead expects an operator or another
// continuation, so a slash there remains division.
func jsRegexAfterControlHeader(t []sourceToken) bool {
	depth := 0
	for i := len(t) - 1; i >= 0; i-- {
		switch t[i].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				if i == 0 || t[i-1].kind != 'i' {
					return false
				}
				switch t[i-1].text {
				case "if", "while", "for", "with", "switch", "catch":
					return true
				default:
					return false
				}
			}
		}
	}
	return false
}

// A closing control block can also be followed by a new expression
// statement. Object literals are excluded by checking the matching opener's
// immediate context.
func jsRegexAfterControlBlock(t []sourceToken) bool {
	depth := 0
	for i := len(t) - 1; i >= 0; i-- {
		switch t[i].text {
		case "}":
			depth++
		case "{":
			depth--
			if depth == 0 {
				if i == 0 {
					return false
				}
				if t[i-1].text == ")" {
					beforeBlock := t[:i]
					return jsRegexAfterControlHeader(beforeBlock) || jsRegexAfterFunctionDeclaration(beforeBlock)
				}
				switch t[i-1].text {
				case "else", "try", "finally", "do":
					return true
				default:
					return jsRegexAfterClassDeclaration(t[:i])
				}
			}
		}
	}
	return false
}

// Function and class declarations end in blocks after which a new expression
// statement may begin. Keep this recognition bounded and require declaration
// context so function/class expressions still leave a following slash as
// division.
func jsRegexAfterFunctionDeclaration(t []sourceToken) bool {
	if len(t) == 0 || t[len(t)-1].text != ")" {
		return false
	}
	depth, open := 0, -1
parenScan:
	for i := len(t) - 1; i >= 0; i-- {
		switch t[i].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				open = i
				break parenScan
			}
		}
	}
	if open < 0 {
		return false
	}
	start := max(0, open-64)
	for i := open - 1; i >= start; i-- {
		switch t[i].text {
		case "function":
			return jsDeclarationBoundary(t, i)
		case ";", "{", "}":
			return false
		}
	}
	return false
}

func jsRegexAfterClassDeclaration(t []sourceToken) bool {
	paren, bracket, brace := 0, 0, 0
	start := max(0, len(t)-64)
	for i := len(t) - 1; i >= start; i-- {
		switch t[i].text {
		case ")":
			paren++
		case "(":
			if paren > 0 {
				paren--
			}
		case "]":
			bracket++
		case "[":
			if bracket > 0 {
				bracket--
			}
		case "}":
			brace++
		case "{":
			if brace > 0 {
				brace--
			} else {
				return false
			}
		case ";":
			if paren == 0 && bracket == 0 && brace == 0 {
				return false
			}
		}
		if paren == 0 && bracket == 0 && brace == 0 && t[i].text == "class" {
			return jsDeclarationBoundary(t, i)
		}
	}
	return false
}

func jsDeclarationBoundary(t []sourceToken, declaration int) bool {
	if declaration == 0 {
		return true
	}
	switch t[declaration-1].text {
	case ";", "{", "}", "export", "default":
		return true
	case "async", "abstract", "declare":
		return jsDeclarationBoundary(t, declaration-1)
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
	buffer := importTokenBuffers.Get().(*tokenBuffer)
	defer buffer.release()
	toks, limited := lexSourceInto(string(content), lang, buffer.tokens)
	buffer.tokens = toks
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
		caps := capabilitiesFor("maven-import", pkg)
		if len(caps) > 0 {
			pkg = strings.Clone(pkg)
		}
		for _, c := range caps {
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
	buffer := importTokenBuffers.Get().(*tokenBuffer)
	defer buffer.release()
	toks, limited := lexSourceInto(string(content), lang, buffer.tokens)
	buffer.tokens = toks
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
		caps := capabilitiesFor(kind, ns)
		if len(caps) > 0 {
			ns = strings.Clone(ns)
		}
		for _, c := range caps {
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
	buffer := importTokenBuffers.Get().(*tokenBuffer)
	defer buffer.release()
	toks, limited := lexSourceInto(string(content), "js", buffer.tokens)
	buffer.tokens = toks
	return parseJSImportsTokens(name, toks, !limited), limited
}

func parseJSImportsTokens(name string, toks []sourceToken, allowCommonJS bool) []Observation {
	out := []Observation{}
	jsxText := make([]bool, len(toks))
	// JSX is not valid in .ts files. A token sequence such as `<number>1`
	// is a TypeScript angle-bracket assertion, and an unmatched-tag heuristic
	// must not hide later imports in that file.
	if !strings.EqualFold(path.Ext(name), ".ts") {
		jsxText = jsJSXTextMask(toks)
	}
	// A truncated token stream cannot rule out a later binding that shadows require.
	shadowed := !allowCommonJS || jsRequireShadowed(toks)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.depth != 0 || t.kind != 'i' || jsxText[i] {
			continue
		}
		if t.text == "import" {
			if i+1 >= len(toks) || toks[i+1].text == "(" || toks[i+1].text == "." {
				continue
			}
			typeOnly := tsImportTypeOnly(toks, i)
			if spec, end, ok := tsImportEqualsRequire(toks, i); ok {
				addJSImport(&out, name, t.line, toks[end].line, spec, typeOnly)
				i = end
				continue
			}
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

// jsJSXTextMask excludes children text from the token stream's apparent
// JavaScript. JSX expressions remain separately brace-scoped and are already
// excluded by the top-level import rule.
func jsJSXTextMask(t []sourceToken) []bool {
	mask := make([]bool, len(t))
	for i := 0; i+1 < len(t); i++ {
		if t[i].text != "<" || (t[i+1].kind != 'i' && t[i+1].text != ">") || !jsJSXStartContext(t, i) {
			continue
		}
		fragment := t[i+1].text == ">"
		openEnd := i + 2
		if fragment {
			openEnd = i + 1
		} else {
			for openEnd < len(t) && t[openEnd].text != ">" && t[openEnd].text != ";" && openEnd < i+64 {
				openEnd++
			}
			if openEnd >= len(t) || t[openEnd].text != ">" {
				continue
			}
			if openEnd > i && t[openEnd-1].text == "/" {
				// A self-closing JSX element has no child-text region.
				continue
			}
		}
		depth, closeEnd := 1, -1
		for j := openEnd + 1; j < len(t) && depth > 0; j++ {
			if t[j].text != "<" || j+1 >= len(t) {
				continue
			}
			closing := t[j+1].text == "/"
			if fragment && closing && j+2 < len(t) && t[j+2].text == ">" {
				depth--
				if depth == 0 {
					closeEnd = j + 2
				}
				j += 2
				continue
			}
			nameAt := j + 1
			if closing {
				nameAt++
			}
			if nameAt >= len(t) || t[nameAt].kind != 'i' {
				continue
			}
			end := nameAt + 1
			for end < len(t) && t[end].text != ">" && end < nameAt+64 {
				end++
			}
			if end >= len(t) || t[end].text != ">" {
				continue
			}
			if closing {
				depth--
				if depth == 0 {
					closeEnd = end
				}
			} else if end == 0 || t[end-1].text != "/" {
				depth++
			}
			j = end
		}
		if closeEnd > openEnd {
			for j := openEnd + 1; j < closeEnd; j++ {
				mask[j] = true
			}
		} else if !fragment && t[i+1].text != "" && t[i+1].text[0] >= 'a' && t[i+1].text[0] <= 'z' {
			// An unmatched lowercase tag is likely malformed JSX. Omit apparent
			// source tokens through EOF rather than treating its children as code.
			for j := openEnd + 1; j < len(mask); j++ {
				mask[j] = true
			}
		}
	}
	return mask
}

func jsJSXStartContext(t []sourceToken, i int) bool {
	if i == 0 {
		return true
	}
	switch t[i-1].text {
	case "=", "(", "=>", ":", ",", "[", "return", "yield":
		return true
	default:
		return false
	}
}

// tsImportEqualsRequire recognizes TypeScript's `import Alias =
// require("module")` form (including its `import type` variant). Without
// advancing over the declaration, its require token is later misread as a
// CommonJS runtime import.
func tsImportEqualsRequire(t []sourceToken, i int) (spec string, end int, ok bool) {
	alias := i + 1
	if alias < len(t) && t[alias].text == "type" {
		alias++
	}
	if alias+5 >= len(t) || t[alias].kind != 'i' || t[alias+1].text != "=" || t[alias+2].text != "require" || t[alias+3].text != "(" || t[alias+4].kind != 's' || t[alias+5].text != ")" {
		return "", 0, false
	}
	return t[alias+4].text, alias + 5, true
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
	if open < 0 || open != i+1 {
		// A default binding before the named imports is runtime evidence even
		// when every named specifier is qualified with `type`.
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
		typeOnlySpecifier := g[0].text == "type" && len(g) >= 2 && g[1].kind == 'i'
		if typeOnlySpecifier && g[1].text == "as" {
			// `{ type as }` imports the type-only name `as`; `{ type as X }`
			// imports the runtime export `type` under the local name `X`.
			typeOnlySpecifier = len(g) == 2
		}
		if !typeOnlySpecifier {
			return false
		}
	}
	return seen
}

func addJSImport(out *[]Observation, name string, line, endLine int, p string, typeOnly bool) {
	packageName := jsImportPackageName(p)
	if packageName == "" {
		return
	}
	// `import` records the validated npm package root, rather than preserving
	// arbitrary subpaths. This keeps retained evidence small and states which
	// catalog entry the source specifier matched.
	packageName = strings.Clone(packageName)
	for _, c := range capabilitiesForJSImport(packageName) {
		props := map[string]string{"import": packageName, "import_representation": "package_root", "evidence_scope": importEvidenceScope(name)}
		if typeOnly {
			props["import_qualifier"] = "type_only"
		}
		*out = append(*out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: line, EndLine: endLine, Properties: props})
	}
}

// jsImportPackageName returns the package root for a bare Node module
// specifier. It deliberately does not lowercase or strip npm-style @version
// suffixes: source imports are not manifest dependency declarations.
func jsImportPackageName(specifier string) string {
	if strings.HasPrefix(specifier, "node:") || strings.ContainsAny(specifier, "\\?# \t\r\n") {
		return "" // Node built-ins and URL/query forms have no npm catalog entry.
	}
	if strings.HasPrefix(specifier, "@") {
		slash := strings.IndexByte(specifier, '/')
		if slash <= 1 || slash == len(specifier)-1 {
			return ""
		}
		end := strings.IndexByte(specifier[slash+1:], '/')
		nameEnd := len(specifier)
		if end >= 0 {
			nameEnd = slash + 1 + end
		}
		if !validNPMImportName(specifier[1:slash]) || !validNPMImportName(specifier[slash+1:nameEnd]) || nameEnd > 214 {
			return ""
		}
		return specifier[:nameEnd]
	}
	if strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") || strings.Contains(specifier, ":") {
		return ""
	}
	if slash := strings.IndexByte(specifier, '/'); slash >= 0 {
		specifier = specifier[:slash]
	}
	if !validNPMImportName(specifier) {
		return ""
	}
	return specifier
}

func validNPMImportName(name string) bool {
	if len(name) == 0 || len(name) > 214 || name == "." || name == ".." || name[0] == '.' || name[0] == '_' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
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
	if pathHasTestDirectory(l) {
		return true
	}
	ext := path.Ext(b)
	originalBase := path.Base(p)
	originalStem := strings.TrimSuffix(originalBase, path.Ext(originalBase))
	switch ext {
	case ".py":
		return strings.HasPrefix(originalBase, "test_") || strings.HasSuffix(originalBase, "_test.py")
	case ".go":
		return strings.HasSuffix(b, "_test.go")
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")
	case ".java", ".kt", ".cs", ".vb":
		return strings.HasSuffix(originalStem, "Test") || strings.HasSuffix(originalStem, "Tests") || strings.HasSuffix(originalStem, "TestCase")
	default:
		return false
	}
}

// pathHasTestDirectory recognizes conventional test directories at any path
// depth, including the whole-repository test and tests roots.
func pathHasTestDirectory(p string) bool {
	for _, segment := range strings.Split(strings.Trim(p, "/"), "/") {
		switch segment {
		case "test", "tests", "__tests__":
			return true
		}
	}
	return false
}
func importEvidenceScope(p string) string {
	if testEvidencePath(p) {
		return "test_path_convention"
	}
	return "non_test_path_convention"
}
