package mapdoc

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var drivePath = regexp.MustCompile(`^[A-Za-z]:[/\\]`)
var fullObjectID = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

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
	if d.Source.Commit != "" && !fullObjectID.MatchString(d.Source.Commit) {
		return fail("source commit must be a resolved 40- or 64-digit object ID")
	}
	if d.Source.Mode == "git" && d.Source.Digest != nil {
		return fail("git source must not contain a directory digest")
	}
	if d.Source.Mode == "directory" && (d.Source.Revision != "" || d.Source.Commit != "" || d.Source.Tree != "") {
		return fail("directory source must not contain git identity")
	}
	if d.Source.Digest != nil && (d.Source.Digest.Algorithm == "" || d.Source.Digest.Scope == "" || d.Source.Digest.Value == "") {
		return fail("source digest requires algorithm, scope, and value")
	}
	questions := map[string]bool{}
	sourceBindingQualified := false
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
		if q.Question == QuestionSourceBinding && q.Scope == "." && (q.Status == CoverageUnknown || q.Status == CoveragePartial) {
			sourceBindingQualified = true
		}
	}
	for _, run := range d.CoverageLedger {
		if run.Tool == "" || run.ReportKind == "" || run.State == "" {
			return fail("coverage ledger requires tool, report kind, and state")
		}
		if err := validatePath(run.Scope); err != nil {
			return fail("coverage ledger scope: %v", err)
		}
		if run.Binding != "verified" && run.Binding != "mismatch" && run.Binding != "unknown" && run.Binding != "caller_asserted" {
			return fail("coverage ledger has invalid binding %q; valid values: verified, mismatch, unknown, caller_asserted", run.Binding)
		}
		for _, filename := range run.CoveredFiles {
			if err := validatePath(filename); err != nil {
				return fail("coverage ledger file: %v", err)
			}
		}
	}
	coverageKeys := map[string]bool{}
	coverageCounts := map[string]int{}
	for _, entry := range d.AnalyzerCoverage {
		if entry.ComponentID == "" || entry.Language == "" || entry.Tool == "" || entry.DescriptorVersion == "" || entry.DescriptorSource == "" {
			return fail("analyzer coverage requires component, language, tool, and descriptor provenance")
		}
		if !slices.Contains([]string{"component_property", "repository_population", "unattributed"}, entry.LanguageBasis) {
			return fail("analyzer coverage has invalid language_basis %q", entry.LanguageBasis)
		}
		if !slices.Contains([]string{"not_run", "unsupported_language", "unsupported_framework", "prerequisite_unmet", "tool_error", "unknown"}, entry.NotCovered) {
			return fail("analyzer coverage has invalid not_covered %q", entry.NotCovered)
		}
		if entry.Reason == "" {
			return fail("analyzer coverage requires reason")
		}
		key := entry.ComponentID + "\x00" + entry.Language + "\x00" + entry.Tool
		if coverageKeys[key] {
			return fail("duplicate analyzer coverage entry %q", key)
		}
		coverageKeys[key] = true
		coverageCounts[entry.NotCovered]++
		for _, filename := range entry.CoveredFiles {
			if err := validatePath(filename); err != nil {
				return fail("analyzer coverage file: %v", err)
			}
		}
	}
	spotReasons := map[string]bool{}
	for _, spot := range d.AnalyzerBlindSpots {
		if !slices.Contains([]string{"not_run", "unsupported_language", "unsupported_framework", "prerequisite_unmet", "tool_error", "unknown"}, spot.Reason) || spot.Entries < 1 {
			return fail("analyzer blind spot requires reason and positive entries")
		}
		if spotReasons[spot.Reason] || coverageCounts[spot.Reason] != spot.Entries {
			return fail("analyzer blind spot %q does not match coverage entries", spot.Reason)
		}
		spotReasons[spot.Reason] = true
	}
	if len(spotReasons) != len(coverageCounts) {
		return fail("analyzer blind spot summary is incomplete")
	}
	if d.Source.Mode == "directory" && d.Source.Digest == nil && !sourceBindingQualified {
		return fail("unbound directory source requires unknown or partial source_binding coverage at scope .")
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
		if err := validateEvidence(n.Evidence, documentation, n.Kind == NodeContent); err != nil {
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
			if err := validateEvidence(f.Evidence, false, false); err != nil {
				return fail("node %q fact %q: %v", n.ID, f.Kind, err)
			}
			if err := validateProperties(f.Properties); err != nil {
				return err
			}
		}
	}
	for _, entry := range d.AnalyzerCoverage {
		if entry.ComponentID != "repository" && !ids[entry.ComponentID] {
			return fail("analyzer coverage references unknown component %q", entry.ComponentID)
		}
		if entry.ComponentID != "repository" {
			for _, node := range d.Nodes {
				if node.ID == entry.ComponentID && node.Kind != NodeComponent {
					return fail("analyzer coverage reference %q is not a component", entry.ComponentID)
				}
			}
		}
	}
	for _, e := range d.Edges {
		if !slices.Contains([]EdgeType{EdgeContains, EdgeMemberOf, EdgeDependsOnLocal, EdgeDependsOn, EdgeBuilds, EdgeRuns, EdgeExposes, EdgeDeclares, EdgeUsesCapability, EdgePackagedIn, EdgeAnalyzedBy}, e.Type) {
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
		if err := validateEvidence(e.Evidence, false, false); err != nil {
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
		if looksAbsolute(v) && !validRouteProperty(k, v) {
			return fmt.Errorf("%w: property %q contains an absolute path", ErrInvalid, k)
		}
	}
	return nil
}

func validRouteProperty(key, value string) bool {
	return key == "route" && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.Contains(value, "\\")
}
func validateEvidence(values []Evidence, documentationFact, directoryAllowed bool) error {
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
		if !slices.Contains([]EvidenceSource{SourceFile, SourceDirectory, SourceConfiguration, SourceCode, SourceComment, SourceDocstring, SourceDocumentation}, e.SourceKind) {
			return fmt.Errorf("invalid evidence source %q", e.SourceKind)
		}
		if e.SourceKind == SourceDirectory && !directoryAllowed {
			return fmt.Errorf("directory evidence is only valid for content nodes")
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

// isDocumentationPath reports whether a file path represents a documentation
// file that should not be used as evidence for non-documentation facts.
//
// A path is documentation-shaped when:
//   - Its extension is a well-known prose markup extension (.md, .mdx,
//     .markdown, .rst, .adoc, .asciidoc), OR
//   - Its basename has one of the conventional readme/changelog/contributing
//     prefixes AND the file carries no extension (bare prose files common in
//     older repositories, e.g. README, CHANGELOG, CONTRIBUTING).
//
// Files that carry those prefixes but have a non-documentation extension are
// NOT documentation: .github/workflows/changelog.yml is a CI workflow, not a
// changelog; deploy/changelog-service.yaml is a Kubernetes manifest;
// contributing.json is a configuration file.
//
// Note: .txt is intentionally excluded even though it can hold prose, because
// it is also used for configuration (requirements.txt, constraints.txt) and
// classifying it as documentation would block those files from evidencing
// non-documentation facts.
func isDocumentationPath(p string) bool {
	base := strings.ToLower(pathBase(p))
	ext := strings.ToLower(filepath.Ext(base))
	// Files with a recognised prose markup extension are always documentation.
	if slices.Contains([]string{".md", ".mdx", ".markdown", ".rst", ".adoc", ".asciidoc"}, ext) {
		return true
	}
	// The readme/changelog/contributing prefix rule applies only when the file
	// has no extension — config and code files that happen to share the prefix
	// (changelog.yml, readme.yaml, contributing.json) are not documentation.
	if ext != "" {
		return false
	}
	return strings.HasPrefix(base, "readme") || strings.HasPrefix(base, "changelog") || strings.HasPrefix(base, "contributing")
}
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
