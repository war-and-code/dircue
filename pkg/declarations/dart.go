package declarations

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
	"unicode/utf8"
)

// dartSection names the tracked pubspec.yaml dependency section.
type dartSection int

const (
	dartSectionNone dartSection = iota
	dartSectionDeps
	dartSectionDevDeps
	dartSectionOverrides
)

// ParseDart reads pubspec.yaml (Dart/Flutter) manifests.
// It extracts the package name, version, runtime and dev dependencies, and
// local path relationships from dependencies:, dev_dependencies:, and
// dependency_overrides: sections.
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

	// Two-pass scanner:
	//   Pass 1: collect simple `  pkgname: version` dependency lines and the
	//           current top-level section header.
	//   Pass 2: look for path: sub-values that follow a package name line.
	//
	// A multi-value dependency block looks like:
	//   dependencies:
	//     appflowy_editor:
	//       path: ../
	//
	// We capture the most-recently-seen package name in a dependency section
	// and, if the next indented line is `path:`, emit a path-dependency reference.
	section := dartSectionNone
	pendingPathPkg := "" // package name awaiting a possible `path:` sub-line
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxStringBytes+1)
	for scanner.Scan() {
		if d.limited {
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Track top-level section headers (non-indented lines).
		if len(line) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			switch trimmed {
			case "dependencies:":
				section = dartSectionDeps
				pendingPathPkg = ""
				continue
			case "dev_dependencies:":
				section = dartSectionDevDeps
				pendingPathPkg = ""
				continue
			case "dependency_overrides:":
				section = dartSectionOverrides
				pendingPathPkg = ""
				continue
			default:
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					section = dartSectionNone
					pendingPathPkg = ""
				}
				continue
			}
		}

		if section == dartSectionNone || strings.HasPrefix(trimmed, "#") {
			pendingPathPkg = ""
			continue
		}

		// Detect `path: <value>` sub-key after a pending package name.
		if pendingPathPkg != "" {
			if m := dartPathSublineRE.FindStringSubmatch(trimmed); m != nil {
				rawPath := m[1]
				cond := dartSectionCondition(section)
				if target, ok := LocalTarget(name, rawPath, "pubspec.yaml"); ok {
					AddReference(d, Reference{Kind: "pub-path-dependency", Value: pendingPathPkg + " path:" + rawPath, Target: target, State: "declared", Evidence: name, Condition: cond})
				} else {
					AddDiagnostic(d, "external-pub-path-dependency", "A pub path dependency is outside the selected inventory.")
				}
				pendingPathPkg = ""
				continue
			}
			// A line that is not a sub-key for this package ends the pending state.
			// Reset only if we're back to package-level indentation (two spaces/one tab).
			pendingPathPkg = ""
		}

		// Detect a package name line: `  pkgname:` or `  pkgname: ^version`.
		// Two-space indented (or tab-indented) lines at the direct child level
		// of a section header are package names.
		if m := dartDepLineRE.FindStringSubmatch(trimmed); m != nil {
			pkgN := m[1]
			// Determine whether this is a simple `pkgname: value` or a block
			// header `pkgname:` (value to follow on sub-lines).
			// m[0] is the full match including the trailing colon (e.g. "http:").
			afterColon := strings.TrimSpace(trimmed[len(m[0]):])
			isBlock := afterColon == "" || strings.HasPrefix(afterColon, "#")
			if isBlock {
				// This is a block header; the next sub-lines may have path:.
				pendingPathPkg = pkgN
			} else {
				// Simple version-pinned dependency.
				if section != dartSectionDevDeps {
					AddRequirement(d, Requirement{Kind: "dart-dependency", Value: pkgN, State: "declared", Evidence: name})
				} else {
					AddRequirement(d, Requirement{Kind: "dart-dependency", Value: pkgN, State: "declared", Evidence: name, Condition: "dev_dependencies"})
				}
				pendingPathPkg = ""
			}
		}
	}
	return d
}

var dartDepLineRE = regexp.MustCompile(`^([a-z][a-z0-9_]{0,127})\s*:`)

// dartPathSublineRE matches a `path: value` line inside a dependency block.
// The value may be a relative path like `../` or `../packages/foo`.
var dartPathSublineRE = regexp.MustCompile(`^path:\s*(.+)$`)

// dartSectionCondition returns the Condition string for a dependency section.
func dartSectionCondition(s dartSection) string {
	switch s {
	case dartSectionDevDeps:
		return "dev_dependencies"
	case dartSectionOverrides:
		return "dependency_overrides"
	default:
		return ""
	}
}

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
