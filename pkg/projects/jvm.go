package projects

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const jvmMavenNamespace = "http://maven.apache.org/POM/4.0.0"
const jvmMaven41Namespace = "http://maven.apache.org/POM/4.1.0"

func jvmKnownMavenNamespace(namespace string) bool {
	return namespace == "" || namespace == jvmMavenNamespace || namespace == jvmMaven41Namespace
}

// IsJVM reports manifests understood by the passive JVM declaration reader.
func IsJVM(name string) bool {
	switch path.Base(name) {
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties", "gradle-wrapper.properties", "toolchains.xml":
		return true
	}
	return false
}

// ParseJVM reads declarations without running Maven, Gradle, plugins, or wrappers.
// Values supplied by inheritance, profiles, and executable build code remain
// conditional or unresolved rather than being presented as an effective build.
func ParseJVM(name string, content []byte) Document {
	d := Document{Projects: []Project{}, Requirements: []Requirement{}, References: []Reference{}, Diagnostics: []Diagnostic{}}
	if !IsJVM(name) {
		return d
	}
	name = path.Clean(name)
	if int64(len(content)) > MaxManifestBytes {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "manifest-too-large", Message: "JVM manifest exceeds the 1 MiB declaration parser limit."})
		return d
	}
	switch path.Base(name) {
	case "pom.xml":
		jvmParseMaven(name, content, &d)
	case "toolchains.xml":
		jvmParseToolchains(name, content, &d)
	case "gradle.properties", "gradle-wrapper.properties":
		jvmParseProperties(name, content, &d)
	default:
		jvmParseGradle(name, content, &d)
	}
	return d
}

type jvmNode struct {
	name     xml.Name
	text     strings.Builder
	children []*jvmNode
}

func (n *jvmNode) child(name string) *jvmNode {
	if n == nil {
		return nil
	}
	for _, c := range n.children {
		if c.name.Local == name && (jvmKnownMavenNamespace(c.name.Space) && c.name.Space == n.name.Space) {
			return c
		}
	}
	return nil
}
func (n *jvmNode) value(name string) string {
	c := n.child(name)
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.text.String())
}
func (n *jvmNode) list(name string) []*jvmNode {
	out := []*jvmNode{}
	if n == nil {
		return out
	}
	for _, c := range n.children {
		if c.name.Local == name && (jvmKnownMavenNamespace(c.name.Space) && c.name.Space == n.name.Space) {
			out = append(out, c)
		}
	}
	return out
}
func jvmXML(content []byte) (*jvmNode, bool) {
	decoded, err := decodeBOMXML(content)
	if err != nil {
		return nil, false
	}
	decoder := xml.NewDecoder(bytes.NewReader(decoded))
	decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if strings.EqualFold(label, "utf-8") || strings.EqualFold(label, "utf-16") {
			return input, nil
		}
		return nil, errors.New("unsupported XML encoding")
	}
	var root *jvmNode
	stack := []*jvmNode{}
	nodes := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return root, root != nil && len(stack) == 0
		}
		if err != nil {
			return nil, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			nodes++
			if len(stack) >= 64 || nodes > 200000 {
				return nil, false
			}
			n := &jvmNode{name: t.Name}
			if len(stack) == 0 {
				if root != nil {
					return nil, false
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				n := stack[len(stack)-1]
				if n.text.Len()+len(t) > 1024*1024 {
					return nil, false
				}
				n.text.Write(t)
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, false
			}
		case xml.Directive:
			return nil, false // DTDs and entity declarations are not accepted.
		}
	}
}
func jvmInvalid(d *Document, name string) {
	d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "invalid_manifest", Message: "XML is malformed, declares an unsupported encoding, uses a DTD, or exceeds the parser limits."})
}
func jvmProject(name, kind string) Project {
	return Project{ID: name, Root: path.Dir(name), Kind: kind, Evidence: []string{name}, Requirements: []Requirement{}, References: []Reference{}}
}

