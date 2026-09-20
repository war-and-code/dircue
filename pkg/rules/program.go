package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/bits"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type rule struct {
	id         string
	filenames  map[string]bool
	extensions map[string]bool
	prefixes   []string
	literal    []byte
}

type mask [MaxRules / 64]uint64

func (m *mask) add(index int) { m[index/64] |= 1 << (index % 64) }
func (m *mask) union(other mask) {
	for i := range m {
		m[i] |= other[i]
	}
}
func (m mask) indices(yield func(int)) {
	for group, word := range m {
		for word != 0 {
			yield(group*64 + bits.TrailingZeros64(word))
			word &= word - 1
		}
	}
}

// Program is immutable after Compile and can be shared by concurrent workers.
// No method exposes configuration text, literal values, or internal slices.
type Program struct {
	digest      string
	rules       []rule
	byFilename  map[string]mask
	byExtension map[string]mask
	prefixOnly  mask
}

func (p *Program) SHA256() string { return p.digest }
func (p *Program) RuleCount() int { return len(p.rules) }
func (p *Program) RuleIDs() []string {
	result := make([]string, len(p.rules))
	for i, r := range p.rules {
		result[i] = r.id
	}
	return result
}

func Compile(data []byte) (*Program, error) {
	fail := func(message string) (*Program, error) { return nil, fmt.Errorf("%w: %s", ErrConfig, message) }
	if len(data) == 0 || len(data) > MaxConfigBytes {
		return fail("configuration size is outside 1..262144 bytes")
	}
	if !strictJSON(data) {
		return fail("expected bounded UTF-8 JSON without nulls, duplicate keys, or unpaired Unicode escapes")
	}
	root, ok := object(data, []string{"schema_version", "rules"})
	if !ok {
		return fail("expected schema_version and rules fields")
	}
	var version string
	if json.Unmarshal(root["schema_version"], &version) != nil || version != ConfigVersion {
		return fail("unsupported schema_version")
	}
	entries, ok := array(root["rules"], MaxRules)
	if !ok || len(entries) == 0 {
		return fail("rules must contain 1..256 entries")
	}
	program := &Program{byFilename: map[string]mask{}, byExtension: map[string]mask{}}
	ids := map[string]bool{}
	total := 0
	for i, raw := range entries {
		fields, ok := object(raw, []string{"id", "match"}, "content")
		if !ok {
			return fail(fmt.Sprintf("rule %d has missing or unknown fields", i+1))
		}
		var id string
		if json.Unmarshal(fields["id"], &id) != nil || !validID(id) || ids[id] {
			return fail(fmt.Sprintf("rule %d has an invalid or duplicate id", i+1))
		}
		ids[id] = true
		selectors, ok := object(fields["match"], nil, "filenames", "extensions", "path_prefixes")
		if !ok || len(selectors) == 0 {
			return fail(fmt.Sprintf("rule %d requires a metadata selector", i+1))
		}
		r := rule{id: id}
		for _, name := range []string{"filenames", "extensions", "path_prefixes"} {
			raw, exists := selectors[name]
			if !exists {
				continue
			}
			values, valid := array(raw, MaxSelectorValues)
			if !valid || len(values) == 0 {
				return fail(fmt.Sprintf("rule %d selector %s requires 1..64 values", i+1, name))
			}
			total += len(values)
			if total > MaxTotalSelectors {
				return fail("total selector values exceed 4096")
			}
			set := map[string]bool{}
			for _, value := range values {
				var text string
				if json.Unmarshal(value, &text) != nil || !validSelector(name, text) || set[text] {
					return fail(fmt.Sprintf("rule %d has an invalid or duplicate %s value", i+1, name))
				}
				set[text] = true
			}
			switch name {
			case "filenames":
				r.filenames = set
			case "extensions":
				r.extensions = set
			case "path_prefixes":
				for value := range set {
					r.prefixes = append(r.prefixes, value)
				}
				slices.Sort(r.prefixes)
			}
		}
		if raw, exists := fields["content"]; exists {
			content, valid := object(raw, []string{"contains_utf8"})
			var literal string
			if !valid || json.Unmarshal(content["contains_utf8"], &literal) != nil || literal == "" || len(literal) > MaxLiteralBytes {
				return fail(fmt.Sprintf("rule %d requires a nonempty literal of at most 4096 UTF-8 bytes", i+1))
			}
			r.literal = []byte(literal)
		}
		program.rules = append(program.rules, r)
	}
	slices.SortFunc(program.rules, func(a, b rule) int { return strings.Compare(a.id, b.id) })
	// Each rule uses only one index anchor, so unioning buckets never duplicates
	// matches. Remaining selectors are still checked with AND semantics.
	for i, r := range program.rules {
		switch {
		case len(r.filenames) > 0:
			for name := range r.filenames {
				m := program.byFilename[name]
				m.add(i)
				program.byFilename[name] = m
			}
		case len(r.extensions) > 0:
			for name := range r.extensions {
				anchor := path.Ext(name)
				m := program.byExtension[anchor]
				m.add(i)
				program.byExtension[anchor] = m
			}
		default:
			program.prefixOnly.add(i)
		}
	}
	sum := sha256.Sum256(data)
	program.digest = hex.EncodeToString(sum[:])
	return program, nil
}

