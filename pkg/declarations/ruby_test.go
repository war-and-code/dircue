package declarations

import "testing"

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
