package scanner

import (
	"bufio"
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"dircue/pkg/explain"
	"dircue/pkg/profile"
	enry "github.com/go-enry/go-enry/v2"
)

const maxAttributesBytes int64 = 1 << 20

// Bound compiled matchers and per-file rule evaluation across every attribute
// file in a scan. Exceeding this limit fails the scan instead of silently
// omitting rules.
const maxAttributeRules = 10_000

type overrides struct {
	languageSet                                    bool
	lfsTracked                                     bool
	language                                       string
	vendored, generated, detectable, documentation *bool
}

type assignment struct {
	name, value string
	source      string
	line        int
}
type attributeRule struct {
	macro    string
	scope    string
	basename bool
	pattern  *regexp.Regexp
	values   []assignment
}

// parseAttributes reads repository-local Git attribute patterns and macros.
// System and global attribute configuration is intentionally not consulted.
func parseAttributes(filename string, content []byte) ([]attributeRule, []profile.Warning) {
	rules, warnings, _ := parseAttributesBounded(filename, content, maxAttributeRules)
	return rules, warnings
}

func parseAttributesBounded(filename string, content []byte, ruleLimit int) ([]attributeRule, []profile.Warning, bool) {
	return parseAttributesBoundedFrom(filename, filename, content, ruleLimit)
}

// parseAttributesBoundedFrom separates matching scope from evidence origin.
// Git info attributes match at the repository root like .gitattributes while
// retaining .git/info/attributes as their actual provenance.
func parseAttributesBoundedFrom(filename, source string, content []byte, ruleLimit int) ([]attributeRule, []profile.Warning, bool) {
	var rules []attributeRule
	var warnings []profile.Warning
	warn := func(line int, message string) {
		warnings = append(warnings, profile.Warning{Path: source, Code: "unsupported_gitattributes", Message: fmt.Sprintf("line %d: %s", line, message)})
	}
	scope := path.Dir(filename)
	if scope == "." {
		scope = ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	scanner.Buffer(make([]byte, 4096), int(maxAttributesBytes)+1)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields, splitErr := attributeFields(text)
		if splitErr != nil {
			warn(line, splitErr.Error())
			continue
		}
		pattern := fields[0]
		if strings.HasPrefix(pattern, "!") {
			warn(line, "negative patterns are not allowed by Git attributes")
			continue
		}
		// Git accepts a pattern with an empty attribute list as a silent no-op.
		// This occurs in Kubernetes' attributes and is distinct from a malformed
		// or negative pattern, both of which are still diagnosed.
		if len(fields) == 1 {
			continue
		}
		macro := ""
		if strings.HasPrefix(pattern, "[attr]") {
			if scope != "" {
				warn(line, "attribute macros are only valid in root .gitattributes")
				continue
			}
			macro = strings.TrimPrefix(pattern, "[attr]")
			pattern = "*"
		}
		// A trailing slash matches directories themselves, never descendants.
		if strings.HasSuffix(pattern, "/") {
			continue
		}
		basename := !strings.Contains(pattern, "/")
		pattern = strings.TrimPrefix(pattern, "/")
		re, err := compileGlob(pattern)
		if err != nil {
			warn(line, err.Error())
			continue
		}
		rule := attributeRule{macro: macro, scope: scope, basename: basename, pattern: re}
		for _, token := range fields[1:] {
			name, value := token, "true"
			unset := false
			if strings.HasPrefix(name, "-") {
				name, value, unset = name[1:], "false", true
			} else if strings.HasPrefix(name, "!") {
				name, value = name[1:], ""
			}
			if key, val, ok := strings.Cut(name, "="); ok {
				name, value = key, val
				if val == "" {
					value = "__empty_attribute__"
				}
			}

			switch name {
			case "linguist-language":
				// Linguist treats an explicitly unset value-taking attribute as
				// removal of the override, restoring ordinary detection.
				if unset {
					value = ""
				}
				if value != "" {
					language, ok := enry.GetLanguageByAlias(value)
					if !ok {
						warn(line, "unknown language alias "+value)
						value = "__unknown_language__"
						language = value
					}
					value = language
				}
			case "linguist-vendored", "linguist-generated", "linguist-detectable", "linguist-documentation":

			default:
				if strings.HasPrefix(name, "linguist-") {
					warn(line, "unsupported attribute "+name)
				}
			}
			rule.values = append(rule.values, assignment{name: name, value: value, source: source, line: line})
		}
		if len(rule.values) > 0 {
			if len(rules) >= ruleLimit {
				return rules, warnings, true
			}
			rules = append(rules, rule)
		}
	}
	return rules, warnings, false
}

