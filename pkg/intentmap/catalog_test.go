package intentmap

import (
	"context"
	"strings"
	"testing"

	"github.com/war-and-code/dircue/pkg/declarations"
)

// TestCatalogEcosystemExactMatching verifies that the ecosystem-aware catalog
// maps real, published package names to the correct capability in each
// ecosystem, and that confusingly-similar names do NOT match.
func TestCatalogEcosystemExactMatching(t *testing.T) {
	type tc struct {
		kind       string
		pkg        string // raw value as it would arrive from the declarations parser
		wantCap    string // expected capability, or "" for no match
		mustAbsent string // this capability must NOT appear
	}

	tests := []tc{
		// ─── npm ────────────────────────────────────────────────────────────
		{"npm-dependency", "pg", "datastore:postgresql", ""},
		{"npm-dependency", "pg-promise", "datastore:postgresql", ""},
		{"npm-dependency", "mysql2", "datastore:mysql", ""},
		{"npm-dependency", "ioredis", "cache:redis", ""},
		{"npm-dependency", "redis", "cache:redis", ""},
		{"npm-dependency", "kafkajs", "messaging:kafka", ""},
		{"npm-dependency", "amqplib", "messaging:amqp", ""},
		{"npm-dependency", "nodemailer", "messaging:smtp", ""},
		{"npm-dependency", "@aws-sdk/client-s3", "storage:object", ""},
		{"npm-dependency", "@aws-sdk/client-dynamodb", "cloud:aws", ""},
		{"npm-dependency", "mongodb", "datastore:mongodb", ""},
		{"npm-dependency", "mongoose", "datastore:mongodb", ""},
		{"npm-dependency", "@elastic/elasticsearch", "search:elasticsearch", ""},
		{"npm-dependency", "jsonwebtoken", "auth:jwt", ""},
		{"npm-dependency", "passport", "auth:oauth2", ""},
		{"npm-dependency", "prisma", "datastore:relational", ""},
		{"npm-dependency", "typeorm", "datastore:relational", ""},
		{"npm-dependency", "sequelize", "datastore:relational", ""},
		{"npm-dependency", "knex", "datastore:relational", ""},
		// Negative: redis-mock is a test double, not in the catalog
		{"npm-dependency", "redis-mock", "", "cache:redis"},
		// Negative: pgp (a PGP/GPG library) must NOT map to postgres
		{"npm-dependency", "pgp", "", "datastore:postgresql"},

		// ─── RubyGems ───────────────────────────────────────────────────────
		{"ruby-gem-dependency", "pg", "datastore:postgresql", ""},
		{"ruby-gem-dependency", "mysql2", "datastore:mysql", ""},
		{"ruby-gem-dependency", "redis", "cache:redis", ""},
		{"ruby-gem-dependency", "bunny", "messaging:amqp", ""},
		{"ruby-gem-dependency", "aws-sdk-s3", "storage:object", ""},
		{"ruby-gem-dependency", "aws-sdk-s3", "cloud:aws", ""},
		{"ruby-gem-dependency", "chewy", "search:elasticsearch", ""},
		{"ruby-gem-dependency", "elasticsearch", "search:elasticsearch", ""},
		{"ruby-gem-dependency", "devise", "auth:oauth2", ""},
		{"ruby-gem-dependency", "omniauth", "auth:oauth2", ""},
		{"ruby-gem-dependency", "doorkeeper", "auth:oauth2", ""},
		{"ruby-gem-dependency", "jwt", "auth:jwt", ""},
		{"ruby-gem-dependency", "mongoid", "datastore:mongodb", ""},
		{"ruby-gem-dependency", "ruby-kafka", "messaging:kafka", ""},
		// Negative: sidekiq (background job framework, not a DB/cache driver)
		{"ruby-gem-dependency", "sidekiq", "", ""},
		// Negative: rack (web framework)
		{"ruby-gem-dependency", "rack", "", "datastore:postgresql"},

		// ─── Maven / Gradle ─────────────────────────────────────────────────
		{"maven-dependency", "org.postgresql:postgresql:42.3.1", "datastore:postgresql", ""},
		{"maven-dependency", "com.mysql:mysql-connector-j:8.0.33", "datastore:mysql", ""},
		{"maven-dependency", "mysql:mysql-connector-java:8.0.28", "datastore:mysql", ""},
		{"maven-dependency", "org.springframework.boot:spring-boot-starter-data-redis:3.0.0", "cache:redis", ""},
		{"maven-dependency", "org.springframework.boot:spring-boot-starter-data-jpa:3.0.0", "datastore:relational", ""},
		{"maven-dependency", "org.springframework.boot:spring-boot-starter-amqp:3.0.0", "messaging:amqp", ""},
		{"maven-dependency", "org.springframework.boot:spring-boot-starter-security:3.0.0", "auth:oauth2", ""},
		{"maven-dependency", "org.springframework.boot:spring-boot-starter-oauth2-client:3.0.0", "auth:oauth2", ""},
		{"maven-dependency", "org.apache.kafka:kafka-clients:3.3.1", "messaging:kafka", ""},
		{"maven-dependency", "software.amazon.awssdk:s3:2.20.0", "storage:object", ""},
		{"maven-dependency", "software.amazon.awssdk:dynamodb:2.20.0", "cloud:aws", ""},
		{"maven-dependency", "io.jsonwebtoken:jjwt:0.11.5", "auth:jwt", ""},
		{"maven-dependency", "com.h2database:h2:2.1.214", "datastore:relational", ""},
		{"gradle-dependency", "org.postgresql:postgresql:42.3.1", "datastore:postgresql", ""},
		{"gradle-dependency", "redis.clients:jedis:4.3.0", "cache:redis", ""},
		{"gradle-dependency", "org.mongodb:mongodb-driver-sync:4.9.0", "datastore:mongodb", ""},
		// Negative: a groupId that isn't in the catalog
		{"maven-dependency", "com.example:my-lib:1.0", "", "datastore:postgresql"},

		// ─── NuGet ──────────────────────────────────────────────────────────
		{"package-reference", "Npgsql@8.0.1", "datastore:postgresql", ""},
		{"package-reference", "StackExchange.Redis@2.6.116", "cache:redis", ""},
		{"package-reference", "Microsoft.Extensions.Caching.StackExchangeRedis@8.0.0", "cache:redis", ""},
		{"package-reference", "MassTransit@8.1.0", "messaging:amqp", ""},
		{"package-reference", "MassTransit.RabbitMQ@8.1.0", "messaging:amqp", ""},
		{"package-reference", "RabbitMQ.Client@6.6.0", "messaging:amqp", ""},
		{"package-reference", "Confluent.Kafka@2.3.0", "messaging:kafka", ""},
		{"package-reference", "AWSSDK.S3@3.7.0", "storage:object", ""},
		{"package-reference", "Azure.Storage.Blobs@12.18.0", "storage:object", ""},
		{"package-reference", "MongoDB.Driver@2.22.0", "datastore:mongodb", ""},
		{"package-reference", "Elasticsearch.Net@7.17.5", "search:elasticsearch", ""},
		{"package-reference", "Microsoft.EntityFrameworkCore@8.0.0", "datastore:relational", ""},
		{"package-reference", "System.IdentityModel.Tokens.Jwt@7.3.1", "auth:jwt", ""},
		{"package-reference", "Microsoft.AspNetCore.Authentication.JwtBearer@8.0.0", "auth:jwt", ""},
		// Negative: version-only package ref should not match an unrelated name
		{"package-reference", "Newtonsoft.Json@13.0.3", "", "cache:redis"},

		// ─── Go module imports ───────────────────────────────────────────────
		{"go-import", "github.com/redis/go-redis/v9", "cache:redis", ""},
		{"go-import", "github.com/jackc/pgx/v5", "datastore:postgresql", ""},
		{"go-import", "github.com/lib/pq", "datastore:postgresql", ""},
		{"go-import", "go.mongodb.org/mongo-driver/mongo", "datastore:mongodb", ""},
		{"go-import", "github.com/segmentio/kafka-go", "messaging:kafka", ""},
		{"go-import", "github.com/aws/aws-sdk-go-v2/service/s3", "storage:object", ""},
		{"go-import", "github.com/aws/aws-sdk-go-v2/aws", "cloud:aws", ""},
		{"go-import", "github.com/golang-jwt/jwt/v4", "auth:jwt", ""},
		{"go-import", "github.com/elastic/go-elasticsearch/v8", "search:elasticsearch", ""},
		{"go-import", "github.com/nats-io/nats.go", "messaging:nats", ""},
		{"go-import", "github.com/rabbitmq/amqp091-go", "messaging:amqp", ""},
		// Negative: "github.com/redis/redis-go" doesn't exist; "go-redis" must not match arbitrary paths
		{"go-import", "github.com/redis/completely-different", "", "cache:redis"},

		// ─── Go module requires ──────────────────────────────────────────────
		{"go-require", "github.com/redis/go-redis/v9 v9.0.5", "cache:redis", ""},
		{"go-require", "github.com/jackc/pgx/v5 v5.5.0", "datastore:postgresql", ""},
		{"go-require", "github.com/aws/aws-sdk-go-v2/service/s3 v1.40.0", "storage:object", ""},
		{"go-require", "golang.org/x/crypto v0.15.0", "crypto:library", ""},

		// ─── Rust / Cargo ────────────────────────────────────────────────────
		{"cargo-dependency", "sqlx", "datastore:relational", ""},
		{"cargo-dependency", "tokio-postgres", "datastore:postgresql", ""},
		{"cargo-dependency", "redis", "cache:redis", ""},
		{"cargo-dependency", "rdkafka", "messaging:kafka", ""},
		{"cargo-dependency", "lapin", "messaging:amqp", ""},
		{"cargo-dependency", "aws-sdk-s3", "storage:object", ""},
		{"cargo-dependency", "aws-sdk-s3; source=registry", "storage:object", ""},
		{"cargo-dependency", "jsonwebtoken", "auth:jwt", ""},
		{"cargo-dependency", "lettre", "messaging:smtp", ""},
		{"cargo-dependency", "reqwest", "net:http-client", ""},
		{"cargo-dependency", "ring", "crypto:library", ""},
		// Negative: common utility crates not in the catalog
		{"cargo-dependency", "serde", "", "datastore:relational"},
		{"cargo-dependency", "tokio", "", "cache:redis"},

		// ─── PHP / Composer ──────────────────────────────────────────────────
		{"php-dependency", "predis/predis", "cache:redis", ""},
		{"php-dependency", "doctrine/dbal", "datastore:relational", ""},
		{"php-dependency", "doctrine/orm", "datastore:relational", ""},
		{"php-dependency", "aws/aws-sdk-php", "storage:object", ""},
		{"php-dependency", "firebase/php-jwt", "auth:jwt", ""},
		{"php-dependency", "php-amqplib/php-amqplib", "messaging:amqp", ""},
		{"php-dependency", "mongodb/mongodb", "datastore:mongodb", ""},
		{"php-dependency", "elasticsearch/elasticsearch", "search:elasticsearch", ""},
		{"php-dependency", "league/oauth2-server", "auth:oauth2", ""},
		{"php-dependency", "guzzlehttp/guzzle", "net:http-client", ""},
		// Negative: symfony/console is a CLI framework, not a DB driver
		{"php-dependency", "symfony/console", "", "datastore:relational"},

		// ─── Python PyPI ─────────────────────────────────────────────────────
		{"python-dependency", "redis>=5", "cache:redis", ""},
		{"python-dependency", "aio-pika>=9", "messaging:amqp", ""},
		{"python-dependency", "psycopg2-binary", "datastore:postgresql", ""},
		{"python-dependency", "psycopg2>=2.9", "datastore:postgresql", ""},
		{"python-dependency", "python-jose[cryptography]>=3.3", "auth:jwt", ""},
		{"python-dependency", "boto3>=1.26", "cloud:aws", ""},
		{"python-dependency", "sqlalchemy>=2.0", "datastore:relational", ""},
		{"python-dependency", "pymongo>=4.3", "datastore:mongodb", ""},
		{"python-dependency", "confluent-kafka>=2.3", "messaging:kafka", ""},
		{"python-dependency", "pyjwt>=2.8", "auth:jwt", ""},
		{"python-dependency", "requests>=2.28", "net:http-client", ""},
		{"python-dependency", "cryptography>=41", "crypto:library", ""},
		{"python-dependency", "sendgrid>=6.9", "messaging:smtp", ""},
		// Normalize underscores/dots per PEP 503
		{"python-dependency", "PyJWT>=2.0", "auth:jwt", ""},
		{"python-dependency", "SQLAlchemy>=2.0", "datastore:relational", ""},
		// Negative: pylint is a code-quality tool
		{"python-dependency", "pylint>=3", "", "datastore:relational"},

		// ─── Python import names ─────────────────────────────────────────────
		{"python-import", "jose", "auth:jwt", ""},
		{"python-import", "psycopg2", "datastore:postgresql", ""},
		{"python-import", "redis", "cache:redis", ""},
		{"python-import", "boto3", "cloud:aws", ""},
		{"python-import", "pymongo", "datastore:mongodb", ""},
		{"python-import", "kafka", "messaging:kafka", ""},
		{"python-import", "pika", "messaging:amqp", ""},
		{"python-import", "requests", "net:http-client", ""},
		// Negative: os, sys, json are standard library
		{"python-import", "os", "", ""},
		{"python-import", "sys", "", ""},
		{"python-import", "json", "", ""},
	}

	for _, tt := range tests {
		caps := capabilitiesFor(tt.kind, tt.pkg)
		capSet := make(map[string]bool, len(caps))
		for _, c := range caps {
			capSet[c] = true
		}
		if tt.wantCap != "" && !capSet[tt.wantCap] {
			t.Errorf("[%s] %q → missing capability %q; got %v", tt.kind, tt.pkg, tt.wantCap, caps)
		}
		if tt.mustAbsent != "" && capSet[tt.mustAbsent] {
			t.Errorf("[%s] %q → unexpected capability %q (should be absent); got %v", tt.kind, tt.pkg, tt.mustAbsent, caps)
		}
	}
}

