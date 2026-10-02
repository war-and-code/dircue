package declarations

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ParseJulia reads Project.toml (Julia) manifests.
// Extracts `name = "value"` and `version = "value"`.
func ParseJulia(name string, content []byte) *Document {
	d := NewDocument(name, "julia-project")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-julia-project", "Project.toml exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	raw, err := ValidateTOML(content)
	if err != nil {
		d.Parsed = false
		AddDiagnostic(d, "invalid-julia-project", "Project.toml could not be parsed within TOML limits.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "julia-project-toml-v1", State: "declared", Evidence: name})
	if n, ok := raw["name"].(string); ok && juliaNameOK(n) {
		d.Project.Name = n
		AddRequirement(d, Requirement{Kind: "julia-project-name", Value: n, State: "declared", Evidence: name})
	} else {
		AddDiagnostic(d, "missing-julia-project-name", "Project.toml has no supported static name field.")
	}
	if v, ok := raw["version"].(string); ok && len(v) <= MaxStringBytes {
		d.Project.Version = v
	}
	return d
}

func juliaNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}

// ParseR reads DESCRIPTION (R package) manifests.
// Extracts `Package: name` and `Version: version`.
func ParseR(name string, content []byte) *Document {
	d := NewDocument(name, "r-package")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-r-description", "DESCRIPTION exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "r-description-v1", State: "declared", Evidence: name})
	if m := rPackageNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if rNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "r-package-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-r-package-name", "DESCRIPTION has no supported Package: field.")
	}
	if m := rVersionRE.FindSubmatch(content); m != nil {
		ver := strings.TrimSpace(string(m[1]))
		if len(ver) <= MaxStringBytes {
			d.Project.Version = ver
		}
	}
	return d
}

var rPackageNameRE = regexp.MustCompile(`(?m)^Package:\s*([A-Za-z][A-Za-z0-9.]{0,127})`)
var rVersionRE = regexp.MustCompile(`(?m)^Version:\s*([0-9][A-Za-z0-9._-]{0,63})`)

func rNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}

// ParseClojure reads deps.edn or project.clj (Clojure) manifests.
// deps.edn: records a clojure-deps component (no standard name field).
// project.clj: extracts `(defproject name/name version ...)`.
func ParseClojure(name string, content []byte) *Document {
	base := baseName(name)
	if base == "project.clj" {
		return parseLeiningenProject(name, content)
	}
	// deps.edn
	d := NewDocument(name, "clojure-deps")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-clojure-deps", "deps.edn exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "clojure-deps-edn-v1", State: "declared", Evidence: name})
	AddDiagnostic(d, "clojure-name-from-directory", "deps.edn has no standard project name field; the component name derives from its directory.")
	return d
}

func parseLeiningenProject(name string, content []byte) *Document {
	d := NewDocument(name, "clojure-leiningen")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-clojure-project", "project.clj exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "leiningen-project-clj-v1", State: "declared", Evidence: name})
	if m := leiningenNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if clojureNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "clojure-project-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-clojure-project-name", "A static defproject declaration was not found in project.clj.")
	}
	return d
}

// (defproject org.name/proj-name "1.0.0" ...)
// or (defproject name "1.0.0" ...)
var leiningenNameRE = regexp.MustCompile(`(?m)^\s*\(defproject\s+([A-Za-z][A-Za-z0-9_./\-]{0,127})\s`)

func clojureNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}

// ParsePerl reads cpanfile or Makefile.PL (Perl) manifests.
// cpanfile: records a perl-cpanfile component (no name in cpanfile).
// Makefile.PL: extracts NAME from WriteMakefile(NAME => 'dist-name').
func ParsePerl(name string, content []byte) *Document {
	base := baseName(name)
	if base == "Makefile.PL" {
		return parseMakefilePL(name, content)
	}
	// cpanfile
	d := NewDocument(name, "perl-cpanfile")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-perl-cpanfile", "cpanfile exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "cpanfile-static-v1", State: "declared", Evidence: name})
	AddDiagnostic(d, "perl-name-from-directory", "cpanfile has no project name field; the component name derives from its directory.")
	return d
}

