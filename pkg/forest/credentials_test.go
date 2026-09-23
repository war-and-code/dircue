package forest_test

import (
	"testing"

	"dircue/pkg/forest"
)

func TestStripCredentials(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "https_with_user_and_token",
			input: "https://user:ghp_canary_token@github.com/org/repo.git",
			want:  "https://github.com/org/repo.git",
		},
		{
			name:  "https_with_user_only",
			input: "https://user@github.com/org/repo.git",
			want:  "https://github.com/org/repo.git",
		},
		{
			name:  "https_no_credentials",
			input: "https://github.com/org/repo.git",
			want:  "https://github.com/org/repo.git",
		},
		{
			name:  "scp_git_at",
			input: "git@github.com:org/repo.git",
			want:  "github.com:org/repo.git",
		},
		{
			name:  "scp_custom_user",
			input: "deploy@bitbucket.org:myteam/myrepo.git",
			want:  "bitbucket.org:myteam/myrepo.git",
		},
		{
			name:  "https_with_query_and_fragment",
			input: "https://user:tok@host.example.com/repo.git?foo=bar#section",
			want:  "https://host.example.com/repo.git",
		},
		{
			name:  "scp_with_query",
			input: "git@host.example.com:org/repo.git?token=abc",
			want:  "host.example.com:org/repo.git",
		},
		{
			name:  "ssh_url",
			input: "ssh://git@github.com/org/repo.git",
			want:  "ssh://github.com/org/repo.git",
		},
		{
			name:  "git_protocol",
			input: "git://github.com/org/repo.git",
			want:  "git://github.com/org/repo.git",
		},
		{
			name:  "empty_string",
			input: "",
			want:  "",
		},
		{
			name:  "canary_token_https",
			input: "https://canary_token_1234567890abcdef@corp.example.com/repo.git",
			want:  "https://corp.example.com/repo.git",
		},
		{
			name:  "no_scheme_no_at_unchanged",
			input: "github.com/org/repo.git",
			want:  "github.com/org/repo.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forest.StripCredentials(tt.input)
			if got != tt.want {
				t.Errorf("StripCredentials(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
