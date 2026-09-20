package jsonschema_test

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	private "dircue/internal/jsonschema"
	upstream "github.com/santhosh-tekuri/jsonschema/v5"
)

func TestUnmodifiedUpstreamParity(t *testing.T) {
	dialects := []string{"http://json-schema.org/draft-04/schema", "http://json-schema.org/draft-06/schema", "http://json-schema.org/draft-07/schema", "https://json-schema.org/draft/2019-09/schema", "https://json-schema.org/draft/2020-12/schema"}
	schemas := []string{
		`{"type":"integer","minimum":2,"maximum":10}`,
		`{"type":"number","multipleOf":0.1}`,
		`{"type":"string","pattern":"^[a-z]+$","minLength":2,"maxLength":6}`,
		`{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"],"additionalProperties":false}`,
		`{"type":"array","items":{"type":"number"},"uniqueItems":true,"minItems":1}`,
		`{"oneOf":[{"type":"string"},{"type":"number"}]}`,
		`{"allOf":[{"type":"number"},{"not":{"enum":[2,4]}}]}`,
		`{"if":{"type":"string"},"then":{"minLength":3},"else":{"type":"boolean"}}`,
		`{"definitions":{"value":{"type":"integer"}},"$ref":"#/definitions/value"}`,
		`{"properties":{"x":{"type":"number"}},"unevaluatedProperties":false}`,
		`{"type":42}`, `{"minimum":"bad"}`, `{"pattern":"["}`, `{"required":[1]}`,
		`{"$ref":"https://unavailable.invalid/schema.json"}`,
	}
	values := []string{`null`, `true`, `false`, `0`, `1`, `2`, `10`, `11`, `0.3`, `0.31`, `9007199254740993`, `"a"`, `"abc"`, `"TOOLONG"`, `[]`, `[1]`, `[1,1]`, `["x"]`, `{}`, `{"x":2}`, `{"x":"wrong"}`, `{"x":2,"other":3}`}
	for _, dialect := range dialects {
		for i, body := range schemas {
			t.Run(fmt.Sprintf("%s/%d", dialect, i), func(t *testing.T) {
				var object map[string]any
				if err := json.Unmarshal([]byte(body), &object); err != nil {
					t.Fatal(err)
				}
				object["$schema"] = dialect
				data, _ := json.Marshal(object)
				local := private.NewCompiler()
				oracle := upstream.NewCompiler()
				deny := func(string) (io.ReadCloser, error) { return nil, fmt.Errorf("resource not bundled") }
				local.LoadURL, oracle.LoadURL = deny, deny
				const location = "https://dircue.invalid/parity.json"
				if err := local.AddResource(location, strings.NewReader(string(data))); err != nil {
					t.Fatal(err)
				}
				if err := oracle.AddResource(location, strings.NewReader(string(data))); err != nil {
					t.Fatal(err)
				}
				a, ae := local.Compile(location)
				b, be := oracle.Compile(location)
				if (ae == nil) != (be == nil) {
					t.Fatalf("schema acceptance differs: private=%v upstream=%v", ae, be)
				}
				if ae != nil {
					return
				}
				for _, raw := range values {
					decoder := json.NewDecoder(strings.NewReader(raw))
					decoder.UseNumber()
					var value any
					if err := decoder.Decode(&value); err != nil {
						t.Fatal(err)
					}
					if (a.Validate(value) == nil) != (b.Validate(value) == nil) {
						t.Fatalf("instance acceptance differs for %s", raw)
					}
				}
			})
		}
	}
}

func TestEnumErrorLiteralPercent(t *testing.T) {
	schema, err := private.CompileString("https://dircue.invalid/enum.json", `{"enum":["100% done","%s"]}`)
	if err != nil {
		t.Fatal(err)
	}
	err = schema.Validate("other")
	if err == nil || !strings.Contains(err.Error(), "100% done") || strings.Contains(err.Error(), "%!") || !strings.Contains(err.Error(), "%s") {
		t.Fatalf("enum error text must remain literal: %v", err)
	}
}
