package intentmap

import "testing"

func TestGemfileToolingGroup(t *testing.T) {
	for condition, want := range map[string]bool{
		"gemfile-group:development":        true,
		"gemfile-group:test":               true,
		"gemfile-group:development,test":   true,
		"gemfile-group:production":         false,
		"gemfile-group:development,assets": false,
		"gemfile-group:generic_import":     false,
		"group:dev":                        false,
		"":                                 false,
	} {
		if got := gemfileToolingGroup(condition); got != want {
			t.Errorf("gemfileToolingGroup(%q) = %v, want %v", condition, got, want)
		}
	}
}
