package availability

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type GitmodulesParse struct {
	Complete            bool
	Declarations        []SubmoduleDeclaration
	Diagnostics         []Diagnostic
	OmittedDeclarations int64
	OmittedDiagnostics  int64
}

// ParseGitmodules reads only submodule paths. It intentionally withholds URLs,
// raw section names, update commands, branches, and other configuration.
func ParseGitmodules(evidence string, content []byte, fullSize, maxBytes int64) GitmodulesParse {
	result := GitmodulesParse{Complete: true, Declarations: []SubmoduleDeclaration{}, Diagnostics: []Diagnostic{}}
	declarations := maxHeap[submoduleValue]{}
	diagnostic := func(code, message string) {
		result.Complete = false
		if len(result.Diagnostics) < DefaultDiagnosticLimit {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: evidence, Code: code, Message: message})
		} else {
			result.OmittedDiagnostics++
		}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultGitmodulesBytes
	}
	if fullSize > maxBytes {
		diagnostic("gitmodules-size-limit", fmt.Sprintf("The selected .gitmodules file exceeds the %d byte parser limit.", maxBytes))
		return result
	}
	if fullSize < 0 || int64(len(content)) != fullSize {
		diagnostic("incomplete-gitmodules", "The selected .gitmodules content was incomplete or changed during inspection.")
		return result
	}
	for _, b := range content {
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			diagnostic("binary-gitmodules", "The selected .gitmodules file contains binary control bytes and was not parsed as configuration.")
			return result
		}
		break
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), DefaultStringBytes+1)
	inSubmodule, sectionHasPath := false, false
	seen := map[string]bool{}
	finishSection := func() {
		if inSubmodule && !sectionHasPath {
			diagnostic("submodule-path-missing", "A submodule section has no supported path declaration.")
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			diagnostic("unsupported-gitmodules-syntax", "Line continuations in .gitmodules are not interpreted.")
			continue
		}
		if strings.HasPrefix(line, "[") {
			finishSection()
			inSubmodule = isSubmoduleSection(line)
			sectionHasPath = false
			if !inSubmodule && strings.HasPrefix(strings.ToLower(line), "[submodule") {
				diagnostic("unsupported-submodule-section", "A submodule section header could not be interpreted.")
			}
			continue
		}
		if !inSubmodule {
			continue
		}
		at := strings.IndexByte(line, '=')
		if at < 0 {
			diagnostic("unsupported-gitmodules-syntax", "A submodule configuration line has no equals separator.")
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:at]))
		if key != "path" {
			continue
		}
		if sectionHasPath {
			diagnostic("duplicate-submodule-path", "A submodule section declares its path more than once.")
			continue
		}
		sectionHasPath = true
		value, ok := parseConfigValue(strings.TrimSpace(line[at+1:]))
		if !ok {
			diagnostic("invalid-submodule-path", "A submodule path value could not be interpreted.")
			continue
		}
		value, ok = normalizeSubmodulePath(value)
		if !ok || len(value) > DefaultStringBytes {
			diagnostic("invalid-submodule-path", "A submodule path is not a normalized path inside the selected source.")
			continue
		}
		if seen[value] {
			diagnostic("duplicate-submodule-declaration", "More than one submodule section declares the same path.")
			continue
		}
		full := len(declarations) >= DefaultEvidenceLimit
		old := ""
		if full {
			old = declarations[0].Path
		}
		retained := retain(&declarations, submoduleValue{SubmoduleDeclaration{Path: value, Evidence: evidence}}, DefaultEvidenceLimit)
		if full {
			result.Complete = false
			result.OmittedDeclarations++
		}
		if retained {
			if old != "" {
				delete(seen, old)
			}
			seen[value] = true
		}
	}
	finishSection()
	if err := scanner.Err(); err != nil {
		diagnostic("gitmodules-line-limit", "A .gitmodules line exceeds the supported text limit.")
	}
	result.Declarations = unwrapSubmodules(sortedHeap(declarations))
	slices.SortFunc(result.Diagnostics, func(a, b Diagnostic) int {
		if n := strings.Compare(a.Code, b.Code); n != 0 {
			return n
		}
		return strings.Compare(a.Message, b.Message)
	})
	return result
}

func (c *Collector) AddGitmodules(evidence string, content []byte, fullSize int64) {
	if !c.mutable() {
		return
	}
	parsed := ParseGitmodules(evidence, content, fullSize, c.opts.GitmodulesBytes)
	for _, declaration := range parsed.Declarations {
		c.AddSubmoduleDeclaration(declaration)
	}
	for _, diagnostic := range parsed.Diagnostics {
		c.AddDiagnostic(diagnostic)
	}
	if parsed.OmittedDeclarations > 0 {
		c.report.Coverage.OmittedEvidence += parsed.OmittedDeclarations
		c.partial("gitmodules_evidence_limit", parsed.OmittedDeclarations)
	}
	if parsed.OmittedDiagnostics > 0 {
		c.report.Coverage.OmittedDiagnostics += parsed.OmittedDiagnostics
		c.partial("gitmodules_diagnostic_limit", parsed.OmittedDiagnostics)
	}
	if !parsed.Complete {
		c.MarkInventoryIncomplete("gitmodules_incomplete")
	}
}

// OmitGitmodules records that the selected declaration file was inventoried
// but its contents could not be parsed under a caller-supplied source limit.
func (c *Collector) OmitGitmodules(evidence, reason string) {
	if !c.mutable() {
		return
	}
	if reason == "" {
		reason = "gitmodules_omitted"
	}
	c.AddDiagnostic(Diagnostic{Path: evidence, Code: reason, Message: "The selected .gitmodules content was not read under the source file limit."})
	c.partial(reason, 1)
}

func isSubmoduleSection(line string) bool {
	if len(line) < 2 || line[0] != '[' || line[len(line)-1] != ']' {
		return false
	}
	body := strings.TrimSpace(line[1 : len(line)-1])
	if len(body) < len("submodule") || !strings.EqualFold(body[:len("submodule")], "submodule") {
		return false
	}
	rest := strings.TrimSpace(body[len("submodule"):])
	if len(rest) < 2 || rest[0] != '"' {
		return false
	}
	_, err := strconv.Unquote(rest)
	return err == nil
}

func parseConfigValue(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if value[0] == '"' {
		escaped := false
		for i := 1; i < len(value); i++ {
			if value[i] == '"' && !escaped {
				trailing := strings.TrimSpace(value[i+1:])
				if trailing != "" && !strings.HasPrefix(trailing, "#") && !strings.HasPrefix(trailing, ";") {
					return "", false
				}
				decoded, err := strconv.Unquote(value[:i+1])
				return decoded, err == nil
			}
			if value[i] == '\\' && !escaped {
				escaped = true
			} else {
				escaped = false
			}
		}
		return "", false
	}
	for i, r := range value {
		if (r == '#' || r == ';') && i > 0 && unicode.IsSpace(rune(value[i-1])) {
			value = strings.TrimSpace(value[:i])
			break
		}
	}
	return value, value != "" && !strings.ContainsAny(value, "\r\n\x00")
}

func normalizeSubmodulePath(value string) (string, bool) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", false
	}
	normalized := path.Clean(value)
	if normalized != value || normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || strings.EqualFold(normalized, ".git") || strings.HasPrefix(strings.ToLower(normalized), ".git/") {
		return "", false
	}
	return normalized, true
}
