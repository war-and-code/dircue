package intentmap

import (
	"strings"
)

// capabilitiesFor returns the capability names matched for a declared
// requirement or source import. kind is the Requirement.Kind from the
// declarations package (e.g. "npm-dependency", "go-require",
// "python-dependency") or a synthetic kind used by file parsers
// ("go-import" for Go import paths, "python-import" for Python import names).
//
// Matching strategy by ecosystem:
//
//   - npm-dependency: exact package-name match (case-sensitive; npm names are
//     lowercase by convention). Scoped packages like @aws-sdk/* use prefix
//     matching on the scope prefix.
//   - go-import, go-require: prefix match on module path (after stripping the
//     "module-path version" format of go-require). Matches any sub-path
//     separated by "/" including major-version suffixes like /v9.
//   - python-dependency, python-build-requirement: exact match on PEP 503
//     canonical name (lowercase, runs of [-_.] replaced with "-") after
//     stripping version specifiers.
//   - python-import: exact match on top-level import name as used in source.
//   - ruby-gem-dependency: exact gem name match.
//   - cargo-dependency, cargo-workspace-dependency: exact crate name match.
//   - php-dependency: exact vendor/package match (lowercase).
//   - package-reference (NuGet): case-insensitive exact name match after
//     stripping the @version suffix.
//   - maven-dependency, gradle-dependency: exact group:artifact match (after
//     stripping the optional :version field). group prefix matching for common
//     namespaces such as org.springframework.boot: and software.amazon.awssdk:.
func capabilitiesFor(kind, value string) []string {
	lower := strings.ToLower(value)
	pkg := extractPackageName(kind, lower)

	seen := map[string]bool{}
	var out []string
	add := func(cap string) {
		if !seen[cap] {
			seen[cap] = true
			out = append(out, cap)
		}
	}

	// --- 1. Ecosystem-specific exact matching ---
	if m, ok := exactByKind[kind]; ok {
		if caps, ok2 := m[pkg]; ok2 {
			for _, c := range caps {
				add(c)
			}
		}
	}

	// --- Code generation declarations ---
	// A .NET <Protobuf> item or a Maven protobuf plugin generates protobuf
	// code; the value names the generator rather than a package.
	if kind == "code-generation" && strings.Contains(pkg, "protobuf") {
		add("serialization:protobuf")
	}

	// --- 2. Prefix matching for path-based ecosystems ---
	// Applied when the kind is prefix-matched (Go, Maven, npm scopes, etc.)
	// or when kind is empty/unknown (legacy caller).
	if prefixMatchKind(kind) {
		for _, e := range prefixEntries {
			if coordinateMatch(pkg, e.prefix) {
				add(e.capability)
			}
		}
	}

	return out
}

// prefixMatchKind reports whether prefix matching should be applied.
func prefixMatchKind(kind string) bool {
	switch kind {
	case "go-import", "go-require",
		"maven-dependency", "gradle-dependency",
		"npm-dependency": // scoped packages like @aws-sdk/
		return true
	case "": // legacy: Go import paths and Python import names via old callers
		return true
	}
	return false
}

// extractPackageName returns the lowercased, version-stripped package name
// from a raw requirement value. The returned string is always lowercase.
func extractPackageName(kind, lower string) string {
	switch kind {
	case "npm-dependency", "npm-workspace-dependency":
		// The npm parser appends "@version-range" to the value, e.g.
		// "ioredis@^5.3.2" or "@aws-sdk/client-s3@^3.400.0".
		// For scoped packages the name begins with "@", so strip only
		// the LAST "@" that follows the package name.
		if strings.HasPrefix(lower, "@") {
			// Scoped: @scope/name[@version] — find "@" after the first "/"
			if slash := strings.IndexByte(lower, '/'); slash > 0 {
				if at := strings.LastIndexByte(lower[slash:], '@'); at > 0 {
					lower = lower[:slash+at]
				}
			}
		} else {
			// Regular: name[@version] — strip at first "@"
			if i := strings.IndexByte(lower, '@'); i > 0 {
				lower = lower[:i]
			}
		}
	case "python-dependency", "python-build-requirement":
		// Strip at first non-name character. Python names: [A-Za-z0-9._-]
		if i := strings.IndexAny(lower, "[<>=!~(@ ;"); i > 0 {
			lower = lower[:i]
		}
		// PEP 503 canonical: replace runs of [-_.] with "-"
		lower = normalizePyPI(lower)
	case "go-require":
		// goModuleVersion formats as "module/path@vX.Y.Z".
		// Strip the @version suffix; strip at space too for any alternate form.
		if i := strings.IndexByte(lower, '@'); i > 0 {
			lower = lower[:i]
		} else if i := strings.IndexByte(lower, ' '); i > 0 {
			lower = lower[:i]
		}
	case "cargo-dependency", "cargo-workspace-dependency":
		// cargo_resolve sets Value to "name version" and Condition to the scope.
		// Strip the version (space-separated) and any "; source=..." suffix.
		if i := strings.IndexByte(lower, ' '); i > 0 {
			lower = lower[:i]
		}
		if i := strings.IndexByte(lower, ';'); i > 0 {
			lower = strings.TrimSpace(lower[:i])
		}
	case "package-reference":
		// NuGet: "PackageName@version" or just "PackageName"
		if i := strings.IndexByte(lower, '@'); i > 0 {
			lower = lower[:i]
		}
	case "maven-dependency", "gradle-dependency":
		// "group:artifact:version" — keep only "group:artifact"
		parts := strings.SplitN(lower, ":", 3)
		if len(parts) >= 2 {
			lower = parts[0] + ":" + parts[1]
		}
		// ruby-gem-dependency, php-dependency,
		// go-import, python-import: value is already the package name.
	}
	return lower
}

