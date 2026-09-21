package structure

import (
	"bytes"
	"dircue/internal/jsontext"
	"encoding/json"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const FunctionLimit = 128
const FunctionNameMaxBytes = 256
const MaxFunctionMetricBytes = 64 << 10

var functionMetricGroups = []string{"abc", "cognitive", "cyclomatic", "halstead", "loc", "mi", "nargs", "nexits", "nom", "tokens"}

// FunctionMetricGroups returns the enabled groups for BCA Function-kind spaces.
func FunctionMetricGroups() []string { return append([]string(nil), functionMetricGroups...) }

// FunctionEntry is a provider-defined function space, including nested spaces in
// its metrics. Its line range is one-based and inclusive.
type FunctionEntry struct {
	Index      uint64          `json:"index"`
	Name       string          `json:"name,omitempty"`
	NameStatus string          `json:"name_status"`
	StartLine  uint64          `json:"start_line"`
	EndLine    uint64          `json:"end_line"`
	Metrics    json.RawMessage `json:"metrics"`
}

type FunctionSpaces struct {
	Provider          string          `json:"provider"`
	Rule              string          `json:"rule"`
	RuleVersion       string          `json:"rule_version"`
	Scope             string          `json:"scope"`
	Status            string          `json:"status"`
	SyntaxErrors      bool            `json:"syntax_errors"`
	MetricScope       string          `json:"metric_scope"`
	Order             string          `json:"order"`
	Limit             int             `json:"limit"`
	NameMaxBytes      int             `json:"name_max_bytes"`
	TotalSpaces       uint64          `json:"total_spaces"`
	OmittedSpaces     uint64          `json:"omitted_spaces"`
	InvalidSpanSpaces uint64          `json:"invalid_span_spaces"`
	Entries           []FunctionEntry `json:"entries"`
}

func decodeFunctions(data []byte, content []byte, syntaxErrors bool, syntaxNodes uint64) (*FunctionSpaces, bool) {
	if !jsontext.ValidUnicode(data) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !functionJSONValue(decoder, 0) {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return decodeValidatedFunctions(data, content, syntaxErrors, syntaxNodes)
}

// The caller must first validate this exact JSON subtree's UTF-8, framing,
// duplicate keys and depth, either here or as part of strictFunctionResponse.
// The enclosing response's depth limit is at least as strict as the subtree's.
func decodeValidatedFunctions(data []byte, content []byte, syntaxErrors bool, syntaxNodes uint64) (*FunctionSpaces, bool) {
	var object map[string]json.RawMessage
	required := []string{"provider", "rule", "rule_version", "scope", "status", "syntax_errors", "metric_scope", "order", "limit", "name_max_bytes", "total_spaces", "omitted_spaces", "invalid_span_spaces", "entries"}
	if json.Unmarshal(data, &object) != nil || !functionKeys(object, required, "") {
		return nil, false
	}
	if !boundedFunctionEntries(object["entries"]) {
		return nil, false
	}
	var f FunctionSpaces
	if json.Unmarshal(data, &f) != nil || f.Entries == nil {
		return nil, false
	}
	if f.Provider != "big-code-analysis@2.2.0" || f.Rule != "space-kind-function" || f.RuleVersion != "1.0.0" || f.Scope != "file" || f.MetricScope != "includes_nested_spaces" || f.Order != "provider_preorder" || f.Limit != FunctionLimit || f.NameMaxBytes != FunctionNameMaxBytes || f.SyntaxErrors != syntaxErrors {
		return nil, false
	}
	if f.Status != "complete" && f.Status != "partial" {
		return nil, false
	}
	// BCA's metrics walk opens at most one space per syntax node. Its only
	// synthetic frame is Unit-kind, so it cannot add a Function-kind space.
	if f.TotalSpaces > syntaxNodes || uint64(len(f.Entries)) > f.TotalSpaces || f.InvalidSpanSpaces > f.TotalSpaces-uint64(len(f.Entries)) || f.OmittedSpaces != f.TotalSpaces-uint64(len(f.Entries))-f.InvalidSpanSpaces || len(f.Entries) > FunctionLimit {
		return nil, false
	}
	validSpaces := f.TotalSpaces - f.InvalidSpanSpaces
	if uint64(len(f.Entries)) != min(validSpaces, uint64(FunctionLimit)) || f.OmittedSpaces != validSpaces-uint64(len(f.Entries)) {
		return nil, false
	}
	lines := uint64(bytes.Count(content, []byte{'\n'}))
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}
	var rawEntries []map[string]json.RawMessage
	if json.Unmarshal(object["entries"], &rawEntries) != nil {
		return nil, false
	}
	partial := f.SyntaxErrors || f.OmittedSpaces != 0 || f.InvalidSpanSpaces != 0
	previous := uint64(0)
	for index := range f.Entries {
		entry := &f.Entries[index]
		raw := rawEntries[index]
		if !functionKeys(raw, []string{"index", "name_status", "start_line", "end_line", "metrics"}, "name") || entry.Index <= previous || entry.Index > f.TotalSpaces || entry.Index-uint64(index+1) > f.InvalidSpanSpaces || entry.StartLine < 1 || entry.EndLine < entry.StartLine || entry.EndLine > lines || len(entry.Metrics) > MaxFunctionMetricBytes {
			return nil, false
		}
		previous = entry.Index
		_, named := raw["name"]
		switch entry.NameStatus {
		case "present":
			if !named || entry.Name == "" || len(entry.Name) > FunctionNameMaxBytes || !utf8.ValidString(entry.Name) {
				return nil, false
			}
			for _, r := range entry.Name {
				if unicode.IsControl(r) {
					return nil, false
				}
			}
		case "unavailable":
			if named {
				return nil, false
			}
		case "omitted":
			if named {
				return nil, false
			}
			partial = true
		default:
			return nil, false
		}
		metrics, ok := decodeMetrics(entry.Metrics, functionMetricGroups)
		if !ok {
			return nil, false
		}
		entry.Metrics, _ = json.Marshal(metrics)
	}
	if (f.Status == "partial") != partial {
		return nil, false
	}
	return &f, true
}

func functionKeys(object map[string]json.RawMessage, required []string, optional string) bool {
	if len(object) < len(required) || len(object) > len(required)+1 {
		return false
	}
	for _, name := range required {
		raw, ok := object[name]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	if len(object) != len(required) {
		if optional == "" {
			return false
		}
		if _, ok := object[optional]; !ok {
			return false
		}
	}
	return true
}

// Reject duplicate keys before typed decoding, including keys inside metrics.
func functionJSONValue(decoder *json.Decoder, depth int) bool {
	if depth > 40 {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return false
			}
			keys[key] = true
			if !functionJSONValue(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !functionJSONValue(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

// The opt-in response is decoded into typed structs. Reject JSON duplicates and
// aliases that encoding/json would otherwise match without regard to case.
func strictFunctionResponse(data []byte) bool {
	if !jsontext.ValidUnicode(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !functionJSONValue(decoder, 0) {
		return false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	if !functionFieldSpelling(object, []string{"path", "language", "status", "reason", "source_bytes", "parse_count", "syntax_errors", "observations", "metrics", "provenance", "timings_ns", "functions", "hotspots", "source_sha256"}) {
		return false
	}
	var provenance map[string]json.RawMessage
	if json.Unmarshal(object["provenance"], &provenance) != nil {
		return false
	}
	return functionFieldSpelling(provenance, []string{"bca", "tree_sitter", "grammar"})
}

func functionFieldSpelling(object map[string]json.RawMessage, fields []string) bool {
	for key := range object {
		known := false
		for _, field := range fields {
			if key == field {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

// Check array cardinality before decoding entries into the larger public struct.
func boundedFunctionEntries(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return false
	}
	count := 0
	for decoder.More() {
		if count == FunctionLimit {
			return false
		}
		var entry json.RawMessage
		if decoder.Decode(&entry) != nil {
			return false
		}
		count++
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim(']')
}

func unsupportedFunctionRequest(data []byte) bool {
	var failure struct {
		Status     string                         `json:"status"`
		ParseCount *int                           `json:"parse_count"`
		Error      struct{ Code, Message string } `json:"error"`
	}
	return json.Unmarshal(data, &failure) == nil && failure.Status == "error" && failure.ParseCount != nil && *failure.ParseCount == 0 && failure.Error.Code == "invalid_request" && strings.HasPrefix(failure.Error.Message, "unknown field `functions`")
}
