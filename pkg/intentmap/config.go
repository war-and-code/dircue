package intentmap

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

func configCandidate(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	// .env, .env.*, .env.sample, .env.example, .env.production.sample etc.
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	// Spring application.properties / application-*.properties
	if base == "application.properties" || strings.HasPrefix(base, "application-") && strings.HasSuffix(base, ".properties") {
		return true
	}
	// Spring application.yml / application-*.yml
	if base == "application.yml" || base == "application.yaml" || strings.HasPrefix(base, "application-") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
		return true
	}
	// .NET appsettings*.json
	if strings.HasPrefix(base, "appsettings") && strings.HasSuffix(base, ".json") {
		return true
	}
	// Rails config/database.yml, config/cable.yml, config/storage.yml
	lower := strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	switch {
	case strings.HasSuffix(lower, "config/database.yml"),
		strings.HasSuffix(lower, "config/database.yaml"),
		strings.HasSuffix(lower, "config/cable.yml"),
		strings.HasSuffix(lower, "config/cable.yaml"),
		strings.HasSuffix(lower, "config/storage.yml"),
		strings.HasSuffix(lower, "config/storage.yaml"):
		return true
	}
	return false
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
		value := strings.TrimSpace(trimmed[pos+1:])
		// Strip inline comment.
		if hashIdx := strings.Index(value, " #"); hashIdx >= 0 {
			value = strings.TrimSpace(value[:hashIdx])
		}
		// Strip surrounding quotes.
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if cap, ok := configCapability(key); ok {
			out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, StartLine: index + 1, EndLine: index + 1, Properties: map[string]string{"key": key}})
		}
		if configEntryKey(key) {
			out = append(out, Observation{Kind: KindConfig, Name: key, State: "declared", Basis: "declared_config", Path: name, StartLine: index + 1, EndLine: index + 1})
		}
		// Detect connection-string URIs in values (e.g. DATABASE_URL=postgres://...).
		if value != "" {
			if cap, ok := connectionStringCapability(value); ok {
				out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, StartLine: index + 1, EndLine: index + 1, Properties: map[string]string{"key": key}})
			}
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
			if text, ok := v.(string); ok {
				if amqpURI(text) {
					out = append(out, Observation{Kind: KindCapability, Name: "messaging:amqp", State: "declared", Basis: "declared_config", Path: name, Properties: map[string]string{"key": key}})
				} else if cap, ok2 := connectionStringCapability(text); ok2 {
					out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, Properties: map[string]string{"key": key}})
				}
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

// connectionStringCapability returns a capability from a connection-string URI
// scheme. It does not inspect the credentials or host in the value.
func connectionStringCapability(value string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(v, "postgres://") || strings.HasPrefix(v, "postgresql://"):
		return "datastore:relational", true
	case strings.HasPrefix(v, "mysql://"):
		return "datastore:relational", true
	case strings.HasPrefix(v, "mariadb://"):
		return "datastore:relational", true
	case strings.HasPrefix(v, "mongodb://") || strings.HasPrefix(v, "mongodb+srv://"):
		return "datastore:mongodb", true
	case strings.HasPrefix(v, "redis://") || strings.HasPrefix(v, "rediss://"):
		return "cache:redis", true
	case strings.HasPrefix(v, "amqp://") || strings.HasPrefix(v, "amqps://"):
		return "messaging:amqp", true
	case strings.HasPrefix(v, "kafka://"):
		return "messaging:kafka", true
	case strings.HasPrefix(v, "nats://"):
		return "messaging:nats", true
	case strings.HasPrefix(v, "s3://"):
		return "storage:object", true
	case strings.HasPrefix(v, "smtp://") || strings.HasPrefix(v, "smtps://") || strings.HasPrefix(v, "submission://"):
		return "messaging:smtp", true
	}
	return "", false
}

