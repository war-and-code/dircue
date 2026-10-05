package pathrole

import "testing"

func TestOfIsTotalAndUsesThePriorityOrder(t *testing.T) {
	for path, want := range map[string]string{
		"package.json": Primary,
		"workspaces/arborist/test/fixtures/a/package.json": Fixture,
		"vendor/github.com/x/test/go.mod":                  Vendored,
		"examples/app/tests/package.json":                  Example,
		"src/App.Tests/App.Tests.csproj":                   Test,
		"src/test/java/org/example/docs/Main.java":         Test,
		"docs/site/package.json":                           Docs,
		"scripts/release/package.json":                     Tooling,
	} {
		if got := Of(path); got != want {
			t.Errorf("Of(%q) = %q, want %q", path, got, want)
		}
	}
	if got := Of("tools/a/package.json", "test/b/package.json"); got != Test {
		t.Errorf("multiple paths should take the highest role, got %q", got)
	}
}
