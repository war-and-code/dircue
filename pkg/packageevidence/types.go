// Package packageevidence imports a bounded subset of native Syft JSON.
// It never invokes a scanner, follows evidence paths, or evaluates build code.
package packageevidence

import "errors"

const SupportedSchema = "16.1.10"

var (
	ErrInvalid     = errors.New("invalid Syft report")
	ErrUnsupported = errors.New("unsupported Syft JSON schema")
	ErrLimit       = errors.New("Syft report exceeds import limits")
	ErrContext     = errors.New("invalid package evidence context")
)

type Limits struct {
	Bytes         int64 `json:"bytes"`
	Artifacts     int   `json:"artifacts"`
	Relationships int   `json:"relationships"`
	Files         int   `json:"files"`
	Locations     int   `json:"locations"`
}

func DefaultLimits() Limits {
	return Limits{Bytes: 16 << 20, Artifacts: 20000, Relationships: 50000, Files: 50000, Locations: 100000}
}

type Identity struct {
	Kind      string `json:"kind"`
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

// Binding is trusted caller evidence associating one exact report digest with
// a source snapshot. A Syft directory source ID alone is not such evidence.
type Binding struct {
	ReportSHA256   string
	SourceIdentity Identity
}
type Project struct{ ID, Root string }

// Mapping maps an explicit Syft filesystem coordinate root into the inventory.
// For ordinary native directory reports ReportRoot is "/". No mapping is
// inferred from source.metadata.path or from the machine running the importer.
type Mapping struct{ ReportRoot, InventoryRoot string }
type Context struct {
	// ProjectStatus is complete only when project discovery covered the selected inventory.
	ProjectStatus string

	Projects       []Project
	SourceIdentity Identity
	Mapping        *Mapping
	Binding        *Binding
}
type Options struct {
	Limits  Limits
	Context *Context
}

type Diagnostic struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}
type Configuration struct {
	SHA256              string          `json:"sha256,omitempty"`
	Catalogers          []string        `json:"catalogers"`
	RequestedCatalogers []string        `json:"requested_catalogers"`
	Scope               string          `json:"scope,omitempty"`
	Settings            map[string]bool `json:"settings"`
	Detail              string          `json:"detail"`
}
type Provider struct {
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	SchemaVersion string        `json:"schema_version"`
	Configuration Configuration `json:"configuration"`
}
type Source struct {
	ReportedID     string    `json:"reported_id"`
	Type           string    `json:"type"`
	IDSHA256       string    `json:"id_sha256"`
	MetadataSHA256 string    `json:"metadata_sha256"`
	Identity       *Identity `json:"identity,omitempty"`
	Match          string    `json:"match"`
	MatchBasis     string    `json:"match_basis"`
}
type Location struct {
	Path           string   `json:"path,omitempty"`
	AccessPath     string   `json:"access_path,omitempty"`
	OriginalSHA256 string   `json:"original_sha256"`
	State          string   `json:"state"`
	Evidence       string   `json:"evidence"`
	ProjectIDs     []string `json:"project_ids"`
	Association    string   `json:"association"`
	rawPath        string
	rawAccess      string
}
type Metadata struct {
	Type           string   `json:"type,omitempty"`
	SHA256         string   `json:"sha256,omitempty"`
	DependencyType string   `json:"dependency_type,omitempty"`
	Digests        []Digest `json:"digests"`
}
type Digest struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}
type Package struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Type      string     `json:"type"`
	Language  string     `json:"language,omitempty"`
	PURL      string     `json:"purl,omitempty"`
	FoundBy   string     `json:"found_by"`
	Basis     string     `json:"basis"`
	Locations []Location `json:"locations"`
	Metadata  Metadata   `json:"metadata"`
}
type File struct {
	ID       string   `json:"id"`
	Location Location `json:"location"`
}
type Relationship struct {
	Parent         string `json:"parent"`
	Child          string `json:"child"`
	Type           string `json:"type"`
	State          string `json:"state"`
	ParentKind     string `json:"parent_kind"`
	ChildKind      string `json:"child_kind"`
	MetadataSHA256 string `json:"metadata_sha256,omitempty"`
}
type Coverage struct {
	Import       string `json:"import"`
	ProviderScan string `json:"provider_scan"`
	Snapshot     string `json:"snapshot"`
	Metadata     string `json:"metadata"`
}
type Report struct {
	Status        string         `json:"status"`
	ReportSHA256  string         `json:"report_sha256"`
	ReportBytes   int64          `json:"report_bytes"`
	Provider      Provider       `json:"provider"`
	Source        Source         `json:"source"`
	Coverage      Coverage       `json:"coverage"`
	Limits        Limits         `json:"limits"`
	Packages      []Package      `json:"packages"`
	Files         []File         `json:"files"`
	Relationships []Relationship `json:"relationships"`
	Diagnostics   []Diagnostic   `json:"diagnostics"`
}
