package declarations

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"dircue/internal/jsontext"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

func NewDocument(name, kind string) *Document {
	return &Document{Project: &Project{ID: name, Root: path.Dir(name), Kind: kind, Requirements: []Requirement{}, References: []Reference{}, Interfaces: []Interface{}}, Diagnostics: []Diagnostic{}, Parsed: true}
}

func AddDiagnostic(d *Document, code, message string) {
	if d == nil || len(d.Diagnostics) >= 32 {
		return
	}
	p := "."
	if d.Project != nil {
		p = d.Project.ID
	}
	for _, v := range d.Diagnostics {
		if v.Code == code && v.Message == message {
			return
		}
	}
	d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: p, Code: code, Message: message})
}

func reserve(d *Document, fields ...string) bool {
	if d == nil || d.Project == nil || d.limited {
		return false
	}
	n := 0
	for _, s := range fields {
		if len(s) > MaxStringBytes || !utf8.ValidString(s) {
			d.limited = true
			AddDiagnostic(d, "declaration-limit", "A declaration exceeds the supported text limit.")
			return false
		}
		n += len(s)
	}
	p := d.Project
	if len(p.Requirements)+len(p.References)+len(p.Interfaces) >= MaxObservationsPerManifest || n > (1<<20)-d.retainedBytes {
		d.limited = true
		AddDiagnostic(d, "declaration-limit", "The manifest observation or retained-text limit was reached.")
		return false
	}
	d.retainedBytes += n
	return true
}

func AddRequirement(d *Document, v Requirement) bool {
	if !reserve(d, v.Kind, v.Value, v.State, v.Evidence, v.Condition) {
		return false
	}
	d.Project.Requirements = append(d.Project.Requirements, v)
	return true
}
func AddReference(d *Document, v Reference) bool {
	if !reserve(d, v.Kind, v.Value, v.Target, v.State, v.TargetStatus, v.Evidence, v.Condition) {
		return false
	}
	d.Project.References = append(d.Project.References, v)
	return true
}
func AddInterface(d *Document, v Interface) bool {
	if !reserve(d, v.Kind, v.Name, v.Target, v.State, v.Evidence, v.Condition) {
		return false
	}
	d.Project.Interfaces = append(d.Project.Interfaces, v)
	return true
}

// LocalTarget joins a declaration to its containing directory, never the host.
// Absolute, drive-qualified, URI and escaping paths have no in-scope target.
func LocalTarget(manifest, rawDir, basename string) (string, bool) {
	if len(rawDir) > MaxStringBytes || strings.ContainsAny(rawDir, ":\x00") || strings.IndexFunc(rawDir, unicode.IsControl) >= 0 {
		return "", false
	}
	rawDir = strings.ReplaceAll(rawDir, "\\", "/")
	if strings.HasPrefix(rawDir, "/") || rawDir == "" {
		return "", false
	}
	target := path.Join(path.Dir(manifest), rawDir, basename)
	if target == ".." || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
		return "", false
	}
	return target, true
}

// MatchPattern implements a bounded path glob subset: *, ?, character classes,
// and ** as a complete segment. Richer package-manager dialects are rejected.
func MatchPattern(pattern, relative string) (bool, error) {
	if len(pattern) > 1024 || len(relative) > MaxStringBytes || strings.ContainsAny(pattern, "{}!\\\x00") || strings.Contains(pattern, "@(") || strings.Contains(pattern, "+(") || strings.Contains(pattern, "?(") || strings.Contains(pattern, "*(") || strings.HasPrefix(pattern, "/") {
		return false, errors.New("unsupported workspace pattern")
	}
	pattern = strings.TrimPrefix(pattern, "./")
	ps, rs := strings.Split(strings.TrimSuffix(pattern, "/"), "/"), strings.Split(relative, "/")
	if len(ps) > 64 || len(rs) > 128 {
		return false, errors.New("workspace pattern depth limit")
	}
	for _, s := range ps {
		if s == ".." || s == "" || s == "." || (s != "**" && strings.Contains(s, "**")) {
			return false, errors.New("unsupported workspace pattern")
		}
		if s != "**" {
			if _, err := path.Match(s, ""); err != nil {
				return false, errors.New("invalid workspace pattern")
			}
		}
	}
	previous := make([]bool, len(rs)+1)
	previous[0] = true
	for _, p := range ps {
		next := make([]bool, len(rs)+1)
		if p == "**" {
			next[0] = previous[0]
			for j := 1; j <= len(rs); j++ {
				next[j] = previous[j] || next[j-1]
			}
		} else {
			for j := 1; j <= len(rs); j++ {
				matched, _ := path.Match(p, rs[j-1])
				next[j] = previous[j-1] && matched
			}
		}
		previous = next
	}
	return previous[len(rs)], nil
}

