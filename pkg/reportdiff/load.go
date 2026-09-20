package reportdiff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"dircue/internal/jsontext"
	"dircue/pkg/profile"
	"dircue/schema"
)

// Numeric tokens remain exact json.Number values. These bounds prevent the
// schema validator's rational arithmetic from amplifying tiny exponent inputs.
const MaxNumberBytes = 128
const MaxNumberExponent = 1024

// Load accepts aggregate dircue profiles, not Linguist's language-only JSON.
// Error text deliberately excludes payload values and parser excerpts.
func Load(reader io.Reader) (*Snapshot, error) {
	if reader == nil {
		return nil, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxInputBytes+1))
	if err != nil {
		return nil, ErrInvalid
	}
	if len(data) > MaxInputBytes {
		return nil, ErrLimit
	}
	if !utf8.Valid(data) || !jsontext.ValidUnicode(data) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	nodes := 0
	value, err := readValue(d, 0, &nodes)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	version, ok := object["schema_version"].(string)
	if !ok {
		return nil, ErrInvalid
	}
	level := -1
	for i, known := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		if version == known {
			level = i
		}
	}
	if level < 0 {
		return nil, ErrUnsupported
	}
	for field, minimum := range map[string]int{"metrics": 1, "projects": 2, "structure": 2, "discovery": 3, "graph": 3, "package_evidence": 3, "registries": 3, "rules": 3, "declarations": 4} {
		if _, found := object[field]; found && level < minimum {
			return nil, ErrInvalid
		}
	}
	if !validShape(value, reflect.TypeFor[profile.Report]()) {
		return nil, ErrInvalid
	}
	if schema.ValidateProfile(value) != nil {
		return nil, ErrInvalid
	}
	var result profile.Report
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, ErrInvalid
	}
	if !validStates(result) {
		return nil, ErrInvalid
	}
	digest := sha256.Sum256(data)
	return &Snapshot{profile: result, sha256: hex.EncodeToString(digest[:]), bytes: int64(len(data)), validated: true}, nil
}

func readValue(d *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > MaxJSONDepth || *nodes > MaxJSONNodes {
		return nil, ErrLimit
	}
	token, err := d.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	if value, ok := token.(string); ok {
		if len(value) > MaxStringBytes {
			return nil, ErrLimit
		}
		return value, nil
	}
	if number, ok := token.(json.Number); ok {
		if err := boundedNumber(number); err != nil {
			return nil, err
		}
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delimiter {
	case '{':
		result := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, ErrInvalid
			}
			name, ok := key.(string)
			if !ok || len(name) > MaxStringBytes {
				return nil, ErrLimit
			}
			if _, found := result[name]; found {
				return nil, ErrInvalid
			}
			value, err := readValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result[name] = value
		}
		if token, err := d.Token(); err != nil || token != json.Delim('}') {
			return nil, ErrInvalid
		}
		return result, nil
	case '[':
		result := []any{}
		for d.More() {
			value, err := readValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if token, err := d.Token(); err != nil || token != json.Delim(']') {
			return nil, ErrInvalid
		}
		return result, nil
	}
	return nil, ErrInvalid
}

func boundedNumber(number json.Number) error {
	raw := string(number)
	if len(raw) > MaxNumberBytes {
		return ErrLimit
	}
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		exponent, err := strconv.ParseInt(raw[i+1:], 10, 32)
		if err != nil || exponent < -MaxNumberExponent || exponent > MaxNumberExponent {
			return ErrLimit
		}
	}
	return nil
}

type fieldType struct {
	value    reflect.Type
	optional bool
}

func shapeFields(typ reflect.Type, fields map[string]fieldType) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			shapeFields(field.Type, fields)
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = fieldType{field.Type, strings.Contains(options, "omitempty")}
	}
}

// Exact field names prevent encoding/json's case-insensitive alias matching.
// Required fields cannot silently acquire zero values from missing JSON.
func validShape(value any, typ reflect.Type) bool {
	if value == nil {
		return false
	}
	if typ.Kind() == reflect.Pointer {
		return validShape(value, typ.Elem())
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]fieldType{}
		shapeFields(typ, fields)
		for name, field := range fields {
			v, found := object[name]
			if !found {
				if !field.optional {
					return false
				}
				continue
			}
			if !validShape(v, field.value) {
				return false
			}
		}
		for name := range object {
			if _, found := fields[name]; !found {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return false
		}
		for _, v := range array {
			if !validShape(v, typ.Elem()) {
				return false
			}
		}
		return true
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok || typ.Key().Kind() != reflect.String {
			return false
		}
		for _, v := range object {
			if !validShape(v, typ.Elem()) {
				return false
			}
		}
		return true
	case reflect.String:
		_, ok := value.(string)
		return ok
	case reflect.Bool:
		_, ok := value.(bool)
		return ok
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		n, err := strconv.ParseInt(string(number), 10, typ.Bits())
		return err == nil && n >= 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := strconv.ParseUint(string(number), 10, typ.Bits())
		return err == nil
	case reflect.Float32, reflect.Float64:
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		n, err := strconv.ParseFloat(string(number), typ.Bits())
		if err != nil || n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
			return false
		}
		if n == 0 {
			coefficient := string(number)
			if i := strings.IndexAny(coefficient, "eE"); i >= 0 {
				coefficient = coefficient[:i]
			}
			// A producer can emit a subnormal float, but cannot emit a nonzero
			// value that would disappear when decoded into this float field.
			if strings.ContainsAny(coefficient, "123456789") {
				return false
			}
		}
		return true
	}
	return false
}

func validStates(p profile.Report) bool {
	status := func(s string) bool {
		return s == "complete" || s == "partial" || s == "skipped" || s == "not_applicable"
	}
	if p.Declarations != nil && (!status(p.Declarations.Status) || !sourceMode(p.Declarations.Source)) {
		return false
	}
	if p.Projects != nil && (!status(p.Projects.Status) || !sourceMode(p.Projects.Source)) {
		return false
	}
	if p.Registries != nil && (!status(p.Registries.Status) || !sourceMode(p.Registries.Source.Mode)) {
		return false
	}
	if p.Discovery != nil && (!status(p.Discovery.Status) || !sourceMode(p.Discovery.Source.Mode)) {
		return false
	}
	if p.Metrics != nil && (!status(p.Metrics.Status) || !sourceMode(p.Metrics.Source)) {
		return false
	}
	return true
}

func sourceMode(mode string) bool { return mode == "git" || mode == "directory" }
