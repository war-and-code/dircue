package tokenizer

import (
	"bytes"
	"strings"
)

// Frozen RC1 tokenizer loop, retained solely as an ordered-token test oracle.
// Source v0.1.0-rc.1 linguist.go SHA256: a25e3a34163965df585fdafb66ab8f3450fbbcec617c54bdc43668d0d4c0055f
// It deliberately bypasses asciiIdentifierLength and uses the original lexer.
func referenceLinguistTokenize(content []byte) []string {
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
		lexer := ordinaryLexer
		if beginning {
			lexer = beginningLexer
		}
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
