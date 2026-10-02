package intentmap

import (
	"path"
	"regexp"
	"strings"
)

var (
	javaImportRE = regexp.MustCompile(`^import\s+(?:static\s+)?([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*|\.\*)*)\s*;`)
	vbImportsRE  = regexp.MustCompile(`(?i)^Imports\s+(?:[A-Za-z_][\w]*\s*=\s*)?([A-Za-z_][\w.]*)\s*$`)
	csUsingRE    = regexp.MustCompile(`^using\s+(?:static\s+)?([A-Za-z_][\w.]*)\s*;`)
	csAliasRE    = regexp.MustCompile(`^using\s+[A-Za-z_][\w]*\s*=\s*([A-Za-z_][\w.]*)\s*;`)
	tsFromRE     = regexp.MustCompile(`\bimport\s+(?:type\s+)?(?:[^;]*?\s+from\s+)?['"]([^'"]+)['"]`)
	tsRequireRE  = regexp.MustCompile(`(?:^|[=(:,]\s*)require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// scrubComments removes comments while preserving quoted literals and line positions.
func scrubComments(s string, slash bool) string {
	var b strings.Builder
	b.Grow(len(s))
	quote := byte(0)
	line, block := false, false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		n := byte(0)
		if i+1 < len(s) {
			n = s[i+1]
		}
		if line {
			if c == '\n' {
				line = false
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if block {
			if c == '*' && n == '/' {
				b.WriteString("  ")
				i++
				block = false
			} else if c == '\n' {
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
			continue
		}
		if quote != 0 {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == '/' && n == '*' {
			block = true
			b.WriteString("  ")
			i++
			continue
		}
		if slash && c == '/' && n == '/' {
			line = true
			b.WriteString("  ")
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
func javaImportPackage(s string) string { s = strings.TrimSuffix(s, ".*"); return s }
func parseJVMImports(name string, content []byte) []Observation {
	s := scrubComments(string(content), true)
	lines := strings.Split(s, "\n")
	var out []Observation
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "*") || strings.HasPrefix(l, "//") {
			continue
		}
		start := i + 1
		for strings.HasPrefix(l, "import ") && !strings.Contains(l, ";") && i+1 < len(lines) {
			i++
			l += " " + strings.TrimSpace(lines[i])
		}
		m := javaImportRE.FindStringSubmatch(l)
		if len(m) < 2 {
			continue
		}
		pkg := javaImportPackage(m[1])
		for _, c := range capabilitiesFor("maven-import", pkg+":") {
			out = append(out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: start, EndLine: i + 1, Properties: map[string]string{"import": pkg, "evidence_scope": importEvidenceScope(name)}})
		}
	}
	return out
}
func parseDotnetImports(name string, content []byte) []Observation {
	s := scrubComments(string(content), true)
	lines := strings.Split(s, "\n")
	var out []Observation
	for i, l := range lines {
		if strings.TrimSpace(l) != l {
			continue
		}
		l = strings.TrimSpace(l)
		m := csUsingRE.FindStringSubmatch(l)
		if len(m) < 2 {
			m = csAliasRE.FindStringSubmatch(l)
		}
		if len(m) < 2 && strings.HasSuffix(strings.ToLower(name), ".vb") {
			m = vbImportsRE.FindStringSubmatch(l)
		}
		if len(m) < 2 {
			continue
		}
		for _, c := range capabilitiesFor("nuget-import", m[1]) {
			out = append(out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: i + 1, EndLine: i + 1, Properties: map[string]string{"import": m[1], "evidence_scope": importEvidenceScope(name)}})
		}
	}
	return out
}
func parseJSImports(name string, content []byte) []Observation {
	s := scrubComments(string(content), true)
	lines := strings.Split(s, "\n")
	var out []Observation
	for i, l := range lines {
		if strings.TrimSpace(l) != l {
			continue
		} // Bind require only when not shadowed by a declaration/parameter.
		if strings.Contains(s, "const require") || strings.Contains(s, "let require") || strings.Contains(s, "function (require") || strings.Contains(s, "function(require") {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(l), "import ") && !strings.HasPrefix(strings.TrimSpace(l), "import{") { /* CommonJS pass below */
		}
		ms := tsFromRE.FindAllStringSubmatch(l, -1)
		if !strings.HasPrefix(strings.TrimSpace(l), "import ") && !strings.HasPrefix(strings.TrimSpace(l), "import{") {
			ms = nil
		}
		ms = append(ms, tsRequireRE.FindAllStringSubmatch(l, -1)...)
		for _, m := range ms {
			if len(m) < 2 {
				continue
			}
			p := m[1]
			if strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") {
				continue
			}
			for _, c := range capabilitiesFor("npm-dependency", p) {
				out = append(out, Observation{Kind: KindCapability, Name: c, State: "observed", Basis: "imported", Path: name, StartLine: i + 1, EndLine: i + 1, Properties: map[string]string{"import": p, "evidence_scope": importEvidenceScope(name)}})
			}
		}
	}
	return out
}
func isExtraImport(name string) bool {
	e := strings.ToLower(path.Ext(name))
	return e == ".java" || e == ".kt" || e == ".cs" || e == ".vb" || e == ".ts" || e == ".tsx" || e == ".js" || e == ".jsx" || e == ".mjs" || e == ".cjs"
}

func testEvidencePath(p string) bool {
	l := strings.ToLower(p)
	b := path.Base(l)
	return strings.HasPrefix(l, "tests/") || strings.HasPrefix(l, "test/") || strings.Contains(l, "/src/test/") || strings.Contains(l, "/tests/") || strings.Contains(l, "/__tests__/") || strings.Contains(l, "/test/") || strings.HasPrefix(b, "test_") || strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") || strings.HasSuffix(b, "_test.go")
}

func importEvidenceScope(p string) string {
	if testEvidencePath(p) {
		return "test_path_convention"
	}
	return "non_test_path_convention"
}

var mavenDependencyBlock = regexp.MustCompile(`(?s)<dependency\b[^>]*>(.*?)</dependency\s*>`)
var mavenTag = func(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<` + tag + `\b[^>]*>\s*([^<]+?)\s*</` + tag + `\s*>`)
}

func (d *Detector) collectMavenTestScopes(name string, content []byte) {
	for _, block := range mavenDependencyBlock.FindAllSubmatch(content, -1) {
		body := string(block[1])
		scope := mavenTag("scope").FindStringSubmatch(body)
		if len(scope) < 2 || strings.TrimSpace(scope[1]) != "test" {
			continue
		}
		g := mavenTag("groupId").FindStringSubmatch(body)
		a := mavenTag("artifactId").FindStringSubmatch(body)
		v := mavenTag("version").FindStringSubmatch(body)
		if len(g) < 2 || len(a) < 2 {
			continue
		}
		value := strings.TrimSpace(g[1]) + ":" + strings.TrimSpace(a[1])
		if len(v) > 1 && strings.TrimSpace(v[1]) != "" {
			value += ":" + strings.TrimSpace(v[1])
		}
		d.mu.Lock()
		d.testMaven[name+"\x00"+mavenCoordinate(value)] = true
		d.mu.Unlock()
	}
}
