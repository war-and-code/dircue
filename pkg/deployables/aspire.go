package deployables

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// This lexer intentionally recognizes only enough C# to bind the common
// top-level Aspire declaration shape. It is not a C# parser or evaluator.
type csToken struct {
	kind        string
	text        string
	line        int
	conditional bool
	escaped     bool
}

const maxAspireCSharpTokens = 32768
const maxAspireSourceReferences = 2048
const maxAspireInterpolationDepth = 64
const maxAspireAliasFiles = 512
const maxAspireAliasFileBytes = 256 << 10
const maxAspireAliasInputBytes = 4 << 20

// AspireBounds exposes the finite parser limits for deterministic map settings.
func AspireBounds() (tokens, interpolationDepth, sourceReferences int) {
	return maxAspireCSharpTokens, maxAspireInterpolationDepth, maxAspireSourceReferences
}

// AspireAliasBounds exposes the separate selected-source global-using scan
// limits. The scan runs only when a supported AppHost declaration is present.
func AspireAliasBounds() (files, fileBytes int, inputBytes int64) {
	return maxAspireAliasFiles, maxAspireAliasFileBytes, maxAspireAliasInputBytes
}

type aspireAliasIssue struct {
	path string
	code string
}

type aspireAliasSource struct {
	path string
	data []byte
	size int64
}

func isCSharpSource(name string) bool {
	return strings.EqualFold(path.Ext(name), ".cs")
}

func isDotnetProjectFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".csproj", ".fsproj", ".vbproj":
		return true
	default:
		return false
	}
}

func aspireHostRootsForSource(file string) []string {
	var roots []string
	dir := path.Dir(file)
	for dir != "." && dir != "/" {
		if isAppHostDir(dir) {
			roots = append(roots, dir)
		}
		dir = path.Dir(dir)
	}
	return roots
}

func isPathWithin(root, file string) bool {
	return file == root || strings.HasPrefix(file, strings.TrimSuffix(root, "/")+"/")
}

func possibleAspireGlobalAlias(content []byte) bool {
	if !bytes.Contains(content, []byte("global")) || !bytes.Contains(content, []byte("using")) {
		return false
	}
	return bytes.Contains(content, []byte("Projects")) || bytes.Contains(content, []byte("DistributedApplication")) || bytes.Contains(content, []byte("Project")) || bytes.Contains(content, []byte("Distributed")) || bytes.Contains(content, []byte("\\"))
}

func hasGlobalAspireAlias(tokens []csToken) bool {
	for i := 0; i+3 < len(tokens); i++ {
		if tokens[i].text == "global" && tokens[i+1].text == "using" &&
			(tokens[i+2].text == "Projects" || tokens[i+2].text == "DistributedApplication") && tokens[i+3].text == "=" {
			return true
		}
	}
	return false
}

func inspectAspireAliasSource(source aspireAliasSource) (bool, error) {
	if int64(len(source.data)) != source.size || source.size > maxAspireAliasFileBytes {
		return false, fmt.Errorf("incomplete or oversized C# source")
	}
	// The token prefilter is byte-oriented. UTF-16 or embedded NUL input must
	// not bypass it and then be treated as a complete source view.
	if bytes.IndexByte(source.data, 0) >= 0 || bytes.HasPrefix(source.data, []byte{0xff, 0xfe}) || bytes.HasPrefix(source.data, []byte{0xfe, 0xff}) {
		return false, fmt.Errorf("unsupported C# source encoding")
	}
	if !utf8.Valid(source.data) {
		return false, fmt.Errorf("invalid UTF-8 C# source")
	}
	if !possibleAspireGlobalAlias(source.data) {
		return false, nil
	}
	tokens, err := lexAspireCSharp(string(source.data))
	if err != nil {
		return false, err
	}
	return hasGlobalAspireAlias(tokens), nil
}

