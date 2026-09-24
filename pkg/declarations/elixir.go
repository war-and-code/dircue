package declarations

import (
	"regexp"
	"unicode/utf8"
)

// ParseElixir reads mix.exs manifests. It never executes Elixir code.
// Extracts the app name from a static `app: :atom` declaration.
func ParseElixir(name string, content []byte) *Document {
	d := NewDocument(name, "elixir-mix")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-elixir-mix", "mix.exs exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "mix-exs-static-v1", State: "declared", Evidence: name})
	if m := elixirAppRE.FindSubmatch(content); m != nil {
		app := string(m[1])
		if elixirAtomOK(app) {
			d.Project.Name = app
			AddRequirement(d, Requirement{Kind: "elixir-app-name", Value: app, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-elixir-app-name", "A static app: :atom declaration was not found in mix.exs; the file was not executed.")
	}
	if m := elixirVersionRE.FindSubmatch(content); m != nil {
		ver := string(m[1])
		if len(ver) <= MaxStringBytes {
			d.Project.Version = ver
		}
	}
	return d
}

// Match `app: :atom_name` with optional surrounding whitespace/commas.
var elixirAppRE = regexp.MustCompile(`\bapp:\s*:([a-z][a-z0-9_]{0,127})`)
var elixirVersionRE = regexp.MustCompile(`\bversion:\s*"([A-Za-z0-9][A-Za-z0-9._-]{0,63})"`)

func elixirAtomOK(s string) bool {
	if s == "" || len(s) > MaxStringBytes {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

// ParseErlang reads rebar.config manifests. It never executes Erlang code.
// Erlang apps are identified by presence of rebar.config; the app name
// comes from the directory name (rebar.config has no mandatory name field).
func ParseErlang(name string, content []byte) *Document {
	d := NewDocument(name, "erlang-rebar")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-erlang-rebar", "rebar.config exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "rebar-config-static-v1", State: "declared", Evidence: name})
	// rebar.config doesn't have a mandatory app name; we record the marker present.
	AddDiagnostic(d, "erlang-name-from-directory", "rebar.config has no static app name field; the component name derives from its directory.")
	return d
}
