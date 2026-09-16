package projects

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
)

const MaxManifestBytes int64 = 1 << 20

type inventoryFile struct {
	path, role string
	size       int64
}

// Collector receives a selected inventory; it never walks or opens a path.
// Calls are serial, after concurrent parsing has finished for each file.
type Collector struct {
	report      Report
	files       []inventoryFile
	present     map[string]bool
	directories map[string]bool
	finished    bool
	roles       map[string]Counts
}

func New(source, tree string) *Collector {
	return &Collector{report: Report{Status: "complete", Source: source, Tree: tree, Attribution: "nearest-unique-project-directory", MaxManifestBytes: MaxManifestBytes, Projects: []Project{}, Configurations: []Configuration{}, Composition: []Role{}, Diagnostics: []Diagnostic{}}, present: map[string]bool{}, directories: map[string]bool{}, roles: map[string]Counts{}}
}

func (c *Collector) Add(filename string, size int64, role string, doc Document) {
	c.present[filename] = true
	for dir := path.Dir(filename); !c.directories[dir]; dir = path.Dir(dir) {
		c.directories[dir] = true
		if dir == "." || dir == "/" {
			break
		}
	}
	c.files = append(c.files, inventoryFile{filename, role, size})
	n := c.roles[role]
	n.Files++
	n.Bytes += size
	c.roles[role] = n
	c.report.Projects = append(c.report.Projects, doc.Projects...)
	family, _ := configurationLocation(filename)
	if family != "" || len(doc.Requirements) > 0 || len(doc.References) > 0 {
		c.report.Configurations = append(c.report.Configurations, Configuration{Path: filename, Requirements: doc.Requirements, References: doc.References})
	}
	c.report.Diagnostics = append(c.report.Diagnostics, doc.Diagnostics...)
}

func (c *Collector) Omit() { c.report.OmittedFiles++ }
func (c *Collector) Skip(reason string) *Report {
	c.report.Status = "skipped"
	c.report.Diagnostics = append(c.report.Diagnostics, Diagnostic{Path: ".", Code: reason, Message: "inventory omitted"})
	return &c.report
}

func (c *Collector) Finish() *Report {
	r := &c.report
	if c.finished {
		return r
	}
	c.finished = true
	slices.SortFunc(r.Projects, func(a, b Project) int { return strings.Compare(a.ID, b.ID) })
	r.Projects = coalesceDiscoveredProjects(r.Projects)
	slices.SortFunc(r.Configurations, func(a, b Configuration) int { return strings.Compare(a.Path, b.Path) })
	// Index configuration candidates by ecosystem and directory. This records
	// ancestry, not evaluated inheritance or configuration precedence.
	configs := map[string]map[string][]string{}
	for _, cfg := range r.Configurations {
		family, dir := configurationLocation(cfg.Path)
		if family == "" {
			continue
		}
		if configs[family] == nil {
			configs[family] = map[string][]string{}
		}
		configs[family][dir] = append(configs[family][dir], cfg.Path)
	}
	owners := map[string][]int{}
	for i := range r.Projects {
		p := &r.Projects[i]
		if p.Evidence == nil {
			p.Evidence = []string{}
		}
		slices.Sort(p.Evidence)
		p.Evidence = slices.Compact(p.Evidence)
		if p.Requirements == nil {
			p.Requirements = []Requirement{}
		}
		if p.References == nil {
			p.References = []Reference{}
		}
		p.ConfigurationCandidates = []string{}
		c.resolve(p.References)
		sortObservations(p.Requirements, p.References)
		family := p.Kind
		if family == "solution" {
			family = "dotnet"
		}
		for dir := p.Root; ; dir = path.Dir(dir) {
			p.ConfigurationCandidates = append(p.ConfigurationCandidates, configs[family][dir]...)
			if dir == "." || dir == "/" || path.Dir(dir) == dir {
				break
			}
		}
		slices.Sort(p.ConfigurationCandidates)
		p.ConfigurationCandidates = slices.Compact(p.ConfigurationCandidates)
		if p.Kind != "solution" && p.Kind != "workspace" {
			owners[p.Root] = append(owners[p.Root], i)
		}
	}
	for i := range r.Configurations {
		cfg := &r.Configurations[i]
		if cfg.Requirements == nil {
			cfg.Requirements = []Requirement{}
		}
		if cfg.References == nil {
			cfg.References = []Reference{}
		}
		c.resolve(cfg.References)
		sortObservations(cfg.Requirements, cfg.References)
	}
	// Cache the nearest project directory, including ambiguous and unassigned
	// results. Files in the same directory do not repeat the ancestry search.
	nearest := map[string][]int{}
	for _, f := range c.files {
		dir := path.Dir(f.path)
		ids, known := nearest[dir]
		if !known {
			visited := []string{}
			for {
				if cached, found := nearest[dir]; found {
					ids = cached
					break
				}
				visited = append(visited, dir)
				if len(owners[dir]) > 0 {
					ids = owners[dir]
					break
				}
				if dir == "." || dir == "/" || path.Dir(dir) == dir {
					break
				}
				dir = path.Dir(dir)
			}
			for _, ancestor := range visited {
				nearest[ancestor] = ids
			}
		}
		switch len(ids) {
		case 0:
			r.Unassigned.Files++
			r.Unassigned.Bytes += f.size
		case 1:
			p := &r.Projects[ids[0]]
			p.Files++
			p.Bytes += f.size
		default:
			r.Ambiguous.Files++
			r.Ambiguous.Bytes += f.size
		}
	}
	for name, count := range c.roles {
		r.Composition = append(r.Composition, Role{Name: name, Basis: roleBasis(name), Counts: count})
	}
	slices.SortFunc(r.Composition, func(a, b Role) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(r.Diagnostics, func(a, b Diagnostic) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		if a.Code != b.Code {
			return strings.Compare(a.Code, b.Code)
		}
		return strings.Compare(a.Message, b.Message)
	})
	if len(r.Diagnostics) > 0 || r.OmittedFiles > 0 {
		r.Status = "partial"
	}
	return r
}

