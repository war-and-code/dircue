package componentmap

import "testing"

// Each producer stores different facts in Condition; only genuine build
// conditions keep a local relationship from becoming a definite edge.
func TestIsDefiniteRelationshipSeparatesScopesFromConditions(t *testing.T) {
	for _, tc := range []struct {
		kind, state, condition string
		want                   bool
	}{
		{"npm-local-dependency", "declared", "dependencies", true},
		{"npm-local-dependency", "declared", "devDependencies", true},
		{"npm-local-dependency", "declared", "optionalDependencies", true},
		{"npm-workspace-dependency", "resolved", "peerDependencies", true},
		{"npm-workspace-dependency", "unresolved", "dependencies", false},
		{"go-local-replacement", "declared", "example.com/mod@v1.2.3", false},
		{"pub-path-dependency", "declared", "dev_dependencies", true},
		{"pub-path-dependency", "declared", "dependency_overrides", true},
		{"cargo-path-dependency", "conditional", "dev-dependencies", true},
		{"cargo-path-dependency", "conditional", "build-dependencies", true},
		{"cargo-path-dependency", "conditional", "dependencies; target=cfg(windows)", false},
		{"cargo-workspace-path-dependency", "conditional", "dependencies; optional=true", false},
		{"uv-local-dependency", "resolved", "group:dev", true},
		{"uv-local-dependency", "resolved", "source-inherited-from:pyproject.toml", true},
		{"uv-local-dependency", "conditional", "extra:cli", false},
		{"uv-local-dependency", "conditional", "marker:sys_platform == 'win32'", false},
		{"project-reference", "declared", "", true},
		{"project-reference", "conditional", "'$(Configuration)' == 'Debug'", false},
		{"module", "declared", "profile:release", false},
		{"gradle-module", "conditional", "Gradle script evaluation", false},
		{"parent", "declared", "", true},
	} {
		if got := isDefiniteRelationship(tc.kind, tc.state, tc.condition); got != tc.want {
			t.Errorf("isDefiniteRelationship(%q, %q, %q) = %v, want %v", tc.kind, tc.state, tc.condition, got, tc.want)
		}
	}
}
