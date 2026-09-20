package packageevidence

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
)

type nativeSchema struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}
type nativeDescriptor struct {
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Configuration json.RawMessage `json:"configuration"`
}
type nativeSource struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Metadata json.RawMessage `json:"metadata"`
}
type nativeFile struct {
	ID       string         `json:"id"`
	Location nativeLocation `json:"location"`
}
type nativeRelationship struct {
	Parent   string          `json:"parent"`
	Child    string          `json:"child"`
	Type     string          `json:"type"`
	Metadata json.RawMessage `json:"metadata"`
}

// The importer uses eight fixed shapes. Zero-valued tables do no reflection or
// allocation until their shape is checked; names are immutable after first use.
// Deriving names from the actual type keeps JSON tags and alias checks together.
type fieldTable[T any] struct {
	once  sync.Once
	names []string
}

var (
	documentFields        fieldTable[nativeDocument]
	schemaFieldsTable     fieldTable[nativeSchema]
	descriptorFieldsTable fieldTable[nativeDescriptor]
	sourceFieldsTable     fieldTable[nativeSource]
	packageFields         fieldTable[nativePackage]
	locationFieldsTable   fieldTable[nativeLocation]
	fileFieldsTable       fieldTable[nativeFile]
	relationshipFields    fieldTable[nativeRelationship]
)

// encoding/json folds struct field names, while native Syft field names are
// case-sensitive. Reject aliases before they can replace canonical evidence.
// Arbitrary metadata objects retain their own case-sensitive key namespace.
func canonicalFields[T any](fields map[string]json.RawMessage, table *fieldTable[T]) bool {
	table.once.Do(func() {
		shape := reflect.TypeFor[T]()
		names := make([]string, shape.NumField())
		for i := range names {
			names[i], _, _ = strings.Cut(shape.Field(i).Tag.Get("json"), ",")
		}
		table.names = names
	})
	for _, name := range table.names {
		for key := range fields {
			if key != name && strings.EqualFold(key, name) {
				return false
			}
		}
	}
	return true
}
