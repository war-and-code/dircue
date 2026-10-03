package environments

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
)

const toolchainApplicability = "The declaration is scoped to its source directory; manager-specific ancestor lookup, invocation overrides, project association, precedence, and installed versions were not evaluated."

type toolchainFileKind struct {
	tool string
	kind string
}

var toolchainFilenames = map[string]toolchainFileKind{
	".python-version":     {tool: "python", kind: "python-version"},
	".node-version":       {tool: "node", kind: "node-version"},
	".nvmrc":              {tool: "node", kind: "nvmrc"},
	"rust-toolchain":      {tool: "rust", kind: "rust-toolchain"},
	"rust-toolchain.toml": {tool: "rust", kind: "rust-toolchain-toml"},
}

func observeToolchainDeclarations(ctx context.Context, in Input, limits Limits, files map[string]File, r *Report) error {
	candidates := make([]string, 0)
	for candidate := range files {
		if _, ok := toolchainFilenames[path.Base(candidate)]; ok {
			candidates = append(candidates, candidate)
		}
	}
	slices.Sort(candidates)
	r.Coverage.ToolchainCandidates = len(candidates)
	if len(candidates) > limits.ToolchainFiles {
		omitted := len(candidates) - limits.ToolchainFiles
		r.Status = "partial"
		r.Coverage.OmittedToolchainFiles += int64(omitted)
		r.Boundaries = append(r.Boundaries, Boundary{Reason: "toolchain-file-limit", Detail: "Additional recognized toolchain files were omitted in lexical path order."})
		candidates = candidates[:limits.ToolchainFiles]
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		file := files[candidate]
		kind := toolchainFilenames[path.Base(candidate)]
		declaration := ToolchainDeclaration{SourcePath: candidate, Tool: kind.tool, Kind: kind.kind, State: "unresolved", ScopeDirectory: path.Dir(candidate), Applicability: toolchainApplicability}
		if declaration.ScopeDirectory == "" {
			declaration.ScopeDirectory = "."
		}
		if file.NonRegular {
			r.Status = "partial"
			r.Coverage.OmittedToolchainFiles++
			r.Boundaries = append(r.Boundaries, Boundary{Path: candidate, Reason: "toolchain-file-not-regular", Detail: "The selected toolchain path is not a regular file and was not read."})
			appendToolchainDeclaration(r, limits, declaration)
			continue
		}
		if file.Size < 0 || file.Size > limits.ToolchainFileBytes || file.Size > limits.ToolchainInputBytes-r.Coverage.ToolchainBytes || in.ReadSelected == nil {
			r.Status = "partial"
			r.Coverage.OmittedToolchainFiles++
			declaration.State = "unresolved"
			r.Boundaries = append(r.Boundaries, Boundary{Path: candidate, Reason: "toolchain-file-unread", Detail: "The selected toolchain file exceeded a read bound or lacked a bounded selected-source reader."})
			appendToolchainDeclaration(r, limits, declaration)
			continue
		}
		content, size, err := in.ReadSelected(ctx, candidate, limits.ToolchainFileBytes+1)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return context.Canceled
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return context.DeadlineExceeded
			}
			if in.ErrorPolicy != "continue" {
				return fmt.Errorf("could not read selected toolchain file %s", candidate)
			}
			r.Status = "partial"
			r.Coverage.OmittedToolchainFiles++
			r.Coverage.OmittedFiles++
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: candidate, Code: "file-read-error", Message: "Selected toolchain file could not be read."})
			appendToolchainDeclaration(r, limits, declaration)
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if int64(len(content)) != size || size != file.Size || size > limits.ToolchainFileBytes || size > limits.ToolchainInputBytes-r.Coverage.ToolchainBytes {
			r.Status = "partial"
			r.Coverage.OmittedToolchainFiles++
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: candidate, Code: "incomplete-toolchain-file", Message: "Selected toolchain file changed, was incomplete, or exceeded a read limit."})
			appendToolchainDeclaration(r, limits, declaration)
			continue
		}
		r.Coverage.ToolchainRead++
		r.Coverage.ToolchainBytes += size
		values, state, diagnosticCode := parseToolchainValues(candidate, content)
		declaration.Values = values
		declaration.State = state
		if state != "declared" || diagnosticCode != "" {
			r.Status = "partial"
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Path: candidate, Code: diagnosticCode, Message: "Toolchain declaration is malformed or outside the supported literal subset; raw file contents were not retained."})
		}
		appendToolchainDeclaration(r, limits, declaration)
	}
	detectToolchainConflicts(r)
	return nil
}

