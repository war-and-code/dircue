// Package deployables observes bounded static shipping and automation declarations.
// It never executes repository content, expands templates, reads the host environment,
// or fetches referenced content.
package deployables

import (
	"context"
	"path"
)

const (
	ProviderVersion    = "1.0.0"
	DefaultFileBytes   = int64(256 << 10)
	DefaultInputBytes  = int64(16 << 20)
	DefaultFiles       = 4096
	DefaultDefinitions = 16384
	DefaultReferences  = 32768
	DefaultDiagnostics = 1024
	DefaultStringBytes = 8192
)

// Candidate is a regular file from the caller's already selected source.
// Read returns at most the requested bytes and the file's full selected-source size.
type Candidate struct {
	Path string
	Size int64
	Read func(context.Context, int64) ([]byte, int64, error)
}

type Source struct {
	Mode        string `json:"mode"`
	Tree        string `json:"tree,omitempty"`
	Consistency string `json:"consistency"`
}

type Limits struct {
	FileBytes   int64 `json:"file_bytes"`
	InputBytes  int64 `json:"input_bytes"`
	Files       int   `json:"files"`
	Definitions int   `json:"definitions"`
	References  int   `json:"references"`
	Diagnostics int   `json:"diagnostics"`
	StringBytes int   `json:"string_bytes"`
}

type Coverage struct {
	SelectedFiles       int64 `json:"selected_files"`
	CandidateFiles      int64 `json:"candidate_files"`
	ReadFiles           int64 `json:"read_files"`
	ParsedFiles         int64 `json:"parsed_files"`
	InspectedBytes      int64 `json:"inspected_bytes"`
	RetainedDefinitions int   `json:"retained_definitions"`
	RetainedReferences  int   `json:"retained_references"`
	OmittedFiles        int64 `json:"omitted_files"`
}

// Evidence identifies a parsed declaration. Value is restricted to structural
// identifiers and references; scripts, environment values and arbitrary inputs are omitted.
type Evidence struct {
	Field string `json:"field"`
	Value string `json:"value,omitempty"`
	Line  int    `json:"line,omitempty"`
	Basis string `json:"basis"`
}

type Reference struct {
	Kind          string   `json:"kind"`
	Value         string   `json:"value"`
	Qualification string   `json:"qualification"` // local, external, unresolved, or declared
	Evidence      Evidence `json:"evidence"`
	// Stage is parser-only Dockerfile context used to bind COPY observations to
	// the build stage in which they occur. It is intentionally not public output.
	Stage       string `json:"-"`
	SourceStage string `json:"-"`
	// Checkout is parser-only GitHub Actions context for a working directory:
	// the actions/checkout path that contains it in the same job, and whether
	// that checkout names a repository. Empty when no checkout contains it.
	Checkout      string `json:"-"`
	CheckoutNamed bool   `json:"-"`
	SourcePath    string `json:"-"`
	TargetPath    string `json:"-"`
}

// Definition is a declaration, not proof of a built image, executed workflow,
// provisioned resource, running service, effective permission, or network exposure.
type Definition struct {
	ID           string      `json:"id"`
	Kind         string      `json:"kind"` // container_build, service, workload, infrastructure, workflow, resource, archive
	Provider     string      `json:"provider"`
	Name         string      `json:"name"`
	Path         string      `json:"path"`
	SourceSHA256 string      `json:"source_sha256"`
	Coverage     string      `json:"coverage"` // complete or qualified
	Evidence     []Evidence  `json:"evidence"`
	References   []Reference `json:"references"`
	// DockerPathCopies keeps bounded RUN cp path evidence private while mapbuild
	// checks whether a staged artifact could have reached its final COPY.
	DockerPathCopies []Reference `json:"-"`
	// DockerContextUnknown marks a Dockerfile below the repository root whose
	// build-context directory cannot be inferred from the Dockerfile path. COPY
	// sources remain context-relative, so consumers must not treat them as
	// repository-root paths without separate context evidence.
	DockerContextUnknown bool `json:"-"`
	// DockerPathWrites retains bounded opaque RUN/ADD instructions as private
	// barriers while mapbuild traces staged artifact paths.
	DockerPathWrites []Reference `json:"-"`
	// Format is the packaging format for archive deployables (e.g. "war", "ear").
	// Empty for all other kinds.
	Format string `json:"format,omitempty"`
	// K8sKind holds the raw Kubernetes kind string (e.g. "Deployment") for
	// same-object deduplication across files. Empty for non-Kubernetes providers.
	K8sKind string `json:"k8s_kind,omitempty"`
	// Namespace holds the declared Kubernetes namespace (empty = cluster default).
	Namespace string `json:"namespace,omitempty"`
	// Count is set by aggregation functions to record how many files declared
	// the same logical object. Zero means unaggregated (treat as 1).
	Count int `json:"count,omitempty"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	Provider        string           `json:"provider"`
	ProviderVersion string           `json:"provider_version"`
	Status          string           `json:"status"`
	Source          Source           `json:"source"`
	Selection       string           `json:"selection"`
	Limits          Limits           `json:"limits"`
	Coverage        Coverage         `json:"coverage"`
	Definitions     []Definition     `json:"definitions"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	Omissions       map[string]int64 `json:"omissions"`
	// Directories holds every directory above a selected file, so path
	// references can be checked against the scanned tree. Not serialized.
	Directories map[string]bool `json:"-"`
}

// addDirectories records every ancestor directory of a file path.
func addDirectories(dirs map[string]bool, file string) {
	for dir := path.Dir(file); dir != "." && dir != "/" && !dirs[dir]; dir = path.Dir(dir) {
		dirs[dir] = true
	}
}

type Options struct {
	Source      Source
	FileBytes   int64
	InputBytes  int64
	Files       int
	Definitions int
	References  int
	Diagnostics int
	StringBytes int
}
