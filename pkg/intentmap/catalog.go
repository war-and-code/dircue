package intentmap

import "strings"

type catalogEntry struct {
	capability  string
	coordinates []string
}

var catalog = []catalogEntry{
	{"datastore:postgresql", []string{"github.com/jackc/pgx", "github.com/lib/pq", "npgsql", "psycopg", "asyncpg", "postgresql"}},
	{"datastore:mongodb", []string{"mongodb", "mongo-go-driver", "mongoose"}},
	{"cache:redis", []string{"redis", "stackexchange.redis", "go-redis"}},
	{"messaging:kafka", []string{"kafka", "confluent-kafka", "sarama"}},
	{"messaging:amqp", []string{"rabbitmq.client", "rabbitmq", "amqp091-go", "aio-pika", "pika", "amqp"}},
	{"messaging:event-bus", []string{"masstransit", "azure.messaging.servicebus", "nservicebus"}},
	{"net:http-client", []string{"axios", "requests", "reqwest", "net/http", "httpclient"}},
	{"auth:oidc", []string{"openidconnect", "oidc", "oauth2-proxy"}},
	{"auth:jwt", []string{"jsonwebtoken", "pyjwt", "jwt-go", "golang-jwt"}},
	{"crypto:library", []string{"ring", "cryptography", "bouncycastle", "golang.org/x/crypto"}},
	{"serialization:protobuf", []string{"protobuf", "google.golang.org/protobuf"}},
	{"serialization:yaml", []string{"yaml", "gopkg.in/yaml", "go.yaml.in/yaml"}},
	{"exec:process", []string{"child_process", "system.diagnostics.process"}},
	{"ai:llm-sdk", []string{"openai", "anthropic", "langchain"}},
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
