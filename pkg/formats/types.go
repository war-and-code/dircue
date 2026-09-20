// Package formats inspects bounded file prefixes without expanding containers or executing content.
package formats

import "context"

const ProviderVersion = "1.0.0"
const MaxFiles = 4096
const MaxFileBytes int64 = 64 << 10
const MaxInputBytes int64 = 32 << 20
const MaxPathBytes = 2048
const MaxOutputBytes = 16 << 20
const MaxDepth = 64
const MaxTokens = 65536

type Source struct {
	Mode        string `json:"mode"`
	Tree        string `json:"tree,omitempty"`
	Consistency string `json:"consistency"`
}
type Scope struct {
	Population       string `json:"population"`
	Selection        string `json:"selection"`
	ArchiveExpansion bool   `json:"archive_expansion"`
}
type Limits struct {
	FileBytes          int64 `json:"file_bytes"`
	MaxSourceFileBytes int64 `json:"max_source_file_bytes"`
	InputBytes         int64 `json:"input_bytes"`
	Files              int   `json:"files"`
	PathBytes          int   `json:"path_bytes"`
	OutputBytes        int   `json:"output_bytes"`
	ParserDepth        int   `json:"parser_depth"`
	ParserTokens       int   `json:"parser_tokens"`
}
type Coverage struct {
	SelectedFiles        int64 `json:"selected_files"`
	SelectedBytes        int64 `json:"selected_bytes"`
	InspectedFiles       int64 `json:"inspected_files"`
	InspectedBytes       int64 `json:"inspected_bytes"`
	CompleteReads        int64 `json:"complete_reads"`
	PrefixReads          int64 `json:"prefix_reads"`
	OmittedFiles         int64 `json:"omitted_files"`
	RetainedObservations int64 `json:"retained_observations"`
}

// Evidence describes only the named detector's observation. A signature does not validate a container.
type Evidence struct {
	Format     string `json:"format"`
	Basis      string `json:"basis"`
	Validation string `json:"validation,omitempty"`
}
type Observation struct {
	Path        string     `json:"path"`
	Bytes       int64      `json:"bytes"`
	BytesRead   int64      `json:"bytes_read"`
	ReadScope   string     `json:"read_scope"`
	Evidence    []Evidence `json:"evidence"`
	Diagnostics []string   `json:"diagnostics"`
}
type Report struct {
	Provider        string           `json:"provider"`
	ProviderVersion string           `json:"provider_version"`
	Status          string           `json:"status"`
	Source          Source           `json:"source"`
	Scope           Scope            `json:"scope"`
	Limits          Limits           `json:"limits"`
	Coverage        Coverage         `json:"coverage"`
	Observations    []Observation    `json:"observations"`
	Omissions       map[string]int64 `json:"omissions"`
}

// Candidate reads at most the requested byte count from the scanner's selected source.
// The second return value is the source's full size, not the prefix length.
type Candidate struct {
	Path string
	Size int64
	Read func(context.Context, int64) ([]byte, int64, error)
}
