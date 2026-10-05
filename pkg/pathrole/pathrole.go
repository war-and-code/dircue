// Package pathrole classifies repository paths by conventional layout role.
// It reads only path names, never file content.
package pathrole

import (
	"path"
	"strings"
)

// Roles in priority order. A path that matches several roles takes the
// highest; a path that matches none is primary.
const (
	Vendored = "vendored"
	Fixture  = "fixture"
	Example  = "example"
	Test     = "test"
	Docs     = "docs"
	Tooling  = "tooling"
	Primary  = "primary"
)

var jvmSourceLanguage = map[string]bool{"java": true, "kotlin": true, "scala": true, "groovy": true, "resources": true}

var dotnetProjectExtension = map[string]bool{".csproj": true, ".vbproj": true, ".fsproj": true}

// Classify returns one of "vendored", "fixture", "example", "test", "docs",
// "tooling", or "" when no auxiliary role is inferred. Callers that need a
// total classification use Of.
func Classify(paths ...string) string {
	role, priority := "", 0
	choose := func(candidate string, rank int) {
		if rank > priority {
			role, priority = candidate, rank
		}
	}
	for _, filename := range paths {
		lower := strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
		segments := strings.Split(lower, "/")
		for i, segment := range segments {
			// Below a JVM source set (src/main/java, src/test/kotlin, ...) the
			// remaining segments are package names such as
			// org/springframework/samples, not project layout. The source set
			// itself decides: test source sets are tests, others are not.
			if segment == "src" && i+2 < len(segments) && jvmSourceLanguage[segments[i+2]] {
				if strings.Contains(segments[i+1], "test") {
					choose("test", 3)
				}
				break
			}
			switch segment {
			// vendored external code
			case "vendor", "node_modules", "third_party", ".bingo", "bingo":
				choose("vendored", 6)
			// test fixtures and evaluation truth data
			case "fixtures", "testdata", "__fixtures__", "truth", "cases", "snapshots", "corpus", "golden":
				choose("fixture", 5)
			// examples and demos
			case "examples", "samples", "demo", "demos":
				choose("example", 4)
			// test code
			case "test", "tests", "__tests__", "spec", "specs":
				choose("test", 3)
			// docs/release tooling
			case "docs", "doc", "documentation", "releasing", "translations", "i18n", "locale", "locales":
				choose("docs", 2)
			// other tooling (including devcontainer)
			case "tools", "tooling", "scripts", "hack", "ci", "infra", "benchmarks", "bench", "benches", "fuzz", ".devcontainer":
				choose("tooling", 1)
			default:
				// Directory segments that end with _test, _tests, test, tests
				// identify test modules or crates (e.g. ruff_mdtest, ty_test).
				if strings.HasSuffix(segment, "_test") || strings.HasSuffix(segment, "_tests") ||
					strings.HasSuffix(segment, "-test") || strings.HasSuffix(segment, "-tests") {
					choose("test", 3)
				}
			}
		}
		base := path.Base(lower)
		// .NET test projects by conventional suffixes
		if strings.Contains(base, ".tests.") || strings.HasSuffix(base, "test.csproj") || strings.HasSuffix(base, "tests.csproj") ||
			strings.HasSuffix(base, "test.vbproj") || strings.HasSuffix(base, "tests.vbproj") ||
			strings.HasSuffix(base, "test.fsproj") || strings.HasSuffix(base, "tests.fsproj") ||
			strings.HasSuffix(base, ".unittests.csproj") || strings.HasSuffix(base, ".functionaltests.csproj") ||
			strings.HasSuffix(base, ".integrationtests.csproj") {
			choose("test", 3)
		}
		// .NET project names ending in Tests/UnitTests/FunctionalTests. Other
		// files are excluded so names such as latest.go stay unclassified.
		nameNoExt := strings.TrimSuffix(base, path.Ext(base))
		if !dotnetProjectExtension[path.Ext(base)] {
			nameNoExt = ""
		}
		if strings.HasSuffix(nameNoExt, "tests") || strings.HasSuffix(nameNoExt, "test") ||
			strings.HasSuffix(nameNoExt, "unittests") || strings.HasSuffix(nameNoExt, "functionaltests") ||
			strings.HasSuffix(nameNoExt, "integrationtests") {
			choose("test", 3)
		}
		// .bingo directory as tooling
		if strings.Contains(lower, "/.bingo/") {
			choose("tooling", 1)
		}
	}
	return role
}

// Of returns Classify's role, or Primary when no auxiliary role applies.
func Of(paths ...string) string {
	if role := Classify(paths...); role != "" {
		return role
	}
	return Primary
}
