package profile

const MetricsSchemaVersion = "1.1.0"

// Counts are full-file scc counters. Complexity is scc's lexical estimate,
// not an AST-derived cyclomatic complexity measurement.
type Counts struct {
	Files      int64 `json:"files"`
	Bytes      int64 `json:"bytes"`
	Lines      int64 `json:"lines"`
	Code       int64 `json:"code"`
	Comment    int64 `json:"comment"`
	Blank      int64 `json:"blank"`
	Complexity int64 `json:"complexity"`
}

type LanguageMetrics struct {
	Language string `json:"language"`
	Grammar  string `json:"grammar"`
	Counts
}

// DirectoryMetrics groups counted files by their immediate parent directory.
// It does not imply a project boundary or include descendant directories.
type DirectoryMetrics struct {
	Path string `json:"path"`
	Counts
}

type FileMetrics struct {
	Path     string  `json:"path"`
	Language string  `json:"language,omitempty"`
	Grammar  string  `json:"grammar,omitempty"`
	Status   string  `json:"status"` // counted or skipped
	Reason   string  `json:"reason,omitempty"`
	Counts   *Counts `json:"counts,omitempty"`
}

type MetricSkip struct {
	Reason string `json:"reason"`
	Files  *int64 `json:"files,omitempty"`
}

// MetricsReport describes the counting scope and its coverage independently
// of language statistics. Partial totals contain only completely counted files.
type MetricsReport struct {
	Engine        string             `json:"engine"`
	EngineVersion string             `json:"engine_version"`
	Status        string             `json:"status"` // complete, partial, or skipped
	Scope         string             `json:"scope"`
	MaxFileBytes  int64              `json:"max_file_bytes"`
	Source        string             `json:"source"`         // git or directory
	Tree          string             `json:"tree,omitempty"` // selected Git tree object
	Totals        Counts             `json:"totals"`
	Languages     []LanguageMetrics  `json:"languages"`
	Directories   []DirectoryMetrics `json:"directories"`
	Skipped       []MetricSkip       `json:"skipped"`
	Files         *[]FileMetrics     `json:"files,omitempty"`
}
