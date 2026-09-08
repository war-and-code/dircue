package enry

import "testing"

// General syntax from Linguist 9.7.0 modeline.rb, shebang.rb and heuristics.yml.
// No fixture names or content hashes participate in production classification.
func TestLinguistCompatibilitySyntax(t *testing.T) {
	tests := []struct{ name, filename, content, want string }{
		{"font-is-not-mode", "theme.gtkrc", `fontset = "-*-courier-medium-r-*-*-12-*-*-*-m-*-*-*"`, "GtkRC"},
		{"first-valid-mode", "file.txt", "# -*- ruby -*-\n# -*- python -*-\n", "Ruby"},
		{"explicit-mode", "file.txt", "# -*- coding:utf-8; mode: python; -*-\n", "Python"},
		{"adjacent-mode", "file.txt", "# -*-mode:ruby-*-\n", "Ruby"},
		{"vimball-archive", "archive.vba", "\" Vimball Archiver\nUseVimball\nfinish\n\" vim:ft=help\n", "Vim script"},
		{"adblock-header", "rules.txt", "[Adblock Plus 2.0]\n||example.com^\n", "Adblock Filter List"},
		{"adblock-repeated-grammar", "rules.txt", "[uBlock Origin 1.2; AdGuard 3.0]\nexample.com\n", "Adblock Filter List"},
		{"adblock-invalid-token", "rules.txt", "[Adblock Plus bogus]\nexample.com\n", "Text"},
		{"adblock-not-at-start", "rules.txt", "hello\n[Adblock Plus]\n", "Text"},
		{"node-commonjs", "launcher", "#!/usr/bin/env node\nrequire('./entry.js')\n", "JavaScript"},
		{"exec-wrapper", "tool", "#!/bin/sh\nexec python \"$0\" \"$@\"\n", "Python"},
		{"exec-with-flags-is-shell", "tool", "#!/bin/sh\nexec jq -nef \"$0\" \"$@\"\ntrue\n", "Shell"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetLanguage(tt.filename, []byte(tt.content)); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestSourceMapFooterLineBoundaries(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := "console.log(1);" + newline + "//# sourceMappingURL=main.js.map"
		if !IsGenerated("main.js", []byte(source+newline)) {
			t.Errorf("source map within last two lines missed for %q", newline)
		}
		if IsGenerated("main.js", []byte(source+newline+newline)) {
			t.Errorf("source map before last two lines incorrectly included for %q", newline)
		}
	}
	if IsGenerated("main.js", []byte("console.log(1);\r//# sourceMappingURL=x.map\r")) {
		t.Error("bare CR incorrectly split by Generated#lines")
	}
}
