package declarations

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const pythonMatchBudget = 1 << 20

var pythonNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
var pythonNameSeparators = regexp.MustCompile(`[-_.]+`)
var pythonDependencyName = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(.*)$`)
var pythonVersion = regexp.MustCompile(`^[A-Za-z0-9.!+_-]+$`)
var pythonRequires = regexp.MustCompile(`^[0-9<>=!~*., ()]+$`)
var pythonMarkerText = regexp.MustCompile(`^[A-Za-z0-9_ .'"<>=!~()*+-]+$`)
var pythonScriptName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var pythonEntrypoint = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*(?::[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)?$`)

type pythonDependency struct{ name, condition string }
type pythonSource struct {
	kind, target, marker string
	editable, packaged   *bool
	invalid              bool
}
type pythonData struct {
	project            bool
	managed            bool
	workspace          bool
	invalidPatterns    bool
	patterns, excludes []string
	sources            map[string][]pythonSource
	dependencies       []pythonDependency
}

// ParsePython reads the documented pyproject and uv declaration subset. Values
// are observations, not an evaluated Python environment or dependency solution.
func ParsePython(name string, content []byte) *Document {
	if path.Base(name) != "pyproject.toml" {
		return nil
	}
	d := NewDocument(name, "python")
	raw, err := ValidateTOML(content)
	if err != nil {
		d.Parsed = false
		AddDiagnostic(d, "invalid-python-manifest", "Python manifest could not be parsed within the TOML limits.")
		return d
	}
	data := &pythonData{managed: true, sources: map[string][]pythonSource{}}
	d.Data = data
	AddRequirement(d, Requirement{Kind: "declaration-semantics", Value: "pyproject+uv-0.12.17", State: "declared", Evidence: name})
	project, hasProject := raw["project"]
	p, validProject := project.(map[string]any)
	data.project = hasProject && validProject
	if !hasProject {
		AddRequirement(d, Requirement{Kind: "python-project-table", Value: "absent", State: "missing", Evidence: name})
	}
	if hasProject && !validProject {
		AddDiagnostic(d, "invalid-python-project", "The project field must be a TOML table.")
	}
	dynamic := map[string]bool{}
	if data.project {
		for _, field := range pythonStrings(d, p, "dynamic") {
			if !pythonFieldName(field) {
				AddDiagnostic(d, "invalid-python-dynamic", "A dynamic metadata field is unsupported.")
				continue
			}
			dynamic[field] = true
			AddRequirement(d, Requirement{Kind: "python-dynamic-field", Value: field, State: "dynamic", Evidence: name})
		}

		conflicts := map[string]bool{}
		dynamicFields := make([]string, 0, len(dynamic))
		for field := range dynamic {
			dynamicFields = append(dynamicFields, field)
		}
		slices.Sort(dynamicFields)
		for _, field := range dynamicFields {
			if _, found := p[field]; found {
				conflicts[field] = true
				AddDiagnostic(d, "conflicting-python-metadata", "A metadata field is both static and dynamic.")
				AddRequirement(d, Requirement{Kind: "python-dynamic-conflict", Value: field, State: "unresolved", Evidence: name})
			}
		}
		for _, key := range []string{"name", "version", "requires-python"} {
			value, present := p[key]
			state := "missing"
			text := ""
			if dynamic[key] {
				state = "dynamic"
			}
			if present {
				s, ok := value.(string)
				if !ok || !pythonText(s) || (key == "name" && !pythonNamePattern.MatchString(s)) || (key == "version" && !pythonVersion.MatchString(s)) || (key == "requires-python" && !pythonRequires.MatchString(s)) {
					state = "unsupported"
					AddDiagnostic(d, "invalid-python-metadata", "A project identity or Python requirement has an unsupported value.")
				} else {
					text = s
					state = "declared"
				}
			}
			if dynamic[key] && present {
				state = "unresolved"
				AddDiagnostic(d, "conflicting-python-metadata", "A metadata field is both static and dynamic.")
			}
			if key == "name" && dynamic[key] {
				state = "unsupported"
				text = ""
				AddDiagnostic(d, "dynamic-python-name", "Python project names cannot be declared dynamic.")
			}
			AddRequirement(d, Requirement{Kind: "python-" + key, Value: text, State: state, Evidence: name})
			if state == "declared" {
				if key == "name" {
					d.Project.Name = text
				}
				if key == "version" {
					d.Project.Version = text
				}
			}
		}
		if d.Project.Name == "" {
			AddDiagnostic(d, "missing-python-name", "A project table needs a supported static distribution name.")
		}
		if !conflicts["dependencies"] {
			pythonDependencies(d, data, p, "dependencies", "")
		}
		if optional, ok := pythonTable(d, p, "optional-dependencies"); ok && !conflicts["optional-dependencies"] {
			for _, extra := range pythonKeys(optional) {
				if !pythonNamePattern.MatchString(extra) {
					AddDiagnostic(d, "invalid-python-extra", "An optional dependency group name is unsupported.")
					continue
				}
				pythonDependencies(d, data, optional, extra, "extra:"+pythonCanonical(extra))
			}
		}
		pythonScripts(d, p, "scripts", "python-console-script", conflicts["scripts"])
		pythonScripts(d, p, "gui-scripts", "python-gui-script", conflicts["gui-scripts"])
	}
	if build, ok := pythonTable(d, raw, "build-system"); ok {
		for _, req := range pythonStrings(d, build, "requires") {
			pythonRequirement(d, nil, req, "build-system", "python-build-requirement")
		}
		if value, found := build["build-backend"]; found {
			backend, ok := value.(string)
			if ok && len(backend) <= MaxStringBytes && pythonEntrypoint.MatchString(backend) {
				AddRequirement(d, Requirement{Kind: "python-build-backend", Value: backend, State: "declared", Evidence: name})
				AddInterface(d, Interface{Kind: "python-build-backend", Name: backend, Target: backend, State: "declared", Evidence: name})
			} else {
				AddDiagnostic(d, "unsupported-python-backend", "The build backend is not a supported module or object reference.")
			}
		}
		for _, rawPath := range pythonStrings(d, build, "backend-path") {
			target, ok := pythonLocalTarget(name, rawPath, "")
			_, withinProject := pythonRelative(d.Project.Root, target)
			ok = ok && withinProject
			ref := Reference{Kind: "python-backend-path", Value: "local-backend", State: "unresolved", Evidence: name}
			if ok {
				ref.Target = target
				ref.State = "declared"
			} else {
				AddDiagnostic(d, "external-python-backend", "A backend path is outside its own project directory or unsupported.")
			}
			AddReference(d, ref)
		}
	}
	if groups, ok := pythonTable(d, raw, "dependency-groups"); ok {
		for _, group := range pythonKeys(groups) {
			if !pythonNamePattern.MatchString(group) {
				AddDiagnostic(d, "invalid-python-group", "A dependency group name is unsupported.")
				continue
			}
			items, ok := groups[group].([]any)
			if !ok {
				AddDiagnostic(d, "invalid-python-group", "Dependency group entries must be arrays.")
				continue
			}
			for _, item := range items {
				if d.limited {
					break
				}
				if text, ok := item.(string); ok {
					pythonRequirement(d, data, text, "group:"+pythonCanonical(group), "python-dependency")
					continue
				}
				inc, ok := item.(map[string]any)
				if !ok || len(inc) != 1 {
					AddDiagnostic(d, "unsupported-python-group-item", "A dependency group item is neither a requirement nor a supported include.")
					continue
				}
				target, ok := inc["include-group"].(string)
				if !ok || !pythonNamePattern.MatchString(target) {
					AddDiagnostic(d, "unsupported-python-group-include", "A dependency group include is unsupported.")
					continue
				}
				AddRequirement(d, Requirement{Kind: "python-dependency-group-include", Value: pythonCanonical(target), State: "declared", Evidence: name, Condition: "group:" + pythonCanonical(group)})
			}
		}
	}
	tool, ok := pythonTable(d, raw, "tool")
	if !ok {
		return d
	}
	uv, ok := pythonTable(d, tool, "uv")
	if !ok {
		return d
	}
	if value, found := uv["managed"]; found {
		managed, ok := value.(bool)
		data.managed = ok && managed
		if !ok {
			AddDiagnostic(d, "invalid-uv-managed", "The uv managed setting must be a boolean; uv relationships are not established.")
		} else {
			text := "false"
			if managed {
				text = "true"
			}
			AddRequirement(d, Requirement{Kind: "uv-managed", Value: text, State: "declared", Evidence: name})
		}
	}
	if ws, ok := pythonTable(d, uv, "workspace"); ok {
		data.workspace = true
		if !data.project {
			d.Project.Kind = "python-workspace"
		}
		before := len(d.Diagnostics)
		data.patterns = pythonPatterns(d, ws, "members")
		data.excludes = pythonPatterns(d, ws, "exclude")
		data.invalidPatterns = len(d.Diagnostics) != before
		if len(data.patterns)+len(data.excludes) > MaxPatterns {
			data.invalidPatterns = true
			AddDiagnostic(d, "python-workspace-pattern-limit", "Workspace member and exclude patterns exceed the combined limit.")
		}
		AddRequirement(d, Requirement{Kind: "uv-workspace", Value: "declared", State: "declared", Evidence: name})
	}
	if value, present := uv["package"]; present {
		if b, ok := value.(bool); ok {
			text := "false"
			if b {
				text = "true"
			}
			AddRequirement(d, Requirement{Kind: "uv-package", Value: text, State: "declared", Evidence: name})
		} else {
			AddDiagnostic(d, "invalid-uv-package", "The uv package setting must be a boolean.")
		}
	}
	pythonDependencies(d, data, uv, "dev-dependencies", "legacy-dev")
	if sources, ok := pythonTable(d, uv, "sources"); ok {
		for _, sourceName := range pythonKeys(sources) {
			if d.limited {
				break
			}
			if !pythonNamePattern.MatchString(sourceName) {
				AddDiagnostic(d, "invalid-uv-source-name", "A source name is not a supported Python distribution name.")
				continue
			}
			canonical := pythonCanonical(sourceName)
			if _, found := data.sources[canonical]; found {
				AddDiagnostic(d, "duplicate-uv-source-name", "Multiple source keys have the same normalized distribution name.")
				data.sources[canonical] = []pythonSource{{kind: "unsupported", invalid: true}}
				continue
			}
			// An empty or invalid member mapping still overrides the root mapping.
			data.sources[canonical] = []pythonSource{}
			entries, ok := sources[sourceName].([]any)
			if !ok {
				entries = []any{sources[sourceName]}
			}
			for _, entry := range entries {
				if d.limited {
					break
				}
				s := pythonParseSource(d, entry)
				if AddRequirement(d, Requirement{Kind: "uv-source-declaration", Value: canonical, State: pythonSourceState(s), Evidence: name, Condition: s.marker}) {
					data.sources[canonical] = append(data.sources[canonical], s)
					for _, flag := range []struct {
						kind  string
						value *bool
					}{{"editable", s.editable}, {"package", s.packaged}} {
						if flag.value != nil {
							value := "false"
							if *flag.value {
								value = "true"
							}
							AddRequirement(d, Requirement{Kind: "uv-source-" + flag.kind, Value: canonical + "=" + value, State: pythonSourceState(s), Evidence: name, Condition: s.marker})
						}
					}
				}
			}
			if len(entries) == 0 {
				AddDiagnostic(d, "empty-uv-source", "An empty source list cannot establish a local source.")
			}
		}
	}
	return d
}

func pythonCanonical(s string) string {
	return pythonNameSeparators.ReplaceAllString(strings.ToLower(s), "-")
}
func pythonText(s string) bool {
	if s == "" || len(s) > MaxStringBytes {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func pythonFieldName(s string) bool {
	if !pythonText(s) {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && r != '-' {
			return false
		}
	}
	return true
}
func pythonKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func pythonTable(d *Document, m map[string]any, key string) (map[string]any, bool) {
	v, found := m[key]
	if !found {
		return nil, false
	}
	table, ok := v.(map[string]any)
	if !ok {
		AddDiagnostic(d, "invalid-python-table", "A recognized Python declaration must be a TOML table.")
	}
	return table, ok
}
func pythonStrings(d *Document, m map[string]any, key string) []string {
	v, found := m[key]
	if !found {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		AddDiagnostic(d, "invalid-python-array", "A recognized Python declaration must be an array of strings.")
		return nil
	}
	out := make([]string, 0, min(len(items), MaxObservationsPerManifest))
	for _, item := range items {
		if len(out) >= MaxObservationsPerManifest {
			AddDiagnostic(d, "python-array-limit", "A Python declaration array exceeds the observation limit.")
			break
		}
		s, ok := item.(string)
		if !ok || !pythonText(s) {
			AddDiagnostic(d, "invalid-python-array-item", "A declaration array contains an unsupported string value.")
			continue
		}
		out = append(out, s)
	}
	return out
}
func pythonDependencies(d *Document, data *pythonData, m map[string]any, key, scope string) {
	for _, s := range pythonStrings(d, m, key) {
		if d.limited {
			break
		}
		pythonRequirement(d, data, s, scope, "python-dependency")
	}
}
func pythonRequirement(d *Document, data *pythonData, raw, scope, kind string) {
	if !pythonText(raw) {
		AddDiagnostic(d, "invalid-python-requirement", "A Python requirement is empty, oversized, or contains control characters.")
		return
	}
	value, marker, _ := strings.Cut(raw, ";")
	parts := pythonDependencyName.FindStringSubmatch(strings.TrimSpace(value))
	if len(parts) != 3 {
		AddDiagnostic(d, "unsupported-python-requirement", "A dependency does not begin with a supported distribution name.")
		return
	}
	name := pythonCanonical(parts[1])
	rest := strings.TrimSpace(parts[2])
	if rest != "" && !strings.ContainsAny(rest[:1], "[<>=!~(@") {
		AddDiagnostic(d, "unsupported-python-requirement", "A dependency uses an unsupported requirement form.")
		return
	}
	if marker != "" && !pythonMarkerText.MatchString(marker) {
		AddDiagnostic(d, "unsupported-python-marker", "A dependency marker uses unsupported syntax; its value is omitted.")
		return
	}
	condition := scope
	if strings.TrimSpace(marker) != "" {
		condition = pythonCondition(condition, "marker:"+strings.TrimSpace(marker))
	}
	state := "declared"
	if condition != "" {
		state = "conditional"
	}
	display := strings.TrimSpace(value)
	if strings.Contains(rest, "@") || strings.ContainsAny(display, "/\\:") {
		display = name
		AddDiagnostic(d, "python-requirement-source-withheld", "A direct dependency source is omitted; its distribution name is retained.")
	}
	if AddRequirement(d, Requirement{Kind: kind, Value: display, State: state, Evidence: d.Project.ID, Condition: condition}) && data != nil {
		data.dependencies = append(data.dependencies, pythonDependency{name, condition})
	}
}
func pythonScripts(d *Document, m map[string]any, key, kind string, conflict bool) {
	scripts, ok := pythonTable(d, m, key)
	if !ok {
		return
	}
	for _, name := range pythonKeys(scripts) {
		if d.limited {
			break
		}
		if !pythonText(name) || !pythonScriptName.MatchString(name) {
			AddDiagnostic(d, "invalid-python-script-name", "A script name is empty, oversized, or contains control characters.")
			continue
		}
		target, ok := scripts[name].(string)
		entry := Interface{Kind: kind, Name: name, State: "unresolved", Evidence: d.Project.ID}
		if !conflict && ok && len(target) <= MaxStringBytes && strings.Contains(target, ":") && pythonEntrypoint.MatchString(target) {
			entry.Target = target
			entry.State = "declared"
		} else if !conflict {
			AddDiagnostic(d, "unsupported-python-script-target", "A script target is not a supported module:object entrypoint; the raw value is omitted.")
		}
		AddInterface(d, entry)
	}
}
func pythonPatterns(d *Document, m map[string]any, key string) []string {
	all := pythonStrings(d, m, key)
	out := make([]string, 0, min(len(all), MaxPatterns))
	for _, s := range all {
		if len(out) >= MaxPatterns {
			AddDiagnostic(d, "python-workspace-pattern-limit", "Workspace patterns exceed the supported limit.")
			break
		}
		if _, err := MatchPattern(s, ""); err != nil {
			AddDiagnostic(d, "unsupported-python-workspace-pattern", "A workspace pattern is outside the supported root-relative glob subset.")
			continue
		}
		out = append(out, s)
	}
	return out
}
func pythonParseSource(d *Document, raw any) pythonSource {
	source, ok := raw.(map[string]any)
	if !ok {
		AddDiagnostic(d, "invalid-uv-source", "A source declaration must be a table or list of tables.")
		return pythonSource{kind: "unsupported", invalid: true}
	}
	s := pythonSource{}
	for _, key := range pythonKeys(source) {
		switch key {
		case "marker", "extra", "group":
			value, ok := source[key].(string)
			if !ok || !pythonText(value) || !pythonMarkerText.MatchString(value) || (key != "marker" && !pythonNamePattern.MatchString(value)) {
				s.invalid = true
				continue
			}
			s.marker = pythonCondition(s.marker, key+":"+value)
		case "editable", "package":
			if b, ok := source[key].(bool); !ok {
				s.invalid = true
			} else if key == "editable" {
				s.editable = &b
			} else {
				s.packaged = &b
			}
		case "path":
			if s.kind != "" {
				s.invalid = true
			}
			s.kind = "path"
			value, ok := source[key].(string)
			if !ok {
				s.invalid = true
				continue
			}
			if pythonArchiveSource(value) {
				s.kind = "archive"
				s.invalid = true
				AddDiagnostic(d, "unsupported-uv-archive-source", "Local wheel and source-archive dependencies are not project-manifest references; their raw paths are omitted.")
				continue
			}
			target, ok := pythonLocalTarget(d.Project.ID, value, "pyproject.toml")
			if !ok {
				s.invalid = true
			} else {
				s.target = target
			}
		case "workspace":
			if s.kind != "" {
				s.invalid = true
			}
			s.kind = "workspace"
			switch value := source[key].(type) {
			case bool:
				if !value {
					s.invalid = true
				}
			case string:
				target, ok := pythonLocalTarget(d.Project.ID, value, "pyproject.toml")
				if !ok {
					s.invalid = true
				} else {
					s.target = target
				}
			default:
				s.invalid = true
			}
		case "git", "url", "index":
			if s.kind != "" {
				s.invalid = true
			}
			s.kind = "remote"
			if value, ok := source[key].(string); !ok || !pythonText(value) {
				s.invalid = true
			}
		case "rev", "tag", "branch", "subdirectory":
			if value, ok := source[key].(string); !ok || !pythonText(value) {
				s.invalid = true
			}
		default:
			s.invalid = true
		}
	}
	if s.kind == "" {
		s.invalid = true
	}
	if s.invalid {
		s.target = ""
		AddDiagnostic(d, "unsupported-uv-source", "A source declaration is unsupported, conflicting, or outside the selected root; raw source values are omitted.")
	}
	return s
}
func pythonSourceState(s pythonSource) string {
	if s.invalid || s.kind == "remote" {
		return "unresolved"
	}
	if s.marker != "" {
		return "conditional"
	}
	return "declared"
}
func pythonCondition(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

// uv source paths follow their declared spelling; backslashes are not silently
// translated into separators because that changes POSIX path semantics.
func pythonLocalTarget(manifest, rawDir, basename string) (string, bool) {
	if strings.Contains(rawDir, `\`) {
		return "", false
	}
	return LocalTarget(manifest, rawDir, basename)
}
func pythonArchiveSource(raw string) bool {
	lower := strings.ToLower(raw)
	for _, suffix := range []string{".whl", ".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar.zst", ".egg"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
