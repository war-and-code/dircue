package tokenizer

import (
	"bytes"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
)

// LinguistTokenize is a pure-Go port of Linguist 9.7.0 ext/linguist/tokenizer.l.
// Flex chooses the longest match, then the first rule on equal lengths. Go's
// linear-time regexp engine implements this with Longest and ordered branches.
// Tokens have a 16-byte limit; the upstream native scanner reads at most 100,000 bytes.
func LinguistTokenize(content []byte) []string {
	if len(content) > 100000 {
		content = content[:100000]
	}
	var tokens []string
	emit := func(token string) {
		if len(token) > 16 {
			token = token[:16]
		}
		tokens = append(tokens, token)
	}
	beginning := true
	for len(content) > 0 {
		// Only the identifier rule and one-rune fallback can start with an
		// ASCII letter or underscore, in either lexer state. The identifier
		// wins by longest match and declaration order. Numeric and prefixed
		// identifiers still need the general lexer to resolve competing rules.
		if size := asciiIdentifierLength(content); size > 0 {
			emit(string(content[:size]))
			content = content[size:]
			beginning = false
			continue
		}
		lexer := &ordinaryLexer
		if beginning {
			lexer = &beginningLexer
		}
		lexer = lexer.forInitialByte(content[0])
		match := lexer.expression.FindSubmatchIndex(content)
		if match == nil {
			panic("tokenizer fallback failed")
		}
		selected := -1
		for i, index := range lexer.groups {
			if match[index*2] >= 0 {
				selected = i
				break
			}
		}
		rule := lexer.rules[selected]
		size := match[1]
		lexeme := content[:size]
		content = content[size:]
		beginning = lexeme[len(lexeme)-1] == '\n'
		switch rule.action {
		case "feed":
			emit(string(lexeme))
		case "static":
			emit(rule.value)
		case "block":
			emit(rule.value)
			closing := []byte(rule.end)
			index := bytes.Index(content, closing)
			if index < 0 {
				return tokens
			}
			end := index + len(closing)
			beginning = content[end-1] == '\n'
			content = content[end:]
		case "string":
			quote := lexeme[0]
			for len(content) > 0 {
				c := content[0]
				content = content[1:]
				beginning = c == '\n'
				if c == 0 {
					return tokens
				}
				if c == '\n' || c == quote {
					break
				}
				if c == '\\' {
					if len(content) == 0 || content[0] == 0 {
						return tokens
					}
					beginning = content[0] == '\n'
					content = content[1:]
				}
			}
		case "shebang-env", "shebang":
			token := string(lexeme)
			separator := '/'
			if rule.action == "shebang-env" {
				separator = ' '
			}
			if index := strings.LastIndexByte(token, byte(separator)); index >= 0 {
				token = token[index+1:]
			}
			if token != "env" || rule.action == "shebang-env" {
				if len(token) > 16 {
					token = token[:16]
				}
				tokens = append(tokens, "SHEBANG#!"+token)
			}
			for len(content) > 0 {
				c := content[0]
				content = content[1:]
				if c == 0 {
					return tokens
				}
				if c == '\n' {
					beginning = true
					break
				}
			}
		}
	}
	return tokens
}

func asciiIdentifierLength(content []byte) int {
	if len(content) == 0 || !asciiIdentifierStart(content[0]) {
		return 0
	}
	size := 1
	for size < len(content) {
		c := content[size]
		if !asciiIdentifierStart(c) && !(c >= '0' && c <= '9') {
			break
		}
		size++
	}
	return size
}

func asciiIdentifierStart(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_'
}

type lexicalRule struct{ pattern, action, value, end string }
type compiledLexer struct {
	expression *regexp.Regexp
	groups     []int
	rules      []lexicalRule
	ascii      *[128]*compiledLexer
}

func compileLexer(rules []lexicalRule) compiledLexer {
	ids := make([]int, len(rules))
	first := make([][128]bool, len(rules))
	for i, rule := range rules {
		ids[i] = i
		first[i] = possibleFirstASCII(rule.pattern)
	}
	full := compileLexerExpression(rules, ids)
	full.ascii = new([128]*compiledLexer)
	// Intern within this rule list: a mask identifies both the selected rules
	// and their declaration order. Variable-length masks permit any rule count.
	subsets := make(map[string]*compiledLexer)
	for c := range full.ascii {
		mask := make([]byte, (len(rules)+7)/8)
		var selected []lexicalRule
		var selectedIDs []int
		for i, rule := range rules {
			if first[i][c] {
				mask[i/8] |= 1 << uint(i%8)
				selected = append(selected, rule)
				selectedIDs = append(selectedIDs, i)
			}
		}
		if len(selected) == 0 || len(selected) == len(rules) {
			continue // nil means retain the full expression.
		}
		key := string(mask)
		if prior := subsets[key]; prior != nil {
			full.ascii[c] = prior
			continue
		}
		subset := compileLexerExpression(selected, selectedIDs)
		subsets[key] = &subset
		full.ascii[c] = &subset
	}
	return full
}

func (lexer *compiledLexer) forInitialByte(c byte) *compiledLexer {
	if c < 128 && lexer.ascii != nil && lexer.ascii[c] != nil {
		return lexer.ascii[c]
	}
	return lexer
}