// projectUsingAlias detects the supported SDK-generated global alias forms in
// an AppHost project file. XML decoding ensures comments and lookalike text do
// not trigger the guard.
func projectUsingAlias(data []byte) (bool, error) {
	if bytes.IndexByte(data, 0) >= 0 || bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) || !utf8.Valid(data) {
		return false, fmt.Errorf("unsupported project file encoding")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	stack := make([]string, 0, 8)
	appHostSDK := false
	usingAlias := false
	unresolvedAlias := false
	projectNS := ""
	observeAlias := func(alias string) {
		alias = strings.TrimSpace(alias)
		if alias == "Projects" || alias == "DistributedApplication" {
			usingAlias = true
		}
		if strings.Contains(alias, "$(") || strings.Contains(alias, "@(") || strings.Contains(alias, "%(") {
			unresolvedAlias = true
		}
	}
	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				if appHostSDK && unresolvedAlias {
					return false, fmt.Errorf("unresolved SDK global alias metadata")
				}
				return appHostSDK && usingAlias, nil
			}
			return false, err
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			// MSBuild item metadata may be attributes or child elements:
			// https://learn.microsoft.com/en-us/visualstudio/msbuild/msbuild-items
			if len(stack) == 3 && stack[0] == "Project" && stack[1] == "ItemGroup" && stack[2] == "Using" && elem.Name.Local == "Alias" && elem.Name.Space == projectNS {
				var alias string
				if err := decoder.DecodeElement(&alias, &elem); err != nil {
					return false, err
				}
				observeAlias(alias)
				continue
			}
			if len(stack) == 0 && elem.Name.Local == "Project" {
				projectNS = elem.Name.Space
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Sdk" && strings.Contains(attr.Value, "Aspire.AppHost.Sdk") {
						appHostSDK = true
					}
				}
			}
			if len(stack) == 1 && stack[0] == "Project" && elem.Name.Local == "Sdk" && elem.Name.Space == projectNS {
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Name" && strings.Contains(attr.Value, "Aspire.AppHost.Sdk") {
						appHostSDK = true
					}
				}
			}
			if len(stack) == 2 && stack[0] == "Project" && stack[1] == "ItemGroup" && elem.Name.Local == "Using" && elem.Name.Space == projectNS {
				for _, attr := range elem.Attr {
					if attr.Name.Local == "Alias" && attr.Name.Space == "" {
						observeAlias(attr.Value)
					}
				}
			}
			stack = append(stack, elem.Name.Local)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1] != elem.Name.Local {
				return false, fmt.Errorf("malformed project XML nesting")
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func inspectAspireGlobalAliases(ctx context.Context, appRoot, appProgram string, files []Candidate, projectRoots map[string]bool) (*aspireAliasIssue, error) {
	if !projectRoots[appRoot] {
		return nil, nil
	}
	sources := make([]Candidate, 0)
	for _, file := range files {
		projectInScope := strings.EqualFold(path.Ext(file.Path), ".csproj") && path.Dir(file.Path) == appRoot
		if file.Path == appProgram || (!isCSharpSource(file.Path) && !projectInScope) || !isPathWithin(appRoot, file.Path) {
			continue
		}
		sources = append(sources, file)
	}
	slices.SortFunc(sources, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	var total int64
	for i, file := range sources {
		if i >= maxAspireAliasFiles || file.Size < 0 || file.Size > maxAspireAliasFileBytes || file.Size > maxAspireAliasInputBytes-total {
			return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_limit"}, nil
		}
		total += file.Size
		if file.Read == nil {
			return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_incomplete"}, nil
		}
		content, size, err := file.Read(ctx, int64(maxAspireAliasFileBytes)+1)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_incomplete"}, nil
		}
		if size != file.Size || int64(len(content)) != size {
			return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_incomplete"}, nil
		}
		if strings.EqualFold(path.Ext(file.Path), ".csproj") {
			aliased, inspectErr := projectUsingAlias(content)
			if inspectErr != nil {
				return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_incomplete"}, nil
			}
			if aliased {
				return &aspireAliasIssue{path: file.Path, code: "aspire_global_alias"}, nil
			}
			continue
		}
		aliased, inspectErr := inspectAspireAliasSource(aspireAliasSource{path: file.Path, data: content, size: size})
		if inspectErr != nil {
			return &aspireAliasIssue{path: file.Path, code: "aspire_alias_context_incomplete"}, nil
		}
		if aliased {
			return &aspireAliasIssue{path: file.Path, code: "aspire_global_alias"}, nil
		}
	}
	return nil, nil
}

func parseAspireAppHost(name string, content []byte) ([]Definition, bool, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, false, fmt.Errorf("Aspire AppHost Program.cs contains binary data")
	}
	// Program.cs is a common candidate path across ordinary .NET projects.
	// The supported subset always contains both spellings, so skip lexing
	// irrelevant files with a cheap bounded fixed-string pre-screen.
	if !bytes.Contains(content, []byte("DistributedApplication")) || !bytes.Contains(content, []byte("AddProject")) {
		return nil, false, nil
	}
	source := strings.TrimPrefix(string(content), "\uFEFF")
	tokens, err := lexAspireCSharp(source)
	if err != nil {
		return nil, false, err
	}
	if hasCustomProjectsBinding(tokens) || hasCustomDistributedApplicationBinding(tokens) || hasVisibleAddProjectDeclaration(tokens) {
		return nil, false, nil
	}
	var statements [][]csToken
	var current []csToken
	brace, paren, bracket := 0, 0, 0
	for _, t := range tokens {
		if t.text == "{" {
			brace++
		}
		if t.text == "}" {
			brace--
			if brace == 0 {
				continue
			}
		}
		if brace < 0 {
			return nil, false, fmt.Errorf("malformed C# braces")
		}
		if brace == 0 {
			switch t.text {
			case "(":
				paren++
			case ")":
				paren--
			case "[":
				bracket++
			case "]":
				bracket--
			}
			if paren < 0 || bracket < 0 {
				return nil, false, fmt.Errorf("malformed C# delimiters")
			}
			if t.text == ";" && paren == 0 && bracket == 0 {
				if len(current) > 0 {
					statements = append(statements, current)
					current = nil
				}
				continue
			}
			current = append(current, t)
		}
	}
	if brace != 0 || paren != 0 || bracket != 0 {
		return nil, false, fmt.Errorf("malformed C# delimiters")
	}
	var builderAt = -1
	var refs []Reference
	validCreation := func(s []csToken) int {
		// var builder = DistributedApplication.CreateBuilder(...)
		if len(s) < 8 || s[0].text != "var" || s[1].text != "builder" || s[2].text != "=" ||
			s[3].text != "DistributedApplication" || s[4].text != "." || s[5].text != "CreateBuilder" || s[6].text != "(" {
			return -1
		}
		if matchingParen(s, 6) != len(s)-1 {
			return -1
		}
		for _, t := range s {
			if t.conditional {
				return -1
			}
		}
		return s[0].line
	}
	for si, s := range statements {
		if line := validCreation(s); line > 0 {
			if builderAt >= 0 {
				builderAt = -2
				continue
			}
			builderAt = si
		}
	}
	if builderAt < 0 {
		return nil, false, nil
	}
	creationLine := validCreation(statements[builderAt])
	for si, s := range statements {
		if si <= builderAt {
			continue
		}
		callIndex := findAspireAddProject(s)
		if callIndex >= 0 {
			if containsConditional(s) || !directAspireStatement(s, callIndex) {
				continue
			}
			ident := s[callIndex+4].text
			open := callIndex + 6
			close := matchingParen(s, open)
			if close < 0 || open+1 >= close || s[open+1].kind != "string" || open+2 < close && s[open+2].text != "," {
				continue
			}
			if len(refs) >= maxAspireSourceReferences {
				return nil, false, fmt.Errorf("Aspire static project call observation limit exceeded")
			}
			refs = append(refs, Reference{Kind: "aspire_project", Value: bounded(ident), Qualification: "declared", Evidence: Evidence{Field: "AddProject", Value: bounded(ident), Line: s[callIndex].line, Basis: "aspire-csharp-top-level-static"}})
			continue
		}
		// Any other top-level use or reassignment of builder could shadow or
		// replace the object we observed. Stop attributing calls after it.
		if reassignsAspireBuilder(s) {
			refs = nil
			break
		}
	}
	if len(refs) == 0 {
		return nil, false, nil
	}
	appHostName := path.Base(path.Dir(name))
	d := Definition{Kind: "service", Provider: "aspire-apphost", Name: bounded(appHostName), Coverage: "qualified", Evidence: []Evidence{{Field: "aspire-apphost", Value: bounded(appHostName), Line: creationLine, Basis: "aspire-apphost-top-level-builder"}}, References: refs}
	return []Definition{d}, true, nil
}