func compileGlob(pattern string) (*regexp.Regexp, error) {
	var re strings.Builder
	re.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
				re.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			} else {
				re.WriteString(`\\`)
			}
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if (i > 0 && pattern[i-1] != '/') || (i+2 < len(pattern) && pattern[i+2] != '/') {
					re.WriteString("[^/]*")
					i++
					continue
				}
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					re.WriteString("(?:.*/)?")
					i++
				} else {
					re.WriteString(".*")
				}
			} else {
				re.WriteString("[^/]*")
			}
		case '?':
			re.WriteString("[^/]")
		case '[':
			j := i + 1
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j == len(pattern) || j == i+1 {
				return nil, fmt.Errorf("invalid bracket class in pattern")
			}
			class := pattern[i+1 : j]
			if strings.ContainsAny(class, "/[") {
				return nil, fmt.Errorf("unsupported bracket class in pattern")
			}
			if class[0] == '!' || class[0] == '^' {
				re.WriteString("[^/" + class[1:] + "]")
			} else {
				re.WriteString("[" + class + "]")
			}
			i = j
		default:
			re.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	re.WriteByte('$')
	return regexp.Compile(re.String())
}

func resolveAttributes(filename string, rules []attributeRule) overrides {
	result, _ := resolveAttributesContext(context.Background(), filename, rules)
	return result
}

func resolveAttributesContext(ctx context.Context, filename string, rules []attributeRule) (overrides, error) {
	return resolveAttributeRuleSetsContext(ctx, filename, [][]attributeRule{rules})
}

// resolveAttributesTraceContext retains only the final relevant assignments
// for one explicitly selected path. Ordinary resolution uses a nil recorder.
func resolveAttributesTraceContext(ctx context.Context, filename string, rules []attributeRule) (overrides, []explain.Override, error) {
	return resolveAttributeRuleSetsTraceContext(ctx, filename, [][]attributeRule{rules})
}

// resolveAttributeRuleSetsContext evaluates ancestor rule sets without first
// flattening them. Directory scans keep only each directory's local rules, so
// deeply nested checkouts cannot make retained rule storage grow quadratically.
func resolveAttributeRuleSetsContext(ctx context.Context, filename string, ruleSets [][]attributeRule) (overrides, error) {
	result, _, err := resolveAttributeRuleSets(ctx, filename, ruleSets, false)
	return result, err
}

func resolveAttributeRuleSetsTraceContext(ctx context.Context, filename string, ruleSets [][]attributeRule) (overrides, []explain.Override, error) {
	return resolveAttributeRuleSets(ctx, filename, ruleSets, true)
}

