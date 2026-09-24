package declarations

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ParseDart reads pubspec.yaml (Dart/Flutter) manifests.
// Only the top-level `name:` line is extracted; the YAML is not fully parsed.
func ParseDart(name string, content []byte) *Document {
	d := NewDocument(name, "dart-pub")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-dart-pubspec", "pubspec.yaml exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "pubspec-yaml-v1", State: "declared", Evidence: name})
	pkgName, ver := parseSimpleYAMLIdentifier(content, "name"), parseSimpleYAMLIdentifier(content, "version")
	if pkgName != "" && dartNameOK(pkgName) {
		d.Project.Name = pkgName
		AddRequirement(d, Requirement{Kind: "dart-package-name", Value: pkgName, State: "declared", Evidence: name})
	} else {
		AddDiagnostic(d, "missing-dart-package-name", "pubspec.yaml has no supported static name field.")
	}
	if ver != "" && len(ver) <= MaxStringBytes {
		d.Project.Version = ver
	}
	// Collect dependencies section (simple `  pkgname:` or `  pkgname: ^1.0.0` lines).
	inDeps := false
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxStringBytes+1)
	for scanner.Scan() {
		if d.limited {
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "dependencies:" || trimmed == "dev_dependencies:" {
			inDeps = true
			continue
		}
		// A non-indented non-comment line ends the current dependencies section.
		if inDeps && len(line) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trimmed, "#") {
			inDeps = false
		}
		if !inDeps || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := dartDepLineRE.FindStringSubmatch(trimmed); m != nil {
			AddRequirement(d, Requirement{Kind: "dart-dependency", Value: m[1], State: "declared", Evidence: name})
		}
	}
	return d
}

var dartDepLineRE = regexp.MustCompile(`^([a-z][a-z0-9_]{0,127})\s*:`)

func dartNameOK(s string) bool {
	if s == "" || len(s) > MaxStringBytes {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

// parseSimpleYAMLIdentifier extracts `key: value` from the first top-level
// (non-indented) occurrence. value must be a bare identifier (no quotes needed).
// Quoted values (single or double quotes) are also handled.
var simpleYAMLLineRE = regexp.MustCompile(`(?m)^([a-z_][a-z0-9_]*):\s*['"]?([A-Za-z0-9][A-Za-z0-9._:+~! -]*)['"]?\s*(?:#.*)?$`)

func parseSimpleYAMLIdentifier(content []byte, key string) string {
	for _, m := range simpleYAMLLineRE.FindAllSubmatch(content, -1) {
		if string(m[1]) == key {
			v := strings.TrimRight(string(m[2]), " \t")
			if len(v) <= MaxStringBytes {
				return v
			}
		}
	}
	return ""
}
