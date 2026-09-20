package structure

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func functionEnvelope(raw []byte) []byte {
	return append(append([]byte(`{"provenance":{},"functions":`), raw...), '}')
}

// The standalone decoder retains the original precheck. The production caller
// may reuse a stricter enclosing check only when it proves that precondition.
func compareValidatedFunctions(t *testing.T, envelope, content []byte, syntax bool, nodes uint64) {
	t.Helper()
	if !strictFunctionResponse(envelope) {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["functions"]; !exists {
		return
	}
	previous, oldOK := decodeFunctions(raw["functions"], content, syntax, nodes)
	current, newOK := decodeValidatedFunctions(raw["functions"], content, syntax, nodes)
	if oldOK != newOK || !reflect.DeepEqual(previous, current) {
		t.Fatalf("validation reuse changed acceptance or decoded value: old=%v new=%v", oldOK, newOK)
	}
}

func TestFunctionValidationReuseBoundaries(t *testing.T) {
	raw, err := json.Marshal(validFunctions())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("def f():\n    return 0\n")
	valid := functionEnvelope(raw)
	if !strictFunctionResponse(valid) {
		t.Fatal("valid envelope rejected")
	}
	compareValidatedFunctions(t, valid, content, false, 100)
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte(`"limit":128`), []byte(`"limit":2,"limit":128`), 1),
		bytes.Replace(raw, []byte(`"limit":128`), []byte(`"limit":128,"\u006cimit":128`), 1),
		bytes.Replace(raw, []byte(`"value":0`), []byte(`"value":0,"value":1`), 1),
		append(append([]byte(nil), raw...), []byte(` null`)...),
		bytes.Replace(raw, []byte(`"name":"f"`), []byte{'"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"'}, 1),
	} {
		if strictFunctionResponse(functionEnvelope(malformed)) {
			t.Fatal("malformed input reached the validated decoder")
		}
	}
	for _, replaced := range []struct{ old, new string }{
		{`"limit":128`, `"limit":129`},
		{`"index":1`, `"index":0`},
		{`"start_line":1`, `"start_line":-1`},
		{`"end_line":2`, `"end_line":18446744073709551616`},
		{`"value":0`, `"value":1e999`},
		{`"value":0`, `"value":true`},
		{`"name_status":"present"`, `"name_status":"unavailable"`},
		{`"name":"f"`, `"name":"f\u0000"`},
		{`"status":"complete"`, `"status":"partial"`},
		{`"syntax_errors":false`, `"syntax_errors":null`},
	} {
		altered := bytes.Replace(raw, []byte(replaced.old), []byte(replaced.new), 1)
		if bytes.Equal(altered, raw) {
			t.Fatal("mutation did not change seed")
		}
		if _, ok := decodeFunctions(altered, content, false, 100); ok {
			t.Fatalf("invalid seed accepted: %s", replaced.new)
		}
		compareValidatedFunctions(t, functionEnvelope(altered), content, false, 100)
	}
	// Verify the inherited depth precondition at both sides of the limit,
	// including unknown nested properties allowed by the envelope validator.
	for depth := 0; depth < 44; depth++ {
		envelope := []byte(`{"provenance":{},"functions":` + strings.Repeat(`{"x":`, depth) + `0` + strings.Repeat(`}`, depth) + `}`)
		compareValidatedFunctions(t, envelope, content, false, 100)
		if depth > 40 && strictFunctionResponse(envelope) {
			t.Fatal("depth limit weakened")
		}
	}
}

func FuzzFunctionValidationReuse(f *testing.F) {
	raw, _ := json.Marshal(validFunctions())
	for _, seed := range [][]byte{functionEnvelope(raw), functionEnvelope([]byte(`null`)),
		functionEnvelope([]byte(`{}`)), functionEnvelope([]byte(`{"x":[{},0,null]}`)),
		[]byte(`{"provenance":{},"functions":{"x":1,"\u0078":2}}`)} {
		f.Add(seed, []byte("def f():\n    return 0\n"), false, uint64(100))
	}
	f.Fuzz(func(t *testing.T, envelope, content []byte, syntax bool, nodes uint64) {
		if len(envelope) > 64<<10 || len(content) > 64<<10 {
			t.Skip()
		}
		compareValidatedFunctions(t, envelope, content, syntax, nodes)
	})
}
