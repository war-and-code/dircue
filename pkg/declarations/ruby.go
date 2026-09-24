package declarations

import (
	"bufio"
	"bytes"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

func baseName(name string) string { return path.Base(name) }

// ParseRuby reads Gemfile or *.gemspec manifests. It never executes Ruby code.
// Gemfile: records a ruby-bundler component; name is taken from co-located
// *.gemspec when the gemspec name call is unambiguous, otherwise left blank.
// *.gemspec: records a ruby-gem component; name from the static name= literal.
func ParseRuby(name string, content []byte) *Document {
	base := baseName(name)
	if base == "Gemfile" {
		return parseGemfile(name, content)
	}
	if strings.HasSuffix(base, ".gemspec") {
		return parseGemspec(name, content)
	}
	return nil
}

var gemspecNameRE = regexp.MustCompile(`(?m)^\s*[A-Za-z_][A-Za-z0-9_.]*\s*\.\s*name\s*=\s*['"]([A-Za-z0-9][A-Za-z0-9_.-]*)['"]`)
var gemVersionRE = regexp.MustCompile(`(?m)^\s*[A-Za-z_][A-Za-z0-9_.]*\s*\.\s*version\s*=\s*['"]([A-Za-z0-9][A-Za-z0-9._-]*)['"]`)

func parseGemfile(name string, content []byte) *Document {
	d := NewDocument(name, "ruby-bundler")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-ruby-gemfile", "Gemfile exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "gemfile-static-v1", State: "declared", Evidence: name})
	// Parse gem dependencies from simple `gem 'name'` or `gem "name"` lines.
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxStringBytes+1)
	for scanner.Scan() {
		if d.limited {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if dep, ok := parseGemDep(text); ok {
			AddRequirement(d, Requirement{Kind: "ruby-gem-dependency", Value: dep, State: "declared", Evidence: name})
		}
	}
	return d
}

var gemDepRE = regexp.MustCompile(`^gem\s+['"]([A-Za-z0-9][A-Za-z0-9_.-]*)['"]`)

func parseGemDep(line string) (string, bool) {
	m := gemDepRE.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func parseGemspec(name string, content []byte) *Document {
	d := NewDocument(name, "ruby-gem")
	if len(content) > int(MaxManifestBytes) || !utf8.Valid(content) {
		d.Parsed = false
		AddDiagnostic(d, "invalid-ruby-gemspec", "Gemspec exceeds the byte limit or is not valid UTF-8.")
		return d
	}
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "gemspec-static-v1", State: "declared", Evidence: name})
	if m := gemspecNameRE.FindSubmatch(content); m != nil {
		n := string(m[1])
		if rubyNameOK(n) {
			d.Project.Name = n
			AddRequirement(d, Requirement{Kind: "ruby-gem-name", Value: n, State: "declared", Evidence: name})
		}
	} else {
		AddDiagnostic(d, "missing-ruby-gem-name", "A static .name= assignment was not found; gemspec was not executed.")
	}
	if m := gemVersionRE.FindSubmatch(content); m != nil {
		ver := string(m[1])
		if len(ver) <= MaxStringBytes {
			d.Project.Version = ver
		}
	}
	return d
}

func rubyNameOK(s string) bool {
	if s == "" || len(s) > MaxStringBytes {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
