package declarations

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ParseDeno reads deno.json or deno.jsonc manifests.
// Extracts the optional `name` field.
func ParseDeno(name string, content []byte) *Document {
	d := NewDocument(name, "deno")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-deno-config", "deno.json/deno.jsonc exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	// Strip C-style // and /* */ comments for jsonc before JSON parse.
	cleaned := stripJSONC(content)
	raw, err := ValidateJSON(cleaned)
	if err != nil {
		// Try original content (standard JSON).
		raw2, err2 := ValidateJSON(content)
		if err2 != nil {
			d.Parsed = false
			AddDiagnostic(d, "invalid-deno-config", "deno.json could not be parsed within JSON limits.")
			return d
		}
		raw = raw2
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "deno-json-v2", State: "declared", Evidence: name})
	if n, ok := raw["name"].(string); ok && len(n) > 0 && len(n) <= MaxStringBytes {
		d.Project.Name = n
		AddRequirement(d, Requirement{Kind: "deno-package-name", Value: n, State: "declared", Evidence: name})
	}
	if v, ok := raw["version"].(string); ok && len(v) <= MaxStringBytes {
		d.Project.Version = v
	}
	return d
}

// stripJSONC removes // line comments and /* */ block comments.
// This is a best-effort transformation for static name extraction only.
func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	i := 0
	inStr := false
	escaped := false
	for i < len(src) {
		c := src[i]
		if inStr {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inStr = false
			}
			out = append(out, c)
			i++
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			i++
			continue
		}
		if i+1 < len(src) && c == '/' && src[i+1] == '/' {
			// Skip to end of line.
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(src) && c == '/' && src[i+1] == '*' {
			// Skip block comment.
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

// module(name = "repo_name", ...)
var bazelModuleNameRE = regexp.MustCompile(`(?m)\bmodule\s*\([^)]*name\s*=\s*"([A-Za-z0-9][A-Za-z0-9_.-]{0,127})"`)

// workspace(name = "repo_name")
var bazelWorkspaceNameRE = regexp.MustCompile(`(?m)\bworkspace\s*\([^)]*name\s*=\s*"([A-Za-z0-9][A-Za-z0-9_.-]{0,127})"`)

// ParseBazel reads MODULE.bazel or WORKSPACE manifests (root-level only).
// MODULE.bazel: extracts `module(name = "name")`.
// WORKSPACE: extracts `workspace(name = "name")`.
func ParseBazel(name string, content []byte) *Document {
	base := baseName(name)
	kind := "bazel-workspace"
	if base == "MODULE.bazel" {
		kind = "bazel-module"
	}
	d := NewDocument(name, kind)
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-bazel-manifest", "MODULE.bazel/WORKSPACE exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "bazel-manifest-static-v1", State: "declared", Evidence: name})
	re := bazelWorkspaceNameRE
	reqKind := "bazel-workspace-name"
	if base == "MODULE.bazel" {
		re = bazelModuleNameRE
		reqKind = "bazel-module-name"
	}
	if m := re.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if bazelNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: reqKind, Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-bazel-project-name", "A static name field was not found; the file was not executed.")
	}
	return d
}

func bazelNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}
