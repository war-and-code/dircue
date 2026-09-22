package declarations

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"dircue/pkg/projects"
)

const MaxInventoryPaths = 200000
const MaxInputBytes int64 = 64 << 20
const MaxOutputBytes = 16 << 20
const MaxDiagnostics = 1024

// Candidate reads only the already selected source snapshot.
type Candidate struct {
	Path   string
	Size   int64
	Read   func(context.Context, int64) ([]byte, int64, error)
	Legacy *projects.Document
}

type candidates []Candidate

func (h candidates) Len() int           { return len(h) }
func (h candidates) Less(i, j int) bool { return h[i].Path > h[j].Path }
func (h candidates) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidates) Push(v any)        { *h = append(*h, v.(Candidate)) }
func (h *candidates) Pop() any          { v := (*h)[len(*h)-1]; *h = (*h)[:len(*h)-1]; return v }

type Collector struct {
	report        Report
	files         map[string]bool
	paths         candidates
	inventory     candidates
	readLimit     int64
	finished      bool
	finishErr     error
	retainRecords bool
	records       []ProjectRecord
	// errorPolicy controls per-file read handling at Finish time. "continue"
	// records a per-path "file-read-error" diagnostic and marks the manifest
	// omitted; anything else fails hard, preserving the default trust
	// boundary.
	errorPolicy string
	readErrors  []string
}

// SetErrorPolicy configures how Finish reacts to per-manifest read failures.
// Pass "continue" to record a per-path "file-read-error" diagnostic and
// keep the remaining candidates; the default preserves the historical
// fail-fast contract. Call before Finish.
func (c *Collector) SetErrorPolicy(policy string) { c.errorPolicy = policy }

// ReadErrors returns the paths of selected manifests Finish skipped because
// their bounded read failed under the continue policy. The scanner uses
// this to emit same-shaped "file_read_error" warnings at the top level.
func (c *Collector) ReadErrors() []string { return c.readErrors }

func New(source, tree string, maxFileBytes int64) *Collector {
	limit := MaxManifestBytes
	if maxFileBytes > 0 {
		limit = min(limit, maxFileBytes)
	}
	return &Collector{report: Report{Provider: "dircue", ProviderVersion: "1.0.0", Status: "complete", SupportedEcosystems: []string{"cargo", "dotnet", "go", "gradle", "maven", "npm", "python-uv"}, Selection: "selected-regular-files-excluding-node-modules", Source: source, Tree: tree, Limits: Limits{ManifestBytes: limit, Documents: MaxDocuments, ObservationsPerManifest: MaxObservationsPerManifest, TotalObservations: MaxTotalObservations, StringBytes: MaxStringBytes, Patterns: MaxPatterns, InventoryPaths: MaxInventoryPaths, InputBytes: MaxInputBytes, OutputBytes: MaxOutputBytes, ResolutionWork: map[string]int{"npm": 1 << 20, "python-uv": pythonMatchBudget, "cargo": cargoMaxResolutionWork}}, Projects: []Project{}, Diagnostics: []Diagnostic{}}, files: map[string]bool{}, readLimit: limit}
}

// IsManifest excludes installed npm contents even when language analysis includes them.
func IsManifest(name string) bool {
	if strings.Contains("/"+name+"/", "/node_modules/") {
		return false
	}
	switch path.Base(name) {
	case "package.json", "go.mod", "go.work", "Cargo.toml", "pyproject.toml":
		return true
	}
	return projects.IsDotnet(name) || projects.IsJVM(name)
}

func (c *Collector) diagnostic(code, message string) {
	c.report.Status = "partial"
	for _, d := range c.report.Diagnostics {
		if d.Code == code && d.Path == "." {
			return
		}
	}
	c.report.Diagnostics = append(c.report.Diagnostics, Diagnostic{Path: ".", Code: code, Message: message})
}

func (c *Collector) Omit() { c.report.Coverage.OmittedFiles++ }
func (c *Collector) Skip(reason string) *Report {
	c.report.Status = "skipped"
	c.report.Diagnostics = append(c.report.Diagnostics, Diagnostic{Path: ".", Code: reason, Message: "The selected inventory was not analyzed."})
	return &c.report
}

