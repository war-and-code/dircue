package registries

import (
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func parseCargoConfig(c *Configuration, content []byte) {
	if reason := cargoTOMLLimit(content); reason != "" {
		c.fail(reason, "incomplete")
		return
	}
	var root map[string]any
	if err := toml.Unmarshal(content, &root); err != nil {
		c.fail("invalid_toml", "invalid")
		return
	}
	tokens := 0
	if reason := tomlTreeBudget(root, 1, &tokens); reason != "" {
		c.fail(reason, "incomplete")
		return
	}
	// Cargo can merge additional configuration files into this one. Those
	// files are outside the selected declaration and cannot be represented as
	// complete registry coverage by this reader.
	if include, exists := root["include"]; exists {
		if values, isArray := include.([]any); !isArray || len(values) > 0 {
			c.omit("unsupported_cargo_include")
		}
	}
	parseCargoRegistries(c, root)
	parseCargoSources(c, root)
	parseCargoDefault(c, root)
}

func cargoTOMLLimit(content []byte) string {
	depth, tokens := 0, 0
	quote := byte(0)
	triple := false
	escape := false
	comment := false
	keySegments := 1
	keyContext := true
	lineStart := true
	header := false
	headerCloses := 0
	headerArrayTable := false
	contextSegments := 0
	valueSegments := 0
	arrayDepth := 0
	valueArrayDepth := 0
	type inlineContext struct {
		contextSegments int
		valueSegments   int
		arrayDepth      int
		valueArrayDepth int
	}
	inlineTables := []inlineContext{}
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if comment {
			if ch == '\n' || ch == '\r' {
				comment = false
				keySegments = 1
				keyContext = valueArrayDepth == 0
				lineStart = true
			}
			continue
		}
		if quote != 0 {
			if escape {
				escape = false
				continue
			}
			if quote == '"' && ch == '\\' {
				escape = true
				continue
			}
			if triple {
				if ch == quote && i+2 < len(content) && content[i+1] == quote && content[i+2] == quote {
					i += 2
					quote, triple = 0, false
				}
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '\n' || ch == '\r' {
			keySegments = 1
			keyContext = valueArrayDepth == 0
			lineStart = true
			continue
		}
		if lineStart && (ch == ' ' || ch == '\t') {
			continue
		}
		if lineStart {
			lineStart = false
			if ch == '[' && keyContext && valueArrayDepth == 0 && len(inlineTables) == 0 {
				header = true
				headerCloses = 1
				headerArrayTable = i+1 < len(content) && content[i+1] == '['
				if headerArrayTable {
					headerCloses = 2
				}
				keySegments, keyContext = 1, true
			}
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			triple = i+2 < len(content) && content[i+1] == ch && content[i+2] == ch
			if triple {
				i += 2
			}
			continue
		}
		switch ch {
		case '[', '{':
			if ch == '[' && header {
				// Table headers contribute their absolute dotted path depth.
				tokens++
			} else {
				depth++
				if depth > MaxTOMLDepth {
					return "toml_depth_limit"
				}
				tokens++
				if ch == '[' {
					arrayDepth++
					if !keyContext {
						valueArrayDepth++
						if contextSegments+keySegments+valueArrayDepth+1 > MaxTOMLDepth {
							return "toml_depth_limit"
						}
					}
				} else {
					inlineTables = append(inlineTables, inlineContext{contextSegments: contextSegments, valueSegments: valueSegments, arrayDepth: arrayDepth, valueArrayDepth: valueArrayDepth})
					contextSegments = valueSegments + valueArrayDepth
					valueArrayDepth = 0
					keySegments, keyContext = 1, true
				}
			}
		case ']', '}':
			if ch == ']' && header {
				tokens++
				headerCloses--
				if headerCloses == 0 {
					header = false
					contextSegments = keySegments
					if headerArrayTable {
						contextSegments++
					}
					keyContext = false
					if contextSegments+1 > MaxTOMLDepth {
						return "toml_depth_limit"
					}
				}
			} else {
				if depth > 0 {
					depth--
				}
				tokens++
				if ch == ']' {
					if arrayDepth > 0 {
						arrayDepth--
					}
					if valueArrayDepth > 0 {
						valueArrayDepth--
					}
				} else if len(inlineTables) > 0 {
					last := inlineTables[len(inlineTables)-1]
					inlineTables = inlineTables[:len(inlineTables)-1]
					contextSegments, valueSegments = last.contextSegments, last.valueSegments
					valueArrayDepth = last.valueArrayDepth
					keyContext = false
				}
			}
		case '=', ',', '.':
			tokens++
			if ch == '=' {
				if keyContext && contextSegments+keySegments+1 > MaxTOMLDepth {
					return "toml_depth_limit"
				}
				valueSegments = contextSegments + keySegments
				valueArrayDepth = 0
				keyContext = false
			} else if ch == ',' {
				if len(inlineTables) > 0 && arrayDepth == inlineTables[len(inlineTables)-1].arrayDepth {
					keySegments, keyContext = 1, true
				}
			} else if keyContext {
				keySegments++
				baseSegments := contextSegments
				if header {
					baseSegments = 0
				}
				// Each dotted-key segment creates a map layer. Include the
				// enclosing table or inline-table path before Unmarshal builds
				// the nested maps.
				if baseSegments+keySegments+1 > MaxTOMLDepth {
					return "toml_depth_limit"
				}
			}
		}
		if tokens > MaxTOMLTokens {
			return "toml_token_limit"
		}
	}
	return ""
}

func tomlTreeBudget(v any, depth int, tokens *int) string {
	if depth > MaxTOMLDepth {
		return "toml_depth_limit"
	}
	*tokens++
	switch item := v.(type) {
	case map[string]any:
		for _, key := range sortedMapKeys(item) {
			*tokens++
			if *tokens > MaxTOMLTokens {
				return "toml_token_limit"
			}
			if reason := tomlTreeBudget(item[key], depth+1, tokens); reason != "" {
				return reason
			}
		}
	case []any:
		for _, child := range item {
			if reason := tomlTreeBudget(child, depth+1, tokens); reason != "" {
				return reason
			}
		}
	}
	if *tokens > MaxTOMLTokens {
		return "toml_token_limit"
	}
	return ""
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func parseCargoRegistries(c *Configuration, root map[string]any) {
	value, exists := root["registries"]
	if !exists {
		return
	}
	registries, ok := stringMap(value)
	if !ok {
		c.omit("unsupported_cargo_registries_table")
		return
	}
	for _, name := range sortedMapKeys(registries) {
		entry, ok := stringMap(registries[name])
		if !ok {
			c.omit("unsupported_cargo_registry_entry")
			continue
		}
		index, exists := entry["index"]
		if !exists {
			omitUnknownCargoKeys(c, entry, "index")
			continue
		}
		raw, ok := index.(string)
		if !ok {
			c.omit("unsupported_cargo_registry_index")
			continue
		}
		d := Declaration{Section: "cargoRegistry", Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(name, "name"), Endpoint: cargoEndpoint(raw)}
		c.add(qualify(c, d))
		omitUnknownCargoKeys(c, entry, "index")
	}
}

func parseCargoSources(c *Configuration, root map[string]any) {
	value, exists := root["source"]
	if !exists {
		return
	}
	sources, ok := stringMap(value)
	if !ok {
		c.omit("unsupported_cargo_source_table")
		return
	}
	for _, name := range sortedMapKeys(sources) {
		entry, ok := stringMap(sources[name])
		if !ok {
			c.omit("unsupported_cargo_source_entry")
			continue
		}
		c.add(qualify(c, Declaration{Section: "cargoSource", Operation: "add", Semantics: "declared", Applicability: "unresolved", Name: label(name, "name")}))
		for _, field := range []string{"directory", "git", "local-registry", "registry"} {
			value, exists := entry[field]
			if !exists {
				continue
			}
			raw, ok := value.(string)
			if !ok {
				c.omit("unsupported_cargo_source_value")
				continue
			}
			endpointValue := cargoEndpoint(raw)
			if field == "directory" || field == "local-registry" {
				endpointValue = cargoLocalPath(raw)
			}
			d := Declaration{Section: "cargoSource", Operation: "source", Semantics: "declared", Applicability: "unresolved", Name: label(name, "name"), Scope: label(field, "name"), Endpoint: endpointValue}
			c.add(qualify(c, d))
		}
		if target, exists := entry["replace-with"]; exists {
			raw, ok := target.(string)
			if !ok {
				c.omit("unsupported_cargo_replacement")
			} else {
				d := Declaration{Section: "cargoSource", Operation: "replace", Semantics: "declared", Applicability: "unresolved", Name: label(name, "name"), Scope: label(raw, "name")}
				c.add(qualify(c, d))
			}
		}
		omitUnknownCargoKeys(c, entry, "directory", "git", "local-registry", "registry", "replace-with")
	}
}

func parseCargoDefault(c *Configuration, root map[string]any) {
	value, exists := root["registry"]
	if !exists {
		return
	}
	registry, ok := stringMap(value)
	if !ok {
		c.omit("unsupported_cargo_registry_settings")
		return
	}
	defaultValue, exists := registry["default"]
	if exists {
		if name, ok := defaultValue.(string); ok {
			c.add(qualify(c, Declaration{Section: "cargoRegistry", Operation: "default", Semantics: "declared", Applicability: "unresolved", Name: label(name, "name")}))
		} else {
			c.omit("unsupported_cargo_default")
		}
	}
	omitUnknownCargoKeys(c, registry, "default")
}

func cargoEndpoint(value string) *Endpoint {
	if strings.HasPrefix(value, "sparse+https://") {
		value = strings.TrimPrefix(value, "sparse+")
	} else if strings.HasPrefix(value, "sparse+http://") {
		value = strings.TrimPrefix(value, "sparse+")
	}
	e := endpoint(value)
	// These Cargo fields are URLs, unlike NuGet packageSources where a local
	// path is a valid source. Do not turn a malformed relative URL into a local
	// path fact just because the shared sanitizer supports that other format.
	if e.Status == "local_path" {
		return &Endpoint{Status: "invalid"}
	}
	return e
}

func cargoLocalPath(value string) *Endpoint {
	preparePatterns()
	if variable.MatchString(value) {
		return &Endpoint{Status: "unresolved"}
	}
	return &Endpoint{Status: "local_path"}
}

func stringMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func omitUnknownCargoKeys(c *Configuration, entry map[string]any, allowed ...string) {
	allow := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allow[key] = struct{}{}
	}
	for key := range entry {
		if _, ok := allow[key]; !ok {
			c.omit("unsupported_cargo_field")
		}
	}
}
