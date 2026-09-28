package cli

import "testing"

func TestDetectNameForOS(t *testing.T) {
	cases := []struct {
		argv0 string
		goos  string
		want  string
	}{
		// dircue stays dircue on all platforms
		{"/usr/local/bin/dircue", "linux", "dircue"},
		{"dircue", "linux", "dircue"},
		{"dircue.exe", "windows", "dircue"},
		{"DIRCUE.EXE", "windows", "dircue"},
		{"/usr/local/bin/dircue", "darwin", "dircue"},

		// dirq is only matched when it is the exact basename
		{"dirq", "linux", "dirq"},
		{"dirq", "darwin", "dirq"},
		{"/usr/local/bin/dirq", "linux", "dirq"},
		{"/usr/local/bin/dirq", "darwin", "dirq"},
		{"dirq.exe", "windows", "dirq"},
		{"dirq.exe", "linux", "dirq"},
		{"DIRQ.EXE", "windows", "dirq"},
		{"DIRQ", "windows", "dirq"},
		{"Dirq", "windows", "dirq"}, // case-insensitive on Windows

		// case-sensitive on non-Windows: "Dirq" and "DIRQ" should stay dircue
		{"Dirq", "linux", "dircue"},
		{"DIRQ", "linux", "dircue"},
		{"Dirq", "darwin", "dircue"},

		// partial matches should not match
		{"dircue-dirq", "linux", "dircue"},
		{"mydirq", "linux", "dircue"},
		{"dirqx", "linux", "dircue"},

		// paths with directories (using forward slashes, portable across host platforms)
		{"/opt/bin/dirq", "linux", "dirq"},
		{"/opt/bin/dircue", "linux", "dircue"},

		// empty argv0 should default to dircue
		{"", "linux", "dircue"},
		{"", "windows", "dircue"},
	}
	for _, c := range cases {
		got := detectNameForOS(c.argv0, c.goos)
		if got != c.want {
			t.Errorf("detectNameForOS(%q, %q) = %q; want %q", c.argv0, c.goos, got, c.want)
		}
	}
}
