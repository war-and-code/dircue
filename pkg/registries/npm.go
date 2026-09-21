package registries

import (
	"bytes"
	"encoding/json"
	"strings"
)

func parseNPM(c *Configuration, content []byte) {
	section := false
	for _, raw := range bytes.FieldsFunc(content, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if len(raw) > MaxNPMLineBytes {
			c.fail("npm_line_limit", "incomplete")
			return
		}
		line := strings.TrimSpace(string(raw))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = true
			c.omit("unsupported_npm_section")
			continue
		}
		key, value, hasEquals := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if strings.HasPrefix(key, "\"") || strings.HasPrefix(key, "'") {
			decoded, ok := npmValue(key)
			if ok && (decoded == "registry" || strings.HasPrefix(decoded, "@") && strings.HasSuffix(decoded, ":registry")) {
				c.omit("unsupported_npm_key")
			}
			continue
		}
		array := strings.HasSuffix(key, "[]")
		base := strings.TrimSuffix(key, "[]")
		isDefault := base == "registry"
		isScoped := strings.HasPrefix(base, "@") && strings.HasSuffix(base, ":registry")
		if !isDefault && !isScoped {
			continue
		}
		if array {
			c.omit("unsupported_npm_array_registry")
			continue
		}
		if section {
			c.omit("unsupported_npm_section_registry")
			continue
		}
		if !hasEquals {
			c.omit("invalid_npm_declaration")
			continue
		}
		decoded, ok := npmValue(value)
		if !ok || decoded == "true" || decoded == "false" || decoded == "null" {
			c.omit("unsupported_npm_value")
			continue
		}
		d := Declaration{Section: "registry", Operation: "registry", Semantics: "declared", Endpoint: endpoint(decoded)}
		if isScoped {
			d.Scope = label(strings.TrimSuffix(base, ":registry"), "scope")
		}
		c.add(qualify(c, d))
	}
}

// This accepts the ordinary string subset of npm/ini. Unsupported or ambiguous
// quoted/array values are omitted instead of being reinterpreted as URLs.
func npmValue(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", true
	}
	if v[0] == '"' {
		var decoded string
		if json.Unmarshal([]byte(v), &decoded) != nil {
			return "", false
		}
		return decoded, true
	}
	if v[0] == '\'' {
		if len(v) < 2 || v[len(v)-1] != '\'' {
			return "", false
		}
		inside := v[1 : len(v)-1]
		if json.Valid([]byte(inside)) {
			trimmed := strings.TrimSpace(inside)
			if trimmed[0] != '"' {
				return "", false
			}
			var text string
			if json.Unmarshal([]byte(inside), &text) != nil {
				return "", false
			}
			return text, true
		}
		if strings.ContainsAny(inside, "'\"\\\r\n") {
			return "", false
		}
		return inside, true
	}
	var out strings.Builder
	escaped := false
	for _, ch := range v {
		if escaped {
			if ch != '\\' && ch != ';' && ch != '#' {
				out.WriteByte('\\')
			}
			out.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '#' || ch == ';' {
			break
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		out.WriteRune(ch)
	}
	if escaped {
		out.WriteByte('\\')
	}
	return strings.TrimSpace(out.String()), true
}
