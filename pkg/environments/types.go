// Package environments maps declared build environment requirements without
// probing installed tools, executing repository code, or contacting a network.
package environments

import (
	"context"

	"github.com/war-and-code/dircue/pkg/declarations"
)

const (
	Provider                            = "dircue"
	ProviderVersion                     = "1.1.0"
	LegacyProviderVersion               = "1.0.0"
	DefaultMaxInventoryPaths            = 200000
	DefaultMaxGlobalJSONBytes     int64 = 1 << 20
	DefaultMaxToolchainFiles            = 4096
	DefaultMaxToolchainFileBytes  int64 = 64 << 10
	DefaultMaxToolchainInputBytes int64 = 4 << 20
	DefaultMaxInputBytes          int64 = 16 << 20
	DefaultMaxOutputBytes               = 16 << 20
	DefaultMaxRequirements              = 65536
	DefaultMaxContexts                  = 4096
)

type File struct {
	Path       string `json:"path"`
	Size       int64  `json:"bytes"`
	NonRegular bool   `json:"non_regular,omitempty"`
}
type Invocation struct {
	ProjectID string `json:"project_id"`
	Directory string `json:"directory"`
}
type ReadSelected func(context.Context, string, int64) ([]byte, int64, error)

type Input struct {
	Source            string
	Tree              string
	Inventory         []File
	InventoryComplete bool
	OmittedFiles      int64
	Declarations      declarations.Report
	ProjectRecords    []declarations.ProjectRecord
	InvocationStarts  []Invocation
	ReadSelected      ReadSelected
	// ErrorPolicy controls how Analyze reacts to per-file read failures.
	// "continue" degrades that selection to unresolved with a per-path
	// "file-read-error" diagnostic and marks the report partial; anything
	// else preserves the historical fail-fast contract.
	ErrorPolicy string
}

type Limits struct {
	InventoryPaths      int   `json:"inventory_paths"`
	GlobalJSONBytes     int64 `json:"global_json_bytes"`
	ToolchainFiles      int   `json:"toolchain_files,omitempty"`
	ToolchainFileBytes  int64 `json:"toolchain_file_bytes,omitempty"`
	ToolchainInputBytes int64 `json:"toolchain_input_bytes,omitempty"`
	InputBytes          int64 `json:"input_bytes"`
	Requirements        int   `json:"requirements"`
	Contexts            int   `json:"contexts"`
	OutputBytes         int   `json:"output_bytes"`
}
type Coverage struct {
	InventoryPaths        int   `json:"inventory_paths"`
	ProjectRecords        int   `json:"project_records"`
	ParsedProjectRecords  int   `json:"parsed_project_records"`
	Contexts              int   `json:"contexts"`
	GlobalJSONCandidates  int   `json:"global_json_candidates"`
	GlobalJSONRead        int   `json:"global_json_read"`
	ToolchainCandidates   int   `json:"toolchain_candidates,omitempty"`
	ToolchainRead         int   `json:"toolchain_read,omitempty"`
	ToolchainBytes        int64 `json:"toolchain_bytes,omitempty"`
	ToolchainDeclarations int   `json:"toolchain_declarations,omitempty"`
	OmittedToolchainFiles int64 `json:"omitted_toolchain_files,omitempty"`
	InputBytes            int64 `json:"input_bytes"`
	Requirements          int   `json:"requirements"`
	OmittedFiles          int64 `json:"omitted_files"`
	OmittedRequirements   int   `json:"omitted_requirements"`
}

type Requirement struct {
	ProjectID     string `json:"project_id"`
	ContextID     string `json:"context_id"`
	Dimension     string `json:"dimension"`
	Kind          string `json:"kind"`
	Value         string `json:"value"`
	State         string `json:"state"`
	Evidence      string `json:"evidence"`
	Condition     string `json:"condition,omitempty"`
	Applicability string `json:"applicability"`
}

type Selection struct {
	ContextID       string `json:"context_id"`
	ProjectID       string `json:"project_id"`
	StartDirectory  string `json:"start_directory"`
	StartBasis      string `json:"start_basis"`
	GlobalJSON      string `json:"global_json,omitempty"`
	SDKVersion      string `json:"sdk_version,omitempty"`
	RollForward     string `json:"roll_forward,omitempty"`
	AllowPrerelease *bool  `json:"allow_prerelease,omitempty"`
	State           string `json:"state"`
	Applicability   string `json:"applicability"`
}

// ToolchainDeclaration records a bounded literal from a known toolchain file.
// It does not assert that a manager selects, installs, or applies the value.
type ToolchainDeclaration struct {
	SourcePath     string   `json:"source_path"`
	Tool           string   `json:"tool"`
	Kind           string   `json:"kind"`
	Values         []string `json:"values,omitempty"`
	State          string   `json:"state"`
	ScopeDirectory string   `json:"scope_directory"`
	Applicability  string   `json:"applicability"`
}

type Conflict struct {
	ContextID   string   `json:"context_id"`
	Dimension   string   `json:"dimension"`
	Values      []string `json:"values"`
	Evidence    []string `json:"evidence"`
	Explanation string   `json:"explanation"`
}
type Boundary struct {
	Path      string `json:"path,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	Reason    string `json:"reason"`
	Detail    string `json:"detail,omitempty"`
}
type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	Provider              string                 `json:"provider"`
	ProviderVersion       string                 `json:"provider_version"`
	Status                string                 `json:"status"`
	Source                string                 `json:"source"`
	Tree                  string                 `json:"tree,omitempty"`
	SemanticsReference    string                 `json:"semantics_reference"`
	Limits                Limits                 `json:"limits"`
	Coverage              Coverage               `json:"coverage"`
	Requirements          []Requirement          `json:"requirements"`
	Selections            []Selection            `json:"selections"`
	ToolchainDeclarations []ToolchainDeclaration `json:"toolchain_declarations,omitempty"`
	Conflicts             []Conflict             `json:"conflicts"`
	Boundaries            []Boundary             `json:"boundaries"`
	Diagnostics           []Diagnostic           `json:"diagnostics"`
}
