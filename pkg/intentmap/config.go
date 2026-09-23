package intentmap

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

func configCandidate(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == ".env" || strings.HasPrefix(base, ".env.") || base == "application.properties" || strings.HasPrefix(base, "application-") && strings.HasSuffix(base, ".properties") || strings.HasPrefix(base, "appsettings") && strings.HasSuffix(base, ".json")
}

func parseConfig(name string, content []byte) []Observation {
	if strings.HasSuffix(strings.ToLower(name), ".json") {
		return parseJSONConfig(name, content)
	}
	var out []Observation
	for index, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		pos := strings.IndexAny(trimmed, "=:")
		if pos <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:pos])
		if !safeConfigKey(key) {
			continue
		}
		if cap, ok := configCapability(key); ok {
			out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, StartLine: index + 1, EndLine: index + 1, Properties: map[string]string{"key": key}})
		}
		if configEntryKey(key) {
			out = append(out, Observation{Kind: KindConfig, Name: key, State: "declared", Basis: "declared_config", Path: name, StartLine: index + 1, EndLine: index + 1})
		}
	}
	return out
}
func parseJSONConfig(name string, content []byte) []Observation {
	out, _ := parseJSONConfigBounded(name, content)
	return out
}

func parseJSONConfigBounded(name string, content []byte) ([]Observation, bool) {
	var root map[string]any
	if json.Unmarshal(content, &root) != nil {
		return nil, false
	}
	var out []Observation
	depthLimited := false
	var walk func(map[string]any, string, int)
	walk = func(m map[string]any, prefix string, depth int) {
		if depth > 32 {
			depthLimited = true
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			v := m[k]
			key := k
			if prefix != "" {
				key = prefix + ":" + k
			}
			if child, ok := v.(map[string]any); ok {
				walk(child, key, depth+1)
			}
			if text, ok := v.(string); ok && amqpURI(text) {
				out = append(out, Observation{Kind: KindCapability, Name: "messaging:amqp", State: "declared", Basis: "declared_config", Path: name, Properties: map[string]string{"key": key}})
			}
			if cap, ok := configCapability(key); ok {
				out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, Properties: map[string]string{"key": key}})
			}
			if configEntryKey(key) {
				out = append(out, Observation{Kind: KindConfig, Name: key, State: "declared", Basis: "declared_config", Path: name})
			}
		}
	}
	walk(root, "", 0)
	return out, depthLimited
}

func amqpURI(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "amqp://") || strings.HasPrefix(value, "amqps://")
}

func safeConfigKey(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r == '-' || r == '.' || r == ':' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}
func configEntryKey(s string) bool {
	u := strings.ToUpper(s)
	return u == "PORT" || u == "SERVER.PORT" || u == "ASPNETCORE_URLS" || strings.HasSuffix(u, ":URLS") || strings.HasSuffix(u, ".PORT")
}
func configCapability(s string) (string, bool) {
	u := strings.ToUpper(s)
	switch {
	case strings.Contains(u, "REDIS"):
		return "cache:redis", true
	case strings.Contains(u, "RABBITMQ") || strings.Contains(u, "AMQP"):
		return "messaging:amqp", true
	case strings.Contains(u, "EVENTBUS") || strings.Contains(u, "EVENT_BUS") || strings.Contains(u, "MESSAGEBROKER"):
		return "messaging:event-bus", true
	case strings.Contains(u, "KAFKA"):
		return "messaging:kafka", true
	case strings.Contains(u, "CONNECTIONSTRINGS") || strings.Contains(u, "DATABASE_URL") || strings.Contains(u, "DATASOURCE"):
		return "datastore:relational", true
	case strings.Contains(u, "OIDC") || strings.Contains(u, "OPENID"):
		return "auth:oidc", true
	case strings.Contains(u, "JWT"):
		return "auth:jwt", true
	}
	return "", false
}
