package packageevidence

import (
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validate checks coordinate syntax without reading a report or inventory.
func (m Mapping) Validate() error {
	if !relative(m.InventoryRoot) || !validReportRoot(m.ReportRoot) {
		return ErrContext
	}
	return nil
}

// Associate returns a copy with evidence mapped into the caller's inventory.
// A path match is only a candidate until the exact imported report is bound to
// the selected snapshot. It does not establish compiler input membership.
// The input must be the report returned by Import; serialized JSON deliberately
// omits raw, potentially sensitive provider paths and cannot be remapped.
func Associate(input *Report, ctx Context) (*Report, error) {
	if input == nil || len(ctx.Projects) > 100000 {
		return nil, ErrContext
	}
	switch ctx.ProjectStatus {
	case "", "unknown", "complete", "partial", "skipped":
	default:
		return nil, ErrContext
	}
	if !validIdentity(ctx.SourceIdentity) {
		return nil, ErrContext
	}
	owners := map[string][]string{}
	seen := map[string]bool{}
	for _, p := range ctx.Projects {
		if !relative(p.ID) || !relative(p.Root) || seen[p.ID] {
			return nil, ErrContext
		}
		seen[p.ID] = true
		root := path.Clean(p.Root)
		owners[root] = append(owners[root], p.ID)
	}
	for root := range owners {
		slices.Sort(owners[root])
	}
	if ctx.Mapping != nil {
		if ctx.Mapping.Validate() != nil {
			return nil, ErrContext
		}
	}
	out := *input
	out.Diagnostics = []Diagnostic{}
	out.Status = "complete"
	for _, d := range input.Diagnostics {
		if d.Code != "source-identity-mismatch" && d.Code != "partial-project-inventory" && d.Code != "unknown-project-inventory" {
			out.Diagnostics = append(out.Diagnostics, d)
			out.Status = "partial"
		}
	}
	out.Coverage.Import = input.Coverage.Import
	if input.Coverage.Import != "complete" {
		out.Status = "partial"
	}
	out.Packages = append([]Package{}, input.Packages...)
	out.Files = append([]File{}, input.Files...)
	out.Source.Identity = nil
	out.Source.Match = "unknown"
	out.Source.MatchBasis = "No digest-bound source snapshot identity was supplied"
	if ctx.Binding != nil {
		if len(ctx.Binding.ReportSHA256) != 64 || !hexDigest.MatchString(ctx.Binding.ReportSHA256) || !validIdentity(ctx.Binding.SourceIdentity) || ctx.Binding.SourceIdentity.Digest == "" {
			return nil, ErrContext
		}
		identity := ctx.Binding.SourceIdentity
		out.Source.Identity = &identity
		if !strings.EqualFold(ctx.Binding.ReportSHA256, out.ReportSHA256) {
			out.Source.Match = "mismatched"
			out.Source.MatchBasis = "Caller binding identifies a different report digest"
		} else if ctx.SourceIdentity.Digest != "" {
			if identity.Kind == ctx.SourceIdentity.Kind && identity.Algorithm == ctx.SourceIdentity.Algorithm && strings.EqualFold(identity.Digest, ctx.SourceIdentity.Digest) {
				out.Source.Match = "matched"
				out.Source.MatchBasis = "Trusted caller binding matches this report digest and selected source identity"
			} else {
				out.Source.Match = "mismatched"
				out.Source.MatchBasis = "Caller-bound report source differs from the selected source identity"
			}
		}
	}
	out.Coverage.Snapshot = out.Source.Match
	if out.Source.Match == "mismatched" {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: "source-identity-mismatch", Count: 1})
		out.Status = "partial"
	}
	if ctx.ProjectStatus != "complete" {
		code := "partial-project-inventory"
		if ctx.ProjectStatus == "" || ctx.ProjectStatus == "unknown" {
			code = "unknown-project-inventory"
		}
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: code, Count: 1})
		out.Status = "partial"
	}
	cache := map[string][]string{}
	associate := func(original Location) Location {
		loc := original
		loc.Path = ""
		loc.AccessPath = ""
		loc.ProjectIDs = []string{}
		loc.State = "unmapped"
		loc.Association = "unmapped"
		if out.Source.Type != "directory" {
			loc.State = "external"
			loc.Association = "external"
			return loc
		}
		if ctx.Mapping == nil {
			return loc
		}
		if loc.rawPath == "" {
			return loc
		}
		mapped, ok := mapPath(loc.rawPath, *ctx.Mapping)
		if !ok {
			loc.State = "external"
			loc.Association = "external"
			return loc
		}
		loc.Path = mapped
		loc.State = "mapped"
		access := loc.rawAccess
		if access == "" {
			access = loc.rawPath
		}
		if colon := strings.Index(access, ":"); colon >= 0 {
			archive := access[:colon]
			inside := access[colon+1:]
			archivePath, valid := mapPath(archive, *ctx.Mapping)
			if valid && relative(inside) && !strings.Contains(inside, ":") {
				loc.AccessPath = archivePath + ":" + path.Clean(inside)
			}
			loc.State = "nested-archive"
			loc.Association = "nested-archive"
			return loc
		}
		if accessPath, valid := mapPath(access, *ctx.Mapping); valid {
			loc.AccessPath = accessPath
			if accessPath != mapped {
				loc.State = "ambiguous-access-path"
				loc.Association = "ambiguous"
				return loc
			}
		} else {
			loc.State = "external-access-path"
			loc.Association = "external"
			return loc
		}
		if out.Source.Match == "mismatched" {
			loc.Association = "source-mismatch"
			return loc
		}
		if ctx.ProjectStatus != "complete" {
			loc.Association = "project-inventory-incomplete"
			return loc
		}
		dir := path.Dir(mapped)
		ids, found := cache[dir]
		if !found {
			for ancestor := dir; ; ancestor = path.Dir(ancestor) {
				if len(owners[ancestor]) > 0 {
					ids = owners[ancestor]
					break
				}
				if ancestor == "." {
					break
				}
			}
			cache[dir] = ids
		}
		loc.ProjectIDs = append([]string{}, ids...)
		switch len(ids) {
		case 0:
			loc.Association = "unassigned"
		case 1:
			loc.Association = "matched"
		default:
			loc.Association = "ambiguous"
		}
		if out.Source.Match != "matched" && len(ids) > 0 {
			if len(ids) == 1 {
				loc.Association = "source-unverified"
			} else {
				loc.Association = "ambiguous-source-unverified"
			}
		}
		return loc
	}
	for i := range out.Packages {
		out.Packages[i].Locations = make([]Location, len(input.Packages[i].Locations))
		for j, location := range input.Packages[i].Locations {
			out.Packages[i].Locations[j] = associate(location)
		}
	}
	for i := range out.Files {
		out.Files[i].Location = associate(input.Files[i].Location)
	}
	slices.SortFunc(out.Diagnostics, func(a, b Diagnostic) int { return strings.Compare(a.Code, b.Code) })
	return &out, nil
}
func validIdentity(id Identity) bool {
	if id.Kind == "" && id.Algorithm == "" && id.Digest == "" {
		return true
	}
	if !identifier.MatchString(id.Kind) || !identifier.MatchString(id.Algorithm) || !hexDigest.MatchString(id.Digest) {
		return false
	}
	switch id.Algorithm {
	case "sha256", "sha-256":
		return len(id.Digest) == 64
	case "sha1", "sha-1", "git-sha1":
		return len(id.Digest) == 40
	default:
		return false
	}
}
func relative(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	if value == "" || len(value) > 4096 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:\x00\r\n?#") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return false
		}
	}
	clean := path.Clean(value)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}
func validReportRoot(value string) bool {
	if value == "/" {
		return true
	}
	return relative(strings.TrimPrefix(value, "/"))
}
func mapPath(value string, mapping Mapping) (string, bool) {
	if !utf8.ValidString(value) || safeText(strings.TrimPrefix(value, "/")) == "" {
		return "", false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return "", false
		}
	}
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\\:\x00\r\n?#") {
		return "", false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", false
		}
	}
	root := path.Clean(mapping.ReportRoot)
	value = path.Clean(value)
	var rest string
	switch {
	case root == "/":
		if !strings.HasPrefix(value, "/") {
			return "", false
		}
		rest = strings.TrimPrefix(value, "/")
	case root == ".":
		if strings.HasPrefix(value, "/") {
			return "", false
		}
		rest = value
	case value == root:
		rest = "."
	case strings.HasPrefix(value, root+"/"):
		rest = strings.TrimPrefix(value, root+"/")
	default:
		return "", false
	}
	if rest == "" {
		rest = "."
	}
	if !relative(rest) {
		return "", false
	}
	result := path.Join(mapping.InventoryRoot, rest)
	return result, relative(result)
}