func (c *Collector) Add(name string, candidate *Candidate) {
	c.report.Coverage.SelectedFiles++
	if !utf8.ValidString(name) || (candidate != nil && !utf8.ValidString(candidate.Path)) {
		c.Omit()
		c.diagnostic("invalid-path-text", "A selected filename cannot be represented faithfully as UTF-8.")
		if candidate != nil {
			c.report.Coverage.ManifestCandidates++
		}
		return
	}
	// Once inventory coverage is capped no absence is a definite missing target.
	if c.files[name] {
		return
	}
	if len(c.files) < MaxInventoryPaths && len(name) <= MaxStringBytes {
		c.files[name] = true
		heap.Push(&c.inventory, Candidate{Path: name})
	} else {
		c.Omit()
		c.diagnostic("inventory-limit", "The declaration inventory path limit was reached.")
		if len(name) <= MaxStringBytes && len(c.inventory) > 0 && name < c.inventory[0].Path {
			delete(c.files, c.inventory[0].Path)
			c.inventory[0] = Candidate{Path: name}
			heap.Fix(&c.inventory, 0)
			c.files[name] = true
		}
	}
	if candidate == nil {
		return
	}
	if len(candidate.Path) > MaxStringBytes {
		c.Omit()
		return
	}
	c.report.Coverage.ManifestCandidates++
	if len(c.paths) < MaxDocuments {
		heap.Push(&c.paths, *candidate)
		return
	}
	c.diagnostic("manifest-count-limit", "Only the lexically first supported manifests were retained.")
	c.Omit()
	if candidate.Path < c.paths[0].Path {
		c.paths[0] = *candidate
		heap.Fix(&c.paths, 0)
	}
}

func (c *Collector) Finish(ctx context.Context) (report *Report, err error) {
	if c.finished {
		return &c.report, c.finishErr
	}
	c.finished = true
	defer func() { c.finishErr = err }()
	slices.SortFunc(c.paths, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	docs := make([]*Document, 0, len(c.paths))
	var bytesRead int64
	for _, candidate := range c.paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if candidate.Size > c.readLimit || candidate.Size < 0 || candidate.Size > MaxInputBytes-bytesRead {
			c.Omit()
			c.diagnostic("manifest-read-limit", "One or more manifests exceeded the per-file or total manifest read limit.")
			continue
		}
		var d *Document
		if candidate.Legacy != nil {
			d = fromLegacy(candidate.Path, *candidate.Legacy)
			bytesRead += candidate.Size
		} else {
			if candidate.Read == nil {
				return nil, errors.New("declaration candidate has no selected-source reader")
			}
			content, size, err := candidate.Read(ctx, c.readLimit+1)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if c.errorPolicy == "continue" {
					// Preserve remaining manifests. The per-path diagnostic
					// keeps the omission attributable and stays disjoint
					// from the module's other incompleteness reasons.
					c.Omit()
					c.report.Status = "partial"
					if len(c.report.Diagnostics) < MaxDiagnostics {
						c.report.Diagnostics = append(c.report.Diagnostics, Diagnostic{Path: candidate.Path, Code: "file-read-error", Message: "A selected manifest could not be read."})
					} else {
						c.report.Coverage.OmittedDiagnostics++
					}
					c.readErrors = append(c.readErrors, candidate.Path)
					continue
				}
				return nil, errors.New("could not read a selected declaration manifest")
			}
			if size != int64(len(content)) || size > c.readLimit || size > MaxInputBytes-bytesRead {
				c.Omit()
				c.diagnostic("incomplete-manifest", "One or more manifests changed, were incomplete, or exceeded a read limit.")
				continue
			}
			bytesRead += size
			d = Parse(candidate.Path, content)
		}
		if d != nil {
			docs = append(docs, d)
			if d.Parsed {
				c.report.Coverage.ParsedManifests++
			}
		}
	}
	for _, resolve := range []func([]*Document, map[string]bool){ResolveNPM, ResolveGo, ResolvePython, ResolveCargo} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolve(docs, c.files)
	}
	resolveLegacy(docs, c.files)
	totalBytes := 0
	for _, d := range docs {
		for _, diagnostic := range d.Diagnostics {
			if len(c.report.Diagnostics) < MaxDiagnostics {
				c.report.Diagnostics = append(c.report.Diagnostics, diagnostic)
			} else {
				c.report.Coverage.OmittedDiagnostics++
				c.report.Status = "partial"
			}
		}
		if d.Project == nil {
			continue
		}
		p := d.Project
		// Canonical ordering is independent of parser maps and scanner workers.
		sortJSON(p.Requirements)
		sortJSON(p.References)
		sortJSON(p.Interfaces)
		n := len(p.Requirements) + len(p.References) + len(p.Interfaces)
		encoded, _ := json.Marshal(p)
		if n > MaxTotalObservations-c.report.Coverage.RetainedObservations || len(encoded) > MaxOutputBytes-totalBytes {
			c.Omit()
			c.diagnostic("report-limit", "The retained declaration report limit was reached.")
			continue
		}
		totalBytes += len(encoded)
		c.report.Coverage.RetainedObservations += n
		c.report.Projects = append(c.report.Projects, *p)
	}
	if len(c.report.Diagnostics) > 0 || c.report.Coverage.OmittedFiles > 0 {
		c.report.Status = "partial"
	}
	if c.report.Status == "partial" {
		for i := range c.report.Projects {
			for j := range c.report.Projects[i].References {
				r := &c.report.Projects[i].References[j]
				if r.TargetStatus == "missing" {
					r.TargetStatus = "unresolved"
					r.State = "unresolved"
				}
			}
		}
	}
	sortJSON(c.report.Diagnostics)
	c.boundOutput()
	if c.retainRecords {
		retained := make(map[string]Project, len(c.report.Projects))
		for _, p := range c.report.Projects {
			retained[p.ID] = p
		}
		for _, d := range docs {
			if d.Project == nil {
				continue
			}
			if p, ok := retained[d.Project.ID]; ok {
				c.records = append(c.records, ProjectRecord{Project: p, Parsed: d.Parsed, Complete: !d.limited})
			}
		}
		slices.SortFunc(c.records, func(a, b ProjectRecord) int { return strings.Compare(a.Project.ID, b.Project.ID) })
	}
	// Adapter metadata and source callbacks need not survive the returned report.
	c.paths = nil
	c.files = nil
	c.inventory = nil
	return &c.report, nil
}

