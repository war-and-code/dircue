package declarations

import (
	"strings"
	"testing"
)

func TestParseRailsApp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string // expected app name; "" means nil document expected
	}{
		{
			name:    "config/application.rb",
			content: "require_relative \"boot\"\n\nmodule Mastodon\n  class Application < Rails::Application\n  end\nend\n",
			want:    "Mastodon",
		},
		{
			name:    "config/application.rb",
			content: "module MyApp\n  class Application < Rails::Application\n  end\nend\n",
			want:    "MyApp",
		},
		{
			name:    "config/application.rb",
			content: "# no module here\nclass Application < Rails::Application\nend\n",
			want:    "",
		},
		{
			// lowercase module name — not a valid Rails app module
			name:    "config/application.rb",
			content: "module myapp\n  class Application\n  end\nend\n",
			want:    "",
		},
	} {
		doc := ParseRailsApp(tc.name, []byte(tc.content))
		if tc.want == "" {
			if doc != nil {
				t.Errorf("ParseRailsApp(%q, %q): expected nil, got doc with name %q", tc.name, tc.content, doc.Project.Name)
			}
		} else {
			if doc == nil {
				t.Errorf("ParseRailsApp(%q, ...): expected doc with name %q, got nil", tc.name, tc.want)
				continue
			}
			if doc.Project.Kind != "ruby-rails-app" {
				t.Errorf("ParseRailsApp: kind %q, want %q", doc.Project.Kind, "ruby-rails-app")
			}
			if doc.Project.Name != tc.want {
				t.Errorf("ParseRailsApp: name %q, want %q", doc.Project.Name, tc.want)
			}
		}
	}
}

func TestIsManifestRailsApp(t *testing.T) {
	shouldMatch := []string{
		"config/application.rb",
		"app/config/application.rb",
		"services/myapp/config/application.rb",
	}
	shouldNotMatch := []string{
		"application.rb",
		"config/application_helper.rb",
		"app/controllers/application_controller.rb",
		"lib/application.rb",
	}
	for _, p := range shouldMatch {
		if !IsManifest(p) {
			t.Errorf("IsManifest(%q) = false, want true", p)
		}
	}
	for _, p := range shouldNotMatch {
		if IsManifest(p) {
			t.Errorf("IsManifest(%q) = true, want false", p)
		}
	}
}

// TestGemfileConditionalBlocks verifies that gems inside if/unless/case blocks
// are marked conditional, group-scoped gems carry group conditions, and optional
// groups carry both conditional state and group condition (#fix-3).
// These tests fail on 427c2f8 (no block tracking) and pass after.
func TestGemfileConditionalBlocks(t *testing.T) {
	discourse := `source "https://rubygems.org"

gem "rails", "~> 7.0"

if ENV["IMPORT"] == "1"
  gem "mysql2"
  gem "sequel"
end

group :development, :test do
  gem "rspec-rails"
  gem "factory_bot_rails"
end

group :optional_feature, optional: true do
  gem "sidekiq-pro"
end

unless RUBY_PLATFORM == "java"
  gem "oj"
end
`
	d := ParseRuby("Gemfile", []byte(discourse))
	if d == nil {
		t.Fatal("ParseRuby returned nil")
	}

	findReq := func(value string) *Requirement {
		for i := range d.Project.Requirements {
			if d.Project.Requirements[i].Kind == "ruby-gem-dependency" && d.Project.Requirements[i].Value == value {
				return &d.Project.Requirements[i]
			}
		}
		return nil
	}

	// rails is unconditional.
	r := findReq("rails")
	if r == nil {
		t.Error("rails gem not found")
	} else {
		if r.State != "declared" {
			t.Errorf("rails state = %q, want declared", r.State)
		}
		if r.Condition != "" {
			t.Errorf("rails condition = %q, want empty", r.Condition)
		}
	}

	// mysql2 is inside if ENV[...] → conditional.
	r = findReq("mysql2")
	if r == nil {
		t.Error("mysql2 gem not found")
	} else {
		if r.State != "conditional" {
			t.Errorf("mysql2 state = %q, want conditional", r.State)
		}
		if r.Condition != "if_block" {
			t.Errorf("mysql2 condition = %q, want if_block", r.Condition)
		}
	}

	// sequel is also inside the same if block.
	r = findReq("sequel")
	if r == nil {
		t.Error("sequel gem not found")
	} else if r.State != "conditional" {
		t.Errorf("sequel state = %q, want conditional", r.State)
	}

	// rspec-rails is inside group :development, :test → declared with group condition.
	r = findReq("rspec-rails")
	if r == nil {
		t.Error("rspec-rails gem not found")
	} else {
		if r.State != "declared" {
			t.Errorf("rspec-rails state = %q, want declared", r.State)
		}
		if r.Condition == "" {
			t.Errorf("rspec-rails has no group condition")
		}
		if !strings.HasPrefix(r.Condition, "gemfile-group:") {
			t.Errorf("rspec-rails condition = %q, want gemfile-group:...", r.Condition)
		}
	}

	// sidekiq-pro is inside group :optional_feature, optional: true → conditional.
	r = findReq("sidekiq-pro")
	if r == nil {
		t.Error("sidekiq-pro gem not found")
	} else {
		if r.State != "conditional" {
			t.Errorf("sidekiq-pro state = %q, want conditional", r.State)
		}
		if !strings.HasPrefix(r.Condition, "gemfile-group:") {
			t.Errorf("sidekiq-pro condition = %q, want gemfile-group:...", r.Condition)
		}
	}

	// oj is inside unless ... → conditional.
	r = findReq("oj")
	if r == nil {
		t.Error("oj gem not found")
	} else {
		if r.State != "conditional" {
			t.Errorf("oj state = %q, want conditional", r.State)
		}
	}
}

func TestGemfileUnconditionalUnaffected(t *testing.T) {
	gemfile := `gem "rails"
gem "pg"
gem "puma"
`
	d := ParseRuby("Gemfile", []byte(gemfile))
	if d == nil {
		t.Fatal("ParseRuby returned nil")
	}
	for _, r := range d.Project.Requirements {
		if r.Kind != "ruby-gem-dependency" {
			continue
		}
		if r.State != "declared" || r.Condition != "" {
			t.Errorf("gem %q: state=%q condition=%q, want state=declared condition=''", r.Value, r.State, r.Condition)
		}
	}
}

func TestGemfileOneLineAndModifierConditionals(t *testing.T) {
	body := "source \"https://rubygems.org\"\nif ENV[\"X\"] then gem \"mysql2\" end\ngem \"tiny_tds\" if ENV[\"IMPORT\"]\ngem \"rails\"\n"
	d := parseGemfile("Gemfile", []byte(body))
	got := map[string][2]string{}
	for _, r := range d.Project.Requirements {
		if r.Kind == "ruby-gem-dependency" {
			got[r.Value] = [2]string{r.State, r.Condition}
		}
	}
	if got["mysql2"][0] != "conditional" || got["tiny_tds"][0] != "conditional" {
		t.Fatalf("one-line and modifier gems must be conditional: %v", got)
	}
	if got["rails"] != [2]string{"declared", ""} {
		t.Fatalf("a one-line block must not leave later gems conditional: %v", got)
	}
}