// hasVisibleAddProjectDeclaration declines attribution when the AppHost source
// itself declares an unqualified generic AddProject method. Without C# symbol
// binding we cannot prove which extension method a call resolves to.
func hasVisibleAddProjectDeclaration(tokens []csToken) bool {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind == "ident" && tokens[i].text == "AddProject" && !tokens[i].escaped && tokens[i+1].text == "<" && (i == 0 || tokens[i-1].text != ".") {
			// Any unqualified generic use may bind to a same-source custom
			// declaration or extension. Do not try to parse the remainder here:
			// a conservative linear guard avoids quadratic scans on malformed
			// nested angle/parameter sequences.
			return true
		}
	}
	return false
}

func findAspireAddProject(s []csToken) int {
	for i := 0; i+7 < len(s); i++ {
		if s[i].text == "builder" && s[i+1].text == "." && s[i+2].text == "AddProject" && s[i+3].text == "<" && s[i+4].text == "Projects" && s[i+5].text == "." && s[i+6].kind == "ident" && s[i+7].text == ">" && i+8 < len(s) && s[i+8].text == "(" {
			return i + 2
		}
	}
	return -1
}

func directAspireStatement(s []csToken, add int) bool {
	start := add - 2 // builder
	if start == 0 {
		return true
	}
	if start < 3 || s[start-1].text != "=" {
		return false
	}
	// A local declaration may have a type; reject control flow, invocation,
	// member access, and all other expression prefixes.
	for _, t := range s[:start-1] {
		if t.text == "=" || t.text == "(" || t.text == "." || t.text == "return" {
			return false
		}
	}
	return true
}