// Include diagnostics and the report envelope in the serialized-byte bound.
// Drop lexical tails so arrival order never changes which projects survive.
func (c *Collector) boundOutput() {
	encoded, _ := json.Marshal(&c.report)
	if len(encoded) <= MaxOutputBytes {
		return
	}
	c.diagnostic("report-limit", "The retained declaration report limit was reached.")
	for i := range c.report.Projects {
		for j := range c.report.Projects[i].References {
			r := &c.report.Projects[i].References[j]
			if r.TargetStatus == "missing" {
				r.TargetStatus, r.State = "unresolved", "unresolved"
			}
		}
	}
	sortJSON(c.report.Diagnostics)
	encoded, _ = json.Marshal(&c.report)
	size := len(encoded)
	// Leave room for counter digit growth while subtracting complete records.
	for size > MaxOutputBytes-256 && len(c.report.Projects) > 0 {
		last := len(c.report.Projects) - 1
		p := c.report.Projects[last]
		data, _ := json.Marshal(p)
		size -= len(data)
		c.report.Coverage.RetainedObservations -= len(p.Requirements) + len(p.References) + len(p.Interfaces)
		c.report.Coverage.OmittedFiles++
		c.report.Projects = c.report.Projects[:last]
	}
	for size > MaxOutputBytes-256 && len(c.report.Diagnostics) > 1 {
		last := len(c.report.Diagnostics) - 1
		data, _ := json.Marshal(c.report.Diagnostics[last])
		size -= len(data)
		c.report.Coverage.OmittedDiagnostics++
		c.report.Diagnostics = c.report.Diagnostics[:last]
	}
}

func sortJSON[T any](values []T) {
	slices.SortFunc(values, func(a, b T) int {
		av, _ := json.Marshal(a)
		bv, _ := json.Marshal(b)
		return strings.Compare(string(av), string(bv))
	})
}

func Parse(name string, content []byte) *Document {
	switch path.Base(name) {
	case "package.json":
		return ParseNPM(name, content)
	case "go.mod", "go.work":
		return ParseGo(name, content)
	case "pyproject.toml":
		return ParsePython(name, content)
	case "Cargo.toml":
		return ParseCargo(name, content)
	}
	if projects.IsDotnet(name) || projects.IsJVM(name) {
		return fromLegacy(name, projects.Parse(name, content))
	}
	return nil
}