func appendToolchainDeclaration(r *Report, limits Limits, declaration ToolchainDeclaration) {
	if r.ToolchainDeclarations == nil {
		r.ToolchainDeclarations = []ToolchainDeclaration{}
	}
	if len(r.ToolchainDeclarations) >= limits.ToolchainFiles {
		r.Status = "partial"
		return
	}
	r.ToolchainDeclarations = append(r.ToolchainDeclarations, declaration)
	r.Coverage.ToolchainDeclarations = len(r.ToolchainDeclarations)
}

func parseToolchainValues(file string, content []byte) ([]string, string, string) {
	if len(content) == 0 || !utf8.Valid(content) || strings.ContainsRune(string(content), '\x00') {
		return nil, "unresolved", "invalid-toolchain-text"
	}
	switch path.Base(file) {
	case ".python-version":
		return parsePythonVersionFile(content)
	case ".node-version":
		return parseSingleLineVersion(content)
	case ".nvmrc":
		return parseNVMRC(content)
	case "rust-toolchain":
		trimmed := strings.TrimSpace(string(content))
		if strings.HasPrefix(trimmed, "[") || strings.Contains(trimmed, "=") {
			return parseRustToolchainTOML(content)
		}
		return parseSingleLineVersion(content)
	case "rust-toolchain.toml":
		return parseRustToolchainTOML(content)
	default:
		return nil, "unsupported", "unsupported-toolchain-file"
	}
}

func parsePythonVersionFile(content []byte) ([]string, string, string) {
	lines := strings.Split(string(content), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(values) >= 16 || !safeToolchainSelector(line) {
			return nil, "unsupported", "unsupported-toolchain-value"
		}
		values = append(values, line)
	}
	if len(values) == 0 {
		return nil, "unresolved", "invalid-toolchain-text"
	}
	return values, "declared", ""
}

func parseSingleLineVersion(content []byte) ([]string, string, string) {
	value := strings.TrimSpace(string(content))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return nil, "unsupported", "unsupported-toolchain-value"
	}
	if !safeToolchainSelector(value) {
		return nil, "unsupported", "unsupported-toolchain-value"
	}
	return []string{value}, "declared", ""
}

func parseNVMRC(content []byte) ([]string, string, string) {
	values := make([]string, 0, 1)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		// nvm currently reserves key/value lines for future settings and
		// ignores them when reading .nvmrc.
		if key, value, ok := strings.Cut(line, "="); ok {
			if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
				return nil, "unsupported", "unsupported-toolchain-value"
			}
			continue
		}
		values = append(values, line)
	}
	if len(values) != 1 || !safeToolchainSelector(values[0]) {
		return nil, "unsupported", "unsupported-toolchain-value"
	}
	return values, "declared", ""
}