// normalizePyPI applies PEP 503 normalization: lowercase (already done by
// caller) and replace runs of [-_.] with a single "-".
func normalizePyPI(s string) string {
	if !strings.ContainsAny(s, "._") {
		return s // already canonical (only hyphens)
	}
	var b strings.Builder
	b.Grow(len(s))
	prev := false
	for _, r := range s {
		isSep := r == '-' || r == '_' || r == '.'
		if isSep {
			if !prev {
				b.WriteByte('-')
			}
			prev = true
		} else {
			b.WriteRune(r)
			prev = false
		}
	}
	return b.String()
}

// coordinateMatch checks whether value matches the given coordinate prefix
// with an appropriate separator (/ = @ > < ! ~ [ ; space). This is the
// standard prefix match used for Go module paths, Maven group prefixes, and
// npm scoped packages.
func coordinateMatch(value, coordinate string) bool {
	if !strings.HasPrefix(value, coordinate) {
		return false
	}
	if len(value) == len(coordinate) {
		return true
	}
	switch value[len(coordinate)] {
	case '/', ' ', '@', '>', '<', '=', '!', '~', '[', ';', ':':
		return true
	default:
		return false
	}
}

// prefixEntry is a (prefix, capability) pair used for path-based prefix
// matching, e.g. Go module paths and Maven group:artifact prefixes.
type prefixEntry struct {
	prefix     string
	capability string
}