// possibleFirstASCII conservatively overapproximates the first consumed rune.
// Empty-width assertions are ignored; nullable rules remain eligible for every
// byte. Removing a rule requires proving it cannot match this initial byte.
func possibleFirstASCII(pattern string) [128]bool {
	all := func() (result [128]bool) {
		for i := range result {
			result[i] = true
		}
		return result
	}
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return all()
	}
	program, err := syntax.Compile(expression.Simplify())
	if err != nil {
		return all()
	}
	var result [128]bool
	seen := make([]bool, len(program.Inst))
	pending := []uint32{uint32(program.Start)}
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if int(pc) >= len(program.Inst) {
			return all()
		}
		if seen[pc] {
			continue
		}
		seen[pc] = true
		instruction := &program.Inst[pc]
		switch instruction.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			pending = append(pending, instruction.Out, instruction.Arg)
		case syntax.InstCapture, syntax.InstNop, syntax.InstEmptyWidth:
			pending = append(pending, instruction.Out)
		case syntax.InstMatch:
			return all()
		case syntax.InstFail:
		case syntax.InstRune:
			for c := range result {
				result[c] = result[c] || instruction.MatchRune(rune(c))
			}
		case syntax.InstRune1:
			if len(instruction.Rune) != 1 || instruction.Arg != 0 {
				return all()
			}
			if c := instruction.Rune[0]; c >= 0 && c < 128 {
				result[c] = true
			}
		case syntax.InstRuneAny:
			return all()
		case syntax.InstRuneAnyNotNL:
			for c := range result {
				result[c] = result[c] || c != '\n'
			}
		default:
			return all()
		}
	}
	return result
}

func compileLexerExpression(rules []lexicalRule, ids []int) compiledLexer {
	parts := make([]string, len(rules))
	for i, rule := range rules {
		parts[i] = "(?P<r" + strconv.Itoa(ids[i]) + ">" + rule.pattern + ")"
	}
	expression := regexp.MustCompile("^(?:" + strings.Join(parts, "|") + ")")
	expression.Longest()
	groups := make([]int, len(rules))
	for i := range rules {
		groups[i] = expression.SubexpIndex("r" + strconv.Itoa(ids[i]))
	}
	return compiledLexer{expression: expression, groups: groups, rules: rules}
}

var beginningRules = []lexicalRule{
	{`#![ \t]*(?:[A-Za-z0-9_/]*/)?env(?:[ \t]+(?:[^ \t=]*=[^ \t]*))*[ \t]+[A-Za-z_]+`, "shebang-env", "", ""},
	{`#![ \t]*[A-Za-z_/]+`, "shebang", "", ""},
	{`[ \t]*#+(?: .*|\n)`, "static", "COMMENT#", ""},
	{`[ \t]*//!(?: .*|\n)`, "static", "COMMENT//!", ""},
	{`[ \t]*//.*\n?`, "static", "COMMENT//", ""},
	{`[ \t]*--(?: .*|\n)`, "static", "COMMENT--", ""},
	{`[ \t]*%+(?: .*|\n)`, "static", "COMMENT%", ""},
	{`[ \t]*"(?: .*|\n)`, "static", "COMMENT\"", ""},
	{`[ \t]*;+(?: .*|\n)`, "static", "COMMENT;", ""},
	{`\.[ \t]*\\"(?:.*|\n)`, "static", "COMMENT.\\\"", ""},
	{`'[ \t]*\\"(?:.*|\n)`, "static", "COMMENT'\\\"", ""},
	{`\$! (?:.*|\n)`, "static", "COMMENT$!", ""},
	{`\.ig\n`, "block", "COMMENT.ig", "..\n"},
}
var ordinaryRules = []lexicalRule{
	{`/\*\*/`, "static", "COMMENT/*", ""},
	{`/\*\*`, "block", "COMMENT/**", "*/"}, {`/\*!`, "block", "COMMENT/*!", "*/"}, {`/\*`, "block", "COMMENT/*", "*/"},
	{`<!--`, "block", "COMMENT<!--", "-->"}, {`\{-`, "block", "COMMENT{-", "-}"}, {`\(\*`, "block", "COMMENT(*", "*)"},
	{`"""`, "block", "COMMENT\"\"\"", "\"\"\""}, {`'''`, "block", "COMMENT'''", "'''"},
	{`/--`, "block", "COMMENT/-", "-/"}, {`/-`, "block", "COMMENT/-", "-/"},
	{`""|''`, "skip", "", ""}, {`"|'`, "string", "", ""},
	{`(?:0x[0-9a-fA-F](?:[0-9a-fA-F]|\.)*|[0-9](?:[0-9]|\.)*)(?:[uU][lL]{0,2}|(?:[eE][-+][0-9]*)?[fFlL]*)`, "skip", "", ""},
	{`[.@#$]?[A-Za-z0-9_]+`, "feed", "", ""},
	{`\(+\)+|\{+\}+|\[+\]+|\(+|\)+|\{+|\}+|\[+|\]+|\$(?:\(+|\{+|\[\]+)`, "feed", "", ""},
	{`\(\.\.\.\)|\{\.\.\.\}|\[\.\.\.\]`, "feed", "", ""},
	{`&>|<&|<&-|&>>|>&|\|&|&\|`, "feed", "", ""},
	{`-+>+|<+-+`, "feed", "", ""},
	{`!+=+|[<>]*=+[<>]*|</?[?%!#@]|[?%!]>|[<>/]+|[-+*/%&|^~:]=+|[!=]~|:-`, "feed", "", ""},
	{`\.\*+\??|\.\++\??|\(\?:`, "feed", "", ""},
	{`-+|!+|#+|\$+|%+|&+|\*+|\++|,+|\.+|:+|;+|\?+|@+|\\+|\^+|` + "`" + `+|\|+|~+`, "feed", "", ""},
	{`(?s:.)`, "skip", "", ""},
}
var ordinaryLexer = compileLexer(ordinaryRules)
var beginningLexer = compileLexer(append(append([]lexicalRule{}, beginningRules...), ordinaryRules...))
