package packageevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// Reject duplicate keys before decoding into structs, where the last duplicate
// would otherwise silently replace evidence or limits. Unknown fields are still
// checked for valid, bounded JSON even though the supported importer omits them.
func validateJSON(ctx context.Context, data []byte, limits Limits) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	tokens := 0
	locations := 0
	var value func(int, string) error
	value = func(depth int, field string) error {
		if depth > 64 {
			return ErrLimit
		}
		tokens++
		if tokens > 2000000 {
			return ErrLimit
		}
		if tokens%256 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		token, err := decoder.Token()
		if err != nil {
			return ErrInvalid
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return ErrInvalid
				}
				s, ok := key.(string)
				if !ok || keys[s] {
					return ErrInvalid
				}
				keys[s] = true
				if len(keys) > 100000 {
					return ErrLimit
				}
				childField := ""
				if depth == 0 || (depth == 2 && field == "artifacts" && s == "locations") {
					childField = s
				}
				if err := value(depth+1, childField); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return ErrInvalid
			}
		case '[':
			count := 0
			for decoder.More() {
				count++
				if depth == 1 {
					switch field {
					case "artifacts":
						if count > limits.Artifacts {
							return ErrLimit
						}
					case "artifactRelationships":
						if count > limits.Relationships {
							return ErrLimit
						}
					case "files":
						if count > limits.Files {
							return ErrLimit
						}
						locations++
					}
				}
				if depth == 3 && field == "locations" {
					locations++
				}
				if locations > limits.Locations {
					return ErrLimit
				}
				if err := value(depth+1, field); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := value(0, ""); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalid
	}
	return ctx.Err()
}