var jvmProperty = regexp.MustCompile(`\$\{([^{}]+)\}`)

func jvmResolve(value string, props map[string]string) (string, bool) {
	const maxValueBytes = 65536
	if len(value) > maxValueBytes {
		return "", false
	}
	for iteration := 0; iteration < 16; iteration++ {
		changed := false
		var result strings.Builder
		result.Grow(len(value))
		appendBounded := func(text string) bool {
			if len(text) > maxValueBytes-result.Len() {
				return false
			}
			result.WriteString(text)
			return true
		}
		position := 0
		for position < len(value) {
			match := jvmProperty.FindStringIndex(value[position:])
			if match == nil {
				if !appendBounded(value[position:]) {
					return "", false
				}
				break
			}
			start, end := position+match[0], position+match[1]
			if !appendBounded(value[position:start]) {
				return "", false
			}
			literal := value[start:end]
			replacement, found := props[value[start+2:end-1]]
			if !found {
				replacement = literal
			} else if replacement != literal {
				changed = true
			}
			// Check each replacement before copying: a small manifest can repeat
			// a large property thousands of times and otherwise expand to GiB.
			if !appendBounded(replacement) {
				return "", false
			}
			position = end
		}
		value = result.String()
		if !changed {
			break
		}
	}
	return value, !strings.Contains(value, "${") && !strings.ContainsAny(value, "\r\n\x00")
}
func jvmRequirement(kind, value, evidence, condition string, props map[string]string) Requirement {
	resolved, ok := jvmResolve(value, props)
	state := "declared"
	if condition != "" {
		state = "conditional"
	}
	if !ok {
		state = "unresolved"
		resolved = value
	}
	return Requirement{Kind: kind, Value: resolved, State: state, Evidence: evidence, Condition: condition}
}
func jvmReference(name, kind, value, condition string, props map[string]string) Reference {
	resolved, ok := jvmResolve(value, props)
	r := Reference{Kind: kind, Value: value, State: "declared", Evidence: name, Condition: condition}
	if condition != "" {
		r.State = "conditional"
	}
	// Maven paths are relative to this manifest. Never resolve outside the scan root.
	if !ok || resolved == "" || strings.ContainsAny(resolved, "\\:*?[]\x00") || strings.HasPrefix(resolved, "/") {
		r.State = "unresolved"
		return r
	}
	target := path.Clean(path.Join(path.Dir(name), resolved))
	if target == ".." || strings.HasPrefix(target, "../") {
		r.State = "unresolved"
		return r
	}
	if path.Ext(target) != ".xml" {
		target = path.Join(target, "pom.xml")
	}
	r.Target = target
	return r
}

// The budget covers the entire expanded POM report, not only an individual
// interpolated value. Input size alone cannot bound repeated substitutions.
type jvmObservationBudget struct {
	project             *Project
	document            *Document
	name                string
	bytes, observations int
	exceeded            bool
}

func (b *jvmObservationBudget) fail() {
	if !b.exceeded {
		b.document.Diagnostics = append(b.document.Diagnostics, Diagnostic{Path: b.name, Code: "declaration-budget-exceeded", Message: "JVM declarations exceed the 1 MiB text, 4096 observation, or 100000 Maven property-copy budget; remaining declarations were omitted."})
	}
	b.exceeded = true
}
func (b *jvmObservationBudget) accept(fields ...string) bool {
	if b.exceeded {
		return false
	}
	size := 0
	for _, field := range fields {
		size += len(field)
	}
	if size > (1<<20)-b.bytes || b.observations >= 4096 {
		b.fail()
		return false
	}
	b.bytes += size
	b.observations++
	return true
}
func (b *jvmObservationBudget) requirement(r Requirement) {
	if b.accept(r.Kind, r.Value, r.State, r.Evidence, r.Condition) {
		if b.project == nil {
			b.document.Requirements = append(b.document.Requirements, r)
		} else {
			b.project.Requirements = append(b.project.Requirements, r)
		}
	}
}
func (b *jvmObservationBudget) reference(r Reference) {
	if b.accept(r.Kind, r.Value, r.Target, r.State, r.Evidence, r.Condition) {
		if b.project == nil {
			b.document.References = append(b.document.References, r)
		} else {
			b.project.References = append(b.project.References, r)
		}
	}
}