// TestExtractPackageName verifies the version-stripping helper.
func TestExtractPackageName(t *testing.T) {
	tests := []struct {
		kind  string
		value string
		want  string
	}{
		// python-dependency: strip version specifiers
		{"python-dependency", "redis>=5", "redis"},
		{"python-dependency", "psycopg2-binary", "psycopg2-binary"},
		{"python-dependency", "python-jose[cryptography]>=3.3", "python-jose"},
		{"python-dependency", "SQLAlchemy>=2.0", "sqlalchemy"},
		{"python-dependency", "PyJWT>=2.0", "pyjwt"},
		// go-require: strip version suffix
		{"go-require", "github.com/redis/go-redis/v9 v9.0.5", "github.com/redis/go-redis/v9"},
		{"go-require", "golang.org/x/crypto v0.15.0", "golang.org/x/crypto"},
		// cargo: strip condition suffix
		{"cargo-dependency", "redis; source=registry", "redis"},
		{"cargo-dependency", "sqlx; source=git+https://...", "sqlx"},
		// NuGet: strip @version
		{"package-reference", "StackExchange.Redis@2.6.116", "stackexchange.redis"},
		{"package-reference", "Npgsql", "npgsql"},
		// Maven/Gradle: keep group:artifact
		{"maven-dependency", "org.postgresql:postgresql:42.3.1", "org.postgresql:postgresql"},
		{"maven-dependency", "software.amazon.awssdk:s3:2.20.0", "software.amazon.awssdk:s3"},
		{"gradle-dependency", "org.apache.kafka:kafka-clients:3.3.1", "org.apache.kafka:kafka-clients"},
		// npm: no stripping
		{"npm-dependency", "ioredis", "ioredis"},
		{"npm-dependency", "@aws-sdk/client-s3", "@aws-sdk/client-s3"},
		// ruby: no stripping
		{"ruby-gem-dependency", "pg", "pg"},
		// php: no stripping
		{"php-dependency", "predis/predis", "predis/predis"},
		// go-import: no stripping
		{"go-import", "github.com/jackc/pgx/v5", "github.com/jackc/pgx/v5"},
		// python-import: no stripping
		{"python-import", "jose", "jose"},
	}

	for _, tt := range tests {
		got := extractPackageName(tt.kind, strings.ToLower(tt.value))
		if got != tt.want {
			t.Errorf("extractPackageName(%q, %q) = %q, want %q", tt.kind, tt.value, got, tt.want)
		}
	}
}