// prefixEntries contains prefix-matched coordinates. Each prefix is lowercased.
// Go module paths: matches "prefix/..." and "prefix" exactly.
// Maven: matches "group:artifact" exactly or "group:" prefix for a whole namespace.
// npm: matches "@scope/" prefix for scoped packages.
var prefixEntries = []prefixEntry{
	// Go — relational databases
	{"github.com/jackc/pgx", "datastore:postgresql"},
	{"github.com/lib/pq", "datastore:postgresql"},
	{"github.com/go-pg/pg", "datastore:postgresql"},
	{"github.com/uptrace/bun", "datastore:relational"},
	{"github.com/go-sql-driver/mysql", "datastore:mysql"},
	{"github.com/go-gorm/gorm", "datastore:relational"},
	{"gorm.io/gorm", "datastore:relational"},
	{"gorm.io/driver/postgres", "datastore:postgresql"},
	{"gorm.io/driver/mysql", "datastore:mysql"},
	{"github.com/jmoiron/sqlx", "datastore:relational"},
	// Go — NoSQL
	{"go.mongodb.org/mongo-driver", "datastore:mongodb"},
	// Go — cache
	{"github.com/redis/go-redis", "cache:redis"},
	{"github.com/go-redis/redis", "cache:redis"},
	{"github.com/gomodule/redigo", "cache:redis"},
	// Go — messaging
	{"github.com/segmentio/kafka-go", "messaging:kafka"},
	{"github.com/ibm/sarama", "messaging:kafka"},
	{"github.com/shopify/sarama", "messaging:kafka"},
	{"github.com/confluentinc/confluent-kafka-go", "messaging:kafka"},
	{"github.com/rabbitmq/amqp091-go", "messaging:amqp"},
	{"github.com/streadway/amqp", "messaging:amqp"},
	{"github.com/nats-io/nats.go", "messaging:nats"},
	{"github.com/nats-io/nats-server", "messaging:nats"},
	{"github.com/twmb/franz-go", "messaging:kafka"},
	// Go — auth
	{"github.com/golang-jwt/jwt", "auth:jwt"},
	{"github.com/dgrijalva/jwt-go", "auth:jwt"},
	{"github.com/lestrrat-go/jwx", "auth:jwt"},
	{"github.com/coreos/go-oidc", "auth:oidc"},
	{"github.com/coreos/go-oidc/v3", "auth:oidc"},
	{"golang.org/x/oauth2", "auth:oauth2"},
	// Go — crypto
	{"golang.org/x/crypto", "crypto:library"},
	// Go — search
	{"github.com/elastic/go-elasticsearch", "search:elasticsearch"},
	{"github.com/opensearch-project/opensearch-go", "search:elasticsearch"},
	// Go — cloud
	{"github.com/aws/aws-sdk-go", "cloud:aws"},
	{"github.com/aws/aws-sdk-go-v2", "cloud:aws"},
	{"github.com/aws/aws-sdk-go-v2/service/s3", "storage:object"},
	{"github.com/minio/minio-go", "storage:object"},
	{"google.golang.org/grpc", "net:http-client"},
	{"cloud.google.com/go", "cloud:gcp"},
	{"firebase.google.com/go", "cloud:gcp"},
	{"github.com/azure/azure-sdk-for-go", "cloud:azure"},
	// Go — HTTP / SMTP
	{"net/http", "net:http-client"},
	{"github.com/go-resty/resty", "net:http-client"},
	// Go — serialization
	{"google.golang.org/protobuf", "serialization:protobuf"},
	{"github.com/golang/protobuf", "serialization:protobuf"},
	{"gopkg.in/yaml.v2", "serialization:yaml"},
	{"gopkg.in/yaml.v3", "serialization:yaml"},
	{"go.yaml.in/yaml/v3", "serialization:yaml"},
	// Go — AI
	{"github.com/anthropics/anthropic-sdk-go", "ai:llm-sdk"},
	{"github.com/sashabaranov/go-openai", "ai:llm-sdk"},
	// Go — SMTP
	{"github.com/go-mail/mail", "messaging:smtp"},
	{"gopkg.in/gomail.v2", "messaging:smtp"},
	{"github.com/wneessen/go-mail", "messaging:smtp"},

	// Maven / Gradle — prefix matching uses coordinateMatch which checks that
	// the character following the prefix is a valid separator (/ : @ space etc.).
	// Use "group" or "group:artifact" WITHOUT a trailing colon or slash.
	//
	// Relational
	{"org.postgresql:postgresql", "datastore:postgresql"},
	{"com.mysql:mysql-connector-j", "datastore:mysql"},
	{"mysql:mysql-connector-java", "datastore:mysql"},
	{"com.microsoft.sqlserver:mssql-jdbc", "datastore:relational"},
	{"com.oracle.database.jdbc:ojdbc", "datastore:relational"},
	{"com.h2database:h2", "datastore:relational"},
	{"org.hsqldb:hsqldb", "datastore:relational"},
	{"com.zaxxer:hikaricp", "datastore:relational"},
	{"org.apache.commons:commons-dbcp", "datastore:relational"},
	// ORM / JPA — prefix on group name (separator will be ":")
	{"org.hibernate", "datastore:relational"},
	{"org.hibernate.orm", "datastore:relational"},
	{"javax.persistence:javax.persistence-api", "datastore:relational"},
	{"jakarta.persistence:jakarta.persistence-api", "datastore:relational"},
	{"org.springframework.data:spring-data-jpa", "datastore:relational"},
	{"org.springframework.boot:spring-boot-starter-data-jpa", "datastore:relational"},
	{"org.springframework.boot:spring-boot-starter-jdbc", "datastore:relational"},
	{"org.mybatis", "datastore:relational"},
	// Cache
	{"redis.clients:jedis", "cache:redis"},
	{"org.redisson:redisson", "cache:redis"},
	{"io.lettuce:lettuce-core", "cache:redis"},
	{"org.springframework.data:spring-data-redis", "cache:redis"},
	{"org.springframework.boot:spring-boot-starter-data-redis", "cache:redis"},
	// MongoDB
	{"org.mongodb:mongodb-driver", "datastore:mongodb"},
	{"org.mongodb:mongodb-driver-sync", "datastore:mongodb"},
	{"org.mongodb:mongodb-driver-reactivestreams", "datastore:mongodb"},
	{"org.springframework.data:spring-data-mongodb", "datastore:mongodb"},
	{"org.springframework.boot:spring-boot-starter-data-mongodb", "datastore:mongodb"},
	// Elasticsearch
	{"org.elasticsearch.client", "search:elasticsearch"},
	{"co.elastic.clients:elasticsearch-java", "search:elasticsearch"},
	{"org.springframework.data:spring-data-elasticsearch", "search:elasticsearch"},
	{"org.springframework.boot:spring-boot-starter-data-elasticsearch", "search:elasticsearch"},
	// Kafka
	{"org.apache.kafka:kafka-clients", "messaging:kafka"},
	{"org.apache.kafka:kafka-streams", "messaging:kafka"},
	{"org.springframework.kafka:spring-kafka", "messaging:kafka"},
	{"org.springframework.boot:spring-boot-starter-activemq", "messaging:amqp"},
	// AMQP / RabbitMQ
	{"org.springframework.boot:spring-boot-starter-amqp", "messaging:amqp"},
	{"com.rabbitmq:amqp-client", "messaging:amqp"},
	// Auth — spring security
	{"org.springframework.boot:spring-boot-starter-security", "auth:oauth2"},
	{"org.springframework.boot:spring-boot-starter-oauth2-client", "auth:oauth2"},
	{"org.springframework.boot:spring-boot-starter-oauth2-resource-server", "auth:oauth2"},
	{"org.springframework.security", "auth:oauth2"},
	{"io.jsonwebtoken:jjwt", "auth:jwt"},
	{"com.auth0:java-jwt", "auth:jwt"},
	// Cloud / AWS — "software.amazon.awssdk" prefix (separator is ":")
	{"software.amazon.awssdk:s3", "storage:object"},
	{"software.amazon.awssdk", "cloud:aws"},
	{"com.amazonaws:aws-java-sdk-s3", "storage:object"},
	{"com.amazonaws", "cloud:aws"},
	// Cloud / Azure — "com.azure" prefix
	{"com.azure:azure-storage-blob", "storage:object"},
	{"com.azure", "cloud:azure"},
	// Cloud / GCP — "com.google.cloud" prefix
	{"com.google.cloud", "cloud:gcp"},
	// SMTP
	{"org.springframework.boot:spring-boot-starter-mail", "messaging:smtp"},
	{"com.sun.mail:jakarta.mail", "messaging:smtp"},
	{"org.simplejavamail:simple-java-mail", "messaging:smtp"},
	// Directory / LDAP — client libraries and embedded LDAP servers
	{"org.springframework.ldap", "directory:ldap"},
	{"org.springframework.boot:spring-boot-starter-data-ldap", "directory:ldap"},
	{"com.unboundid:unboundid-ldapsdk", "directory:ldap"},
	{"org.apache.directory.server", "directory:ldap"},
	{"org.apache.directory.client", "directory:ldap"},
	{"com.novell.ldap:jldap", "directory:ldap"},

	// npm scoped prefixes — coordinateMatch with separator "/".
	// Use the scope without trailing slash: "@aws-sdk" matches "@aws-sdk/client-s3"
	// because the character after the prefix is "/".
	{"@aws-sdk", "cloud:aws"},
	{"@elastic/elasticsearch", "search:elasticsearch"},
	{"@azure", "cloud:azure"},
	{"@google-cloud", "cloud:gcp"},

	// Python import names (for parsePythonImports path, via python-import kind)
	// Kept here so prefix matching on import dotted paths works, e.g.
	// "psycopg2.pool" → top-level "psycopg2" is already extracted upstream.
}