func jvmParseMaven(name string, content []byte, d *Document) {
	root, ok := jvmXML(content)
	if !ok {
		jvmInvalid(d, name)
		return
	}
	if root.name.Local != "project" || !jvmKnownMavenNamespace(root.name.Space) {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "unsupported_manifest", Message: "Expected a Maven project element in the Maven namespace."})
		return
	}
	p := jvmProject(name, "maven")
	budget := &jvmObservationBudget{project: &p, document: d, name: name}
	props := map[string]string{}
	if properties := root.child("properties"); properties != nil {
		for _, c := range properties.children {
			if jvmKnownMavenNamespace(c.name.Space) && c.name.Space == properties.name.Space {
				props[c.name.Local] = strings.TrimSpace(c.text.String())
			}
		}
	}
	// Only local declarations are substituted. Parent inheritance and effective
	// profile activation require Maven evaluation and are deliberately absent.
	for _, key := range []string{"groupId", "artifactId", "version"} {
		if value := root.value(key); value != "" {
			props["project."+key] = value
			props["pom."+key] = value
		}
	}
	jvmMavenSection(name, root, "", props, &p, d, budget)
	if parent := root.child("parent"); parent != nil && !budget.exceeded {
		coords := parent.value("groupId") + ":" + parent.value("artifactId") + ":" + parent.value("version")
		budget.requirement(jvmRequirement("maven-parent", coords, name, "", props))
		relative := parent.child("relativePath")
		if relative == nil {
			condition := ""
			if root.name.Space == jvmMaven41Namespace || root.value("modelVersion") == "4.1.0" {
				condition = "Maven default parent lookup; reactor and repository resolution not evaluated"
			}
			budget.reference(jvmReference(name, "parent", "../pom.xml", condition, props))
		} else if value := strings.TrimSpace(relative.text.String()); value != "" {
			budget.reference(jvmReference(name, "parent", value, "", props))
		} else {
			budget.requirement(Requirement{Kind: "maven-parent-lookup", Value: "repository_only", State: "declared", Evidence: name})
		}
	}
	propertyCopies := 0
	for _, profile := range root.child("profiles").list("profile") {
		if budget.exceeded {
			break
		}
		propertyCopies += len(props)
		if propertyCopies > 100000 {
			budget.fail()
			break
		}
		condition := "Maven profile " + profile.value("id")
		profileProps := map[string]string{}
		for k, v := range props {
			profileProps[k] = v
		}
		if properties := profile.child("properties"); properties != nil {
			for _, c := range properties.children {
				if jvmKnownMavenNamespace(c.name.Space) && c.name.Space == properties.name.Space {
					profileProps[c.name.Local] = strings.TrimSpace(c.text.String())
				}
			}
		}
		jvmMavenSection(name, profile, condition, profileProps, &p, d, budget)
	}
	d.Projects = append(d.Projects, p)
}
func jvmMavenSection(name string, n *jvmNode, condition string, props map[string]string, p *Project, d *Document, budget *jvmObservationBudget) {
	if budget.exceeded {
		return
	}
	for _, key := range []string{"groupId", "artifactId", "version", "packaging"} {
		if budget.exceeded {
			return
		}
		if value := n.value(key); value != "" {
			budget.requirement(jvmRequirement("maven-"+key, value, name, condition, props))
		}
	}
	modules := n.child("modules").list("module")
	// Maven 4.1 calls these subprojects; they remain literal directory
	// declarations. Source models and inherited reactor membership are not evaluated.
	modules = append(modules, n.child("subprojects").list("subproject")...)
	for _, module := range modules {
		if budget.exceeded {
			return
		}
		value := strings.TrimSpace(module.text.String())
		if value == "" {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "invalid-module", Message: "Maven module declaration is empty."})
			continue
		}
		budget.reference(jvmReference(name, "module", value, condition, props))
	}
	if properties := n.child("properties"); properties != nil {
		for _, key := range []string{"release", "source", "target"} {
			if budget.exceeded {
				return
			}
			if value := properties.value("maven.compiler." + key); value != "" {
				budget.requirement(jvmRequirement("java-"+key, value, name, condition, props))
			}
		}
	}
	for _, dep := range n.child("dependencies").list("dependency") {
		if budget.exceeded {
			return
		}
		value := dep.value("groupId") + ":" + dep.value("artifactId")
		if version := dep.value("version"); version != "" {
			value += ":" + version
		}
		budget.requirement(jvmRequirement("maven-dependency", value, name, condition, props))
	}
	for _, plugin := range n.child("build").child("plugins").list("plugin") {
		if budget.exceeded {
			return
		}
		artifact := plugin.value("artifactId")
		group := plugin.value("groupId")
		if group == "" {
			group = "org.apache.maven.plugins"
		}
		value := group + ":" + artifact
		if v := plugin.value("version"); v != "" {
			value += ":" + v
		}
		budget.requirement(jvmRequirement("maven-plugin", value, name, condition, props))
		if artifact == "maven-compiler-plugin" {
			for _, key := range []string{"release", "source", "target"} {
				if budget.exceeded {
					return
				}
				if v := plugin.child("configuration").value(key); v != "" {
					budget.requirement(jvmRequirement("java-"+key, v, name, condition, props))
				}
			}
			if v := plugin.child("configuration").child("jdkToolchain").value("version"); v != "" {
				budget.requirement(jvmRequirement("java-toolchain", v, name, condition, props))
			}
		}
		if artifact == "maven-toolchains-plugin" {
			budget.requirement(jvmRequirement("java-toolchain", "Maven toolchains plugin declared", name, condition, props))
		}
		generators := map[string]bool{"protobuf-maven-plugin": true, "jaxb2-maven-plugin": true, "maven-jaxb2-plugin": true, "openapi-generator-maven-plugin": true, "swagger-codegen-maven-plugin": true, "antlr4-maven-plugin": true, "avro-maven-plugin": true}
		if generators[artifact] {
			budget.requirement(jvmRequirement("code-generation", value, name, condition, props))
		}
	}
}
func jvmParseToolchains(name string, content []byte, d *Document) {
	budget := &jvmObservationBudget{document: d, name: name}
	n, ok := jvmXML(content)
	if !ok {
		jvmInvalid(d, name)
		return
	}
	if n.name.Local != "toolchains" || (n.name.Space != "" && n.name.Space != "http://maven.apache.org/TOOLCHAINS/1.1.0" && n.name.Space != "http://maven.apache.org/TOOLCHAINS/1.0.0") {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "unsupported_manifest", Message: "Expected Maven toolchains XML."})
		return
	}
	// Toolchain XML has its own namespace, so match descendants against the root.
	for _, t := range n.children {
		if budget.exceeded {
			return
		}
		if t.name.Local != "toolchain" || t.name.Space != n.name.Space {
			continue
		}
		for _, provides := range t.children {
			if budget.exceeded {
				return
			}
			if provides.name.Local != "provides" || provides.name.Space != n.name.Space {
				continue
			}
			for _, v := range provides.children {
				if budget.exceeded {
					return
				}
				if v.name.Local == "version" && v.name.Space == n.name.Space && strings.TrimSpace(v.text.String()) != "" {
					budget.requirement(jvmRequirement("java-toolchain", strings.TrimSpace(v.text.String()), name, "", nil))
				}
			}
		}
	}
}