func coalesceDiscoveredProjects(projects []Project) []Project {
	out := make([]Project, 0, len(projects))
	indices := map[string]int{}
	for _, p := range projects {
		generic := len(p.Requirements) > 0 && len(p.References) == 0
		for _, req := range p.Requirements {
			if req.Kind != "manifest-discovery" || req.Value != "filename-only" {
				generic = false
			}
		}
		key := p.Kind + "\x00" + p.Root
		if previous, found := indices[key]; generic && found {
			out[previous].Evidence = append(out[previous].Evidence, p.Evidence...)
			out[previous].Requirements = append(out[previous].Requirements, p.Requirements...)
			continue
		}
		if generic {
			indices[key] = len(out)
		}
		out = append(out, p)
	}
	return out
}

func configurationLocation(filename string) (family, dir string) {
	dir = path.Dir(filename)
	switch strings.ToLower(path.Base(filename)) {
	case "global.json", "directory.build.props", "directory.build.targets", "directory.packages.props", "nuget.config", "packages.config":
		return "dotnet", dir
	}
	switch path.Base(filename) {
	case "toolchains.xml":
		return "maven", dir
	case "settings.gradle", "settings.gradle.kts", "gradle.properties":
		return "gradle", dir
	case "gradle-wrapper.properties":
		if path.Base(dir) == "wrapper" && path.Base(path.Dir(dir)) == "gradle" {
			dir = path.Dir(path.Dir(dir))
		}
		return "gradle", dir
	}
	return "", dir
}

func sortObservations(requirements []Requirement, references []Reference) {
	slices.SortFunc(requirements, func(a, b Requirement) int {
		return compareFields([]string{a.Evidence, a.Kind, a.Value, a.Condition, a.State}, []string{b.Evidence, b.Kind, b.Value, b.Condition, b.State})
	})
	slices.SortFunc(references, func(a, b Reference) int {
		return compareFields([]string{a.Evidence, a.Kind, a.Value, a.Target, a.Condition, a.State, a.TargetStatus}, []string{b.Evidence, b.Kind, b.Value, b.Target, b.Condition, b.State, b.TargetStatus})
	})
}
func compareFields(a, b []string) int {
	for i := range a {
		if order := strings.Compare(a[i], b[i]); order != 0 {
			return order
		}
	}
	return 0
}

func (c *Collector) resolve(refs []Reference) {
	for i := range refs {
		ref := &refs[i]
		if ref.Target == "" {
			ref.TargetStatus = "unresolved"
			continue
		}
		target := path.Clean(ref.Target)
		if strings.HasPrefix(target, "/") || target == ".." || strings.HasPrefix(target, "../") || strings.ContainsAny(target, "\\:\x00\r\n") {
			ref.Target = ""
			ref.State = "unresolved"
			ref.TargetStatus = "unresolved"
			c.report.Diagnostics = append(c.report.Diagnostics, Diagnostic{Path: ref.Evidence, Code: "invalid-reference-target", Message: "Reference target is not a path within the selected inventory."})
			continue
		}
		ref.Target = target
		ref.TargetStatus = "missing"
		present := c.present[target]
		if ref.Kind == "gradle-module" {
			present = c.directories[target]
		}
		if present {
			ref.TargetStatus = "present"
		}
		if ref.State == "declared" {
			ref.State = "resolved"
			if !present {
				ref.State = "missing"
			}
		}
	}
}
func roleBasis(role string) string {
	switch role {
	case "test":
		return "path-convention"
	case "configuration":
		return "manifest-name"
	case "vendored", "generated", "documentation":
		return "linguist-rules-and-attributes"
	case "binary":
		return "content-prefix"
	case "source", "data":
		return "detected-language-type"
	default:
		return "not-established"
	}
}

func IsManifest(filename string) bool {
	return IsDotnet(filename) || IsJVM(filename) || genericKind(path.Base(filename)) != ""
}
func Parse(filename string, content []byte) Document {
	if IsDotnet(filename) {
		return ParseDotnet(filename, content)
	}
	if IsJVM(filename) {
		return ParseJVM(filename, content)
	}
	kind := genericKind(path.Base(filename))
	if kind == "" {
		return Document{}
	}
	d := Document{Projects: []Project{{ID: filename, Root: path.Dir(filename), Kind: kind, Evidence: []string{filename}, Requirements: []Requirement{}, References: []Reference{}}}}
	// Other ecosystems retain filename-based discovery. This does not establish
	// package validity or dynamically evaluated workspace membership.
	d.Projects[0].Requirements = append(d.Projects[0].Requirements, Requirement{Kind: "manifest-discovery", Value: "filename-only", State: "declared", Evidence: filename})
	if path.Base(filename) == "package.json" {
		var object map[string]json.RawMessage
		if json.Unmarshal(content, &object) != nil || object == nil {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{filename, "invalid-manifest", "invalid package.json object"})
		}
	}
	return d
}
func genericKind(base string) string {
	switch base {
	case "package.json":
		return "npm"
	case "pyproject.toml", "setup.py", "setup.cfg", "requirements.txt":
		return "python"
	case "go.mod":
		return "go"
	case "go.work":
		return "workspace"
	case "Cargo.toml":
		return "cargo"
	case "Gemfile":
		return "bundler"
	case "composer.json":
		return "composer"
	}
	return ""
}