// TestNpmReferenceProducesCapabilityViaDeclaredDependency verifies that npm
// packages (which are stored as References, not Requirements) reach the
// capability catalog via AddDeclarations.
func TestNpmReferenceProducesCapabilityViaDeclaredDependency(t *testing.T) {
	d := New(Options{})
	d.AddDeclarations([]declarations.Project{{
		ID: "package.json", Root: ".",
		Kind: "npm",
		// npm uses References, not Requirements, for dependencies.
		References: []declarations.Reference{
			{Kind: "npm-dependency", Value: "pg", State: "declared", TargetStatus: "external", Evidence: "package.json", Condition: "dependencies"},
			{Kind: "npm-dependency", Value: "ioredis", State: "declared", TargetStatus: "external", Evidence: "package.json", Condition: "dependencies"},
			{Kind: "npm-dependency", Value: "kafkajs", State: "declared", TargetStatus: "external", Evidence: "package.json", Condition: "dependencies"},
			{Kind: "npm-dependency", Value: "@aws-sdk/client-s3", State: "declared", TargetStatus: "external", Evidence: "package.json", Condition: "dependencies"},
		},
	}})
	r, err := d.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"datastore:postgresql": false,
		"cache:redis":          false,
		"messaging:kafka":      false,
		"storage:object":       false,
	}
	for _, o := range r.Observations {
		if o.Kind == KindCapability && o.Basis == "declared_dependency" {
			want[o.Name] = true
		}
	}
	for cap, found := range want {
		if !found {
			t.Errorf("npm Reference %q did not produce capability %q; observations: %+v", cap, cap, r.Observations)
		}
	}
}