// exactByKind maps requirementKind → (lowercased-package-name → capabilities).
// Exact string matching is used after version stripping by extractPackageName.
var exactByKind = map[string]map[string][]string{
	"npm-dependency":             npmExact,
	"ruby-gem-dependency":        rubyExact,
	"cargo-dependency":           cargoExact,
	"cargo-workspace-dependency": cargoExact,
	"php-dependency":             phpExact,
	"package-reference":          nugetExact,
	"python-dependency":          pypiExact,
	"python-build-requirement":   pypiExact,
	"python-import":              pythonImportExact,
	// Dart / pub
	"dart-dependency": pubExact,
}

// npmExact: npm package name (exact, lowercased) → capabilities.
var npmExact = map[string][]string{
	// Relational DB drivers
	"pg":             {"datastore:postgresql"},
	"pg-promise":     {"datastore:postgresql"},
	"pg-native":      {"datastore:postgresql"},
	"postgres":       {"datastore:postgresql"},
	"mysql2":         {"datastore:mysql"},
	"mysql":          {"datastore:mysql"},
	"mariadb":        {"datastore:mysql"},
	"oracledb":       {"datastore:relational"},
	"better-sqlite3": {"datastore:relational"},
	"sqlite3":        {"datastore:relational"},
	"mssql":          {"datastore:relational"},
	"tedious":        {"datastore:relational"},
	// ORMs / query builders (multi-DB)
	"prisma":         {"datastore:relational"},
	"@prisma/client": {"datastore:relational"},
	"typeorm":        {"datastore:relational"},
	"sequelize":      {"datastore:relational"},
	"knex":           {"datastore:relational"},
	"objection":      {"datastore:relational"},
	"bookshelf":      {"datastore:relational"},
	"mikro-orm":      {"datastore:relational"},
	"drizzle-orm":    {"datastore:relational"},
	// Cache / Redis
	"ioredis":       {"cache:redis"},
	"redis":         {"cache:redis"},
	"@redis/client": {"cache:redis"},
	"node_redis":    {"cache:redis"},
	"connect-redis": {"cache:redis"},
	// MongoDB
	"mongodb":  {"datastore:mongodb"},
	"mongoose": {"datastore:mongodb"},
	"monk":     {"datastore:mongodb"},
	// Elasticsearch
	"@elastic/elasticsearch":         {"search:elasticsearch"},
	"@opensearch-project/opensearch": {"search:elasticsearch"},
	// Kafka
	"kafkajs":                        {"messaging:kafka"},
	"kafka-node":                     {"messaging:kafka"},
	"node-kafka":                     {"messaging:kafka"},
	"@confluentinc/kafka-javascript": {"messaging:kafka"},
	"@confluentinc/schemaregistry":   {"messaging:kafka"},
	// AMQP / RabbitMQ
	"amqplib":                 {"messaging:amqp"},
	"amqp-connection-manager": {"messaging:amqp"},
	"rhea":                    {"messaging:amqp"},
	// NATS
	"nats": {"messaging:nats"},
	// SMTP / email
	"nodemailer":     {"messaging:smtp"},
	"@sendgrid/mail": {"messaging:smtp"},
	"mailgun-js":     {"messaging:smtp"},
	"postmark":       {"messaging:smtp"},
	// Object storage
	"minio":              {"storage:object"},
	"@aws-sdk/client-s3": {"storage:object"},
	// Auth
	"jsonwebtoken":    {"auth:jwt"},
	"jose":            {"auth:jwt"},
	"passport":        {"auth:oauth2"},
	"passport-local":  {"auth:oauth2"},
	"passport-jwt":    {"auth:oauth2"},
	"passport-oauth2": {"auth:oauth2"},
	"openid-client":   {"auth:oidc"},
	// HTTP clients
	"axios":      {"net:http-client"},
	"node-fetch": {"net:http-client"},
	"got":        {"net:http-client"},
	"superagent": {"net:http-client"},
	"undici":     {"net:http-client"},
	// AI
	"openai":            {"ai:llm-sdk"},
	"@anthropic-ai/sdk": {"ai:llm-sdk"},
	"langchain":         {"ai:llm-sdk"},
	// Serialization
	"protobufjs":         {"serialization:protobuf"},
	"@bufbuild/protobuf": {"serialization:protobuf"},
	"js-yaml":            {"serialization:yaml"},
	"yaml":               {"serialization:yaml"},
	// Directory / LDAP
	"ldapjs": {"directory:ldap"},
	"ldapts": {"directory:ldap"},
}

