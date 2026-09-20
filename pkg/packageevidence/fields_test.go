package packageevidence

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func originalCanonical(fields map[string]json.RawMessage, shape reflect.Type) bool {
	for i := 0; i < shape.NumField(); i++ {
		name := strings.Split(shape.Field(i).Tag.Get("json"), ",")[0]
		for key := range fields {
			if key != name && strings.EqualFold(key, name) {
				return false
			}
		}
	}
	return true
}
func checkTable[T any](t *testing.T) {
	t.Helper()
	var table fieldTable[T]
	if table.names != nil {
		t.Fatal("table should begin uninitialized")
	}
	shape := reflect.TypeFor[T]()
	for i := 0; i < shape.NumField(); i++ {
		name := strings.Split(shape.Field(i).Tag.Get("json"), ",")[0]
		cases := []map[string]json.RawMessage{{name: json.RawMessage(`null`)}, {strings.ToUpper(name): json.RawMessage(`null`)}, {name: json.RawMessage(`null`), strings.ToUpper(name): json.RawMessage(`null`)}, {"unrelated-key": json.RawMessage(`null`)}, {"ſource": json.RawMessage(`null`)}, {"acceſsPath": json.RawMessage(`null`)}, {"Key": json.RawMessage(`null`)}, {"İd": json.RawMessage(`null`)}}
		for _, fields := range cases {
			if got, want := canonicalFields(fields, &table), originalCanonical(fields, shape); got != want {
				t.Fatalf("shape %v fields%v got%v want%v", shape, fields, got, want)
			}
		}
	}
	if len(table.names) != shape.NumField() {
		t.Fatal("wrong field count")
	}
	for i, name := range table.names {
		want := strings.Split(shape.Field(i).Tag.Get("json"), ",")[0]
		if name != want {
			t.Fatal("field order/tag differs")
		}
	}
	fields := map[string]json.RawMessage{"unrelated-key": json.RawMessage(`null`)}
	if allocs := testing.AllocsPerRun(100, func() { canonicalFields(fields, &table) }); allocs != 0 {
		t.Fatalf("warm table allocates %g", allocs)
	}
}
func TestFixedFieldTablesMatchReflection(t *testing.T) {
	checkTable[nativeDocument](t)
	checkTable[nativeSchema](t)
	checkTable[nativeDescriptor](t)
	checkTable[nativeSource](t)
	checkTable[nativePackage](t)
	checkTable[nativeLocation](t)
	checkTable[nativeFile](t)
	checkTable[nativeRelationship](t)
}
func TestFieldTableConcurrentFirstUse(t *testing.T) {
	var table fieldTable[nativePackage]
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				if !canonicalFields(map[string]json.RawMessage{"name": json.RawMessage(`"example"`)}, &table) {
					t.Error("canonical spelling rejected")
				}
				if canonicalFields(map[string]json.RawMessage{"Name": json.RawMessage(`"example"`)}, &table) {
					t.Error("alias accepted")
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(table.names) != reflect.TypeFor[nativePackage]().NumField() {
		t.Fatal("table initialization incomplete")
	}
	first := &table.names[0]
	canonicalFields(nil, &table)
	if &table.names[0] != first {
		t.Fatal("table was rebuilt")
	}
}
func TestFieldTableRetainsUnicodeFoldSemantics(t *testing.T) {
	type shape struct {
		Key   string `json:"key"`
		Scope string `json:"scope,omitempty"`
	}
	var table fieldTable[shape]
	for _, key := range []string{"Key", "ſcope", "KEY", "SCOPE"} {
		if canonicalFields(map[string]json.RawMessage{key: nil}, &table) {
			t.Fatalf("accepted alias %q", key)
		}
	}
	for _, key := range []string{"key", "scope", "İd", "unknown"} {
		if !canonicalFields(map[string]json.RawMessage{key: nil}, &table) {
			t.Fatalf("rejected unrelated/canonical %q", key)
		}
	}
}
