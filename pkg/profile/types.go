// Package profile defines the versioned repository profile and detector API.
package profile

import "context"

const SchemaVersion = "1.0.0"

// File is an immutable, bounded view of a regular file. Path is relative to the
// scan root and uses forward slashes. Content must not be retained or modified.
// Language may be empty; manifests and CI files still reach detectors.
type File struct {
	Path     string
	Size     int64
	Content  []byte
	Language string
	Included bool // Whether this file contributes to language statistics.
}

// Detector is a stateless hook called concurrently by scanner workers.
// Implementations must be concurrency-safe and must not execute repository code.
type Detector interface {
	Name() string
	Detect(context.Context, File) ([]Finding, error)
}

// Finding records an observation, not a confirmed dependency or vulnerability.
// Name must be nonempty; Root and Evidence use clean, root-relative paths.
// Root may be "." (empty defaults to "."); at least one evidence path is required.
// Scanner supplies Detector from the hook's Name method.
type Finding struct {
	Kind     string   `json:"kind"` // ecosystem, framework, or layout
	Name     string   `json:"name"`
	Root     string   `json:"root"`
	Evidence []string `json:"evidence"`
	Detector string   `json:"detector"`
}

type Language struct {
	FirstFile  string   `json:"-"` // Lexically first included path, for Linguist CLI ordering.
	Name       string   `json:"name"`
	Bytes      int64    `json:"bytes"`
	Percentage float64  `json:"percentage"`
	FileCount  int64    `json:"file_count"`
	Files      []string `json:"files,omitempty"`
}

type Summary struct {
	ScannedFiles  int64 `json:"scanned_files"`
	AnalyzedFiles int64 `json:"analyzed_files"`
	SkippedFiles  int64 `json:"skipped_files"`
	LanguageBytes int64 `json:"language_bytes"`
}

type Warning struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	// Strategies is CLI-only diagnostic data and does not change the JSON schema.
	Strategies    map[string]string `json:"-"`
	SchemaVersion string            `json:"schema_version"`
	Root          string            `json:"root"`
	Summary       Summary           `json:"summary"`
	Languages     []Language        `json:"languages"`
	Ecosystems    []Finding         `json:"ecosystems"`
	Frameworks    []Finding         `json:"frameworks"`
	Layouts       []Finding         `json:"layouts"`
	Warnings      []Warning         `json:"warnings"`
	Metrics       *MetricsReport    `json:"metrics,omitempty"`
}
