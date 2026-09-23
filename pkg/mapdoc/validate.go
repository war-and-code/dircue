package mapdoc

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var drivePath = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

func Validate(d Document) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if d.SchemaVersion != SchemaVersion {
		return fail("schema_version must be %q", SchemaVersion)
	}
	if d.Kind != "map" {
		return fail("kind must be map")
	}
	if !validCoverage(d.Status) {
		return fail("invalid document status %q", d.Status)
	}
	if d.Source.Mode != "git" && d.Source.Mode != "directory" {
		return fail("source mode must be git or directory")
	}
	if d.Source.Tree != "" && looksAbsolute(d.Source.Tree) {
		return fail("source tree must not be absolute")
	}
	if d.Source.Mode == "git" && (d.Source.Revision == "" || d.Source.Tree == "") {
		return fail("git source requires revision and tree")
	}
	if d.Source.Mode == "git" && d.Source.Digest != "" {
		return fail("git source must not contain a directory digest")
	}
	if d.Source.Mode == "directory" && d.Source.Digest == "" {
		return fail("directory source requires digest")
	}
	if d.Source.Mode == "directory" && (d.Source.Revision != "" || d.Source.Tree != "") {
		return fail("directory source must not contain git identity")
	}
	questions := map[string]bool{}
	for _, q := range d.Coverage {
		if q.Question == "" {
			return fail("coverage question is required")
		}
		if q.Scope == "" {
			return fail("coverage question %q requires scope", q.Question)
		}
		if looksAbsolute(q.Scope) {
			return fail("coverage question %q has absolute scope", q.Question)
		}
		if questions[q.Question+"\x00"+q.Scope] {
			return fail("duplicate coverage question %q at scope %q", q.Question, q.Scope)
		}
		questions[q.Question+"\x00"+q.Scope] = true
		if err := validateCoverage(q.Coverage); err != nil {
			return err
		}
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		if !slices.Contains([]NodeKind{NodeContent, NodeComponent, NodeDeployable, NodeInterface, NodeCapability, NodePackage, NodeToolRun}, n.Kind) {
			return fail("node %q has invalid kind %q", n.ID, n.Kind)
		}
		if len(n.Paths) == 0 {
			return fail("node %q has no identity path", n.ID)
		}
		for _, p := range n.Paths {
			if err := validatePath(p); err != nil {
				return err
			}
		}
		if n.ID != NodeID(n.Kind, n.Paths, n.Discriminator) {
			return fail("node %q has noncanonical id", n.ID)
		}
		if ids[n.ID] {
			return fail("duplicate id %q", n.ID)
		}
		ids[n.ID] = true
		if err := validateCoverage(n.Coverage); err != nil {
			return err
		}
		documentation := n.Kind == NodeContent && n.Properties["role"] == "documentation"
		if err := validateEvidence(n.Evidence, documentation); err != nil {
			return fail("node %q: %v", n.ID, err)
		}
		if err := validateProperties(n.Properties); err != nil {
			return fail("node %q: %v", n.ID, err)
		}
		for _, f := range n.Facts {
			if f.Kind == "" {
				return fail("node %q has fact without kind", n.ID)
			}
			if err := validateCoverage(f.Coverage); err != nil {
				return err
			}
			if err := validateEvidence(f.Evidence, false); err != nil {
				return fail("node %q fact %q: %v", n.ID, f.Kind, err)
			}
			if err := validateProperties(f.Properties); err != nil {
				return err
			}
		}
	}
	for _, e := range d.Edges {
		if !slices.Contains([]EdgeType{EdgeContains, EdgeMemberOf, EdgeDependsOnLocal, EdgeBuilds, EdgeRuns, EdgeExposes, EdgeDeclares, EdgeUsesCapability, EdgePackagedIn, EdgeAnalyzedBy, EdgeConflictsWith}, e.Type) {
			return fail("edge %q has invalid type %q", e.ID, e.Type)
		}
		if !ids[e.From] || !ids[e.To] {
			return fail("edge %q references unknown node", e.ID)
		}
		if e.ID != EdgeID(e.Type, e.From, e.To, e.Discriminator) {
			return fail("edge %q has noncanonical id", e.ID)
		}
		if ids[e.ID] {
			return fail("duplicate id %q", e.ID)
		}
		ids[e.ID] = true
		if err := validateCoverage(e.Coverage); err != nil {
			return err
		}
		if err := validateEvidence(e.Evidence, false); err != nil {
			return fail("edge %q: %v", e.ID, err)
		}
		if err := validateProperties(e.Properties); err != nil {
			return err
		}
	}
	return nil
}