func jvmParseProperties(name string, content []byte, d *Document) {
	budget := &jvmObservationBudget{document: d, name: name}
	// Property files can contain credentials. Read only explicitly selected keys;
	// never report registry URLs, arbitrary values, or JVM command-line arguments.
	for _, line := range strings.Split(string(content), "\n") {
		if budget.exceeded {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.HasSuffix(value, "\\") {
			continue
		}
		switch key {
		case "distributionUrl":
			if path.Base(name) != "gradle-wrapper.properties" {
				continue
			}
			match := jvmGradleDistribution.FindStringSubmatch(value)
			if len(match) == 2 {
				budget.requirement(Requirement{Kind: "gradle-wrapper", Value: match[1], State: "declared", Evidence: name})
			} else {
				budget.requirement(Requirement{Kind: "gradle-wrapper", Value: "custom distribution", State: "unresolved", Evidence: name})
			}
		case "org.gradle.java.installations.auto-download":
			if value == "true" || value == "false" {
				budget.requirement(Requirement{Kind: "java-toolchain-auto-download", Value: value, State: "declared", Evidence: name})
			}
		}
	}
}

var jvmGradleDistribution = regexp.MustCompile(`/gradle-([0-9][0-9A-Za-z.\-]*)-(?:bin|all)\.zip(?:[?#].*)?$`)

// Gradle is executable code. Tokenization lets us recognize a small set of
// top-level declarations while ignoring comments and string contents. Observed
// literals are conditional; they are not the result of evaluating the script.
type jvmToken struct {
	value   string
	literal bool
	line    int
}

func jvmTokens(content []byte) ([]jvmToken, bool) {
	out := []jvmToken{}
	line := 1
	for i := 0; i < len(content); {
		c := content[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if c == '/' && i+1 < len(content) && content[i+1] == '/' {
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(content) && content[i+1] == '*' {
			i += 2
			closed := false
			commentDepth := 1
			for i+1 < len(content) {
				if content[i] == '\n' {
					line++
				}
				if content[i] == '/' && content[i+1] == '*' {
					commentDepth++
					i += 2
					continue
				}
				if content[i] == '*' && content[i+1] == '/' {
					i += 2
					commentDepth--
					if commentDepth == 0 {
						closed = true
						break
					}
					continue
				}
				i++
			}
			if !closed {
				return nil, false
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			startLine := line
			i++
			start := i
			// Triple quoted strings and slashy Groovy strings are not in this subset.
			if i+1 < len(content) && content[i] == quote && content[i+1] == quote {
				return nil, false
			}
			escaped := false
			for i < len(content) && content[i] != quote {
				if content[i] == '\\' {
					escaped = true
					i++
					if i >= len(content) {
						return nil, false
					}
				}
				if content[i] == '\n' {
					return nil, false
				}
				i++
			}
			if i >= len(content) {
				return nil, false
			}
			value := string(content[start:i])
			i++
			if escaped || strings.Contains(value, "$") {
				out = append(out, jvmToken{value: "<dynamic>", line: startLine})
			} else {
				out = append(out, jvmToken{value: value, literal: true, line: startLine})
			}
			continue
		}
		if c == '/' {
			return nil, false
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9') {
			start := i
			for i < len(content) {
				b := content[i]
				if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_') {
					break
				}
				i++
			}
			out = append(out, jvmToken{value: string(content[start:i]), line: line})
			continue
		}
		out = append(out, jvmToken{value: string(c), line: line})
		i++
	}
	stack := []string{}
	for _, token := range out {
		if token.literal {
			continue
		}
		switch token.value {
		case "(", "[", "{":
			stack = append(stack, token.value)
		case ")", "]", "}":
			if len(stack) == 0 {
				return nil, false
			}
			open := stack[len(stack)-1]
			if (token.value == ")" && open != "(") || (token.value == "]" && open != "[") || (token.value == "}" && open != "{") {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		}
	}
	return out, len(stack) == 0
}
func jvmParseGradle(name string, content []byte, d *Document) {
	req := Requirement{Kind: "build-evaluation", Value: "Gradle declarations require build evaluation to resolve", State: "unresolved", Evidence: name}
	var p *Project
	if strings.HasPrefix(path.Base(name), "build.gradle") {
		project := jvmProject(name, "gradle")
		d.Projects = append(d.Projects, project)
		p = &d.Projects[len(d.Projects)-1]
		p.Requirements = append(p.Requirements, req)
	} else {
		d.Requirements = append(d.Requirements, req)
	}
	budget := &jvmObservationBudget{document: d, project: p, name: name, observations: 1, bytes: len(req.Kind) + len(req.Value) + len(req.State) + len(req.Evidence)}
	tokens, ok := jvmTokens(content)
	if !ok {
		d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "unresolved_build_script", Message: "Gradle script contains syntax outside the passive declaration reader."})
		return
	}
	depth := 0
	overrides := map[string]string{}
	unresolvedLayout := false
	for i := 0; i < len(tokens); {
		start := i
		line := tokens[i].line
		for i < len(tokens) && tokens[i].line == line {
			i++
		}
		row := tokens[start:i]
		// Only complete top-level statements are interpreted. A declaration in
		// a closure, multiline call, or conditional is not a root declaration.
		if depth == 0 && len(row) > 0 && !row[0].literal && row[0].value == "include" && strings.HasPrefix(path.Base(name), "settings.gradle") {
			values, valid := jvmInclude(row)
			if valid {
				for _, value := range values {
					if budget.exceeded {
						break
					}
					if value == "" {
						d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: name, Code: "invalid-module", Message: "Gradle module declaration is empty."})
						continue
					}
					r := Reference{Kind: "gradle-module", Value: value, State: "conditional", Evidence: name, Condition: "Gradle script evaluation"}
					trimmed := strings.TrimPrefix(value, ":")
					parts := strings.Split(trimmed, ":")
					safe := trimmed != ""
					for _, part := range parts {
						if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\$*?[]") {
							safe = false
						}
					}
					if safe {
						r.Target = path.Join(path.Dir(name), strings.Join(parts, "/"))
					} else {
						r.State = "unresolved"
					}
					budget.reference(r)
				}
			}
		}
		if depth == 0 {
			if id, target, valid := jvmProjectDir(row); valid {
				id = ":" + strings.TrimPrefix(id, ":")
				if _, exists := overrides[id]; exists {
					unresolvedLayout = true
				}
				overrides[id] = target
			} else {
				for _, token := range row {
					if !token.literal && token.value == "projectDir" {
						unresolvedLayout = true
					}
				}
			}
		} else {
			for _, token := range row {
				if !token.literal && token.value == "projectDir" {
					unresolvedLayout = true
				}
			}
		}
		if depth == 0 && p != nil && len(row) == 3 && !row[0].literal && row[1].value == "=" && (row[0].value == "sourceCompatibility" || row[0].value == "targetCompatibility") {
			value := row[2].value
			if value != "" && (row[2].literal || jvmDigits(value)) {
				kind := "java-source"
				if row[0].value == "targetCompatibility" {
					kind = "java-target"
				}
				budget.requirement(Requirement{Kind: kind, Value: value, State: "conditional", Evidence: name, Condition: "Gradle script evaluation"})
			}
		}
		for _, t := range row {
			if !t.literal {
				if t.value == "{" || t.value == "(" || t.value == "[" {
					depth++
				}
				if t.value == "}" || t.value == ")" || t.value == "]" {
					depth--
				}
			}
		}
	}
	for i := range d.References {
		r := &d.References[i]
		if r.Kind != "gradle-module" {
			continue
		}
		if unresolvedLayout {
			r.Target = ""
			r.State = "unresolved"
			continue
		}
		if value, exists := overrides[":"+strings.TrimPrefix(r.Value, ":")]; exists {
			target := path.Clean(path.Join(path.Dir(name), value))
			if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:$*?[]") || target == ".." || strings.HasPrefix(target, "../") {
				r.Target = ""
				r.State = "unresolved"
			} else {
				if growth := len(target) - len(r.Target); growth > (1<<20)-budget.bytes {
					budget.fail()
					r.Target = ""
					r.State = "unresolved"
				} else {
					budget.bytes += growth
					r.Target = target
				}
			}
		}
	}
	if p != nil {
		jvmGradleToolchains(name, tokens, p, budget)
	}
}

