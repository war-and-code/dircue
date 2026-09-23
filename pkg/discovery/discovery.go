// Package discovery summarizes file metadata without parsing source or expanding archives.
package discovery

import (
	"container/heap"
	"path"
	"slices"
	"strings"

	enry "github.com/go-enry/go-enry/v2"
	"github.com/go-enry/go-enry/v2/data"
)

const RuleVersion = "1.0.0"
const EvidenceLimitPerKind = 256

// Counts count each regular file once. Role counts overlap and must not be summed.
type Counts struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}
type Group struct {
	Name  string `json:"name"`
	Basis string `json:"basis"`
	Counts
}
type Source struct {
	Mode        string `json:"mode"`
	Tree        string `json:"tree,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Consistency string `json:"consistency"`
}
type Scope struct {
	Population           string `json:"population"`
	ContentInspection    string `json:"content_inspection"`
	Attributes           string `json:"attributes"`
	ArchiveExpansion     bool   `json:"archive_expansion"`
	MaxTreeSize          int    `json:"max_tree_size"`
	EvidenceLimitPerKind int    `json:"evidence_limit_per_kind"`
}

// Candidate is filename evidence only: neither validity nor dependency provenance is established.
type Candidate struct {
	Path   string `json:"path"`
	Root   string `json:"root"`
	Kind   string `json:"kind"`
	Format string `json:"format"`
	Basis  string `json:"basis"`
	Bytes  int64  `json:"bytes"`
}
type Report struct {
	LinguistDataCommit string           `json:"linguist_data_commit"`
	Status             string           `json:"status"`
	Engine             string           `json:"engine"`
	RuleVersion        string           `json:"rule_version"`
	Source             Source           `json:"source"`
	Scope              Scope            `json:"scope"`
	Inventory          Counts           `json:"inventory"`
	Categories         []Group          `json:"categories"`
	Roles              []Group          `json:"roles"`
	CandidateCounts    []Group          `json:"candidate_counts"`
	Candidates         []Candidate      `json:"candidates"`
	OmittedCandidates  map[string]int64 `json:"omitted_candidates"`
	Omissions          map[string]int64 `json:"omissions"`
	// This excludes attribute-file and Git object-storage reads by the inventory layer.
	ClassificationBytesRead int64 `json:"classification_bytes_read"`
}

// File is a selected regular file's metadata and already-resolved attribute hints.
// No content or language-statistics inclusion decision is required.
type File struct {
	Path          string
	Size          int64
	Vendored      *bool
	Generated     *bool
	Documentation *bool
}
type Collector struct {
	report     Report
	categories map[string]*Group
	roles      map[string]*Group
	kinds      map[string]*Group
	samples    map[string]*candidateHeap
}

func New(mode, tree string, maxTreeSize int) *Collector {
	consistency := "live_directory_metadata"
	if mode == "git" {
		consistency = "selected_git_tree"
	}
	return &Collector{report: Report{Status: "complete", Engine: "dircue-metadata", RuleVersion: RuleVersion, LinguistDataCommit: data.LinguistCommit,
		Source: Source{Mode: mode, Tree: tree, Consistency: consistency}, Scope: Scope{"regular_files_including_vendor_and_data", "none", "existing_scanner_attribute_policy", false, maxTreeSize, EvidenceLimitPerKind},
		Categories: []Group{}, Roles: []Group{}, CandidateCounts: []Group{}, Candidates: []Candidate{}, OmittedCandidates: map[string]int64{}, Omissions: map[string]int64{}},
		categories: map[string]*Group{}, roles: map[string]*Group{}, kinds: map[string]*Group{}, samples: map[string]*candidateHeap{}}
}
func add(groups map[string]*Group, name, basis string, size int64) {
	key := name + "\x00" + basis
	g := groups[key]
	if g == nil {
		g = &Group{Name: name, Basis: basis}
		groups[key] = g
	}
	g.Files++
	g.Bytes += size
}
func (c *Collector) Add(file File) {
	c.report.Inventory.Files++
	c.report.Inventory.Bytes += file.Size
	category, basis, candidate := classify(file.Path)
	add(c.categories, category, basis, file.Size)
	if candidate != nil {
		candidate.Bytes = file.Size
		add(c.kinds, candidate.Kind, candidate.Basis, file.Size)
		h := c.samples[candidate.Kind]
		if h == nil {
			h = &candidateHeap{}
			c.samples[candidate.Kind] = h
		}
		if len(*h) < EvidenceLimitPerKind {
			heap.Push(h, *candidate)
		} else {
			c.report.OmittedCandidates[candidate.Kind]++
			if candidate.Path < (*h)[0].Path {
				(*h)[0] = *candidate
				heap.Fix(h, 0)
			}
		}
	}
	role := func(name string, override *bool, fallback bool, basis string) {
		if override != nil {
			if *override {
				add(c.roles, name, "gitattributes", file.Size)
			}
			return
		}
		if fallback {
			add(c.roles, name, basis, file.Size)
		}
	}
	role("vendored", file.Vendored, enry.IsVendor(file.Path), "enry_path")
	// Generated-code content heuristics are deliberately not run in metadata discovery.
	role("generated", file.Generated, false, "")
	role("documentation", file.Documentation, enry.IsDocumentation(file.Path), "enry_path")
	if testPath(file.Path) {
		add(c.roles, "test_candidate", "path_name", file.Size)
	}
}
func (c *Collector) Omit(reason string)    { c.report.Omissions[reason]++ }
func (c *Collector) Partial(reason string) { c.report.Status = "partial"; c.Omit(reason) }
func (c *Collector) Skip(reason string) *Report {
	c.report.Status = "skipped"
	c.Omit(reason)
	return c.Finish()
}
func (c *Collector) Finish() *Report {
	groups := func(values map[string]*Group) []Group {
		out := make([]Group, 0, len(values))
		for _, v := range values {
			out = append(out, *v)
		}
		slices.SortFunc(out, func(a, b Group) int {
			if n := strings.Compare(a.Name, b.Name); n != 0 {
				return n
			}
			return strings.Compare(a.Basis, b.Basis)
		})
		return out
	}
	c.report.Categories = groups(c.categories)
	c.report.Roles = groups(c.roles)
	c.report.CandidateCounts = groups(c.kinds)
	c.report.Candidates = []Candidate{}
	for _, h := range c.samples {
		c.report.Candidates = append(c.report.Candidates, (*h)...)
	}
	slices.SortFunc(c.report.Candidates, func(a, b Candidate) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	if len(c.report.OmittedCandidates) > 0 && c.report.Status == "complete" {
		c.report.Status = "partial"
	}
	return &c.report
}

// Independent heaps keep frequent artifacts from displacing scarce manifests.
type candidateHeap []Candidate

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].Path > h[j].Path }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(x any)        { *h = append(*h, x.(Candidate)) }
func (h *candidateHeap) Pop() any          { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }
func testPath(filename string) bool {
	lower := "/" + strings.ToLower(filename)
	base := strings.ToLower(path.Base(filename))
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") || strings.Contains(lower, "/__tests__/") || strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "test.java") || strings.HasSuffix(base, "tests.java") || strings.HasSuffix(base, "test.cs") || strings.HasSuffix(base, "tests.cs") || strings.HasSuffix(base, "spec.cs") || strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
}
func classify(filename string) (string, string, *Candidate) {
	base := path.Base(filename)
	lower := strings.ToLower(base)
	ext := strings.ToLower(path.Ext(base))
	makeCandidate := func(kind, format, basis string) (string, string, *Candidate) {
		return kind, basis, &Candidate{Path: filename, Root: path.Dir(filename), Kind: kind, Format: format, Basis: basis}
	}
	if format, ok := manifestNames[base]; ok {
		return makeCandidate("manifest", format, "filename")
	}
	switch ext {
	case ".csproj", ".fsproj", ".vbproj":
		return makeCandidate("manifest", "dotnet", "extension")
	case ".sln", ".slnx":
		return makeCandidate("shared_configuration", "dotnet_solution", "extension")
	}
	if format, ok := sharedNames[lower]; ok {
		return makeCandidate("shared_configuration", format, "filename")
	}
	if format, ok := artifactExtensions[ext]; ok {
		// GNU Make conventionally uses Makefile.* for included text fragments.
		// The suffix is not evidence that Makefile.lib is a static library.
		if strings.HasPrefix(lower, "makefile.") {
			return "source_candidate", "filename", nil
		}
		return makeCandidate("artifact", format, "extension")
	}
	// Versioned shared libraries retain their .so identity (libexample.so.1.2).
	if i := strings.LastIndex(lower, ".so."); i > 0 && numericVersion(lower[i+4:]) {
		return makeCandidate("artifact", "shared_library", "filename")
	}
	names := enry.GetLanguagesByFilename(base, nil, nil)
	basis := "enry_filename"
	if len(names) == 0 {
		names = extensionCandidates(base)
		basis = "enry_extension"
	}
	category := ""
	for _, language := range names {
		next := "unclassified"
		switch enry.GetLanguageType(language) {
		case enry.Programming, enry.Markup:
			next = "source_candidate"
		case enry.Data:
			next = "data_candidate"
		case enry.Prose:
			next = "documentation_candidate"
		}
		if category != "" && next != category {
			return "unclassified", "ambiguous_name", nil
		}
		category = next
	}
	if category == "" {
		return "unclassified", "no_name_match", nil
	}
	return category, basis, nil
}

// Discovery reports filename hints without content inspection. Generic suffixes
// remain candidates here even though language detection must confirm them.
func extensionCandidates(filename string) []string {
	filename = strings.ToLower(filename)
	for dot := strings.IndexByte(filename, '.'); dot >= 0; dot = strings.IndexByte(filename, '.') {
		filename = filename[dot:]
		if languages, ok := data.LanguagesByExtension[filename]; ok {
			return languages
		}
		filename = filename[1:]
	}
	return nil
}

func numericVersion(value string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

var manifestNames = map[string]string{
	"pom.xml": "maven", "build.gradle": "gradle", "build.gradle.kts": "gradle", "package.json": "npm", "package-lock.json": "npm_lock", "npm-shrinkwrap.json": "npm_lock", "yarn.lock": "yarn_lock", "pnpm-lock.yaml": "pnpm_lock", "bun.lock": "bun_lock", "bun.lockb": "bun_lock",
	"pyproject.toml": "python", "requirements.txt": "python_requirements", "setup.py": "python", "setup.cfg": "python", "Pipfile": "pipenv", "Pipfile.lock": "pipenv_lock", "poetry.lock": "poetry_lock", "uv.lock": "uv_lock",
	"go.mod": "go", "go.sum": "go_checksums", "Cargo.toml": "cargo", "Cargo.lock": "cargo_lock", "Gemfile": "bundler", "Gemfile.lock": "bundler_lock", "composer.json": "composer", "composer.lock": "composer_lock",
	"packages.config": "nuget", "packages.lock.json": "nuget_lock", "pubspec.yaml": "dart", "pubspec.lock": "dart_lock", "Package.swift": "swift", "Package.resolved": "swift_lock", "mix.exs": "elixir", "mix.lock": "elixir_lock", "build.sbt": "sbt"}
var sharedNames = map[string]string{
	"global.json": "dotnet_sdk", "directory.build.props": "msbuild", "directory.build.targets": "msbuild", "directory.packages.props": "nuget", "nuget.config": "nuget", "settings.gradle": "gradle_workspace", "settings.gradle.kts": "gradle_workspace", "gradle.properties": "gradle", "gradle-wrapper.properties": "gradle_wrapper", "toolchains.xml": "maven_toolchains", "settings.xml": "maven_settings", "go.work": "go_workspace", "go.work.sum": "go_workspace_checksums", "pnpm-workspace.yaml": "pnpm_workspace", ".npmrc": "npm", ".yarnrc.yml": "yarn", ".gitattributes": "git_attributes"}
var artifactExtensions = map[string]string{
	".dll": "dotnet_or_native_library", ".exe": "executable", ".so": "shared_library", ".dylib": "shared_library", ".a": "static_library", ".lib": "static_library", ".jar": "java_archive", ".war": "java_web_archive", ".ear": "java_enterprise_archive", ".class": "java_bytecode", ".whl": "python_wheel", ".nupkg": "nuget_package", ".snupkg": "nuget_symbols", ".zip": "zip_archive", ".tar": "tar_archive", ".gz": "gzip_archive", ".tgz": "gzip_archive", ".bz2": "bzip2_archive", ".xz": "xz_archive", ".7z": "7z_archive", ".rar": "rar_archive", ".deb": "debian_package", ".rpm": "rpm_package", ".apk": "android_or_alpine_package", ".wasm": "webassembly"}