// TestCatalogNpmIoredisNotFromRedisPrefix verifies the key correctness property:
// "ioredis" must produce cache:redis via exact matching, not because "redis" is
// a prefix of "ioredis".
func TestCatalogNpmIoredisNotFromRedisPrefix(t *testing.T) {
	caps := capabilitiesFor("npm-dependency", "ioredis")
	found := false
	for _, c := range caps {
		if c == "cache:redis" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ioredis did not produce cache:redis; got %v", caps)
	}
	// Also verify that an unrelated "ioXXX" name does not accidentally match.
	caps2 := capabilitiesFor("npm-dependency", "iodb")
	for _, c := range caps2 {
		if c == "cache:redis" {
			t.Fatalf("iodb should not produce cache:redis; got %v", caps2)
		}
	}
}

// TestCatalogMavenCoordinatesNoFalsePrefix verifies that Maven coordinates
// using group:artifact format work exactly, not by prefix-matching "postgresql"
// inside the coordinate string.
func TestCatalogMavenCoordinatesNoFalsePrefix(t *testing.T) {
	// Exact match on "org.postgresql:postgresql"
	caps := capabilitiesFor("maven-dependency", "org.postgresql:postgresql:42.3.1")
	found := false
	for _, c := range caps {
		if c == "datastore:postgresql" {
			found = true
		}
	}
	if !found {
		t.Fatalf("maven org.postgresql:postgresql should produce datastore:postgresql; got %v", caps)
	}
	// A different artifact in the same group must not match postgresql
	caps2 := capabilitiesFor("maven-dependency", "org.postgresql:r2dbc-postgresql:1.0.0")
	for _, c := range caps2 {
		if c == "datastore:postgresql" {
			t.Fatalf("org.postgresql:r2dbc-postgresql must not match datastore:postgresql via group prefix; got %v", caps2)
		}
	}
}