var errInvalidDocument = errors.New("manifest is invalid or exceeds parser limits")

// ValidateJSON rejects ambiguous duplicate keys before an adapter reads fields.
func ValidateJSON(content []byte) (map[string]any, error) {
	if int64(len(content)) > MaxManifestBytes || !jsontext.ValidUnicode(content) {
		return nil, errInvalidDocument
	}
	d := json.NewDecoder(bytes.NewReader(content))
	d.UseNumber()
	nodes := 0
	value, err := readJSONValue(d, 0, &nodes)
	if err != nil {
		return nil, errInvalidDocument
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errInvalidDocument
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errInvalidDocument
	}
	return object, nil
}

func readJSONValue(d *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 64 || *nodes > 100000 {
		return nil, errInvalidDocument
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if s, ok := t.(string); ok && len(s) > 65536 {
		return nil, errInvalidDocument
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			k, ok := key.(string)
			if !ok || len(k) > MaxStringBytes {
				return nil, errInvalidDocument
			}
			if _, exists := m[k]; exists {
				return nil, errInvalidDocument
			}
			v, err := readJSONValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		if t, err := d.Token(); err != nil || t != json.Delim('}') {
			return nil, errInvalidDocument
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			v, err := readJSONValue(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		if t, err := d.Token(); err != nil || t != json.Delim(']') {
			return nil, errInvalidDocument
		}
		return a, nil
	}
	return nil, errInvalidDocument
}

func ValidateTOML(content []byte) (map[string]any, error) {
	if int64(len(content)) > MaxManifestBytes || !utf8.Valid(content) {
		return nil, errInvalidDocument
	}
	// Inspect the upstream expression AST before map decoding expands dotted
	// keys into nested maps. An input-sized token arena is cheaper than building
	// hundreds of thousands of maps only to reject their depth afterward.
	var parser unstable.Parser
	parser.Reset(content)
	nodeCount, tableDepth := 0, 0
	var inspect func(*unstable.Node, int) bool
	inspect = func(node *unstable.Node, depth int) bool {
		nodeCount++
		if depth > 64 || nodeCount > 100000 {
			return false
		}
		switch node.Kind {
		case unstable.KeyValue:
			keys := node.Key()
			count := 0
			for keys.Next() {
				count++
				nodeCount++
				if count+depth > 64 || nodeCount > 100000 || len(keys.Node().Data) > MaxStringBytes {
					return false
				}
			}
			return inspect(node.Value(), depth+count)
		case unstable.Array, unstable.InlineTable:
			children := node.Children()
			for children.Next() {
				if !inspect(children.Node(), depth+1) {
					return false
				}
			}
		default:
			if len(node.Data) > 65536 {
				return false
			}
		}
		return true
	}
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable {
			tableDepth = 0
			keys := expression.Key()
			for keys.Next() {
				tableDepth++
				nodeCount++
				if tableDepth > 64 || nodeCount > 100000 || len(keys.Node().Data) > MaxStringBytes {
					return nil, errInvalidDocument
				}
			}
			continue
		}
		if !inspect(expression, tableDepth) {
			return nil, errInvalidDocument
		}
	}
	if parser.Error() != nil {
		return nil, errInvalidDocument
	}
	var m map[string]any
	if err := toml.Unmarshal(content, &m); err != nil {
		return nil, errInvalidDocument
	}
	nodes := 0
	var check func(any, int) bool
	check = func(v any, depth int) bool {
		nodes++
		if depth > 64 || nodes > 100000 {
			return false
		}
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if len(k) > MaxStringBytes || !check(e, depth+1) {
					return false
				}
			}
		case []any:
			for _, e := range x {
				if !check(e, depth+1) {
					return false
				}
			}
		case string:
			if len(x) > 65536 {
				return false
			}
		}
		return true
	}
	if !check(m, 0) {
		return nil, errInvalidDocument
	}
	return m, nil
}