func matchingParen(s []csToken, open int) int {
	if open < 0 || open >= len(s) || s[open].text != "(" {
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		if s[i].text == "(" {
			depth++
		}
		if s[i].text == ")" {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
func containsConditional(s []csToken) bool {
	for _, t := range s {
		if t.conditional {
			return true
		}
	}
	return false
}
func hasCustomProjectsBinding(tokens []csToken) bool {
	if hasVarDeconstructionBinding(tokens, "Projects") {
		return true
	}
	ranges := newAspireBindingRanges(tokens)
	for i, t := range tokens {
		if t.kind != "ident" {
			continue
		}
		if !t.escaped && (t.text == "class" || t.text == "struct" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(tokens) && tokens[i+1].text == "Projects" {
			return true
		}
		if !t.escaped && t.text == "namespace" {
			if ranges.containsProjectsBeforeNamespaceBoundary(i) {
				return true
			}
		}
		if !t.escaped && (t.text == "using" || t.text == "global" && i+1 < len(tokens) && tokens[i+1].text == "using" && !tokens[i+1].escaped) {
			start := i
			if t.text == "global" {
				start = i + 1
			}
			if ranges.containsProjectsBeforeSemicolon(start) {
				return true
			}
		}
		if !t.escaped && t.text == "var" && i+1 < len(tokens) && tokens[i+1].text == "Projects" {
			return true
		}
		if t.text == "Projects" && i+1 < len(tokens) && (tokens[i+1].text == "=" || tokens[i+1].text == "++" || tokens[i+1].text == "--") && (i == 0 || tokens[i-1].text != ".") {
			return true
		}
	}
	return false
}

func hasCustomDistributedApplicationBinding(tokens []csToken) bool {
	if hasVarDeconstructionBinding(tokens, "DistributedApplication") {
		return true
	}
	ranges := newAspireBindingRanges(tokens)
	for i, t := range tokens {
		if t.kind != "ident" {
			continue
		}
		if !t.escaped && (t.text == "class" || t.text == "struct" || t.text == "interface" || t.text == "enum" || t.text == "record") && i+1 < len(tokens) && tokens[i+1].text == "DistributedApplication" {
			return true
		}
		if !t.escaped && t.text == "using" {
			if ranges.containsDistributedApplicationBeforeSemicolon(i) {
				return true
			}
		}
		if t.text == "DistributedApplication" && i+1 < len(tokens) && tokens[i+1].text == "=" && (i == 0 || tokens[i-1].text != ".") {
			// A local/value binding wins over the type name at an expression
			// position. Refuse attribution rather than treating its factory as
			// Aspire's CreateBuilder.
			return true
		}
	}
	return false
}

// hasVarDeconstructionBinding detects locals introduced by
// `var (name, ...) = ...`. It builds delimiter and prefix indexes in linear
// time so hostile repeated or nested token sequences cannot make the shadow
// check quadratic.
func hasVarDeconstructionBinding(tokens []csToken, name string) bool {
	closing := make([]int, len(tokens))
	for i := range closing {
		closing[i] = -1
	}
	prefix := make([]int, len(tokens)+1)
	stack := make([]int, 0)
	for i, t := range tokens {
		prefix[i+1] = prefix[i]
		if t.text == name {
			prefix[i+1]++
		}
		switch t.text {
		case "(":
			stack = append(stack, i)
		case ")":
			if len(stack) > 0 {
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				closing[open] = i
			}
		}
	}
	for i, t := range tokens {
		if t.text == "(" {
			end := closing[i]
			if end > i && end+1 < len(tokens) && tokens[end+1].text == "=" && prefix[end]-prefix[i] > 0 {
				// This includes both `var (x, y) = ...` and typed declaration
				// forms such as `(int x, var y) = ...`. It can also reject an
				// assignment tuple to an existing variable, which is safer than
				// attributing a possibly shadowed builder.
				return true
			}
		}
	}
	return false
}

func reassignsAspireBuilder(s []csToken) bool {
	for i, t := range s {
		if t.kind != "ident" || t.text != "builder" {
			continue
		}
		// A ref/out argument lets the callee replace the top-level local. Its
		// later AddProject calls may therefore target a different builder.
		if i > 0 && (s[i-1].text == "ref" || s[i-1].text == "out") {
			return true
		}
		if i > 0 && (s[i-1].text == "var" || s[i-1].text == "DistributedApplicationBuilder") {
			return true
		}
		if i+1 < len(s) && (s[i+1].text == "=" || s[i+1].text == "++" || s[i+1].text == "--" || strings.HasSuffix(s[i+1].text, "=")) {
			if i == 0 || s[i-1].text != "." {
				return true
			}
		}
		if i+2 < len(s) && s[i+2].text == "=" {
			// Opaque string/interpolation tokens have no text. Treat an empty
			// token in this malformed compound-assignment shape as an unknown
			// builder use instead of indexing an empty string or trusting later
			// calls.
			if s[i+1].text == "" || strings.ContainsRune("+-?&|^/%*", rune(s[i+1].text[0])) {
				return true
			}
		}
	}
	return false
}

func lexAspireCSharp(src string) ([]csToken, error) {
	var out []csToken
	line, conditional := 1, 0
	for i := 0; i < len(src); {
		if i == 0 || src[i-1] == '\n' {
			j := i
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == '#' {
				end := strings.IndexByte(src[j:], '\n')
				if end < 0 {
					end = len(src) - j
				}
				directive := strings.TrimSpace(src[j : j+end])
				fields := strings.Fields(strings.TrimPrefix(directive, "#"))
				keyword := ""
				if len(fields) > 0 {
					keyword = fields[0]
				}
				if (strings.HasPrefix(keyword, "if") && keyword != "if") || (strings.HasPrefix(keyword, "end") && keyword != "endif") || (strings.HasPrefix(keyword, "else") && keyword != "else") || (strings.HasPrefix(keyword, "elif") && keyword != "elif") {
					return nil, fmt.Errorf("unsupported C# preprocessor directive")
				}
				switch keyword {
				case "if":
					conditional++
				case "endif":
					conditional--
					if conditional < 0 {
						return nil, fmt.Errorf("unmatched #endif")
					}
				case "else", "elif":
					if conditional == 0 {
						return nil, fmt.Errorf("unmatched C# conditional directive")
					}
				}
				for k := i; k < j+end; k++ {
					if src[k] == '\n' {
						line++
					}
				}
				i = j + end
				continue
			}
		}
		c := src[i]
		if c == '\\' {
			// C# Unicode escapes are permitted in identifiers. Until the lexer
			// normalizes those code points, fail closed instead of letting an
			// escaped binding evade the framework/generated-name shadow guards.
			return nil, fmt.Errorf("unsupported C# escape outside literal")
		}
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if i+1 < len(src) && src[i:i+2] == "//" {
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				break
			}
			i += j
			continue
		}
		if i+1 < len(src) && src[i:i+2] == "/*" {
			start := line
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("unterminated C# comment at line %d", start)
			}
			chunk := src[i : i+2+j+2]
			line += strings.Count(chunk, "\n")
			i += len(chunk)
			continue
		}
		start := i
		tokLine := line
		if c == '\'' {
			end, ok := skipQuoted(src, i, '\'', false)
			if !ok || strings.ContainsAny(src[i:end], "\r\n") {
				return nil, fmt.Errorf("malformed C# character literal")
			}
			out = append(out, csToken{kind: "opaque", line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			i = end
			continue
		}
		if end, kind, value, ok := csharpString(src, i); ok {
			if kind == "limit" {
				return nil, fmt.Errorf("Aspire C# interpolation nesting limit exceeded")
			}
			chunk := src[i:end]
			line += strings.Count(chunk, "\n")
			out = append(out, csToken{kind: kind, text: value, line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			i = end
			continue
		}
		// C# permits @-escaped identifiers, including type names that would
		// otherwise look like framework/generated bindings. Keep the semantic
		// identifier spelling so shadow checks see `@Projects` and
		// `@DistributedApplication` just like their unescaped forms. Verbatim
		// strings have already been consumed by csharpString above.
		if c == '@' && i+1 < len(src) && isCSharpIdentStart(src[i+1]) {
			i += 2
			for i < len(src) && isCSharpIdentPart(src[i]) {
				i++
			}
			out = append(out, csToken{kind: "ident", text: src[start+1 : i], line: tokLine, conditional: conditional > 0, escaped: true})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			continue
		}
		if isCSharpIdentStart(c) {
			i++
			for i < len(src) && isCSharpIdentPart(src[i]) {
				i++
			}
			out = append(out, csToken{kind: "ident", text: src[start:i], line: tokLine, conditional: conditional > 0})
			if len(out) > maxAspireCSharpTokens {
				return nil, fmt.Errorf("Aspire C# token limit exceeded")
			}
			continue
		}
		out = append(out, csToken{kind: "punct", text: src[i : i+1], line: tokLine, conditional: conditional > 0})
		if len(out) > maxAspireCSharpTokens {
			return nil, fmt.Errorf("Aspire C# token limit exceeded")
		}
		i++
	}
	if conditional != 0 {
		return nil, fmt.Errorf("unmatched #if")
	}
	return out, nil
}

func csharpString(s string, at int) (int, string, string, bool) {
	return csharpStringDepth(s, at, 0)
}

func csharpStringDepth(s string, at, interpolationDepth int) (int, string, string, bool) {
	i := at
	dollars := 0
	verbatim := false
	for i < len(s) && (s[i] == '$' || s[i] == '@') {
		if s[i] == '$' {
			dollars++
		} else {
			verbatim = true
		}
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return 0, "", "", false
	}
	q := i
	for i < len(s) && s[i] == '"' {
		i++
	}
	quotes := i - q
	if quotes == 1 && dollars == 0 && !verbatim {
		end, ok := skipQuoted(s, q, '"', false)
		if !ok {
			return len(s), "opaque", "", true
		}
		raw := s[q:end]
		val, err := strconv.Unquote(raw)
		if err != nil {
			return end, "opaque", "", true
		}
		return end, "string", val, true
	}
	if quotes == 1 || quotes >= 3 {
		end, ok := skipCSharpBody(s, i, quotes, dollars, verbatim, interpolationDepth)
		if !ok {
			if end < 0 {
				return len(s), "limit", "", true
			}
			return len(s), "opaque", "", true
		}
		if dollars == 0 {
			return end, "string", "", true
		}
		return end, "opaque", "", true
	}
	return at, "", "", false
}

func skipQuoted(s string, start int, quote byte, verbatim bool) (int, bool) {
	for i := start + 1; i < len(s); i++ {
		if verbatim && s[i] == quote && i+1 < len(s) && s[i+1] == quote {
			i++
			continue
		}
		if !verbatim && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == quote {
			return i + 1, true
		}
	}
	return len(s), false
}
func skipCSharpBody(s string, i, quotes, dollars int, verbatim bool, interpolationDepth int) (int, bool) {
	for i < len(s) {
		if dollars > 0 && s[i] == '{' {
			run := 1
			for i+run < len(s) && s[i+run] == '{' {
				run++
			}
			arity := dollars
			if quotes == 1 {
				arity = 1
				if run == 2 {
					i += run
					continue
				}
				if run > 2 {
					end, ok := skipInterpolation(s, i+run, interpolationDepth+1, 1, 0)
					if !ok {
						if end < 0 {
							return -1, false
						}
						return len(s), false
					}
					i = end
					continue
				}
			}
			if run < arity {
				i += run
				continue
			}
			end, ok := skipInterpolation(s, i+arity, interpolationDepth+1, arity, run-arity)
			if !ok {
				if end < 0 {
					return -1, false
				}
				return len(s), false
			}
			i = end
			continue
		}
		if dollars > 0 && quotes == 1 && s[i] == '}' && i+1 < len(s) && s[i+1] == '}' {
			i += 2
			continue
		}
		if s[i] == '"' {
			n := 0
			for i+n < len(s) && s[i+n] == '"' {
				n++
			}
			if verbatim && quotes == 1 && n >= 2 {
				i += 2
				continue
			}
			if (quotes == 1 && n >= 1) || (quotes >= 3 && n >= quotes) {
				return i + quotes, true
			}
			i += n
			continue
		}
		if !verbatim && quotes == 1 && s[i] == '\\' {
			i += 2
			continue
		}
		i++
	}
	return len(s), false
}
func skipInterpolation(s string, i, nestingDepth, arity, exprDepth int) (int, bool) {
	if nestingDepth > maxAspireInterpolationDepth {
		return -1, false
	}
	depth := exprDepth
	for i < len(s) {
		if i+1 < len(s) && s[i:i+2] == "//" {
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return len(s), false
			}
			i += j
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return len(s), false
			}
			i += j + 4
			continue
		}
		if s[i] == '\'' {
			end, ok := skipQuoted(s, i, '\'', false)
			if !ok || strings.ContainsAny(s[i:end], "\r\n") {
				return len(s), false
			}
			i = end
			continue
		}
		if end, kind, _, ok := csharpStringDepth(s, i, nestingDepth); ok {
			if kind == "limit" {
				return -1, false
			}
			i = end
			continue
		}
		if s[i] == '{' {
			run := 1
			for i+run < len(s) && s[i+run] == '{' {
				run++
			}
			depth += run
			i += run
			continue
		}
		if s[i] == '}' {
			run := 1
			for i+run < len(s) && s[i+run] == '}' {
				run++
			}
			if depth > 0 {
				consumed := min(run, depth)
				depth -= consumed
				i += consumed
				run -= consumed
			}
			if depth == 0 && run >= arity {
				return i + arity, true
			}
			if run > 0 {
				i += run
			}
			continue
		}
		i++
	}
	return len(s), false
}
func isCSharpIdentStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
func isCSharpIdentPart(c byte) bool { return isCSharpIdentStart(c) || (c >= '0' && c <= '9') }