func TestCapabilitiesForCodeGeneration(t *testing.T) {
	for _, test := range []struct {
		value string
		want  []string
	}{
		{"Protobuf", []string{"serialization:protobuf"}},
		{"org.xolstice.maven.plugins:protobuf-maven-plugin", []string{"serialization:protobuf"}},
		{"OpenApiReference", nil},
	} {
		got := capabilitiesFor("code-generation", test.value)
		if len(got) != len(test.want) || (len(got) > 0 && got[0] != test.want[0]) {
			t.Errorf("capabilitiesFor(code-generation, %q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestCapabilitiesForRubyHTTPClients(t *testing.T) {
	for _, gem := range []string{"http", "excon", "faraday"} {
		got := capabilitiesFor("ruby-gem-dependency", gem)
		if len(got) != 1 || got[0] != "net:http-client" {
			t.Errorf("capabilitiesFor(ruby-gem-dependency, %q) = %v, want [net:http-client]", gem, got)
		}
	}
}

func TestPubCapabilityCatalog(t *testing.T) {
	cases := []struct {
		pkg  string
		want string
	}{
		{"http", "net:http-client"},
		{"dio", "net:http-client"},
		{"sqflite", "datastore:relational"},
		{"drift", "datastore:relational"},
		{"sqlite3", "datastore:relational"},
		{"postgres", "datastore:postgresql"},
		{"mongo_dart", "datastore:mongodb"},
		{"firebase_core", "cloud:gcp"},
		{"protobuf", "serialization:protobuf"},
		{"grpc", "serialization:protobuf"},
	}
	for _, tc := range cases {
		got := capabilitiesFor("dart-dependency", tc.pkg)
		found := false
		for _, c := range got {
			if c == tc.want {
				found = true
			}
		}
		if !found {
			t.Errorf("capabilitiesFor(dart-dependency, %q) = %v; want to include %q", tc.pkg, got, tc.want)
		}
	}
}

func TestPubCapabilityCatalogNoFalsePositives(t *testing.T) {
	// Packages that should not match any capability.
	noCap := []string{"provider", "flutter_svg", "collection", "intl", "logging"}
	for _, pkg := range noCap {
		got := capabilitiesFor("dart-dependency", pkg)
		if len(got) != 0 {
			t.Errorf("capabilitiesFor(dart-dependency, %q) = %v; want no capability", pkg, got)
		}
	}
}
