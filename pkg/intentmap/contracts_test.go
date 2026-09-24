package intentmap

import (
	"strings"
	"testing"
)

func contractNames(obs []Observation) []string {
	var out []string
	for _, o := range obs {
		out = append(out, o.Properties["interface_kind"]+":"+o.Name)
	}
	return out
}

func TestParseContractOpenAPIYAML(t *testing.T) {
	src := "openapi: 3.0.0\ninfo:\n  title: Orders\n  version: \"1\"\npaths:\n  /orders:\n    get:\n      responses: {}\n    post:\n      responses: {}\n    parameters: []\n"
	got := strings.Join(contractNames(parseContract("api/openapi.yaml", []byte(src))), ",")
	if got != "api_document:Orders,operation:Orders GET /orders,operation:Orders POST /orders" {
		t.Fatalf("got %s", got)
	}
}

func TestParseContractSwaggerJSONAndAsyncAPI(t *testing.T) {
	swagger := `{"swagger":"2.0","info":{"title":"Pets"},"paths":{"/pets/{id}":{"delete":{}}}}`
	if got := strings.Join(contractNames(parseContract("swagger.json", []byte(swagger))), ","); got != "api_document:Pets,operation:Pets DELETE /pets/{id}" {
		t.Fatalf("swagger got %s", got)
	}
	async := `{"asyncapi":"2.6.0","info":{"title":"Events"},"channels":{}}`
	if got := strings.Join(contractNames(parseContract("events.json", []byte(async))), ","); got != "api_document:Events" {
		t.Fatalf("asyncapi got %s", got)
	}
}

func TestParseContractIgnoresOrdinaryConfigAndMentions(t *testing.T) {
	for name, src := range map[string]string{
		"config.yaml":      "server:\n  port: 8080\n",
		"appsettings.json": `{"Logging":{"LogLevel":{"Default":"Information"}}}`,
		"docs/notes.yaml":  "description: mentions openapi: in a string value\n",
		"schema.graphql":   "scalar Date\n",
		"values.json":      `{"text":"the word openapi appears here"}`,
	} {
		if got := parseContract(name, []byte(src)); got != nil {
			t.Errorf("%s: unexpected contract %v", name, contractNames(got))
		}
	}
}

func TestParseContractGraphQLSchema(t *testing.T) {
	got := parseContract("api/schema.graphql", []byte("# comment\ntype Query {\n  orders: [Order]\n}\n"))
	if len(got) != 1 || got[0].Properties["protocol"] != "graphql" || got[0].StartLine != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestDockerfileExposeResolvesChainedAndDefaultedVariables(t *testing.T) {
	src := "ARG APP_PORT=8080\nENV PORT=$APP_PORT\nENV PORT_ADMIN=9090\nEXPOSE ${PORT}\nEXPOSE $PORT_ADMIN\nEXPOSE ${METRICS:-9100}/tcp\nEXPOSE $UNDEFINED\n"
	var ports []string
	for _, o := range parseDockerfileExpose("Dockerfile", []byte(src)) {
		ports = append(ports, o.Properties["port"])
	}
	if got := strings.Join(ports, ","); got != "8080,9090,9100" {
		t.Fatalf("ports = %s, want 8080,9090,9100", got)
	}
}