func fromLegacy(name string, legacy projects.Document) *Document {
	kind := "configuration"
	if projects.IsDotnet(name) {
		kind = "dotnet-configuration"
	} else if projects.IsJVM(name) {
		kind = "jvm-configuration"
	}
	d := NewDocument(name, kind)
	d.Data = legacyData{}
	for _, diagnostic := range legacy.Diagnostics {
		AddDiagnostic(d, diagnostic.Code, "An existing project reader reported incomplete or unsupported declarations.")
	}
	for _, p := range legacy.Projects {
		if p.ID != name {
			continue
		}
		d.Project.Kind = p.Kind
		for _, r := range p.Requirements {
			addLegacyRequirement(d, r)
		}
		for _, r := range p.References {
			addLegacyReference(d, r)
		}
	}
	for _, r := range legacy.Requirements {
		addLegacyRequirement(d, r)
	}
	for _, r := range legacy.References {
		addLegacyReference(d, r)
	}
	if len(legacy.Diagnostics) > 0 {
		d.Parsed = false
	}
	return d
}

type legacyData struct{}

func resolveLegacy(docs []*Document, files map[string]bool) {
	dirs := map[string]bool{}
	for filename := range files {
		for dir := path.Dir(filename); !dirs[dir]; dir = path.Dir(dir) {
			dirs[dir] = true
			if dir == "." || dir == "/" {
				break
			}
		}
	}
	for _, d := range docs {
		if _, ok := d.Data.(legacyData); !ok || d.Project == nil {
			continue
		}
		for i := range d.Project.References {
			r := &d.Project.References[i]
			if r.Target == "" {
				r.TargetStatus = "unresolved"
				continue
			}
			target := path.Clean(r.Target)
			if strings.HasPrefix(target, "/") || target == ".." || strings.HasPrefix(target, "../") || strings.ContainsAny(target, "\\:\x00\r\n") {
				r.Target = ""
				r.TargetStatus = "unresolved"
				r.State = "unresolved"
				AddDiagnostic(d, "invalid-reference-target", "A declaration target is not a path within the selected inventory.")
				continue
			}
			r.Target = target
			present := files[target]
			if r.Kind == "gradle-module" {
				present = dirs[target]
			}
			r.TargetStatus = "missing"
			if present {
				r.TargetStatus = "present"
			}
			if r.State == "declared" {
				r.State = "missing"
				if present {
					r.State = "resolved"
				}
			}
		}
	}
}

// New declaration reports withhold raw build conditions. Existing project
// reports retain their established contract for callers that require them.
func legacyCondition(value string) string {
	if value != "" {
		return "condition-present; expression-withheld"
	}
	return ""
}

var legacyHostPath = regexp.MustCompile(`(?:[A-Za-z]:[/\\]|\\|(^|[\s"'(=@])/)`)

func unsafeLegacyText(value string) bool {
	return strings.Contains(value, "://") || strings.IndexFunc(value, unicode.IsControl) >= 0 || legacyHostPath.MatchString(value)
}
func addLegacyRequirement(d *Document, r Requirement) {
	r.Condition = legacyCondition(r.Condition)
	if unsafeLegacyText(r.Value) {
		r.Value = "[value-withheld]"
		r.State = "unresolved"
		AddDiagnostic(d, "legacy-value-withheld", "A declaration value contains unsupported path, URL, or control text and was withheld.")
	}
	AddRequirement(d, r)
}
func addLegacyReference(d *Document, r Reference) {
	r.Condition = legacyCondition(r.Condition)
	// A successfully normalized target is already proven confined to the
	// selected inventory. Backslashes in such references are ordinary MSBuild
	// separators, not host-path evidence.
	unsafe := r.Target == "" && unsafeLegacyText(r.Value)
	if r.Target == "" || unsafe {
		r.Value = "[unresolved-path-withheld]"
		r.State = "unresolved"
		r.Target = ""
		if unsafe {
			AddDiagnostic(d, "legacy-reference-withheld", "A declaration reference contains unsupported path, URL, or control text and was withheld.")
		}
	}
	AddReference(d, r)
}
