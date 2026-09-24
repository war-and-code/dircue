package declarations

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ParseCMake reads CMakeLists.txt or meson.build manifests. It never executes build scripts.
// CMakeLists.txt: extracts project name from `project(Name ...)`.
// meson.build: extracts project name from `project('name', ...)` or `project("name", ...)`.
// configure.ac: extracts project name from AC_INIT([name], ...).
func ParseCMake(name string, content []byte) *Document {
	base := baseName(name)
	switch base {
	case "CMakeLists.txt":
		return parseCMakeLists(name, content)
	case "meson.build":
		return parseMesonBuild(name, content)
	case "configure.ac":
		return parseConfigureAC(name, content)
	}
	return nil
}

func parseCMakeLists(name string, content []byte) *Document {
	d := NewDocument(name, "cmake")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-cmake", "CMakeLists.txt exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "cmake-lists-static-v1", State: "declared", Evidence: name})
	if m := cmakeProjectRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if cmakeNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "cmake-project-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-cmake-project-name", "A static project() call was not found in CMakeLists.txt; the file was not executed.")
	}
	return d
}

func parseMesonBuild(name string, content []byte) *Document {
	d := NewDocument(name, "meson")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-meson", "meson.build exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "meson-build-static-v1", State: "declared", Evidence: name})
	if m := mesonProjectRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if cmakeNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "meson-project-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-meson-project-name", "A static project() call was not found in meson.build; the file was not executed.")
	}
	return d
}

func parseConfigureAC(name string, content []byte) *Document {
	d := NewDocument(name, "autoconf")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-autoconf", "configure.ac exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "autoconf-ac-static-v1", State: "declared", Evidence: name})
	if m := acInitRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if cmakeNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "autoconf-project-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-autoconf-project-name", "A static AC_INIT call was not found in configure.ac; the file was not executed.")
	}
	return d
}

// CMake project() first argument is an unquoted name.
var cmakeProjectRE = regexp.MustCompile(`(?im)^\s*project\s*\(\s*([A-Za-z0-9][A-Za-z0-9_.-]{0,127})`)

// Meson project() first argument is a quoted string.
var mesonProjectRE = regexp.MustCompile(`(?im)^\s*project\s*\(\s*['"]([^'"]{1,128})['"]`)

// AC_INIT([name], ...) or AC_INIT(name, ...)
var acInitRE = regexp.MustCompile(`(?im)\bAC_INIT\s*\(\s*\[?([A-Za-z0-9][A-Za-z0-9_. -]{0,127})\]?`)

func cmakeNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}
