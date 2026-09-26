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
	// Track if/unless/case and group do...end blocks to mark conditional and
	// group-scoped gems appropriately.
	var blockStack []gemfileBlock
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxStringBytes+1)
	for scanner.Scan() {
		if d.limited {
			break
		}
		text := strings.TrimSpace(scanner.Text())
		// Strip inline comments before block-keyword detection.
		if idx := strings.Index(text, " #"); idx > 0 {
			text = strings.TrimSpace(text[:idx])
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		// Track block closers first.
		if gemEndRE.MatchString(text) {
			if len(blockStack) > 0 {
				blockStack = blockStack[:len(blockStack)-1]
			}
			continue
		}
		// Try to recognise a block opener.
		if blk, ok := parseGemfileBlock(text); ok {
			blockStack = append(blockStack, blk)
			// A group line may also contain a gem on the same line (rare but legal).
		}
		if dep, ok := parseGemDep(text); ok {
			state, condition := gemfileDepState(blockStack)
			AddRequirement(d, Requirement{Kind: "ruby-gem-dependency", Value: dep, State: state, Evidence: name, Condition: condition})
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

// gemfileBlockKind classifies a Gemfile block for conditional tracking.
type gemfileBlockKind int

const (
	gemfileBlockGroup    gemfileBlockKind = iota // group :name do ... end
	gemfileBlockGroupOpt                         // group :name, optional: true do ... end
	gemfileBlockIf                               // if/unless/case ... end
	gemfileBlockDo                               // other do ... end blocks (e.g. source, platform)
)

type gemfileBlock struct {
	kind      gemfileBlockKind
	condition string // "group:name" or "if_block" or ""
	optional  bool   // true for optional groups
}

// gemGroupRE matches `group :name, :other do` or `group "name" do`.
// Captures the group name fragment (commas and quotes stripped later).
var gemGroupRE = regexp.MustCompile(`^group\s+(.+?)(?:\s+do)?\s*(?:#.*)?$`)

// gemGroupNameRE extracts individual group names from the group argument list.
var gemGroupNameRE = regexp.MustCompile(`[:'""]([A-Za-z0-9_]+)`)

// gemOptionalRE detects `optional: true` in a group line.
var gemOptionalRE = regexp.MustCompile(`\boptional\s*:\s*true\b`)

// gemBlockOpenRE matches lines that open a do...end block without a recognised keyword
// (e.g. source "..." do, platform :... do). We track these to keep end-counts correct.
var gemBlockOpenRE = regexp.MustCompile(`\bdo\s*(?:#.*)?$`)

// gemIfRE matches `if`, `unless`, `case` at the start of a statement.
var gemIfRE = regexp.MustCompile(`^(?:if|unless|case)\b`)

// gemEndRE matches a bare `end` statement.
var gemEndRE = regexp.MustCompile(`^end\b`)

// parseGemfileBlock tries to classify the current line as a block opener.
// Returns (block, true) when a block starts on this line, or a zero-value and false.
func parseGemfileBlock(line string) (gemfileBlock, bool) {
	// group :name, :other do
	if gm := gemGroupRE.FindStringSubmatch(line); gm != nil {
		args := gm[1]
		names := gemGroupNameRE.FindAllStringSubmatch(args, -1)
		var nameList []string
		for _, n := range names {
			if n[1] != "" {
				nameList = append(nameList, n[1])
			}
		}
		cond := ""
		if len(nameList) > 0 {
			cond = "group:" + strings.Join(nameList, ",")
		} else {
			cond = "group:unknown"
		}
		opt := gemOptionalRE.MatchString(args)
		kind := gemfileBlockGroup
		if opt {
			kind = gemfileBlockGroupOpt
		}
		return gemfileBlock{kind: kind, condition: cond, optional: opt}, true
	}
	// if / unless / case
	if gemIfRE.MatchString(line) {
		return gemfileBlock{kind: gemfileBlockIf, condition: "if_block"}, true
	}
	// other do...end blocks (source, platform, etc.)
	if gemBlockOpenRE.MatchString(line) {
		return gemfileBlock{kind: gemfileBlockDo}, true
	}
	return gemfileBlock{}, false
}

// gemfileDepState returns the State and Condition for a gem declaration
// given the current block stack.
func gemfileDepState(stack []gemfileBlock) (state, condition string) {
	for i := len(stack) - 1; i >= 0; i-- {
		b := stack[i]
		switch b.kind {
		case gemfileBlockIf:
			return "conditional", b.condition
		case gemfileBlockGroupOpt:
			return "conditional", b.condition
		case gemfileBlockGroup:
			return "declared", b.condition
		}
	}
	return "declared", ""
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

// railsModuleNameRE matches the first top-level module declaration in
// config/application.rb, e.g. "module Mastodon". The name must start with an
// uppercase letter and contain only alphanumeric characters.
var railsModuleNameRE = regexp.MustCompile(`(?m)^\s*module\s+([A-Z][A-Za-z0-9]+)\s*(?:#.*)?$`)

// ParseRailsApp reads config/application.rb to extract the Rails application
// module name. It never executes Ruby code and returns nil if no name is found.
// The resulting document uses kind "ruby-rails-app" and is not itself a
// component; it exists only to carry the name hint for the co-located Gemfile.
func ParseRailsApp(name string, content []byte) *Document {
	if len(content) > 64*1024 { // generous limit for a config file
		return nil
	}
	m := railsModuleNameRE.Find(content)
	if m == nil {
		return nil
	}
	sub := railsModuleNameRE.FindSubmatch(content)
	if len(sub) < 2 {
		return nil
	}
	appName := string(sub[1])
	if len(appName) > 128 {
		return nil
	}
	d := NewDocument(name, "ruby-rails-app")
	d.Project.Name = appName
	return d
}
