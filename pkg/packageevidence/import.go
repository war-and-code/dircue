package packageevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type nativeDocument struct {
	Artifacts     []json.RawMessage `json:"artifacts"`
	Relationships []json.RawMessage `json:"artifactRelationships"`
	Files         []json.RawMessage `json:"files"`
	Source        json.RawMessage   `json:"source"`
	Descriptor    json.RawMessage   `json:"descriptor"`
	Schema        struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	} `json:"schema"`
}
type nativeLocation struct {
	Path        string            `json:"path"`
	AccessPath  string            `json:"accessPath"`
	Annotations map[string]string `json:"annotations"`
}
type nativePackage struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	Type         string           `json:"type"`
	Language     string           `json:"language"`
	PURL         string           `json:"purl"`
	FoundBy      string           `json:"foundBy"`
	Locations    []nativeLocation `json:"locations"`
	MetadataType string           `json:"metadataType"`
	Metadata     json.RawMessage  `json:"metadata"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,255}$`)
var hexDigest = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// Import accepts only native schema 16.1.10. Complete means the supported import
// finished, never that Syft found every package or evaluated the build graph.
// Parse, limit, and cancellation failures return no report.
func Import(ctx context.Context, reader io.Reader, opts Options) (*Report, error) {
	limits, err := effectiveLimits(opts.Limits)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, reader}, limits.Bytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limits.Bytes {
		return nil, ErrLimit
	}
	if !utf8.Valid(data) {
		return nil, ErrInvalid
	}
	if err := validateJSON(ctx, data, limits); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, ErrInvalid
	}
	for _, key := range []string{"artifacts", "artifactRelationships", "source", "descriptor", "schema", "distro"} {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("%w: missing %s", ErrInvalid, key)
		}
	}
	if !isArray(fields["artifacts"]) || !isArray(fields["artifactRelationships"]) {
		return nil, ErrInvalid
	}
	var native nativeDocument
	if !canonicalFields(fields, reflect.TypeOf(native)) || json.Unmarshal(data, &native) != nil {
		return nil, ErrInvalid
	}
	var schemaFields map[string]json.RawMessage
	if json.Unmarshal(fields["schema"], &schemaFields) != nil || !canonicalFields(schemaFields, reflect.TypeOf(native.Schema)) {
		return nil, ErrInvalid
	}
	if native.Schema.Version != SupportedSchema {
		return nil, ErrUnsupported
	}
	if native.Schema.URL == "" {
		return nil, ErrInvalid
	}
	if rawFiles, present := fields["files"]; present && !isArray(rawFiles) {
		return nil, ErrInvalid
	}
	if len(native.Artifacts) > limits.Artifacts || len(native.Relationships) > limits.Relationships || len(native.Files) > limits.Files {
		return nil, ErrLimit
	}
	var descriptor struct {
		Name          string          `json:"name"`
		Version       string          `json:"version"`
		Configuration json.RawMessage `json:"configuration"`
	}
	var source struct {
		Name     string          `json:"name"`
		Version  string          `json:"version"`
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(native.Descriptor, &descriptor) != nil || descriptor.Name != "syft" || !identifier.MatchString(descriptor.Version) {
		return nil, ErrInvalid
	}
	if json.Unmarshal(native.Source, &source) != nil || !identifier.MatchString(source.ID) || !identifier.MatchString(source.Type) || source.Metadata == nil {
		return nil, ErrInvalid
	}
	var sourceFields map[string]json.RawMessage
	_ = json.Unmarshal(native.Source, &sourceFields)
	var descriptorFields map[string]json.RawMessage
	_ = json.Unmarshal(native.Descriptor, &descriptorFields)
	if !canonicalFields(sourceFields, reflect.TypeOf(source)) || !canonicalFields(descriptorFields, reflect.TypeOf(descriptor)) {
		return nil, ErrInvalid
	}
	for _, key := range []string{"id", "name", "version", "type", "metadata"} {
		if _, present := sourceFields[key]; !present || (key != "metadata" && !isString(sourceFields[key])) {
			return nil, ErrInvalid
		}
	}
	report := &Report{Status: "complete", ReportSHA256: digest(data), ReportBytes: int64(len(data)), Limits: limits,
		Provider: Provider{Name: "syft", Version: descriptor.Version, SchemaVersion: SupportedSchema, Configuration: configuration(descriptor.Configuration)},
		Source:   Source{ReportedID: source.ID, Type: source.Type, IDSHA256: digest([]byte(source.ID)), MetadataSHA256: digest(source.Metadata), Match: "unknown", MatchBasis: "Syft source ID is not a verified inventory snapshot"},
		Coverage: Coverage{Import: "complete", ProviderScan: "unknown", Snapshot: "unknown", Metadata: "supported-subset"},
		Packages: []Package{}, Files: []File{}, Relationships: []Relationship{}, Diagnostics: []Diagnostic{}}
	for key := range fields {
		switch key {
		case "artifacts", "artifactRelationships", "files", "source", "descriptor", "schema", "distro":
		default:
			report.warn("unsupported-top-level-field")
		}
	}
	nodes := map[string]string{source.ID: "source"}
	locationCount := 0
	for _, raw := range native.Artifacts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var p nativePackage
		if json.Unmarshal(raw, &p) != nil || !identifier.MatchString(p.ID) || !identifier.MatchString(p.Type) || !identifier.MatchString(p.FoundBy) {
			return nil, ErrInvalid
		}
		var shape map[string]json.RawMessage
		_ = json.Unmarshal(raw, &shape)
		if !canonicalFields(shape, reflect.TypeOf(p)) {
			return nil, ErrInvalid
		}
		for _, key := range []string{"id", "name", "version", "type", "foundBy", "locations", "licenses", "language", "cpes", "purl"} {
			if _, ok := shape[key]; !ok {
				return nil, ErrInvalid
			}
		}
		for _, key := range []string{"id", "name", "version", "type", "foundBy", "language", "purl"} {
			if !isString(shape[key]) {
				return nil, ErrInvalid
			}
		}
		if !isArray(shape["locations"]) || !isArray(shape["licenses"]) || !isArray(shape["cpes"]) {
			return nil, ErrInvalid
		}
		if _, exists := nodes[p.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate artifact ID", ErrInvalid)
		}
		nodes[p.ID] = "package"
		locationCount += len(p.Locations)
		if locationCount > limits.Locations {
			return nil, ErrLimit
		}
		item := Package{ID: p.ID, Name: safeText(p.Name), Version: safeText(p.Version), Type: p.Type, Language: safeText(p.Language), PURL: safePURL(p.PURL), FoundBy: p.FoundBy, Basis: basis(p.MetadataType, p.FoundBy), Locations: []Location{}, Metadata: metadata(p.MetadataType, p.Metadata)}
		if item.Name == "" && p.Name != "" {
			report.warn("redacted-package-name")
		}
		if item.Version == "" && p.Version != "" {
			report.warn("redacted-package-version")
		}
		if item.PURL == "" && p.PURL != "" {
			report.warn("redacted-package-purl")
		}
		if item.Name == "" {
			report.warn("package-name-unavailable")
		}
		if len(p.Locations) == 0 {
			report.warn("package-evidence-unavailable")
		}
		var locationShapes []map[string]json.RawMessage
		_ = json.Unmarshal(shape["locations"], &locationShapes)
		for _, entry := range locationShapes {
			if !canonicalFields(entry, reflect.TypeOf(nativeLocation{})) {
				return nil, ErrInvalid
			}
			if _, ok := entry["path"]; !ok || !isString(entry["path"]) {
				return nil, ErrInvalid
			}
			if _, ok := entry["accessPath"]; !ok || !isString(entry["accessPath"]) {
				return nil, ErrInvalid
			}
		}
		for _, location := range p.Locations {
			if location.Path == "" {
				report.warn("package-evidence-unavailable")
			}
			item.Locations = append(item.Locations, newLocation(location))
		}
		slices.SortFunc(item.Locations, func(a, b Location) int { return strings.Compare(a.OriginalSHA256, b.OriginalSHA256) })
		report.Packages = append(report.Packages, item)
	}
	for _, raw := range native.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var file struct {
			ID       string         `json:"id"`
			Location nativeLocation `json:"location"`
		}
		if json.Unmarshal(raw, &file) != nil || !identifier.MatchString(file.ID) || file.Location.Path == "" {
			return nil, ErrInvalid
		}
		var fileFields, locationFields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fileFields)
		_ = json.Unmarshal(fileFields["location"], &locationFields)
		if !canonicalFields(fileFields, reflect.TypeOf(file)) || !canonicalFields(locationFields, reflect.TypeOf(nativeLocation{})) || !isString(locationFields["path"]) {
			return nil, ErrInvalid
		}
		if _, exists := nodes[file.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate node ID", ErrInvalid)
		}
		nodes[file.ID] = "file"
		locationCount++
		if locationCount > limits.Locations {
			return nil, ErrLimit
		}
		report.Files = append(report.Files, File{ID: file.ID, Location: newLocation(file.Location)})
	}
	relationships := map[string]bool{}
	for _, raw := range native.Relationships {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var row struct {
			Parent   string          `json:"parent"`
			Child    string          `json:"child"`
			Type     string          `json:"type"`
			Metadata json.RawMessage `json:"metadata"`
		}
		if json.Unmarshal(raw, &row) != nil || !identifier.MatchString(row.Parent) || !identifier.MatchString(row.Child) || !identifier.MatchString(row.Type) {
			return nil, ErrInvalid
		}
		var rowFields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &rowFields)
		if !canonicalFields(rowFields, reflect.TypeOf(row)) {
			return nil, ErrInvalid
		}
		key := row.Parent + "\x00" + row.Child + "\x00" + row.Type
		if relationships[key] {
			return nil, fmt.Errorf("%w: duplicate relationship", ErrInvalid)
		}
		relationships[key] = true
		item := Relationship{Parent: row.Parent, Child: row.Child, Type: row.Type, State: "reported", ParentKind: nodes[row.Parent], ChildKind: nodes[row.Child]}
		if item.ParentKind == "" {
			item.ParentKind = "unknown"
		}
		if item.ChildKind == "" {
			item.ChildKind = "unknown"
		}
		if item.ParentKind == "unknown" || item.ChildKind == "unknown" {
			item.State = "unresolved-endpoint"
			report.warn("relationship-endpoint-unavailable")
		}
		switch row.Type {
		case "contains", "dependency-of", "described-by", "evident-by", "ownership-by-file-overlap":
		default:
			item.State = "unsupported-type"
			report.warn("relationship-type-unsupported")
		}
		if row.Metadata != nil {
			item.MetadataSHA256 = digest(row.Metadata)
		}
		report.Relationships = append(report.Relationships, item)
	}
	slices.SortFunc(report.Packages, func(a, b Package) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(report.Files, func(a, b File) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(report.Relationships, func(a, b Relationship) int {
		if a.Parent != b.Parent {
			return strings.Compare(a.Parent, b.Parent)
		}
		if a.Child != b.Child {
			return strings.Compare(a.Child, b.Child)
		}
		return strings.Compare(a.Type, b.Type)
	})
	slices.SortFunc(report.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Code, b.Code) })
	if opts.Context != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return Associate(report, *opts.Context)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}

