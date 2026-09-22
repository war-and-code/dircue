package enry

import (
	"bytes"
	"reflect"
	"testing"
)

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

func TestExtensionNarrowsPriorCandidates(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		candidates []string
		want       []string
	}{
		{"empty candidates use extension order", "tool.pl", nil, []string{"Perl", "Prolog", "Raku"}},
		{"intersection uses prior order", "header.h", []string{"Objective-C", "C", "C++"}, []string{"Objective-C", "C", "C++"}},
		{"intersection removes duplicates", "tool.pl", []string{"Pod", "Perl", "Perl"}, []string{"Perl"}},
		{"empty intersection", "tool.pl", []string{"Python"}, nil},
		{"generic extension preserves candidates", "mesh.stl", []string{"Text", "STL", "Text"}, []string{"Text", "STL", "Text"}},
		{"generic extension introduces no candidates", "settings.app", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetLanguagesByExtension(tt.filename, nil, tt.candidates)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}

	content := []byte("#!/usr/bin/perl\n=head1 NAME\nfoo :- bar\n")
	if got := GetLanguagesByContent("checkpatch.pl", content, nil); !reflect.DeepEqual(got, []string{"Prolog"}) {
		t.Fatalf("fixture must exercise the conflicting Prolog heuristic, got %v", got)
	}
	if got := GetLanguage("checkpatch.pl", content); got != "Perl" {
		t.Fatalf("shebang candidates were not narrowed by extension: got %q want Perl", got)
	}
	if got := GetLanguage("settings.app", []byte(`{"name":"settings","port":8080}`)); got != OtherLanguage {
		t.Fatalf("generic extension introduced an unconfirmed language: got %q", got)
	}
	if got := GetLanguage("settings.app", []byte("{application, kernel, []}.\n")); got != "Erlang" {
		t.Fatalf("generic extension blocked a heuristic match: got %q want Erlang", got)
	}
}

func TestFilenameNarrowsPriorCandidates(t *testing.T) {
	if got := GetLanguagesByFilename("Rakefile", nil, nil); !reflect.DeepEqual(got, []string{"Ruby"}) {
		t.Fatalf("empty candidates: got %v want [Ruby]", got)
	}
	if got := GetLanguagesByFilename("Rakefile", nil, []string{"Python", "Ruby", "Ruby"}); !reflect.DeepEqual(got, []string{"Ruby"}) {
		t.Fatalf("candidate intersection: got %v want [Ruby]", got)
	}
	if got := GetLanguagesByFilename("Rakefile", nil, []string{"Python"}); got != nil {
		t.Fatalf("empty intersection: got %v want nil", got)
	}
}

func TestHeuristicsUseFirst50KiB(t *testing.T) {
	const limit = 50 * 1024
	headerWithTryAt := func(offset int) []byte {
		content := bytes.Repeat([]byte("x"), offset-1)
		return append(content, []byte("\ntry_value;\n")...)
	}

	if got := GetLanguage("kvm_host.h", headerWithTryAt(limit-3)); got != "C++" {
		t.Fatalf("complete try at byte %d: got %q want C++", limit-3, got)
	}
	if got := GetLanguage("kvm_host.h", headerWithTryAt(limit-2)); got != "C" {
		t.Fatalf("truncated try at byte %d: got %q want C", limit-2, got)
	}
	if got := GetLanguage("kvm_host.h", headerWithTryAt(56423)); got != "C" {
		t.Fatalf("late try beyond 50 KiB: got %q want C", got)
	}

	multibytePrefix := bytes.Repeat([]byte("é"), limit/len([]byte("é")))
	multibyte := append(multibytePrefix, []byte("\ntry_value;\n")...)
	if got := GetLanguage("raw-bytes.h", multibyte); got != "C" {
		t.Fatalf("heuristic limit counted characters instead of raw bytes: got %q want C", got)
	}
}

func TestCentroidClassifierUsesFirst50KiB(t *testing.T) {
	const limit = 50 * 1024
	prefix := append([]byte("use strict; my $value = 1; sub value { return $value; }\n"), bytes.Repeat([]byte(" "), limit)...)
	prefix = prefix[:limit]
	suffix := bytes.Repeat([]byte("public class Example { public static void main(String[] args) {} }\n"), 1600)
	candidates := []string{"Java", "Perl"}
	want := GetLanguagesByClassifier("", prefix, candidates)
	if other := GetLanguagesByClassifier("", suffix[:limit], candidates); reflect.DeepEqual(other, want) {
		t.Fatalf("adversarial suffix does not distinguish classifier rankings: %v", want)
	}
	content := append(append([]byte{}, prefix...), suffix...)
	if got := GetLanguagesByClassifier("", content, candidates); !reflect.DeepEqual(got, want) {
		t.Fatalf("bytes after 50 KiB changed ranking: got %v want %v", got, want)
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