func validID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, b := range []byte(value) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' {
			continue
		}
		if i == 0 || b != '.' && b != '_' && b != '-' {
			return false
		}
	}
	return true
}

func validSelector(kind, value string) bool {
	if value == "" || len(value) > MaxPathBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	switch kind {
	case "filenames":
		return value != "." && value != ".." && !strings.Contains(value, "/")
	case "extensions":
		return len(value) > 1 && value[0] == '.' && !strings.Contains(value[1:], "/")
	case "path_prefixes":
		return strings.HasSuffix(value, "/") && validPath(strings.TrimSuffix(value, "/"))
	}
	return false
}

func validPath(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= MaxPathBytes && utf8.ValidString(value) &&
		!strings.ContainsAny(value, "\\\x00") && !(len(value) >= 2 && value[1] == ':') && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") && path.Clean(value) == value
}

func validateFile(file File) error {
	if file.Size < 0 || !validPath(file.Path) {
		return ErrFile
	}
	return nil
}

func (p *Program) candidates(file File) mask {
	base, ext := path.Base(file.Path), path.Ext(file.Path)
	selected := p.prefixOnly
	selected.union(p.byFilename[base])
	selected.union(p.byExtension[ext])
	var matching mask
	selected.indices(func(i int) {
		r := p.rules[i]
		if len(r.filenames) > 0 && !r.filenames[base] {
			return
		}
		if len(r.extensions) > 0 {
			found := false
			for suffix := range r.extensions {
				if strings.HasSuffix(base, suffix) {
					found = true
					break
				}
			}
			if !found {
				return
			}
		}
		if len(r.prefixes) > 0 {
			found := false
			for _, prefix := range r.prefixes {
				if strings.HasPrefix(file.Path, prefix) {
					found = true
					break
				}
			}
			if !found {
				return
			}
		}
		matching.add(i)
	})
	return matching
}

func (p *Program) MatchMetadata(file File) (Decision, error) {
	if err := validateFile(file); err != nil {
		return Decision{}, err
	}
	decision := Decision{Matches: []Observation{}, ContentRuleIDs: []string{}}
	p.candidates(file).indices(func(i int) {
		r := p.rules[i]
		if r.literal != nil {
			decision.ContentRuleIDs = append(decision.ContentRuleIDs, r.id)
		} else {
			decision.Matches = append(decision.Matches, Observation{Path: file.Path, RuleID: r.id, Evidence: "metadata", Size: file.Size})
		}
	})
	return decision, nil
}

func (p *Program) MatchContent(file File, content []byte) ([]Observation, error) {
	if err := validateFile(file); err != nil {
		return nil, err
	}
	if file.Size > MaxContentBytes || len(content) > MaxContentBytes {
		return nil, ErrContentTooLarge
	}
	if int64(len(content)) != file.Size {
		return nil, ErrIncomplete
	}
	if !utf8.Valid(content) {
		return nil, ErrInvalidUTF8
	}
	matched := []Observation{}
	candidate := false
	digest := ""
	p.candidates(file).indices(func(i int) {
		r := p.rules[i]
		if r.literal == nil {
			return
		}
		candidate = true
		if bytes.Contains(content, r.literal) {
			if digest == "" {
				sum := sha256.Sum256(content)
				digest = hex.EncodeToString(sum[:])
			}
			matched = append(matched, Observation{Path: file.Path, RuleID: r.id, Evidence: "content", Size: file.Size, SourceSHA256: digest})
		}
	})
	if !candidate {
		return nil, ErrNotCandidate
	}
	return matched, nil
}
