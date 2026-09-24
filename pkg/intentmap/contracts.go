package intentmap

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v2"
)

// maxContractOperations bounds the operations recorded from one OpenAPI
// document; the rest are counted, not listed.
const maxContractOperations = 500

var (
	contractMarker     = regexp.MustCompile(`(?m)^\s*(openapi|swagger|asyncapi)\s*:`)
	jsonContractMarker = regexp.MustCompile(`"(openapi|swagger|asyncapi)"\s*:`)
	graphqlRoot        = regexp.MustCompile(`(?m)^\s*(?:extend\s+)?(?:schema\s*\{|type\s+(?:Query|Mutation|Subscription)\b)`)
	httpMethods        = map[string]bool{"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true}
)

// contractCandidate reports whether a file may hold a declared API contract:
// OpenAPI, Swagger or AsyncAPI in YAML or JSON, or a GraphQL schema.
func contractCandidate(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml", ".json", ".graphql", ".graphqls", ".gql":
		return true
	}
	return false
}

// parseContract recognizes a declared API contract from its content. It
// returns nil for ordinary YAML or JSON, so other readers can handle it.
func parseContract(name string, content []byte) []Observation {
	switch strings.ToLower(path.Ext(name)) {
	case ".graphql", ".graphqls", ".gql":
		loc := graphqlRoot.FindIndex(content)
		if loc == nil {
			return nil
		}
		line := bytes.Count(content[:loc[0]], []byte("\n")) + 1
		return []Observation{{Kind: KindInterface, Name: path.Base(name), State: "declared", Basis: "declared_contract", Path: name, StartLine: line, EndLine: line, Properties: map[string]string{"protocol": "graphql", "interface_kind": "api_document"}}}
	}
	head := content
	if len(head) > 4096 {
		head = head[:4096]
	}
	marker := contractMarker
	if strings.EqualFold(path.Ext(name), ".json") {
		marker = jsonContractMarker
	}
	match := marker.FindSubmatch(head)
	if match == nil {
		return nil
	}
	format := string(match[1])
	var doc map[string]any
	if strings.EqualFold(path.Ext(name), ".json") {
		if err := json.Unmarshal(content, &doc); err != nil {
			return nil
		}
	} else {
		var raw map[any]any
		if err := yaml.Unmarshal(content, &raw); err != nil {
			return nil
		}
		doc = stringKeys(raw)
	}
	if _, ok := doc[format]; !ok {
		return nil
	}
	title := path.Base(name)
	if info, ok := doc["info"].(map[string]any); ok {
		if t, ok := info["title"].(string); ok && strings.TrimSpace(t) != "" {
			title = strings.TrimSpace(t)
		}
	}
	protocol := "http"
	if format == "asyncapi" {
		protocol = "asyncapi"
	}
	out := []Observation{{Kind: KindInterface, Name: title, State: "declared", Basis: "declared_contract", Path: name, Properties: map[string]string{"protocol": protocol, "interface_kind": "api_document", "contract_format": format}}}
	if format == "asyncapi" {
		return out
	}
	paths, _ := doc["paths"].(map[string]any)
	var ops []string
	for p, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for m := range methods {
			if httpMethods[strings.ToLower(m)] {
				ops = append(ops, strings.ToUpper(m)+" "+p)
			}
		}
	}
	sort.Strings(ops)
	if len(ops) > maxContractOperations {
		out[0].Properties["operations_omitted"] = itoa(len(ops) - maxContractOperations)
		ops = ops[:maxContractOperations]
	}
	for _, op := range ops {
		out = append(out, Observation{Kind: KindInterface, Name: title + " " + op, State: "declared", Basis: "declared_contract", Path: name, Properties: map[string]string{"protocol": "http", "interface_kind": "operation", "contract": title}})
	}
	return out
}

// stringKeys converts YAML maps with arbitrary keys to string-keyed maps,
// recursively, so YAML and JSON documents share one traversal.
func stringKeys(in map[any]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		key, ok := k.(string)
		if !ok {
			continue
		}
		if m, ok := v.(map[any]any); ok {
			out[key] = stringKeys(m)
			continue
		}
		out[key] = v
	}
	return out
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
