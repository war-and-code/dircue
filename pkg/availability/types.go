// Package availability describes source-acquisition boundaries without
// fetching, hydrating, executing filters, or expanding the selected source.
package availability

import "context"

const (
	ProviderVersion                    = "1.0.0"
	PointerSizeCutoff            int64 = 1024
	DefaultContentBytes          int64 = 16 << 20
	DefaultGitmodulesBytes       int64 = 1 << 20
	DefaultCheckoutMetadataBytes int64 = 16 << 20
	DefaultEvidenceLimit               = 4096
	DefaultDiagnosticLimit             = 1024
	DefaultBoundaryPaths               = 16384
	DefaultCorrelationWork             = 1 << 20
	DefaultOutputBytes                 = 8 << 20
	DefaultStringBytes                 = 8192
)

type Source struct {
	Mode             string `json:"mode"`
	Tree             string `json:"tree,omitempty"`
	Consistency      string `json:"consistency"`
	CheckoutMetadata string `json:"checkout_metadata"`
}

type Options struct {
	Source                Source
	MaxFileBytes          int64
	ContentBytes          int64
	GitmodulesBytes       int64
	CheckoutMetadataBytes int64
	EvidenceLimit         int
	DiagnosticLimit       int
	BoundaryPaths         int
	CorrelationWork       int
	OutputBytes           int
	StringBytes           int
	InventoryComplete     bool
	InventoryStatusSet    bool
}

type Bounds struct {
	PointerBytes          int64 `json:"pointer_bytes"`
	MaxFileBytes          int64 `json:"max_file_bytes"`
	ContentBytes          int64 `json:"content_bytes"`
	GitmodulesBytes       int64 `json:"gitmodules_bytes"`
	CheckoutMetadataBytes int64 `json:"checkout_metadata_bytes"`
	EvidencePerKind       int   `json:"evidence_per_kind"`
	Diagnostics           int   `json:"diagnostics"`
	BoundaryPaths         int   `json:"boundary_paths"`
	CorrelationWork       int   `json:"correlation_work"`
	OutputBytes           int   `json:"output_bytes"`
	StringBytes           int   `json:"string_bytes"`
}

type Coverage struct {
	SelectedInventoryComplete bool  `json:"selected_inventory_complete"`
	CheckoutMetadataInspected bool  `json:"checkout_metadata_inspected"`
	CheckoutMetadataComplete  bool  `json:"checkout_metadata_complete"`
	SelectedRegularFiles      int64 `json:"selected_regular_files"`
	PointerCandidates         int64 `json:"pointer_candidates"`
	PointerInspections        int64 `json:"pointer_inspections"`
	ContentBytesRead          int64 `json:"content_bytes_read"`
	OmittedPointerFiles       int64 `json:"omitted_pointer_files"`
	IndexedBoundaryPaths      int   `json:"indexed_boundary_paths"`
	OmittedBoundaryPaths      int64 `json:"omitted_boundary_paths"`
	CorrelationWork           int   `json:"correlation_work"`
	OmittedCorrelations       int64 `json:"omitted_correlations"`
	OmittedEvidence           int64 `json:"omitted_evidence"`
	OmittedDiagnostics        int64 `json:"omitted_diagnostics"`
}

type Counts struct {
	ValidPointers     int64 `json:"valid_pointers"`
	CanonicalPointers int64 `json:"canonical_pointers"`
	PointerLikeFiles  int64 `json:"pointer_like_files"`
	LFSTrackedFiles   int64 `json:"lfs_tracked_files"`
	// LFSAttributedNonPointerFiles observes only filter=lfs plus non-pointer
	// selected content. It does not establish successful LFS hydration.
	LFSAttributedNonPointerFiles int64 `json:"lfs_attributed_non_pointer_files"`
	Gitlinks                     int64 `json:"gitlinks"`
	SubmoduleDeclarations        int64 `json:"submodule_declarations"`
	MatchedSubmodules            int64 `json:"matched_submodules"`
	SparseIndications            int64 `json:"sparse_indications"`
	MissingReferences            int64 `json:"missing_references"`
	QualifiedMissingReferences   int64 `json:"qualified_missing_references"`
}

// Read returns at most limit bytes and the full selected-source size. A read
// error remains fatal to the availability report.
type Read func(context.Context, int64) ([]byte, int64, error)

type File struct {
	Path         string
	Size         int64
	LFSAttribute string // tracked, untracked, or unknown
	Read         Read
}

type LFSObservation struct {
	Path                string `json:"path"`
	Kind                string `json:"kind"` // valid_pointer or pointer_like
	Reason              string `json:"reason,omitempty"`
	LFSAttribute        string `json:"lfs_attribute"`
	SourceFileBytes     int64  `json:"source_file_bytes"`
	MetadataComplete    bool   `json:"metadata_complete"`
	Version             string `json:"version,omitempty"`
	OIDAlgorithm        string `json:"oid_algorithm,omitempty"`
	OIDDigest           string `json:"oid_digest,omitempty"`
	DeclaredObjectBytes int64  `json:"declared_object_bytes"`
	Canonical           bool   `json:"canonical"`
	ExtensionCount      int    `json:"extension_count,omitempty"`
}

type Gitlink struct {
	Path     string `json:"path"`
	Commit   string `json:"commit"`
	Declared bool   `json:"declared"`
}

type SubmoduleDeclaration struct {
	Path       string `json:"path"`
	Evidence   string `json:"evidence"`
	HasGitlink bool   `json:"has_gitlink"`
}

type SparseIndication struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Evidence  string `json:"evidence"`
	Supported bool   `json:"supported"`
}

type MissingReference struct {
	Project  string `json:"project"`
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Evidence string `json:"evidence"`
}

type ReferenceCorrelation struct {
	MissingReference
	BoundaryKind  string `json:"boundary_kind,omitempty"`
	BoundaryPath  string `json:"boundary_path,omitempty"`
	Qualification string `json:"qualification"` // established_boundary, unqualified, or unknown
}

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Report struct {
	Provider        string                 `json:"provider"`
	ProviderVersion string                 `json:"provider_version"`
	Status          string                 `json:"status"`
	Source          Source                 `json:"source"`
	Bounds          Bounds                 `json:"bounds"`
	Coverage        Coverage               `json:"coverage"`
	Counts          Counts                 `json:"counts"`
	LFS             []LFSObservation       `json:"lfs"`
	Gitlinks        []Gitlink              `json:"gitlinks"`
	Submodules      []SubmoduleDeclaration `json:"submodules"`
	Sparse          []SparseIndication     `json:"sparse"`
	References      []ReferenceCorrelation `json:"references"`
	Diagnostics     []Diagnostic           `json:"diagnostics"`
	Omissions       map[string]int64       `json:"omissions"`
}