// parseYAMLConfig extracts capability and config-entry observations from a
// YAML config file by building dotted key paths from indented lines. It
// supports common Spring application.yml and Rails config/*.yml patterns
// without importing a full YAML library.
func parseYAMLConfig(name string, content []byte) []Observation {
	var out []Observation
	type stackEntry struct {
		indent int
		key    string
	}
	var stack []stackEntry
	lineNum := 0
	for _, rawLine := range strings.Split(string(content), "\n") {
		lineNum++
		// Skip blank lines and comments.
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Count leading spaces for indent level.
		indent := len(rawLine) - len(strings.TrimLeft(rawLine, " "))
		// Pop stack entries whose indent >= current.
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		// Parse "key: value" or "key:" lines.
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		if !safeConfigKey(key) {
			continue
		}
		value := strings.TrimSpace(trimmed[colonIdx+1:])
		// Strip inline YAML comment from value.
		if hashIdx := strings.Index(value, " #"); hashIdx >= 0 {
			value = strings.TrimSpace(value[:hashIdx])
		}
		// Build the full dotted key from the stack.
		parts := make([]string, 0, len(stack)+1)
		for _, e := range stack {
			parts = append(parts, e.key)
		}
		parts = append(parts, key)
		fullKey := strings.Join(parts, ".")
		// Push onto the stack for nested children.
		stack = append(stack, stackEntry{indent, key})
		// Check the full dotted key against capability catalog.
		if cap, ok := configCapability(fullKey); ok {
			out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum, Properties: map[string]string{"key": fullKey}})
		}
		if configEntryKey(fullKey) {
			out = append(out, Observation{Kind: KindConfig, Name: fullKey, State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum})
		}
		// Also check value as a connection-string URI.
		if value != "" && value != "|" && value != ">" {
			if cap, ok := connectionStringCapability(value); ok {
				out = append(out, Observation{Kind: KindCapability, Name: cap, State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum, Properties: map[string]string{"key": fullKey}})
			}
			if amqpURI(value) {
				out = append(out, Observation{Kind: KindCapability, Name: "messaging:amqp", State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum, Properties: map[string]string{"key": fullKey}})
			}
		}
		// Rails config/database.yml: detect adapter value.
		if key == "adapter" && safeConfigValue(value) {
			if railsAdapterCapability(value) != "" {
				out = append(out, Observation{Kind: KindCapability, Name: railsAdapterCapability(value), State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum, Properties: map[string]string{"key": fullKey, "adapter": value}})
			}
		}
	}
	return out
}

// railsAdapterCapability maps a Rails database adapter name to a capability.
func railsAdapterCapability(adapter string) string {
	switch strings.ToLower(strings.TrimSpace(adapter)) {
	case "postgresql", "postgis", "pg":
		return "datastore:relational"
	case "mysql2", "mysql", "trilogy":
		return "datastore:relational"
	case "sqlite3", "sqlite":
		return "datastore:relational"
	case "oracle_enhanced", "oracle":
		return "datastore:relational"
	case "sqlserver":
		return "datastore:relational"
	}
	return ""
}

// safeConfigValue checks a config value is safe to log (no credentials pattern,
// and not an interpolation placeholder). It allows plain words and simple paths.
func safeConfigValue(s string) bool {
	if len(s) > 128 {
		return false
	}
	// Reject values that look like credentials or interpolations.
	lower := strings.ToLower(s)
	if strings.ContainsAny(s, "$%{}<>") {
		return false
	}
	_ = lower
	return safeConfigKey(s)
}

// prismaDatasourceRe finds a datasource block; prismaProviderRe reads its
// provider. Generator blocks also have a provider field, so the search is
// confined to the datasource block. provider = env(...) forms are not matched:
// the provider is then chosen at runtime.
var (
	prismaDatasourceRe = regexp.MustCompile(`(?m)^\s*datasource\s+\w+\s*\{`)
	prismaProviderRe   = regexp.MustCompile(`(?m)^\s*provider\s*=\s*"(\w+)"`)
)

// parsePrismaSchema extracts a datastore capability from a Prisma schema file
// by reading the datasource block's provider field. Prisma allows one
// datasource per schema.
func parsePrismaSchema(name string, content []byte) []Observation {
	start := prismaDatasourceRe.FindIndex(content)
	if start == nil {
		return nil
	}
	block := content[start[1]:]
	if end := bytes.IndexByte(block, '}'); end >= 0 {
		block = block[:end]
	}
	m := prismaProviderRe.FindSubmatchIndex(block)
	if m == nil {
		return nil
	}
	provider := strings.ToLower(string(block[m[2]:m[3]]))
	capability := prismaProviderCapability(provider)
	if capability == "" {
		return nil
	}
	lineNum := 1 + bytes.Count(content[:start[1]+m[0]], []byte("\n"))
	return []Observation{{Kind: KindCapability, Name: capability, State: "declared", Basis: "declared_config", Path: name, StartLine: lineNum, EndLine: lineNum, Properties: map[string]string{"provider": provider}}}
}

