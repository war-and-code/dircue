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
		if state != "declared" {
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
		values = append(values, line)
	}
	if len(values) != 1 || !safeToolchainSelector(values[0]) {
		return nil, "unsupported", "unsupported-toolchain-value"
	}
	return values, "declared", ""
}

func parseRustToolchainTOML(content []byte) ([]string, string, string) {
	if !tomlNestingWithinBound(content, 64) {
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
	channel, hasChannel := toolchain["channel"]
	_, hasPath := toolchain["path"]
	if hasChannel == hasPath {
		return nil, "unresolved", "invalid-toolchain-toml"
	}
	if !hasChannel {
		return nil, "unresolved", "unresolved-toolchain-path"
	}
	value, ok := channel.(string)
	if !ok || !safeToolchainSelector(value) {
		return nil, "unresolved", "invalid-toolchain-channel"
	}
	return []string{value}, "declared", ""
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
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "-") || strings.Contains(value, "..") || strings.ContainsAny(value, "$%{}\\\"'`,;\t\r\n") {
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

func detectToolchainConflicts(r *Report) {
	declarations := r.ToolchainDeclarations
	for i, declaration := range declarations {
		if declaration.State != "declared" || len(declaration.Values) == 0 {
			continue
		}
		for j := i + 1; j < len(declarations); j++ {
			other := declarations[j]
			if other.State != "declared" || other.Tool != declaration.Tool || len(other.Values) == 0 {
				continue
			}
			if other.ScopeDirectory == declaration.ScopeDirectory {
				if slices.Equal(declaration.Values, other.Values) {
					continue
				}
				r.Conflicts = append(r.Conflicts, Conflict{
					ContextID:   "toolchain:" + declaration.Tool + "@" + declaration.ScopeDirectory,
					Dimension:   "toolchain-declaration",
					Values:      []string{strings.Join(declaration.Values, " "), strings.Join(other.Values, " ")},
					Evidence:    []string{declaration.SourcePath, other.SourcePath},
					Explanation: "Same-directory declarations for this tool disagree; manager-specific precedence was not inferred.",
				})
				continue
			}
			if isNestedScope(declaration.ScopeDirectory, other.ScopeDirectory) || isNestedScope(other.ScopeDirectory, declaration.ScopeDirectory) {
				r.Boundaries = append(r.Boundaries, Boundary{Path: other.SourcePath, Reason: "nested-toolchain-declaration", Detail: "Nested declarations were retained separately; manager-specific lookup and the applicable winner were not determined."})
			}
		}
	}
}

func isNestedScope(parent, child string) bool {
	if parent == "." {
		return child != "."
	}
	return child != parent && strings.HasPrefix(child, parent+"/")
}