func validateCoverage(c Coverage) error {
	if !validCoverage(c.Status) {
		return fmt.Errorf("%w: invalid coverage %q", ErrInvalid, c.Status)
	}
	if c.Status != CoverageComplete && len(c.Reasons) == 0 {
		return fmt.Errorf("%w: %s coverage requires a reason", ErrInvalid, c.Status)
	}
	return nil
}
func validCoverage(v CoverageStatus) bool {
	return v == CoverageComplete || v == CoveragePartial || v == CoverageUnknown
}
func validatePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if looksAbsolute(p) {
		return fmt.Errorf("%w: absolute path %q", ErrInvalid, p)
	}
	if strings.Contains(p, "\\") || p != normalizePath(p) || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%w: path is not clean root-relative: %q", ErrInvalid, p)
	}
	return nil
}
func looksAbsolute(v string) bool {
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, "\\") || drivePath.MatchString(v) || filepath.IsAbs(v)
}
func validateProperties(p map[string]string) error {
	for k, v := range p {
		if k == "" {
			return fmt.Errorf("%w: empty property name", ErrInvalid)
		}
		if looksAbsolute(v) {
			return fmt.Errorf("%w: property %q contains an absolute path", ErrInvalid, k)
		}
	}
	return nil
}
func validateEvidence(values []Evidence, documentationFact bool) error {
	if len(values) == 0 {
		return fmt.Errorf("evidence is required")
	}
	for _, e := range values {
		if !slices.Contains([]EvidenceBasis{BasisDeclaredConfig, BasisCodeSyntax, BasisResolvedReference, BasisProviderReported, BasisRuleInferred, BasisFilenameHint}, e.Basis) {
			return fmt.Errorf("invalid basis %q", e.Basis)
		}
		if err := validatePath(e.Path); err != nil {
			return err
		}
		if !slices.Contains([]EvidenceSource{SourceFile, SourceConfiguration, SourceCode, SourceComment, SourceDocstring, SourceDocumentation}, e.SourceKind) {
			return fmt.Errorf("invalid evidence source %q", e.SourceKind)
		}
		if !documentationFact && (e.SourceKind == SourceComment || e.SourceKind == SourceDocstring || e.SourceKind == SourceDocumentation || isDocumentationPath(e.Path)) {
			return fmt.Errorf("documentation cannot evidence a non-documentation fact")
		}
		if (e.Rule == nil) == (e.Provider == nil) {
			return fmt.Errorf("evidence requires exactly one rule or provider")
		}
		p := e.Rule
		if p == nil {
			p = e.Provider
		}
		if p.ID == "" || p.Version == "" {
			return fmt.Errorf("evidence producer id and version are required")
		}
		if e.Basis == BasisProviderReported && e.Provider == nil {
			return fmt.Errorf("provider_reported evidence requires provider")
		}
		if e.Span != nil {
			if e.Span.StartLine < 1 || e.Span.EndLine < e.Span.StartLine || e.Span.StartColumn < 0 || e.Span.EndColumn < 0 {
				return fmt.Errorf("invalid evidence span")
			}
		}
	}
	return nil
}
func isDocumentationPath(p string) bool {
	base := strings.ToLower(pathBase(p))
	ext := strings.ToLower(filepath.Ext(base))
	return strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "changelog") || strings.HasPrefix(base, "contributing") || slices.Contains([]string{".md", ".mdx", ".markdown", ".rst", ".adoc", ".asciidoc"}, ext)
}
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
