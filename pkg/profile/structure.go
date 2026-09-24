package profile

import "github.com/war-and-code/dircue/pkg/structure"

// StructureReport records selected source coverage. Observations are additive
// syntax-node counts; BCA metrics remain per-file because many are not additive.
type StructureReport struct {
	Hotspots           *structure.HotspotReport `json:"hotspots,omitempty"`
	Functions          *FunctionReport          `json:"functions,omitempty"`
	SupportedLanguages []structure.Capability   `json:"supported_languages"`
	ObservationFiles   map[string]int64         `json:"observation_files"`
	Engine             string                   `json:"engine"`
	EngineVersion      string                   `json:"engine_version"`
	Status             string                   `json:"status"`
	Scope              string                   `json:"scope"`
	Source             string                   `json:"source"`
	Tree               string                   `json:"tree,omitempty"`
	MaxFileBytes       int64                    `json:"max_file_bytes"`
	AnalyzedFiles      int64                    `json:"analyzed_files"`
	PartialFiles       int64                    `json:"partial_files"`
	ParseCount         int64                    `json:"parse_count"`
	Omissions          map[string]int64         `json:"omissions"`
	Observations       map[string]uint64        `json:"observations"`
	Files              *[]structure.File        `json:"files,omitempty"`
}

// FunctionReport retains bounded function-space evidence from selected files.
// ParentStatus and Omissions describe the file coverage behind these counts.
type FunctionReport struct {
	Provider             string             `json:"provider"`
	Rule                 string             `json:"rule"`
	RuleVersion          string             `json:"rule_version"`
	Scope                string             `json:"scope"`
	Status               string             `json:"status"`
	ParentStatus         string             `json:"parent_status"`
	MetricScope          string             `json:"metric_scope"`
	MetricGroups         []string           `json:"metric_groups"`
	Order                string             `json:"order"`
	Limit                int                `json:"limit"`
	PerFileLimit         int                `json:"per_file_limit"`
	NameMaxBytes         int                `json:"name_max_bytes"`
	AnalyzedFiles        int64              `json:"analyzed_files"`
	PartialFiles         int64              `json:"partial_files"`
	Omissions            map[string]int64   `json:"omissions"`
	TotalSpaces          uint64             `json:"total_spaces"`
	OmittedSpaces        uint64             `json:"omitted_spaces"`
	PerFileOmittedSpaces uint64             `json:"per_file_omitted_spaces"`
	ReportOmittedSpaces  uint64             `json:"report_omitted_spaces"`
	InvalidSpanSpaces    uint64             `json:"invalid_span_spaces"`
	Entries              []FunctionEvidence `json:"entries"`
}

type FunctionEvidence struct {
	Path         string `json:"path"`
	Language     string `json:"language"`
	SourceSHA256 string `json:"source_sha256"`
	structure.FunctionEntry
}
