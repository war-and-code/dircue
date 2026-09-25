package intentmap

import (
	"regexp"
	"strings"
)

var protoTokens = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*|[{}();=]`)

func parseProto(name string, content []byte) []Observation {
	clean := stripProtoComments(string(content))
	indices := protoTokens.FindAllStringIndex(clean, -1)
	tokens := make([]string, len(indices))
	for i, index := range indices {
		tokens[i] = clean[index[0]:index[1]]
	}
	var out []Observation
	packageName := ""
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i] == "package" {
			packageName = tokens[i+1]
			continue
		}
		if tokens[i] != "service" || !identifier(tokens[i+1]) {
			continue
		}
		service := tokens[i+1]
		qualified := service
		if packageName != "" {
			qualified = packageName + "." + service
		}
		line := lineAt(clean, indices[i+1][0])
		out = append(out, Observation{Kind: KindInterface, Name: qualified, State: "declared", Basis: "declared_contract", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"protocol": "protobuf", "interface_kind": "service"}})
		j := i + 2
		for j < len(tokens) && tokens[j] != "{" {
			j++
		}
		if j == len(tokens) {
			continue
		}
		depth := 1
		for j++; j < len(tokens) && depth > 0; j++ {
			switch tokens[j] {
			case "{":
				depth++
			case "}":
				depth--
			case "rpc":
				if depth == 1 && j+1 < len(tokens) && identifier(tokens[j+1]) {
					rpc := tokens[j+1]
					rpcLine := lineAt(clean, indices[j+1][0])
					out = append(out, Observation{Kind: KindInterface, Name: qualified + "/" + rpc, State: "declared", Basis: "declared_contract", Path: name, StartLine: rpcLine, EndLine: rpcLine, Properties: map[string]string{"protocol": "protobuf", "interface_kind": "operation", "service": qualified}})
				}
			}
		}
		i = j - 1
	}
	return out
}

func stripProtoComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inBlock := false
	inLine := false
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		next := byte(0)
		if i+1 < len(s) {
			next = s[i+1]
		}
		if inLine {
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if inBlock {
			if c == '*' && next == '/' {
				b.WriteString("  ")
				i++
				inBlock = false
			} else if c == '\n' {
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if !inString && c == '/' && next == '/' {
			b.WriteString("  ")
			i++
			inLine = true
			continue
		}
		if !inString && c == '/' && next == '*' {
			b.WriteString("  ")
			i++
			inBlock = true
			continue
		}
		if inString && c != '"' && c != '\n' {
			b.WriteByte(' ')
		} else {
			b.WriteByte(c)
		}
		if c == '"' && !escaped {
			inString = !inString
		}
		escaped = c == '\\' && !escaped
		if c != '\\' {
			escaped = false
		}
	}
	return b.String()
}
func identifier(s string) bool {
	return s != "" && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z') || s[0] == '_')
}
func lineAt(s string, offset int) int { return 1 + strings.Count(s[:offset], "\n") }