func parseMakefilePL(name string, content []byte) *Document {
	d := NewDocument(name, "perl-extutils")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-perl-makefile-pl", "Makefile.PL exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "makefile-pl-static-v1", State: "declared", Evidence: name})
	if m := perlMakefileNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if perlNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "perl-dist-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-perl-dist-name", "A static NAME => 'value' was not found in Makefile.PL; the file was not executed.")
	}
	return d
}

var perlMakefileNameRE = regexp.MustCompile(`(?m)\bNAME\s*=>\s*['"]([A-Za-z][A-Za-z0-9_:.-]{0,127})['"]`)

func perlNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}

// ParseZig reads build.zig manifests. It never executes Zig code.
// Zig has no mandatory name in build.zig; we record the file as a zig component
// and derive the name from the directory.
func ParseZig(name string, content []byte) *Document {
	d := NewDocument(name, "zig-build")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-zig-build", "build.zig exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "zig-build-zig-v1", State: "declared", Evidence: name})
	// Check for the addExecutable / addStaticLibrary call for name.
	if m := zigExecutableNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if zigNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "zig-executable-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "zig-name-from-directory", "A static .name assignment was not found in build.zig; the component name derives from its directory.")
	}
	return d
}

// b.addExecutable(.{ .name = "myapp", ... }) in Zig 0.12+
// or b.addExecutable("myapp", ...) in earlier versions.
var zigExecutableNameRE = regexp.MustCompile(`\.name\s*=\s*"([A-Za-z0-9][A-Za-z0-9_.-]{0,127})"`)

func zigNameOK(s string) bool {
	return s != "" && len(s) <= MaxStringBytes
}

// setupCfgProjectSectionRE matches any [metadata] or [options] section header, which
// are the markers for a setuptools project declaration. A setup.cfg with only tool
// sections ([flake8], [mypy], [isort], etc.) is not a project declaration.
var setupCfgProjectSectionRE = regexp.MustCompile(`(?m)^\[(metadata|options)\]`)