// jvmProjectDir recognizes the literal form project(":id").projectDir = file("dir").
func jvmProjectDir(row []jvmToken) (string, string, bool) {
	if len(row) == 12 && row[11].value == ";" && !row[11].literal {
		row = row[:11]
	}
	pattern := []string{"project", "(", "", ")", ".", "projectDir", "=", "file", "(", "", ")"}
	if len(row) != len(pattern) {
		return "", "", false
	}
	for i, expected := range pattern {
		if i == 2 || i == 9 {
			if !row[i].literal {
				return "", "", false
			}
		} else if row[i].literal || row[i].value != expected {
			return "", "", false
		}
	}
	return row[2].value, row[9].value, true
}

func jvmGradleToolchains(name string, tokens []jvmToken, p *Project, budget *jvmObservationBudget) {
	scopes := []string{}
	for i, token := range tokens {
		if budget.exceeded {
			return
		}
		if token.literal {
			continue
		}
		if token.value == "{" {
			label := ""
			if i > 0 && !tokens[i-1].literal {
				label = tokens[i-1].value
			}
			scopes = append(scopes, label)
		} else if token.value == "}" {
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
		}
		if token.value != "languageVersion" || len(scopes) < 2 || scopes[len(scopes)-1] != "toolchain" || scopes[len(scopes)-2] != "java" {
			continue
		}
		pattern := []string{"languageVersion", "=", "JavaLanguageVersion", ".", "of", "(", "", ")"}
		if i+len(pattern) > len(tokens) {
			continue
		}
		valid := true
		for j, expected := range pattern {
			if j == 6 {
				valid = valid && !tokens[i+j].literal && jvmDigits(tokens[i+j].value)
			} else {
				valid = valid && !tokens[i+j].literal && tokens[i+j].value == expected
			}
		}
		if valid {
			next := i + len(pattern)
			if next < len(tokens) && tokens[next].line == tokens[next-1].line && tokens[next].value != "}" && tokens[next].value != ";" {
				continue
			}
			budget.requirement(Requirement{Kind: "java-toolchain", Value: tokens[i+6].value, State: "conditional", Evidence: name, Condition: "Gradle script evaluation"})
		}
	}
}

func jvmInclude(row []jvmToken) ([]string, bool) {
	row = row[1:]
	if len(row) > 0 && row[len(row)-1].value == ";" && !row[len(row)-1].literal {
		row = row[:len(row)-1]
	}
	if len(row) > 0 && row[0].value == "(" && !row[0].literal {
		if len(row) < 2 || row[len(row)-1].value != ")" || row[len(row)-1].literal {
			return nil, false
		}
		row = row[1 : len(row)-1]
	}
	values := []string{}
	for i, t := range row {
		if i%2 == 0 {
			if !t.literal {
				return nil, false
			}
			values = append(values, t.value)
		} else if t.value != "," || t.literal {
			return nil, false
		}
	}
	return values, len(values) > 0 && len(row)%2 == 1
}
func jvmDigits(s string) bool { _, err := strconv.ParseUint(s, 10, 16); return err == nil }