// prismaProviderCapability maps a Prisma datasource provider name to a
// capability ID. Unknown providers return "".
func prismaProviderCapability(provider string) string {
	switch provider {
	case "postgresql":
		return "datastore:postgresql"
	case "mysql", "mariadb":
		return "datastore:mysql"
	case "mongodb":
		return "datastore:mongodb"
	case "sqlite", "sqlserver", "cockroachdb":
		return "datastore:relational"
	}
	return ""
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
	// Match only keys that declare the application's own listener address or
	// port. Datastore connection ports (DB_PORT, test.replica.port, etc.) must
	// never match — they are capability evidence, not interface declarations.
	switch u {
	case "PORT", "HTTP_PORT", "HTTPS_PORT",
		"LISTEN_ADDR", "LISTEN_PORT",
		"SERVER.PORT",                                                        // Spring application.yml / application.properties
		"ASPNETCORE_URLS", "ASPNETCORE_HTTP_PORTS", "ASPNETCORE_HTTPS_PORTS": // ASP.NET Core
		return true
	}
	// KESTREL:ENDPOINTS:*:URL and similar ASP.NET Core hierarchy forms.
	return strings.HasSuffix(u, ":URL") && strings.HasPrefix(u, "KESTREL:")
}
func configCapability(s string) (string, bool) {
	u := strings.ToUpper(s)
	switch {
	// Cache
	case strings.Contains(u, "REDIS"):
		return "cache:redis", true

	// Messaging - AMQP / RabbitMQ
	case strings.Contains(u, "RABBITMQ") || strings.Contains(u, "AMQP"):
		return "messaging:amqp", true

	// Messaging - event bus
	case strings.Contains(u, "EVENTBUS") || strings.Contains(u, "EVENT_BUS") || strings.Contains(u, "MESSAGEBROKER"):
		return "messaging:event-bus", true

	// Messaging - Kafka
	case strings.Contains(u, "KAFKA"):
		return "messaging:kafka", true

	// Messaging - NATS
	case strings.Contains(u, "NATS_URL") || strings.Contains(u, "NATS_URI") || strings.Contains(u, "NATS_HOST") || strings.Contains(u, "NATS_SERVER"):
		return "messaging:nats", true

	// Messaging - SMTP / e-mail (specific keys only; never generic HOST)
	case strings.Contains(u, "SMTP_SERVER") || strings.Contains(u, "SMTP_HOST") ||
		strings.Contains(u, "SMTP_PORT") || strings.Contains(u, "SMTP_LOGIN") ||
		strings.Contains(u, "MAIL_SERVER") || strings.Contains(u, "MAIL_HOST") ||
		strings.Contains(u, "MAILGUN") || strings.Contains(u, "SENDGRID") ||
		strings.Contains(u, "POSTMARK") || strings.Contains(u, "MAILCHIMP"):
		return "messaging:smtp", true

	// Datastore - relational databases
	case strings.Contains(u, "CONNECTIONSTRINGS") || strings.Contains(u, "DATABASE_URL") ||
		strings.Contains(u, "DATASOURCE") || strings.Contains(u, "SPRING.DATASOURCE") ||
		strings.Contains(u, "DB_HOST") || strings.Contains(u, "DB_NAME") ||
		strings.Contains(u, "DB_USER") || strings.Contains(u, "DB_PASS") ||
		strings.Contains(u, "DB_PORT") || strings.Contains(u, "DB_DATABASE") ||
		strings.Contains(u, "PGHOST") || strings.Contains(u, "PGDATABASE") ||
		strings.Contains(u, "POSTGRES_DB") || strings.Contains(u, "POSTGRES_HOST") ||
		strings.Contains(u, "POSTGRES_URL") || strings.Contains(u, "MYSQL_HOST") ||
		strings.Contains(u, "MYSQL_DATABASE") || strings.Contains(u, "MARIADB_HOST"):
		return "datastore:relational", true

	// Datastore - MongoDB (specific enough keys only)
	case strings.Contains(u, "MONGODB_URI") || strings.Contains(u, "MONGODB_URL") ||
		strings.Contains(u, "MONGODB_HOST") || strings.Contains(u, "MONGO_URI") ||
		strings.Contains(u, "MONGO_URL") || strings.Contains(u, "MONGO_HOST") ||
		strings.Contains(u, "MONGO_DATABASE"):
		return "datastore:mongodb", true

	// Storage - object storage
	case strings.Contains(u, "S3_ENABLED") || strings.Contains(u, "S3_BUCKET") ||
		strings.Contains(u, "S3_HOSTNAME") || strings.Contains(u, "S3_HOST") ||
		strings.Contains(u, "S3_ENDPOINT") || strings.Contains(u, "AWS_S3_BUCKET") ||
		strings.Contains(u, "MINIO_BUCKET") || strings.Contains(u, "MINIO_ENDPOINT") ||
		strings.Contains(u, "OBJECT_STORAGE_BUCKET") || strings.Contains(u, "CLOUDFRONT_DISTRIBUTION"):
		return "storage:object", true

	// Search - Elasticsearch / OpenSearch
	case strings.Contains(u, "ES_HOST") || strings.Contains(u, "ES_URL") ||
		strings.Contains(u, "ELASTICSEARCH_HOST") || strings.Contains(u, "ELASTICSEARCH_URL") ||
		strings.Contains(u, "OPENSEARCH_HOST") || strings.Contains(u, "OPENSEARCH_URL") ||
		strings.Contains(u, "ELASTIC_HOST") || strings.Contains(u, "ELASTIC_URL"):
		return "search:elasticsearch", true

	// Auth - OIDC
	case strings.Contains(u, "OIDC") || strings.Contains(u, "OPENID"):
		return "auth:oidc", true

	// Auth - JWT
	case strings.Contains(u, "JWT"):
		return "auth:jwt", true
	}
	return "", false
}
