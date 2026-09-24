package intentmap

import "strings"

type catalogEntry struct {
	capability  string
	coordinates []string
}

var catalog = []catalogEntry{
	// Relational databases. psycopg matches psycopg 3.x (the psycopg package);
	// psycopg2 and psycopg2-binary are listed separately because they do not
	// start with the psycopg coordinate (the hyphen is not a valid separator).
	{"datastore:postgresql", []string{"github.com/jackc/pgx", "github.com/lib/pq", "npgsql", "psycopg2-binary", "psycopg2", "psycopg", "asyncpg", "postgresql", "databases"}},
	{"datastore:mysql", []string{"mysqlconnector", "mysql-connector-python", "pymysql", "mysql2", "go-sql-driver"}},
	{"datastore:mongodb", []string{"mongodb", "mongo-go-driver", "mongoose", "pymongo"}},
	{"datastore:elasticsearch", []string{"elasticsearch", "elastic.clients.elasticsearch", "opensearch"}},
	{"cache:redis", []string{"redis", "stackexchange.redis", "microsoft.extensions.caching.stackexchangeredis", "go-redis", "aioredis"}},
	{"messaging:kafka", []string{"kafka", "confluent-kafka", "sarama", "kafka-python"}},
	{"messaging:amqp", []string{"rabbitmq.client", "rabbitmq", "amqp091-go", "aio-pika", "pika", "amqp"}},
	{"messaging:event-bus", []string{"masstransit", "azure.messaging.servicebus", "nservicebus"}},
	{"net:http-client", []string{"axios", "requests", "httpx", "aiohttp", "reqwest", "net/http", "httpclient"}},
	{"auth:oidc", []string{"openidconnect", "oidc", "oauth2-proxy", "authlib"}},
	// auth:jwt covers the major JWT libraries per ecosystem. python-jose is
	// the common FastAPI JWT library (package name python-jose, import name jose).
	{"auth:jwt", []string{"jsonwebtoken", "pyjwt", "python-jose", "jose", "jwt-go", "golang-jwt", "spring-security-oauth2", "spring-security"}},
	{"auth:oauth2", []string{"passport", "devise", "omniauth", "doorkeeper", "oauth2", "microsoft.aspnetcore.authentication"}},
	{"crypto:library", []string{"ring", "cryptography", "bouncycastle", "golang.org/x/crypto", "pyopenssl", "nacl"}},
	{"serialization:protobuf", []string{"protobuf", "google.golang.org/protobuf"}},
	{"serialization:yaml", []string{"yaml", "gopkg.in/yaml", "go.yaml.in/yaml"}},
	{"exec:process", []string{"child_process", "system.diagnostics.process", "subprocess", "os/exec"}},
	{"ai:llm-sdk", []string{"openai", "anthropic", "langchain", "huggingface"}},
	{"cloud:aws", []string{"boto3", "botocore", "aws-sdk", "amazon.lambda", "amazon.dynamodb", "amazon.s3"}},
	{"cloud:azure", []string{"azure-storage-blob", "azure-cosmos", "azure.storage", "azure.identity", "microsoft.azure"}},
	{"cloud:gcp", []string{"google-cloud", "google.cloud", "firebase-admin"}},
}

func capabilitiesFor(value string) []string {
	v := strings.ToLower(value)
	seen := map[string]bool{}
	var out []string
	for _, e := range catalog {
		for _, c := range e.coordinates {
			if coordinateMatch(v, c) {
				if !seen[e.capability] {
					seen[e.capability] = true
					out = append(out, e.capability)
				}
				break
			}
		}
	}
	return out
}
func coordinateMatch(value, coordinate string) bool {
	if !strings.HasPrefix(value, coordinate) {
		return false
	}
	if len(value) == len(coordinate) {
		return true
	}
	switch value[len(coordinate)] {
	case '/', ' ', '@', '>', '<', '=', '!', '~', '[', ';':
		return true
	default:
		return false
	}
}