// encoding/json folds struct field names, while native Syft field names are
// case-sensitive. Reject aliases before they can replace canonical evidence.
// Arbitrary metadata objects retain their own case-sensitive key namespace.
func canonicalFields(fields map[string]json.RawMessage, shape reflect.Type) bool {
	for i := 0; i < shape.NumField(); i++ {
		name := strings.Split(shape.Field(i).Tag.Get("json"), ",")[0]
		for key := range fields {
			if key != name && strings.EqualFold(key, name) {
				return false
			}
		}
	}
	return true
}
func effectiveLimits(in Limits) (Limits, error) {
	out := DefaultLimits()
	if in.Bytes != 0 {
		out.Bytes = in.Bytes
	}
	if in.Artifacts != 0 {
		out.Artifacts = in.Artifacts
	}
	if in.Relationships != 0 {
		out.Relationships = in.Relationships
	}
	if in.Files != 0 {
		out.Files = in.Files
	}
	if in.Locations != 0 {
		out.Locations = in.Locations
	}
	if out.Bytes < 1 || out.Bytes > 128<<20 || out.Artifacts < 1 || out.Artifacts > 100000 || out.Relationships < 1 || out.Relationships > 250000 || out.Files < 1 || out.Files > 100000 || out.Locations < 1 || out.Locations > 250000 {
		return Limits{}, ErrLimit
	}
	return out, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func digest(data []byte) string { d := sha256.Sum256(data); return hex.EncodeToString(d[:]) }
func isString(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '"'
}
func isArray(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '['
}
func (r *Report) warn(code string) {
	r.Status = "partial"
	r.Coverage.Import = "partial"
	for i := range r.Diagnostics {
		if r.Diagnostics[i].Code == code {
			r.Diagnostics[i].Count++
			return
		}
	}
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Count: 1})
}
func safeText(value string) string {
	if len(value) > 4096 {
		return ""
	}
	decoded := value
	for pass := 0; pass < 5; pass++ {
		if strings.Contains(decoded, "://") || strings.ContainsAny(decoded, "\\?\x00") || strings.HasPrefix(decoded, "/") || (strings.Contains(decoded, ":") && strings.Contains(decoded, "@")) {
			return ""
		}
		for _, c := range decoded {
			if unicode.IsControl(c) {
				return ""
			}
		}
		if !strings.Contains(decoded, "%") {
			return value
		}
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			return ""
		}
		decoded = next
	}
	return ""
}
func safePURL(value string) string {
	if !strings.HasPrefix(value, "pkg:") || strings.ContainsAny(value, "?#") {
		return ""
	}
	body := strings.TrimPrefix(value, "pkg:")
	// The scheme colon is not user information; inspect the body separately so
	// ordinary scoped package names and @version separators remain usable.
	if safeText(body) == "" || !strings.Contains(body, "/") {
		return ""
	}
	decoded := body
	for pass := 0; pass < 5; pass++ {
		if strings.Contains(decoded, "//") || strings.ContainsAny(decoded, "?#") {
			return ""
		}
		if !strings.Contains(decoded, "%") {
			break
		}
		decoded, _ = url.PathUnescape(decoded)
	}
	return value
}
func newLocation(l nativeLocation) Location {
	evidence := "unknown"
	if l.Annotations["evidence"] == "primary" || l.Annotations["evidence"] == "supporting" {
		evidence = l.Annotations["evidence"]
	}
	identity, _ := json.Marshal([2]string{l.Path, l.AccessPath})
	return Location{OriginalSHA256: digest(identity), State: "unmapped", Evidence: evidence, ProjectIDs: []string{}, Association: "unmapped", rawPath: l.Path, rawAccess: l.AccessPath}
}
func basis(kind, cataloger string) string {
	switch kind {
	case "dotnet-packages-lock-entry", "javascript-npm-package-lock-entry", "javascript-pnpm-lock-entry", "javascript-yarn-lock-entry", "java-gradle-lockfile-entry", "rust-cargo-lock-entry", "python-poetry-lock-entry", "python-uv-lock-entry":
		return "lockfile-resolution"
	case "dotnet-deps-entry":
		return "built-artifact-manifest"
	case "java-archive", "dotnet-portable-executable-entry", "go-module-buildinfo-entry":
		return "built-artifact"
	case "dpkg-db-entry", "apk-db-entry", "rpm-db-entry", "alpm-db-entry":
		return "installed-package"
	case "javascript-npm-package", "python-pip-requirements-entry", "go-module-entry", "java-pom-project":
		return "manifest-declaration"
	case "python-package":
		if cataloger == "python-installed-package-cataloger" {
			return "installed-package"
		}
	}
	return "unknown"
}
func metadata(kind string, raw json.RawMessage) Metadata {
	out := Metadata{Digests: []Digest{}}
	if identifier.MatchString(kind) {
		out.Type = kind
	}
	if raw == nil {
		return out
	}
	out.SHA256 = digest(raw)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return out
	}
	if kind == "dotnet-packages-lock-entry" || kind == "dotnet-deps-entry" {
		var value string
		_ = json.Unmarshal(fields["type"], &value)
		switch value {
		case "Direct", "Transitive", "Project", "package", "project":
			out.DependencyType = value
		}
	}
	if kind == "java-archive" {
		var values []Digest
		if json.Unmarshal(fields["digest"], &values) == nil {
			for _, d := range values {
				switch d.Algorithm {
				case "sha1", "sha256", "sha512":
					if len(d.Value) == map[string]int{"sha1": 40, "sha256": 64, "sha512": 128}[d.Algorithm] && hexDigest.MatchString(d.Value) {
						out.Digests = append(out.Digests, d)
					}
				}
			}
		}
	}
	return out
}
func configuration(raw json.RawMessage) Configuration {
	result := Configuration{Catalogers: []string{}, RequestedCatalogers: []string{}, Settings: map[string]bool{}, Detail: "allowlisted-subset; other configuration retained by digest only"}
	if raw == nil {
		return result
	}
	result.SHA256 = digest(raw)
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return result
	}
	get := func(keys ...string) any {
		var value any = fields
		for _, key := range keys {
			m, ok := value.(map[string]any)
			if !ok {
				return nil
			}
			value = m[key]
		}
		return value
	}
	list := func(value any) []string {
		out := []string{}
		if values, ok := value.([]any); ok {
			for _, v := range values {
				if s, ok := v.(string); ok && identifier.MatchString(s) && len(out) < 256 {
					out = append(out, s)
				}
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	result.Catalogers = list(get("catalogers", "used"))
	result.RequestedCatalogers = list(get("catalogers", "requested", "default"))
	if value, ok := get("search", "scope").(string); ok {
		switch value {
		case "squashed", "all-layers", "deep-squashed":
			result.Scope = value
		}
	}
	for _, keys := range [][]string{{"packages", "java-archive", "use-network"}, {"packages", "java-archive", "resolve-transitive-dependencies"}, {"packages", "java-archive", "include-indexed-archives"}, {"packages", "java-archive", "include-unindexed-archives"}, {"packages", "golang", "search-remote-licenses"}, {"packages", "javascript", "search-remote-licenses"}, {"packages", "python", "search-remote-licenses"}, {"relationships", "package-file-ownership"}} {
		if value, ok := get(keys...).(bool); ok {
			result.Settings[strings.Join(keys, ".")] = value
		}
	}
	return result
}