func resolveAttributeRuleSets(ctx context.Context, filename string, ruleSets [][]attributeRule, retainTrace bool) (overrides, []explain.Override, error) {
	var result overrides
	macros := map[string][]assignment{"binary": {{name: "diff", value: "false"}, {name: "merge", value: "false"}, {name: "text", value: "false"}}}
	var traced map[string]explain.Override
	if retainTrace {
		traced = make(map[string]explain.Override, 6)
	}
	checked := 0
	for _, rules := range ruleSets {
		for _, rule := range rules {
			if checked%64 == 0 {
				if err := ctx.Err(); err != nil {
					return overrides{}, nil, err
				}
			}
			checked++
			if rule.macro != "" {
				macros[rule.macro] = rule.values
			}
		}
	}
	checked = 0
	for _, rules := range ruleSets {
		for _, rule := range rules {
			if checked%64 == 0 {
				if err := ctx.Err(); err != nil {
					return overrides{}, nil, err
				}
			}
			checked++
			if rule.macro != "" {
				continue
			}
			candidate := filename
			if rule.scope != "" {
				var ok bool
				candidate, ok = strings.CutPrefix(filename, rule.scope+"/")
				if !ok {
					continue
				}
			}
			if rule.basename {
				candidate = path.Base(candidate)
			}
			if !rule.pattern.MatchString(candidate) {
				continue
			}
			var values []assignment
			expanded := 0
			var expandErr error
			var expand func([]assignment, map[string]bool, int)
			expand = func(assignments []assignment, active map[string]bool, depth int) {
				if expandErr != nil || depth > 32 || len(values) >= 4096 {
					return
				}
				for _, value := range assignments {
					if len(values) >= 4096 {
						return
					}
					expanded++
					if expanded%64 == 0 {
						if err := ctx.Err(); err != nil {
							expandErr = err
							return
						}
					}
					values = append(values, value)
					if value.value != "true" || active[value.name] {
						continue
					}
					if nested, ok := macros[value.name]; ok {
						active[value.name] = true
						expand(nested, active, depth+1)
						delete(active, value.name)
					}
				}
			}
			expand(rule.values, make(map[string]bool), 0)
			if expandErr != nil {
				return overrides{}, nil, expandErr
			}
			for _, value := range values {
				var boolean *bool
				if value.value != "" {
					v := value.value != "false"
					boolean = &v
				}
				switch value.name {
				case "filter":
					result.lfsTracked = value.value == "lfs"
				case "linguist-language":
					result.language = value.value
					result.languageSet = value.value != ""
					if value.value == "__unknown_language__" {
						result.language = ""
					}
				case "linguist-vendored":
					result.vendored = boolean
				case "linguist-generated":
					result.generated = boolean
				case "linguist-detectable":
					result.detectable = boolean
				case "linguist-documentation":
					result.documentation = boolean
				}
				if traced != nil && traceableAttribute(value.name) {
					traced[value.name] = tracedOverride(value)
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return overrides{}, nil, err
	}
	values := make([]explain.Override, 0, len(traced))
	for _, value := range traced {
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b explain.Override) int { return strings.Compare(a.Attribute, b.Attribute) })
	return result, values, nil
}

func traceableAttribute(name string) bool {
	switch name {
	case "filter", "linguist-language", "linguist-vendored", "linguist-generated", "linguist-detectable", "linguist-documentation":
		return true
	}
	return false
}

func tracedOverride(value assignment) explain.Override {
	display := value.value
	switch display {
	case "":
		display = "unset"
	case "__empty_attribute__":
		display = "true"
	case "__unknown_language__":
		display = "unknown"
	}
	result := explain.Override{Attribute: value.name, Value: display, Provenance: "unavailable"}
	if value.source != "" && value.line > 0 {
		result.Source = value.source
		result.Line = value.line
		result.Provenance = "retained"
	}
	return result
}

func overrideBool(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

// attributeFields accepts Git's C-quoted first field, including octal bytes.
// Attribute values themselves are whitespace-delimited (language aliases use
// hyphens, for example linguist-language=Emacs-Lisp).
func attributeFields(text string) ([]string, error) {
	if !strings.HasPrefix(text, `"`) {
		return strings.Fields(text), nil
	}
	escaped := false
	for i := 1; i < len(text); i++ {
		if text[i] == '"' && !escaped {
			field, err := strconv.Unquote(text[:i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid quoted pattern: %w", err)
			}
			return append([]string{field}, strings.Fields(text[i+1:])...), nil
		}
		if text[i] == '\\' {
			escaped = !escaped
		} else {
			escaped = false
		}
	}
	return nil, fmt.Errorf("unterminated quoted pattern")
}

// Rugged/libgit2 as shipped with Linguist 9.7.0 ignores C-quoted patterns.
// Preserve that observed behavior for Git snapshots. Flat directory mode
// accepts the Git specification's quoted patterns as a documented extension.
func parseGitAttributes(filename string, content []byte) ([]attributeRule, []profile.Warning) {
	rules, warnings, _ := parseGitAttributesBounded(filename, content, maxAttributeRules)
	return rules, warnings
}

func parseGitAttributesBounded(filename string, content []byte, ruleLimit int) ([]attributeRule, []profile.Warning, bool) {
	return parseGitAttributesBoundedFrom(filename, filename, content, ruleLimit)
}

func parseGitAttributesBoundedFrom(filename, source string, content []byte, ruleLimit int) ([]attributeRule, []profile.Warning, bool) {
	lines := strings.Split(string(content), "\n")
	var warnings []profile.Warning
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), `"`) {
			lines[i] = ""
			warnings = append(warnings, profile.Warning{Path: source, Code: "unsupported_gitattributes", Message: fmt.Sprintf("line %d: quoted pattern ignored to match Linguist 9.7.0", i+1)})
		}
	}
	rules, more, exceeded := parseAttributesBoundedFrom(filename, source, []byte(strings.Join(lines, "\n")), ruleLimit)
	return rules, append(warnings, more...), exceeded
}
