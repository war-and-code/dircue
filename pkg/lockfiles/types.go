// Package lockfiles compares a deliberately narrow subset of package
// declarations with checked-in lockfile records. It never resolves packages,
// performs a restore, or contacts a registry.
package lockfiles

import (
	"context"

	"github.com/war-and-code/dircue/pkg/declarations"
)

const (
	Provider                       = "dircue"
	ProviderVersion                = "1.0.0"
	DefaultMaxInventoryPaths       = 200000
	DefaultMaxLockfiles            = 256
	DefaultMaxFileBytes      int64 = 1 << 20
	DefaultMaxInputBytes     int64 = 16 << 20
	DefaultMaxPackageNames         = 65536
	DefaultMaxContexts             = 4096
	DefaultMaxOutputBytes          = 16 << 20
)

type File struct {
	Path       string `json:"path"`
	Size       int64  `json:"bytes"`
	NonRegular bool   `json:"non_regular,omitempty"`
}

// ReadSelected reads bytes from the source snapshot already selected by the
// caller (a Git tree or confined directory). The module never opens paths.
type ReadSelected func(context.Context, string, int64) ([]byte, int64, error)

type Input struct {
	Source            string
	Tree              string
	Inventory         []File
	InventoryComplete bool
	OmittedFiles      int64
	Declarations      declarations.Report
	ProjectRecords    []declarations.ProjectRecord
	ReadSelected      ReadSelected
	// ErrorPolicy "continue" retains later observations after a selected read
	// error; the default returns a wrapped error.
	ErrorPolicy string
}

type Limits struct {
	InventoryPaths int   `json:"inventory_paths"`
	Lockfiles      int   `json:"lockfiles"`
	FileBytes      int64 `json:"file_bytes"`
	InputBytes     int64 `json:"input_bytes"`
	PackageNames   int   `json:"package_names"`
	Contexts       int   `json:"contexts"`
	OutputBytes    int   `json:"output_bytes"`
}

type Coverage struct {
	InventoryPaths  int   `json:"inventory_paths"`
	ProjectRecords  int   `json:"project_records"`
	LockCandidates  int   `json:"lock_candidates"`
	LockfilesRead   int   `json:"lockfiles_read"`
	InputBytes      int64 `json:"input_bytes"`
	PackageNames    int   `json:"package_names"`
	OmittedFiles    int64 `json:"omitted_files"`
	OmittedContexts int   `json:"omitted_contexts"`
}

// Context describes one statically associated project/lockfile pair. The
// association status is separate from the scoped comparison results.
// Context records an npm or NuGet project and its nearest supported lockfile
// association. AssociationState is observed, missing, unsupported,
// indeterminate, or not_applicable.
type Context struct {
	ProjectID        string     `json:"project_id"`
	Ecosystem        string     `json:"ecosystem"`
	ManifestPath     string     `json:"manifest_path"`
	LockfilePath     string     `json:"lockfile_path,omitempty"`
	AssociationState string     `json:"association_state"`
	LockfileVersion  string     `json:"lockfile_version,omitempty"`
	Checks           []Check    `json:"checks"`
	Boundaries       []Boundary `json:"boundaries"`
}

// Check statuses describe only the named syntactic check. "match" never
// means that a package manager would produce the same resolved graph.
// Check reports one explicitly named comparison. Status is match, different,
// indeterminate, or not_applicable.
type Check struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Compared    int      `json:"compared"`
	Missing     []string `json:"missing,omitempty"`
	Mismatched  []string `json:"mismatched,omitempty"`
	Unexpected  []string `json:"unexpected,omitempty"`
	Explanation string   `json:"explanation"`
}

type Boundary struct {
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Report summarizes bounded inspection of the selected inventory. Status is
// complete, partial, or skipped and describes coverage, not dependency health.
type Report struct {
	Provider        string       `json:"provider"`
	ProviderVersion string       `json:"provider_version"`
	Status          string       `json:"status"`
	Source          string       `json:"source"`
	Tree            string       `json:"tree,omitempty"`
	Semantics       []string     `json:"semantics"`
	Limits          Limits       `json:"limits"`
	Coverage        Coverage     `json:"coverage"`
	Contexts        []Context    `json:"contexts"`
	Diagnostics     []Diagnostic `json:"diagnostics"`
}
