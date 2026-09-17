package profile

import "dircue/pkg/structure"

// StructureReport records selected source coverage. Observations are additive
// syntax-node counts; BCA metrics remain per-file because many are not additive.
type StructureReport struct {
	SupportedLanguages []structure.Capability `json:"supported_languages"`
	ObservationFiles   map[string]int64       `json:"observation_files"`
	Engine             string                 `json:"engine"`
	EngineVersion      string                 `json:"engine_version"`
	Status             string                 `json:"status"`
	Scope              string                 `json:"scope"`
	Source             string                 `json:"source"`
	Tree               string                 `json:"tree,omitempty"`
	MaxFileBytes       int64                  `json:"max_file_bytes"`
	AnalyzedFiles      int64                  `json:"analyzed_files"`
	PartialFiles       int64                  `json:"partial_files"`
	ParseCount         int64                  `json:"parse_count"`
	Omissions          map[string]int64       `json:"omissions"`
	Observations       map[string]uint64      `json:"observations"`
	Files              *[]structure.File      `json:"files,omitempty"`
}