// ParsePythonSetupCfg reads setup.cfg. If the file contains only tool-configuration
// sections (no [metadata] or [options]), nil is returned so that no spurious Python
// component is created. When project sections are present the name is extracted and
// the document participates in the normal auxiliary-doc deduplication.
func ParsePythonSetupCfg(name string, content []byte) *Document {
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d := newPythonAuxDocument(name, "setup-cfg-static-v1")
		d.Parsed = false
		AddDiagnostic(d, "invalid-python-setup-cfg", "setup.cfg exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	// A tool-configuration-only setup.cfg ([flake8], [mypy], [isort], etc.) is not a
	// Python project declaration; returning nil prevents a spurious (root) component.
	if !setupCfgProjectSectionRE.Match(content) {
		return nil
	}
	d := newPythonAuxDocument(name, "setup-cfg-static-v1")
	if m := setupCfgNameRE.FindSubmatch(content); m != nil {
		n := strings.TrimSpace(string(m[1]))
		if pythonNamePattern.MatchString(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "python-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-python-setup-cfg-name", "setup.cfg [metadata] section has no supported static name field.")
	}
	return d
}

// Match [metadata] section followed (possibly after blank/comment lines) by `name = value`.
var setupCfgNameRE = regexp.MustCompile(`(?ms)\[metadata\][^\[]*\bname\s*=\s*([A-Za-z0-9][A-Za-z0-9._-]{0,254})`)

// pyprojectIsToolOnly reports whether a parsed pyproject.toml TOML map is a
// tool-configuration-only file that should not produce a Python component.
//
// The core signal is the presence of a [dependency-groups] table (PEP 735)
// with actual entries alongside the ABSENCE of all distribution indicators.
// A pyproject.toml used only for managing dev/lint/test tooling (like gitea's
// root pyproject.toml in a Go repository) is suppressed; a minimal library
// with just [project] name+version is kept because it has no dev-group signal.
//
// A pyproject.toml is tool-only when ALL of the following hold:
//
//  1. Has a non-empty [dependency-groups] table — the PEP 735 dev-group
//     pattern is the distinguishing signal; without it a minimal [project]
//     table is treated as a genuine Python project.
//  2. No [build-system] — not used to build a Python distribution.
//  3. No [project].dependencies — no runtime dependency list (static).
//  4. No [project].dynamic — no dynamic fields (dynamic metadata implies
//     build-time resolution, i.e. a distribution).
//  5. No [project].optional-dependencies — no runtime extras.
//  6. No [project].scripts or [project].gui-scripts — no installed commands.
//  7. No packaging layout ([tool.setuptools.packages/find],
//     [tool.hatch.build], [tool.flit.metadata], [tool.pdm] build config).
//  8. Not a uv workspace coordinator ([tool.uv.workspace] absent).
//
// This is the pyproject.toml analogue of ParsePythonSetupCfg returning nil
// for a setup.cfg that has no [metadata] or [options] sections.
func pyprojectIsToolOnly(raw map[string]any) bool {
	// 1. Require a non-empty [dependency-groups] table as the positive signal.
	//    Without it a bare [project] table is a genuine (if minimal) library.
	groups, hasGroups := raw["dependency-groups"].(map[string]any)
	if !hasGroups || len(groups) == 0 {
		return false
	}

	// 2. [build-system] means this pyproject builds a Python package.
	if _, hasBuildSystem := raw["build-system"]; hasBuildSystem {
		return false
	}

	// 3–6. Check [project] for distribution indicators.
	if p, ok := raw["project"].(map[string]any); ok {
		// Runtime dependency list (static).
		if deps, ok := p["dependencies"].([]any); ok && len(deps) > 0 {
			return false
		}
		// Dynamic metadata fields imply build-time resolution.
		if dynList, ok := p["dynamic"].([]any); ok && len(dynList) > 0 {
			return false
		}
		// Optional extras (runtime dependency groups).
		if _, ok := p["optional-dependencies"]; ok {
			return false
		}
		// Console or GUI entry-point scripts.
		if _, ok := p["scripts"]; ok {
			return false
		}
		if _, ok := p["gui-scripts"]; ok {
			return false
		}
	}

	// 7–8. Inspect [tool.*] for packaging or workspace indicators.
	if tool, ok := raw["tool"].(map[string]any); ok {
		// Maturin is a distribution backend/configuration, including binary
		// bindings that install a command into the Python package.
		if _, ok := tool["maturin"]; ok {
			return false
		}
		// uv workspace coordinator — manages Python member projects.
		if uv, ok := tool["uv"].(map[string]any); ok {
			if _, hasWS := uv["workspace"]; hasWS {
				return false
			}
		}
		// setuptools packaging layout.
		if st, ok := tool["setuptools"].(map[string]any); ok {
			if _, ok := st["packages"]; ok {
				return false
			}
			if _, ok := st["find"]; ok {
				return false
			}
			if _, ok := st["find-namespace"]; ok {
				return false
			}
		}
		// Hatch build configuration.
		if hatch, ok := tool["hatch"].(map[string]any); ok {
			if _, ok := hatch["build"]; ok {
				return false
			}
		}
		// Flit metadata (legacy; implies a distributable library).
		if flit, ok := tool["flit"].(map[string]any); ok {
			if _, ok := flit["metadata"]; ok {
				return false
			}
		}
		// PDM build configuration.
		if pdm, ok := tool["pdm"].(map[string]any); ok {
			if build, ok := pdm["build"].(map[string]any); ok {
				if _, ok := build["package-includes"]; ok {
					return false
				}
			}
		}
	}
	return true
}
