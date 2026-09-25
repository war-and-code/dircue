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

	// Line scanner for the three dependency sections. The first indented line
	// under a section fixes the package indentation; deeper lines belong to
	// the preceding package's block (for example `path:`, `hosted:`, `sdk:` or
	// `version:`) and are never read as package names:
	//
	//   dependencies:
	//     http: ^1.2.0
	//     appflowy_editor:
	//       path: ../
	section := dartSectionNone
	packageIndent := -1
	blockPkg := "" // package whose block the following deeper lines belong to
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxStringBytes+1)
	for scanner.Scan() {
		if d.limited {
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 {
			switch trimmed {
			case "dependencies:":
				section = dartSectionDeps
			case "dev_dependencies:":
				section = dartSectionDevDeps
			case "dependency_overrides:":
				section = dartSectionOverrides
			default:
				section = dartSectionNone
			}
			packageIndent, blockPkg = -1, ""
			continue
		}
		if section == dartSectionNone {
			continue
		}
		if packageIndent < 0 {
			packageIndent = indent
		}
		if indent > packageIndent {
			// A key inside the current package's block. Only a direct local
			// `path:` source is a project relationship. A `path:` under `git:`
			// names a directory inside a remote repository; it is never read
			// because only the block's first line can name the source.
			if blockPkg == "" {
				continue
			}
			if m := dartPathSublineRE.FindStringSubmatch(trimmed); m != nil {
				rawPath := strings.Trim(strings.TrimSpace(stripDartComment(m[1])), `"'`)
				if target, ok := LocalTarget(name, rawPath, "pubspec.yaml"); ok {
					AddReference(d, Reference{Kind: "pub-path-dependency", Value: blockPkg + " path:" + rawPath, Target: target, State: "declared", Evidence: name, Condition: dartSectionCondition(section)})
				} else {
					AddDiagnostic(d, "external-pub-path-dependency", "A pub path dependency is outside the selected inventory.")
				}
			}
			// Only the first nested level can carry the source key.
			blockPkg = ""
			continue
		}
		blockPkg = ""
		if indent != packageIndent {
			continue
		}
		m := dartDepLineRE.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		pkgName := m[1]
		if value := strings.TrimSpace(stripDartComment(trimmed[len(m[0]):])); value == "" {
			blockPkg = pkgName
		}
		// Overrides replace the source of a package declared elsewhere; they
		// do not declare a dependency of their own.
		if section != dartSectionOverrides {
			AddRequirement(d, Requirement{Kind: "dart-dependency", Value: pkgName, State: "declared", Evidence: name, Condition: dartSectionCondition(section)})
		}
	}
	return d
}

var dartDepLineRE = regexp.MustCompile(`^([a-z][a-z0-9_]{0,127})\s*:`)

// dartPathSublineRE matches a `path: value` line inside a dependency block.
// The value may be a relative path like `../` or `../packages/foo`.
var dartPathSublineRE = regexp.MustCompile(`^path:\s*(.+)$`)

func stripDartComment(value string) string {
	if i := strings.Index(value, " #"); i >= 0 {
		return value[:i]
	}
	if strings.HasPrefix(value, "#") {
		return ""
	}
	return value
}

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
