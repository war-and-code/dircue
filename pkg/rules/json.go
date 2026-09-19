package rules

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject them
// before decoding so a policy's spelling cannot change during compilation.
func validUnicode(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	quoted := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		value, ok := hex4(data[i+1:])
		if !ok || value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		i += 4
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, ok := hex4(data[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !quoted
}

func hex4(data []byte) (uint16, bool) {
	if len(data) < 4 {
		return 0, false
	}
	var value uint16
	for _, b := range data[:4] {
		value <<= 4
		switch {
		case b >= '0' && b <= '9':
			value += uint16(b - '0')
		case b >= 'a' && b <= 'f':
			value += uint16(b-'a') + 10
		case b >= 'A' && b <= 'F':
			value += uint16(b-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func strictJSON(data []byte) bool {
	if !validUnicode(data) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	budget := 32768
	if !jsonValue(d, 0, &budget) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func jsonValue(d *json.Decoder, depth int, budget *int) bool {
	*budget -= 1
	if depth > 8 || *budget < 0 {
		return false
	}
	token, err := d.Token()
	if err != nil || token == nil {
		return false
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return true
	}
	switch delim {
	case '{':
		keys := make(map[string]bool)
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || keys[key] {
				return false
			}
			keys[key] = true
			if !jsonValue(d, depth+1, budget) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for d.More() {
			if !jsonValue(d, depth+1, budget) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func object(data []byte, required []string, optional ...string) (map[string]json.RawMessage, bool) {
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) != nil || result == nil {
		return nil, false
	}
	allowed := make(map[string]bool)
	for _, key := range required {
		if _, ok := result[key]; !ok {
			return nil, false
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key := range result {
		if !allowed[key] {
			return nil, false
		}
	}
	return result, true
}

func array(data []byte, maximum int) ([]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, false
	}
	result := []json.RawMessage{}
	for d.More() {
		if len(result) == maximum {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		result = append(result, value)
	}
	token, err = d.Token()
	return result, err == nil && token == json.Delim(']')
}