// rubyExact: gem name (exact, lowercased) → capabilities.
var rubyExact = map[string][]string{
	// Relational DB drivers
	"pg":           {"datastore:postgresql"},
	"mysql2":       {"datastore:mysql"},
	"trilogy":      {"datastore:mysql"},
	"sqlite3":      {"datastore:relational"},
	"activerecord": {"datastore:relational"},
	// ORMs
	"sequel":       {"datastore:relational"},
	"rom":          {"datastore:relational"},
	"hanami-model": {"datastore:relational"},
	// Cache / Redis
	"redis":   {"cache:redis"},
	"hiredis": {"cache:redis"},
	// MongoDB
	"mongo":   {"datastore:mongodb"},
	"mongoid": {"datastore:mongodb"},
	"bson":    {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch": {"search:elasticsearch"},
	"chewy":         {"search:elasticsearch"},
	"searchkick":    {"search:elasticsearch"},
	// Kafka
	"ruby-kafka": {"messaging:kafka"},
	"kafka":      {"messaging:kafka"},
	"rdkafka":    {"messaging:kafka"},
	"karafka":    {"messaging:kafka"},
	// AMQP / RabbitMQ
	"bunny":      {"messaging:amqp"},
	"march_hare": {"messaging:amqp"},
	// SMTP / email
	"mail":          {"messaging:smtp"},
	"sendgrid-ruby": {"messaging:smtp"},
	"mailgun-ruby":  {"messaging:smtp"},
	// Object storage / cloud
	"aws-sdk-s3":           {"storage:object", "cloud:aws"},
	"aws-sdk-core":         {"cloud:aws"},
	"aws-sdk":              {"cloud:aws"},
	"fog-aws":              {"cloud:aws"},
	"google-cloud-storage": {"storage:object", "cloud:gcp"},
	"azure-storage":        {"storage:object", "cloud:azure"},
	// Auth
	"jwt":                     {"auth:jwt"},
	"ruby-jwt":                {"auth:jwt"},
	"devise":                  {"auth:oauth2"},
	"omniauth":                {"auth:oauth2"},
	"omniauth_openid_connect": {"auth:oidc"},
	"doorkeeper":              {"auth:oauth2"},
	"sorcery":                 {"auth:oauth2"},
	"warden":                  {"auth:oauth2"},
	// HTTP clients
	"faraday":     {"net:http-client"},
	"httparty":    {"net:http-client"},
	"rest-client": {"net:http-client"},
	"typhoeus":    {"net:http-client"},
	"http":        {"net:http-client"},
	"excon":       {"net:http-client"},
	// Crypto
	"bcrypt":  {"crypto:library"},
	"openssl": {"crypto:library"},
	// AI
	"ruby-openai": {"ai:llm-sdk"},
}

// cargoExact: crate name (exact, lowercased) → capabilities.
var cargoExact = map[string][]string{
	// Relational DB
	"sqlx":           {"datastore:relational"},
	"diesel":         {"datastore:relational"},
	"sea-orm":        {"datastore:relational"},
	"sea-query":      {"datastore:relational"},
	"tokio-postgres": {"datastore:postgresql"},
	"postgres":       {"datastore:postgresql"},
	"mysql":          {"datastore:mysql"},
	"mysql_async":    {"datastore:mysql"},
	"rusqlite":       {"datastore:relational"},
	"libsqlite3-sys": {"datastore:relational"},
	"r2d2":           {"datastore:relational"},
	"bb8":            {"datastore:relational"},
	// Cache / Redis
	"redis":          {"cache:redis"},
	"deadpool-redis": {"cache:redis"},
	"fred":           {"cache:redis"},
	// MongoDB
	"mongodb": {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch": {"search:elasticsearch"},
	"opensearch":    {"search:elasticsearch"},
	// Kafka
	"rdkafka": {"messaging:kafka"},
	"kafka":   {"messaging:kafka"},
	// AMQP
	"lapin": {"messaging:amqp"},
	"amqp":  {"messaging:amqp"},
	// NATS
	"nats":       {"messaging:nats"},
	"async-nats": {"messaging:nats"},
	// SMTP
	"lettre": {"messaging:smtp"},
	// Object storage / cloud
	"aws-sdk-s3":           {"storage:object", "cloud:aws"},
	"aws-config":           {"cloud:aws"},
	"aws-credential-types": {"cloud:aws"},
	"azure_core":           {"cloud:azure"},
	"azure_storage":        {"storage:object", "cloud:azure"},
	"google-cloud-storage": {"storage:object", "cloud:gcp"},
	// Auth
	"jsonwebtoken":  {"auth:jwt"},
	"jwt":           {"auth:jwt"},
	"pasetors":      {"auth:jwt"},
	"oauth2":        {"auth:oauth2"},
	"openidconnect": {"auth:oidc"},
	// HTTP
	"reqwest": {"net:http-client"},
	"hyper":   {"net:http-client"},
	"ureq":    {"net:http-client"},
	"isahc":   {"net:http-client"},
	// Crypto
	"ring":    {"crypto:library"},
	"rustls":  {"crypto:library"},
	"openssl": {"crypto:library"},
	// Serialization
	"prost":      {"serialization:protobuf"},
	"protobuf":   {"serialization:protobuf"},
	"serde_yaml": {"serialization:yaml"},
	// AI
	"async-openai": {"ai:llm-sdk"},
}

// phpExact: composer vendor/package (exact, lowercased) → capabilities.
var phpExact = map[string][]string{
	// Relational DB / ORM
	"doctrine/dbal":       {"datastore:relational"},
	"doctrine/orm":        {"datastore:relational"},
	"illuminate/database": {"datastore:relational"},
	"laravel/framework":   {"datastore:relational"},
	"cakephp/database":    {"datastore:relational"},
	"cycle/orm":           {"datastore:relational"},
	// Cache / Redis
	"predis/predis":     {"cache:redis"},
	"phpredis/phpredis": {"cache:redis"},
	// MongoDB
	"mongodb/mongodb":      {"datastore:mongodb"},
	"doctrine/mongodb-odm": {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch/elasticsearch": {"search:elasticsearch"},
	"ruflin/elastica":             {"search:elasticsearch"},
	// Kafka
	"arnaud-lb/php-rdkafka": {"messaging:kafka"},
	"longlang/phpkafka":     {"messaging:kafka"},
	// AMQP
	"php-amqplib/php-amqplib": {"messaging:amqp"},
	"enqueue/amqp-lib":        {"messaging:amqp"},
	// SMTP / email
	"swiftmailer/swiftmailer": {"messaging:smtp"},
	"symfony/mailer":          {"messaging:smtp"},
	"sendgrid/sendgrid":       {"messaging:smtp"},
	"mailgun/mailgun-php":     {"messaging:smtp"},
	// Object storage / cloud
	"aws/aws-sdk-php":            {"storage:object", "cloud:aws"},
	"league/flysystem-aws-s3-v3": {"storage:object", "cloud:aws"},
	"google/cloud-storage":       {"storage:object", "cloud:gcp"},
	// Auth
	"firebase/php-jwt":        {"auth:jwt"},
	"lcobucci/jwt":            {"auth:jwt"},
	"web-token/jwt-framework": {"auth:jwt"},
	"league/oauth2-server":    {"auth:oauth2"},
	"league/oauth2-client":    {"auth:oauth2"},
	"symfony/security-bundle": {"auth:oauth2"},
	// HTTP
	"guzzlehttp/guzzle":   {"net:http-client"},
	"symfony/http-client": {"net:http-client"},
	// Crypto
	"paragonie/sodium_compat": {"crypto:library"},
	// Serialization
	"google/protobuf": {"serialization:protobuf"},
	// AI
	"openai-php/client": {"ai:llm-sdk"},
}

// nugetExact: NuGet package name (lowercased) → capabilities.
var nugetExact = map[string][]string{
	// Relational DB
	"npgsql":                                {"datastore:postgresql"},
	"npgsql.entityframeworkcore.postgresql": {"datastore:postgresql"},
	"mysql.data":                            {"datastore:mysql"},
	"mysqlconnector":                        {"datastore:mysql"},
	"pomelo.entityframeworkcore.mysql":      {"datastore:mysql"},
	"microsoft.data.sqlclient":              {"datastore:relational"},
	"system.data.sqlclient":                 {"datastore:relational"},
	"oracle.entityframeworkcore":            {"datastore:relational"},
	"oracle.manageddataaccess":              {"datastore:relational"},
	"microsoft.entityframeworkcore":         {"datastore:relational"},
	"dapper":                                {"datastore:relational"},
	"repodb":                                {"datastore:relational"},
	// Cache / Redis
	"stackexchange.redis":                             {"cache:redis"},
	"microsoft.extensions.caching.stackexchangeredis": {"cache:redis"},
	"servicestack.redis":                              {"cache:redis"},
	// MongoDB
	"mongodb.driver": {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch.net":             {"search:elasticsearch"},
	"elastic.clients.elasticsearch": {"search:elasticsearch"},
	"nest":                          {"search:elasticsearch"},
	"opensearch.net":                {"search:elasticsearch"},
	// Kafka
	"confluent.kafka": {"messaging:kafka"},
	"kafka.client":    {"messaging:kafka"},
	// AMQP / RabbitMQ
	"rabbitmq.client":             {"messaging:amqp"},
	"masstransit":                 {"messaging:amqp"},
	"masstransit.rabbitmq":        {"messaging:amqp"},
	"masstransit.azureservicebus": {"messaging:event-bus"},
	"masstransit.amazonsqs":       {"messaging:event-bus"},
	"nservicebus":                 {"messaging:event-bus"},
	"azure.messaging.servicebus":  {"messaging:event-bus"},
	"azure.messaging.eventhubs":   {"messaging:event-bus"},
	// SMTP / email
	"mailkit":  {"messaging:smtp"},
	"sendgrid": {"messaging:smtp"},
	// Object storage / cloud
	"awssdk.s3":               {"storage:object", "cloud:aws"},
	"awssdk.core":             {"cloud:aws"},
	"azure.storage.blobs":     {"storage:object", "cloud:azure"},
	"azure.storage.queues":    {"cloud:azure"},
	"azure.identity":          {"cloud:azure"},
	"microsoft.azure.storage": {"storage:object", "cloud:azure"},
	"google.cloud.storage":    {"storage:object", "cloud:gcp"},
	"google.apis.storage.v1":  {"storage:object", "cloud:gcp"},
	// Auth
	"system.identitymodel.tokens.jwt":                   {"auth:jwt"},
	"microsoft.aspnetcore.authentication.jwtbearer":     {"auth:jwt"},
	"microsoft.identitymodel.tokens":                    {"auth:jwt"},
	"microsoft.aspnetcore.authentication.openidconnect": {"auth:oidc"},
	"microsoft.aspnetcore.authentication":               {"auth:oauth2"},
	"aspnetcore.authentication":                         {"auth:oauth2"},
	// HTTP
	"system.net.http": {"net:http-client"},
	"refit":           {"net:http-client"},
	// Crypto
	"bouncycastle.cryptography":               {"crypto:library"},
	"system.security.cryptography.algorithms": {"crypto:library"},
	// Serialization
	"google.protobuf": {"serialization:protobuf"},
	"protobuf-net":    {"serialization:protobuf"},
	"yamldotnet":      {"serialization:yaml"},
	// AI
	"azure.ai.openai":          {"ai:llm-sdk"},
	"microsoft.semantickernel": {"ai:llm-sdk"},
	// Directory / LDAP
	"novell.directory.ldap":              {"directory:ldap"},
	"system.directoryservices.protocols": {"directory:ldap"},
	"system.directoryservices.ldap":      {"directory:ldap"},
}

// pypiExact: PyPI canonical name (lowercased, PEP 503 normalized) → capabilities.
// Keys should already be canonical (hyphens only, no underscores/dots).
var pypiExact = map[string][]string{
	// Relational DB drivers
	"psycopg2":               {"datastore:postgresql"},
	"psycopg2-binary":        {"datastore:postgresql"},
	"psycopg":                {"datastore:postgresql"},
	"asyncpg":                {"datastore:postgresql"},
	"aiopg":                  {"datastore:postgresql"},
	"mysqlclient":            {"datastore:mysql"},
	"pymysql":                {"datastore:mysql"},
	"aiomysql":               {"datastore:mysql"},
	"mysql-connector-python": {"datastore:mysql"},
	"cx-oracle":              {"datastore:relational"},
	"pyodbc":                 {"datastore:relational"},
	// ORM / query builder (multi-DB)
	"sqlalchemy":   {"datastore:relational"},
	"alembic":      {"datastore:relational"},
	"tortoise-orm": {"datastore:relational"},
	"peewee":       {"datastore:relational"},
	"django":       {"datastore:relational"},
	// Cache / Redis
	"redis":    {"cache:redis"},
	"aioredis": {"cache:redis"},
	"hiredis":  {"cache:redis"},
	// MongoDB
	"pymongo": {"datastore:mongodb"},
	"motor":   {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch":     {"search:elasticsearch"},
	"elasticsearch-dsl": {"search:elasticsearch"},
	"opensearch-py":     {"search:elasticsearch"},
	// Kafka
	"kafka-python":    {"messaging:kafka"},
	"confluent-kafka": {"messaging:kafka"},
	"aiokafka":        {"messaging:kafka"},
	"faust-streaming": {"messaging:kafka"},
	// AMQP
	"pika":     {"messaging:amqp"},
	"aio-pika": {"messaging:amqp"},
	"amqp":     {"messaging:amqp"},
	"celery":   {"messaging:amqp"},
	// NATS
	"nats-py": {"messaging:nats"},
	"nats":    {"messaging:nats"},
	// SMTP / email
	"sendgrid":   {"messaging:smtp"},
	"mailgun2":   {"messaging:smtp"},
	"postmarker": {"messaging:smtp"},
	"emails":     {"messaging:smtp"},
	// Object storage / cloud
	"boto3":                {"cloud:aws"},
	"botocore":             {"cloud:aws"},
	"aiobotocore":          {"cloud:aws"},
	"s3transfer":           {"storage:object", "cloud:aws"},
	"google-cloud-storage": {"storage:object", "cloud:gcp"},
	"azure-storage-blob":   {"storage:object", "cloud:azure"},
	"azure-cosmos":         {"cloud:azure"},
	"firebase-admin":       {"cloud:gcp"},
	// Auth
	"pyjwt":            {"auth:jwt"},
	"python-jose":      {"auth:jwt"},
	"authlib":          {"auth:oidc"},
	"oauthlib":         {"auth:oauth2"},
	"social-auth-core": {"auth:oauth2"},
	"django-allauth":   {"auth:oauth2"},
	// HTTP clients
	"requests": {"net:http-client"},
	"httpx":    {"net:http-client"},
	"aiohttp":  {"net:http-client"},
	"urllib3":  {"net:http-client"},
	"httpcore": {"net:http-client"},
	// Crypto
	"cryptography": {"crypto:library"},
	"pyopenssl":    {"crypto:library"},
	"pynacl":       {"crypto:library"},
	// Serialization
	"protobuf":    {"serialization:protobuf"},
	"grpcio":      {"serialization:protobuf"},
	"pyyaml":      {"serialization:yaml"},
	"ruamel-yaml": {"serialization:yaml"},
	// AI
	"openai":          {"ai:llm-sdk"},
	"anthropic":       {"ai:llm-sdk"},
	"langchain":       {"ai:llm-sdk"},
	"huggingface-hub": {"ai:llm-sdk"},
	// Directory / LDAP
	"ldap3":       {"directory:ldap"},
	"python-ldap": {"directory:ldap"},
}

// pythonImportExact: Python top-level import name (exact, lowercased) → capabilities.
// Import names may differ from PyPI package names (e.g. PyPI "python-jose" → import "jose").
var pythonImportExact = map[string][]string{
	// Relational DB
	"psycopg2":   {"datastore:postgresql"},
	"psycopg":    {"datastore:postgresql"},
	"asyncpg":    {"datastore:postgresql"},
	"aiopg":      {"datastore:postgresql"},
	"pymysql":    {"datastore:mysql"},
	"aiomysql":   {"datastore:mysql"},
	"pyodbc":     {"datastore:relational"},
	"sqlalchemy": {"datastore:relational"},
	"alembic":    {"datastore:relational"},
	"peewee":     {"datastore:relational"},
	"django":     {"datastore:relational"},
	"tortoise":   {"datastore:relational"},
	// Cache / Redis
	"redis":    {"cache:redis"},
	"aioredis": {"cache:redis"},
	// MongoDB
	"pymongo": {"datastore:mongodb"},
	"motor":   {"datastore:mongodb"},
	// Elasticsearch
	"elasticsearch": {"search:elasticsearch"},
	// Kafka
	"kafka":           {"messaging:kafka"},
	"confluent_kafka": {"messaging:kafka"},
	"aiokafka":        {"messaging:kafka"},
	// AMQP
	"pika":     {"messaging:amqp"},
	"aio_pika": {"messaging:amqp"},
	"celery":   {"messaging:amqp"},
	// SMTP
	"sendgrid": {"messaging:smtp"},
	// Cloud / AWS
	"boto3":    {"cloud:aws"},
	"botocore": {"cloud:aws"},
	// Cloud / GCP
	"firebase_admin": {"cloud:gcp"},
	// Auth
	"jose":    {"auth:jwt"},
	"jwt":     {"auth:jwt"},
	"authlib": {"auth:oidc"},
	// HTTP
	"requests": {"net:http-client"},
	"httpx":    {"net:http-client"},
	"aiohttp":  {"net:http-client"},
	// Crypto
	"cryptography": {"crypto:library"},
	"nacl":         {"crypto:library"},
	// Serialization
	// "google" alone is too broad — not a capability signal; omitted
	"yaml": {"serialization:yaml"},
	// AI
	"openai":    {"ai:llm-sdk"},
	"anthropic": {"ai:llm-sdk"},
	"langchain": {"ai:llm-sdk"},
	// Directory / LDAP
	"ldap3": {"directory:ldap"},
	"ldap":  {"directory:ldap"},
}

// pubExact: Dart pub package name (exact, lowercased) → capabilities.
// Only well-known, unambiguous packages are listed. Packages with multiple
// possible roles or primarily used for testing are omitted.
// dev_dependencies are filtered by the capability engine and do not reach here.
var pubExact = map[string][]string{
	// HTTP clients — the Dart `http` package is the canonical HTTP client.
	"http":    {"net:http-client"},
	"dio":     {"net:http-client"},
	"chopper": {"net:http-client"},
	// SQLite / relational (embedded)
	"sqflite":            {"datastore:relational"},
	"drift":              {"datastore:relational"},
	"sqlite3":            {"datastore:relational"},
	"sqflite_common_ffi": {"datastore:relational"},
	// PostgreSQL
	"postgres": {"datastore:postgresql"},
	// MongoDB
	"mongo_dart": {"datastore:mongodb"},
	// Redis
	"redis_dart": {"cache:redis"},
	// Firebase (GCP)
	"firebase_core":      {"cloud:gcp"},
	"firebase_auth":      {"cloud:gcp", "auth:oauth2"},
	"firebase_database":  {"cloud:gcp", "datastore:mongodb"},
	"firebase_firestore": {"cloud:gcp"},
	"cloud_firestore":    {"cloud:gcp"},
	"firebase_storage":   {"cloud:gcp", "storage:object"},
	// Serialization
	"protobuf": {"serialization:protobuf"},
	"grpc":     {"serialization:protobuf"},
	// Auth
	"jwt_decode": {"auth:jwt"},
	// AI / LLM
	"langchain_dart":       {"ai:llm-sdk"},
	"google_generative_ai": {"ai:llm-sdk"},
	"openai_dart":          {"ai:llm-sdk"},
}
