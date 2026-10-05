// Package profile defines the versioned repository profile and detector API.
package profile

import (
	"context"

	"github.com/war-and-code/dircue/pkg/assessment"
	"github.com/war-and-code/dircue/pkg/availability"
	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/discovery"
	"github.com/war-and-code/dircue/pkg/environments"
	"github.com/war-and-code/dircue/pkg/explain"
	"github.com/war-and-code/dircue/pkg/focus"
	"github.com/war-and-code/dircue/pkg/formats"
	"github.com/war-and-code/dircue/pkg/lockfiles"
	"github.com/war-and-code/dircue/pkg/packageevidence"
	"github.com/war-and-code/dircue/pkg/projects"
	"github.com/war-and-code/dircue/pkg/registries"
	"github.com/war-and-code/dircue/pkg/rules"
)

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

const ExpandedSchemaVersion = "1.2.0"

const EnhancedSchemaVersion = "1.3.0"

const DeclarationsSchemaVersion = "1.4.0"

const ContentSchemaVersion = "1.5.0"

const TargetedSchemaVersion = "1.6.0"

const EnvironmentSchemaVersion = "1.7.0"

const LockfilesSchemaVersion = "1.8.0"

const AssessmentSchemaVersion = "1.9.0"

// SummarizedTree records a recognized environment or build-output tree that
// was counted rather than scanned in detail.
type SummarizedTree struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Ecosystem  string `json:"ecosystem"`
	Marker     string `json:"marker"`
	Basis      string `json:"basis"`
	Entries    int64  `json:"entries"`
	Bytes      int64  `json:"bytes"`
	Bounded    bool   `json:"bounded"`
	LowerBound bool   `json:"lower_bound,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type Report struct {
	Assessment      *assessment.Report      `json:"assessment,omitempty"`
	Lockfiles       *lockfiles.Report       `json:"lockfiles,omitempty"`
	Environments    *environments.Report    `json:"environments,omitempty"`
	Explanation     *explain.Report         `json:"explanation,omitempty"`
	Availability    *availability.Report    `json:"availability,omitempty"`
	Focus           *focus.Report           `json:"focus,omitempty"`
	FocusedMetrics  *FocusedMetrics         `json:"focused_metrics,omitempty"`
	Formats         *formats.Report         `json:"formats,omitempty"`
	Declarations    *declarations.Report    `json:"declarations,omitempty"`
	Registries      *registries.Report      `json:"registries,omitempty"`
	Rules           *rules.Report           `json:"rules,omitempty"`
	PackageEvidence *packageevidence.Report `json:"package_evidence,omitempty"`
	Discovery       *discovery.Report       `json:"discovery,omitempty"`
	Graph           *projects.GraphReport   `json:"graph,omitempty"`
	Projects        *projects.Report        `json:"projects,omitempty"`
	Structure       *StructureReport        `json:"structure,omitempty"`
	// SummarizedTrees holds environment and build-output trees that were
	// recognized and counted rather than walked in detail. Only populated
	// when the scanner was run with SummarizeTrees=true on a directory source.
	SummarizedTrees []SummarizedTree `json:"summarized_trees,omitempty"`
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