func parseRustToolchainTOML(content []byte) ([]string, string, string) {
	if !tomlNestingWithinBound(content, 64) || !tomlDottedNestingWithinBound(content, 64) {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	var root map[string]any
	if err := toml.Unmarshal(content, &root); err != nil || root == nil {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	if len(root) != 1 || root["toolchain"] == nil {
		return nil, "unsupported", "unsupported-toolchain-toml"
	}
	toolchain, ok := root["toolchain"].(map[string]any)
	if !ok || len(toolchain) == 0 {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	for key := range toolchain {
		if key != "channel" && key != "path" && key != "components" && key != "targets" && key != "profile" {
			return nil, "unsupported", "unsupported-toolchain-toml"
		}
	}
	metadataUnsupported := false
	for _, key := range []string{"components", "targets"} {
		if raw, exists := toolchain[key]; exists && !rustupStringArray(raw) {
			metadataUnsupported = true
		}
	}
	if raw, exists := toolchain["profile"]; exists {
		profile, ok := raw.(string)
		if !ok || (profile != "minimal" && profile != "default" && profile != "complete") {
			metadataUnsupported = true
		}
	}
	channel, hasChannel := toolchain["channel"]
	toolchainPath, hasPath := toolchain["path"]
	if hasChannel == hasPath {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	if _, ok := toolchainPath.(string); hasPath && !ok {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	if !hasChannel {
		return nil, "unresolved", "unresolved-toolchain-path"
	}
	value, ok := channel.(string)
	if !ok || !safeToolchainSelector(value) {
		return nil, "unresolved", "invalid-toolchain-channel"
	}
	if metadataUnsupported {
		return []string{value}, "declared", "unsupported-toolchain-toml"
	}
	return []string{value}, "declared", ""
}

// tomlDottedNestingWithinBound counts key path components in table headers and
// assignments before decoding. Dotted keys build nested maps just like arrays
// and inline tables, so bracket depth alone does not bound TOML decoder depth.
func tomlDottedNestingWithinBound(content []byte, maxDepth int) bool {
	depth := 0
	quote := byte(0)
	triple := false
	comment := false
	keySegments := 1
	keyContext := true
	lineStart := true
	header := false
	headerCloses := 0
	headerArrayTable := false
	contextSegments := 0
	valueSegments := 0
	arrayDepth := 0
	valueArrayDepth := 0
	assignmentPending := false
	type inlineContext struct {
		contextSegments int
		valueSegments   int
		arrayDepth      int
		valueArrayDepth int
	}
	inlineTables := []inlineContext{}
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if comment {
			if ch == '\n' || ch == '\r' {
				comment = false
				keySegments = 1
				if !assignmentPending {
					keyContext = valueArrayDepth == 0
				}
				lineStart = true
			}
			continue
		}
		if quote != 0 {
			if quote == '"' && ch == '\\' {
				i++
				continue
			}
			if triple {
				if ch == quote && i+2 < len(content) && content[i+1] == quote && content[i+2] == quote {
					i += 2
					quote, triple = 0, false
				}
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '#' {
			comment = true
			continue
		}
		if ch == '\n' || ch == '\r' {
			keySegments = 1
			if !assignmentPending {
				keyContext = valueArrayDepth == 0
			}
			lineStart = true
			continue
		}
		if lineStart && (ch == ' ' || ch == '\t') {
			continue
		}
		if lineStart {
			lineStart = false
			if ch == '[' && keyContext && valueArrayDepth == 0 && len(inlineTables) == 0 {
				header = true
				headerCloses = 1
				headerArrayTable = i+1 < len(content) && content[i+1] == '['
				if headerArrayTable {
					headerCloses = 2
				}
				keySegments, keyContext = 1, true
			}
		}
		if ch == '"' || ch == '\'' {
			if assignmentPending {
				assignmentPending = false
			}
			quote = ch
			triple = i+2 < len(content) && content[i+1] == ch && content[i+2] == ch
			if triple {
				i += 2
			}
			continue
		}
		if assignmentPending && ch != '=' && ch != '#' && ch != ' ' && ch != '\t' && ch != '\r' {
			assignmentPending = false
		}
		switch ch {
		case '[', '{':
			if ch == '[' && header {
				continue
			}
			depth++
			if depth > maxDepth {
				return false
			}
			if ch == '[' {
				arrayDepth++
				if !keyContext {
					valueArrayDepth++
					if contextSegments+keySegments+valueArrayDepth+1 > maxDepth {
						return false
					}
				}
			} else {
				inlineTables = append(inlineTables, inlineContext{contextSegments: contextSegments, valueSegments: valueSegments, arrayDepth: arrayDepth, valueArrayDepth: valueArrayDepth})
				contextSegments = valueSegments + valueArrayDepth
				valueArrayDepth = 0
				keySegments, keyContext = 1, true
			}
		case ']', '}':
			if ch == ']' && header {
				headerCloses--
				if headerCloses == 0 {
					header = false
					contextSegments = keySegments
					if headerArrayTable {
						contextSegments++
					}
					keyContext = false
					if contextSegments+1 > maxDepth {
						return false
					}
				}
			} else {
				if depth > 0 {
					depth--
				}
				if ch == ']' {
					if arrayDepth > 0 {
						arrayDepth--
					}
					if valueArrayDepth > 0 {
						valueArrayDepth--
					}
				} else if len(inlineTables) > 0 {
					last := inlineTables[len(inlineTables)-1]
					inlineTables = inlineTables[:len(inlineTables)-1]
					contextSegments, valueSegments = last.contextSegments, last.valueSegments
					valueArrayDepth = last.valueArrayDepth
					keyContext = false
				}
			}
		case '=', ',', '.':
			if ch == '=' {
				if keyContext && contextSegments+keySegments+1 > maxDepth {
					return false
				}
				valueSegments = contextSegments + keySegments
				valueArrayDepth = 0
				keyContext = false
				assignmentPending = true
			} else if ch == ',' {
				if len(inlineTables) > 0 && arrayDepth == inlineTables[len(inlineTables)-1].arrayDepth {
					keySegments, keyContext = 1, true
				}
			} else if keyContext {
				keySegments++
				baseSegments := contextSegments
				if header {
					baseSegments = 0
				}
				if baseSegments+keySegments+1 > maxDepth {
					return false
				}
			}
		}
	}
	return true
}

func rustupStringArray(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok || !safeRustupListValue(text) {
			return false
		}
	}
	return true
}

func safeRustupListValue(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._+-", r) {
			continue
		}
		return false
	}
	return true
}

// tomlNestingWithinBound limits arrays and inline tables before handing
// selected text to the TOML decoder. Brackets inside strings and comments do
// not contribute to the bound.
func tomlNestingWithinBound(content []byte, maxDepth int) bool {
	depth := 0
	quote := byte(0)
	triple := false
	for i := 0; i < len(content); i++ {
		c := content[i]
		if quote != 0 {
			if quote == '"' && c == '\\' && i+1 < len(content) {
				i++
				continue
			}
			if c == quote {
				if triple {
					if i+2 < len(content) && content[i+1] == quote && content[i+2] == quote {
						i += 2
						quote, triple = 0, false
					}
				} else {
					quote = 0
				}
			}
			continue
		}
		switch c {
		case '#':
			for i+1 < len(content) && content[i+1] != '\n' {
				i++
			}
		case '"', '\'':
			quote = c
			if i+2 < len(content) && content[i+1] == c && content[i+2] == c {
				triple = true
				i += 2
			}
		case '[', '{':
			depth++
			if depth > maxDepth {
				return false
			}
		case ']', '}':
			depth--
			if depth < 0 {
				return true // The TOML decoder reports the syntax error.
			}
		}
	}
	return true
}

func safeToolchainSelector(value string) bool {
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "-") || credentialLookingSelector(value) || strings.Contains(value, "..") || strings.ContainsAny(value, "$%{}\\\"'`,;\t\r\n") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." {
			return false
		}
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._+-/*", r) {
			if i == 0 && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func credentialLookingSelector(value string) bool {
	if len(value) == 20 && (strings.HasPrefix(value, "AKIA") || strings.HasPrefix(value, "ASIA")) {
		for _, r := range value[4:] {
			if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
				return false
			}
		}
		return true
	}
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "sk-", "xoxb-", "xoxp-", "xoxa-", "xoxr-", "xoxs-", "AIza"} {
		if strings.HasPrefix(value, prefix) && len(value) >= len(prefix)+20 {
			return true
		}
	}
	return false
}

func detectToolchainConflicts(r *Report) {
	byScope := make(map[toolchainScopeKey][]ToolchainDeclaration, len(r.ToolchainDeclarations))
	for _, declaration := range r.ToolchainDeclarations {
		key := toolchainScopeKey{tool: declaration.Tool, scope: declaration.ScopeDirectory}
		byScope[key] = append(byScope[key], declaration)
	}
	for _, declaration := range r.ToolchainDeclarations {
		if declaration.ScopeDirectory == "." {
			continue
		}
		for parent := path.Dir(declaration.ScopeDirectory); ; parent = path.Dir(parent) {
			if len(byScope[toolchainScopeKey{tool: declaration.Tool, scope: parent}]) > 0 {
				r.Boundaries = append(r.Boundaries, Boundary{Path: declaration.SourcePath, Reason: "nested-toolchain-declaration", Detail: "Nested declarations were retained separately; manager-specific lookup and the applicable winner were not determined."})
				break
			}
			if parent == "." {
				break
			}
		}
	}
	scopes := make([]toolchainScopeKey, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	slices.SortFunc(scopes, func(a, b toolchainScopeKey) int {
		if a.tool != b.tool {
			return strings.Compare(a.tool, b.tool)
		}
		return strings.Compare(a.scope, b.scope)
	})
	for _, scope := range scopes {
		declarations := byScope[scope]
		for i, declaration := range declarations {
			if declaration.State != "declared" || len(declaration.Values) == 0 {
				continue
			}
			for _, other := range declarations[i+1:] {
				if other.State != "declared" || len(other.Values) == 0 || slices.Equal(declaration.Values, other.Values) {
					continue
				}
				r.Conflicts = append(r.Conflicts, Conflict{
					ContextID:   "toolchain:" + declaration.Tool + "@" + declaration.ScopeDirectory,
					Dimension:   "toolchain-declaration",
					Values:      []string{strings.Join(declaration.Values, " "), strings.Join(other.Values, " ")},
					Evidence:    []string{declaration.SourcePath, other.SourcePath},
					Explanation: "Same-directory declarations for this tool disagree; manager-specific precedence was not inferred.",
				})
			}
		}
	}
}

type toolchainScopeKey struct {
	tool  string
	scope string
}
