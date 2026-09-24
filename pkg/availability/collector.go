package availability

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

type keyed interface{ evidenceKey() string }

type maxHeap[T keyed] []T

func (h maxHeap[T]) Len() int           { return len(h) }
func (h maxHeap[T]) Less(i, j int) bool { return h[i].evidenceKey() > h[j].evidenceKey() }
func (h maxHeap[T]) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *maxHeap[T]) Push(v any)        { *h = append(*h, v.(T)) }
func (h *maxHeap[T]) Pop() any {
	old := *h
	v := old[len(old)-1]
	*h = old[:len(old)-1]
	return v
}

func retain[T keyed](h *maxHeap[T], value T, limit int) bool {
	if limit <= 0 {
		return false
	}
	if len(*h) < limit {
		heap.Push(h, value)
		return true
	}
	if value.evidenceKey() < (*h)[0].evidenceKey() {
		(*h)[0] = value
		heap.Fix(h, 0)
		return true
	}
	return false
}

type fileCandidate struct{ File }

func (f fileCandidate) evidenceKey() string { return f.Path }

type gitlinkValue struct{ Gitlink }

func (v gitlinkValue) evidenceKey() string { return v.Path + "\x00" + v.Commit }

type submoduleValue struct{ SubmoduleDeclaration }

func (v submoduleValue) evidenceKey() string { return v.Path + "\x00" + v.Evidence }

type sparseValue struct{ SparseIndication }

func (v sparseValue) evidenceKey() string { return v.Path + "\x00" + v.Kind + "\x00" + v.Evidence }

type referenceValue struct{ MissingReference }

func (v referenceValue) evidenceKey() string {
	return v.Target + "\x00" + v.Project + "\x00" + v.Kind + "\x00" + v.Evidence
}

type diagnosticValue struct{ Diagnostic }

func (v diagnosticValue) evidenceKey() string { return v.Path + "\x00" + v.Code + "\x00" + v.Message }

type boundaryValue struct{ path, kind string }

func (v boundaryValue) evidenceKey() string { return v.path + "\x00" + v.kind }

type Collector struct {
	opts        Options
	report      Report
	files       maxHeap[fileCandidate]
	gitlinks    maxHeap[gitlinkValue]
	submodules  maxHeap[submoduleValue]
	sparse      maxHeap[sparseValue]
	references  maxHeap[referenceValue]
	diagnostics maxHeap[diagnosticValue]
	boundaries  maxHeap[boundaryValue]
	finished    bool
	finishErr   error
}

func New(opts Options) *Collector {
	if opts.MaxFileBytes < 0 {
		opts.MaxFileBytes = 0
	}
	if opts.ContentBytes <= 0 {
		opts.ContentBytes = DefaultContentBytes
	}
	if opts.ContentBytes < PointerSizeCutoff {
		opts.ContentBytes = PointerSizeCutoff
	}
	opts.ContentBytes = min(opts.ContentBytes, DefaultContentBytes)
	if opts.GitmodulesBytes <= 0 {
		opts.GitmodulesBytes = DefaultGitmodulesBytes
	}
	opts.GitmodulesBytes = min(opts.GitmodulesBytes, DefaultGitmodulesBytes)
	if opts.CheckoutMetadataBytes <= 0 {
		opts.CheckoutMetadataBytes = DefaultCheckoutMetadataBytes
	}
	opts.CheckoutMetadataBytes = min(opts.CheckoutMetadataBytes, DefaultCheckoutMetadataBytes)
	if opts.EvidenceLimit <= 0 {
		opts.EvidenceLimit = DefaultEvidenceLimit
	}
	opts.EvidenceLimit = min(opts.EvidenceLimit, DefaultEvidenceLimit)
	if opts.DiagnosticLimit <= 0 {
		opts.DiagnosticLimit = DefaultDiagnosticLimit
	}
	opts.DiagnosticLimit = min(opts.DiagnosticLimit, DefaultDiagnosticLimit)
	if opts.BoundaryPaths <= 0 {
		opts.BoundaryPaths = DefaultBoundaryPaths
	}
	opts.BoundaryPaths = min(opts.BoundaryPaths, DefaultBoundaryPaths)
	if opts.CorrelationWork <= 0 {
		opts.CorrelationWork = DefaultCorrelationWork
	}
	opts.CorrelationWork = min(opts.CorrelationWork, DefaultCorrelationWork)
	if opts.OutputBytes <= 0 {
		opts.OutputBytes = DefaultOutputBytes
	}
	if opts.OutputBytes < 4096 {
		opts.OutputBytes = 4096
	}
	opts.OutputBytes = min(opts.OutputBytes, DefaultOutputBytes)
	if opts.StringBytes <= 0 {
		opts.StringBytes = DefaultStringBytes
	}
	opts.StringBytes = min(opts.StringBytes, DefaultStringBytes)
	if opts.Source.Consistency == "" {
		if opts.Source.Mode == "git" {
			opts.Source.Consistency = "selected_git_tree"
		} else {
			opts.Source.Consistency = "live_directory"
		}
	}
	if opts.Source.CheckoutMetadata == "" {
		opts.Source.CheckoutMetadata = "not_inspected"
	}
	inventoryComplete := opts.InventoryComplete
	if !opts.InventoryStatusSet {
		inventoryComplete = true
	}
	r := Report{
		Provider: "dircue", ProviderVersion: ProviderVersion, Status: "complete", Source: opts.Source,
		Bounds:   Bounds{PointerBytes: PointerSizeCutoff, MaxFileBytes: opts.MaxFileBytes, ContentBytes: opts.ContentBytes, GitmodulesBytes: opts.GitmodulesBytes, CheckoutMetadataBytes: opts.CheckoutMetadataBytes, EvidencePerKind: opts.EvidenceLimit, Diagnostics: opts.DiagnosticLimit, BoundaryPaths: opts.BoundaryPaths, CorrelationWork: opts.CorrelationWork, OutputBytes: opts.OutputBytes, StringBytes: opts.StringBytes},
		Coverage: Coverage{SelectedInventoryComplete: inventoryComplete}, LFS: []LFSObservation{}, Gitlinks: []Gitlink{}, Submodules: []SubmoduleDeclaration{}, Sparse: []SparseIndication{}, References: []ReferenceCorrelation{}, Diagnostics: []Diagnostic{}, Omissions: map[string]int64{},
	}
	if !inventoryComplete {
		r.Status = "partial"
		r.Omissions["selected_inventory_incomplete"] = 1
	}
	return &Collector{opts: opts, report: r}
}

func (c *Collector) partial(reason string, count int64) {
	if count <= 0 {
		count = 1
	}
	c.report.Status = "partial"
	c.report.Omissions[reason] += count
}

func (c *Collector) mutable() bool { return !c.finished }

func (c *Collector) AddDiagnostic(d Diagnostic) {
	if !c.mutable() {
		return
	}
	if !validText(d.Path, c.opts.StringBytes) || !validText(d.Code, c.opts.StringBytes) || !validText(d.Message, c.opts.StringBytes) {
		c.report.Coverage.OmittedDiagnostics++
		c.partial("diagnostic_limit", 1)
		return
	}
	full := len(c.diagnostics) >= c.opts.DiagnosticLimit
	retain(&c.diagnostics, diagnosticValue{d}, c.opts.DiagnosticLimit)
	if full {
		c.report.Coverage.OmittedDiagnostics++
		c.partial("diagnostic_limit", 1)
	}
}

func (c *Collector) MarkInventoryIncomplete(reason string) {
	if !c.mutable() {
		return
	}
	c.report.Coverage.SelectedInventoryComplete = false
	c.partial(reason, 1)
}

func (c *Collector) MarkCheckoutMetadataInspected() {
	if !c.mutable() {
		return
	}
	if !c.report.Coverage.CheckoutMetadataInspected {
		c.report.Coverage.CheckoutMetadataInspected = true
		c.report.Coverage.CheckoutMetadataComplete = true
	}
}

func (c *Collector) MarkCheckoutMetadataIncomplete(reason string) {
	if !c.mutable() {
		return
	}
	c.report.Coverage.CheckoutMetadataInspected = true
	c.report.Coverage.CheckoutMetadataComplete = false
	c.partial(reason, 1)
}

func (c *Collector) AddFile(file File) {
	if !c.mutable() {
		return
	}
	c.report.Coverage.SelectedRegularFiles++
	if file.LFSAttribute == "tracked" {
		c.report.Counts.LFSTrackedFiles++
	}
	if !validPath(file.Path, c.opts.StringBytes) || file.Size < 0 || file.Read == nil {
		c.report.Coverage.OmittedPointerFiles++
		c.partial("invalid_file_candidate", 1)
		return
	}
	if file.LFSAttribute != "tracked" && file.LFSAttribute != "untracked" && file.LFSAttribute != "unknown" {
		file.LFSAttribute = "unknown"
	}
	c.report.Coverage.PointerCandidates++
	if c.opts.MaxFileBytes > 0 && file.Size > c.opts.MaxFileBytes {
		c.report.Coverage.OmittedPointerFiles++
		c.partial("file_size_limit", 1)
		return
	}
	limit := int(c.opts.ContentBytes / PointerSizeCutoff)
	if limit < 1 {
		limit = 1
	}
	full := len(c.files) >= limit
	retain(&c.files, fileCandidate{file}, limit)
	if full {
		c.report.Coverage.OmittedPointerFiles++
		c.partial("pointer_inspection_limit", 1)
	}
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func (c *Collector) AddGitlink(value Gitlink) {
	if !c.mutable() {
		return
	}
	c.report.Counts.Gitlinks++
	if !validPath(value.Path, c.opts.StringBytes) || !objectIDPattern.MatchString(value.Commit) {
		c.AddDiagnostic(Diagnostic{Path: value.Path, Code: "invalid-gitlink", Message: "Gitlink path or commit object ID is invalid."})
		c.partial("invalid_gitlink", 1)
		return
	}
	full := len(c.gitlinks) >= c.opts.EvidenceLimit
	retain(&c.gitlinks, gitlinkValue{value}, c.opts.EvidenceLimit)
	if full {
		c.report.Coverage.OmittedEvidence++
		c.partial("gitlink_evidence_limit", 1)
	}
	c.addBoundary(boundaryValue{value.Path, "gitlink"})
}

func (c *Collector) AddSubmoduleDeclaration(value SubmoduleDeclaration) {
	if !c.mutable() {
		return
	}
	c.report.Counts.SubmoduleDeclarations++
	if !validPath(value.Path, c.opts.StringBytes) || !validPath(value.Evidence, c.opts.StringBytes) {
		c.AddDiagnostic(Diagnostic{Path: value.Evidence, Code: "invalid-submodule-path", Message: "Submodule declaration path is not a normalized selected-source path."})
		c.partial("invalid_submodule_declaration", 1)
		return
	}
	full := len(c.submodules) >= c.opts.EvidenceLimit
	retain(&c.submodules, submoduleValue{value}, c.opts.EvidenceLimit)
	if full {
		c.report.Coverage.OmittedEvidence++
		c.partial("submodule_evidence_limit", 1)
	}
}

func (c *Collector) AddSparseIndication(value SparseIndication) {
	if !c.mutable() {
		return
	}
	c.MarkCheckoutMetadataInspected()
	c.report.Counts.SparseIndications++
	if value.Path != "" && !validPath(value.Path, c.opts.StringBytes) || !validEvidencePath(value.Evidence, c.opts.StringBytes) || !validText(value.Kind, c.opts.StringBytes) {
		c.AddDiagnostic(Diagnostic{Path: value.Evidence, Code: "invalid-sparse-indication", Message: "Sparse-checkout indication contains invalid evidence."})
		c.partial("invalid_sparse_indication", 1)
		return
	}
	if !value.Supported {
		c.MarkCheckoutMetadataIncomplete("unsupported_sparse_metadata")
	}
	full := len(c.sparse) >= c.opts.EvidenceLimit
	retain(&c.sparse, sparseValue{value}, c.opts.EvidenceLimit)
	if full {
		c.report.Coverage.OmittedEvidence++
		c.partial("sparse_evidence_limit", 1)
	}
	if value.Supported && value.Path != "" && (value.Kind == "skip_worktree" || value.Kind == "sparse_directory") {
		c.addBoundary(boundaryValue{value.Path, value.Kind})
	}
}

func (c *Collector) AddMissingReference(value MissingReference) {
	if !c.mutable() {
		return
	}
	c.report.Counts.MissingReferences++
	if !validPath(value.Target, c.opts.StringBytes) || !validPath(value.Evidence, c.opts.StringBytes) || !validText(value.Project, c.opts.StringBytes) || !validText(value.Kind, c.opts.StringBytes) {
		c.AddDiagnostic(Diagnostic{Path: value.Evidence, Code: "invalid-missing-reference", Message: "Missing-reference evidence is not normalized."})
		c.partial("invalid_missing_reference", 1)
		return
	}
	full := len(c.references) >= c.opts.EvidenceLimit
	retain(&c.references, referenceValue{value}, c.opts.EvidenceLimit)
	if full {
		c.report.Coverage.OmittedEvidence++
		c.partial("reference_evidence_limit", 1)
	}
}

func (c *Collector) addBoundary(value boundaryValue) {
	full := len(c.boundaries) >= c.opts.BoundaryPaths
	retain(&c.boundaries, value, c.opts.BoundaryPaths)
	if full {
		c.report.Coverage.OmittedBoundaryPaths++
		c.partial("boundary_path_limit", 1)
	}
}

func (c *Collector) Finish(ctx context.Context) (_ *Report, err error) {
	if c.finished {
		return &c.report, c.finishErr
	}
	c.finished = true
	defer func() { c.finishErr = err }()
	files := sortedHeap(c.files)
	for _, candidate := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, size, err := candidate.Read(ctx, PointerSizeCutoff)
		if err != nil {
			return nil, fmt.Errorf("read availability candidate %s: %w", candidate.Path, err)
		}
		if int64(len(data)) > PointerSizeCutoff {
			data = data[:PointerSizeCutoff]
		}
		c.report.Coverage.PointerInspections++
		c.report.Coverage.ContentBytesRead += int64(len(data))
		if size != candidate.Size {
			c.partial("input_changed", 1)
			continue
		}
		inspection := InspectLFSPointer(data, size)
		if inspection.Kind == "" {
			if candidate.LFSAttribute == "tracked" && int64(len(data)) == min(size, PointerSizeCutoff) {
				c.report.Counts.LFSAttributedNonPointerFiles++
			}
			continue
		}
		observation := LFSObservation{Path: candidate.Path, Kind: inspection.Kind, Reason: inspection.Reason, LFSAttribute: candidate.LFSAttribute, SourceFileBytes: size, MetadataComplete: inspection.Kind == "valid_pointer", Version: inspection.Version, OIDAlgorithm: inspection.OIDAlgorithm, OIDDigest: inspection.OIDDigest, DeclaredObjectBytes: inspection.DeclaredObjectBytes, Canonical: inspection.Canonical, ExtensionCount: inspection.ExtensionCount}
		if inspection.Kind == "valid_pointer" {
			c.report.Counts.ValidPointers++
			if inspection.Canonical {
				c.report.Counts.CanonicalPointers++
			}
			c.addBoundary(boundaryValue{candidate.Path, "lfs_pointer"})
		} else {
			c.report.Counts.PointerLikeFiles++
		}
		// The number of inspected files is already bounded; retain applies the
		// smaller serialized-evidence bound independently.
		if !retainLFS(&c.report, observation, c.opts.EvidenceLimit) {
			c.report.Coverage.OmittedEvidence++
			c.partial("lfs_evidence_limit", 1)
		}
	}
	c.report.Gitlinks = unwrapGitlinks(sortedHeap(c.gitlinks))
	c.report.Submodules = unwrapSubmodules(sortedHeap(c.submodules))
	c.report.Sparse = unwrapSparse(sortedHeap(c.sparse))
	c.report.Diagnostics = unwrapDiagnostics(sortedHeap(c.diagnostics))
	if err := c.finishRelationships(ctx); err != nil {
		return nil, err
	}
	c.report.Coverage.IndexedBoundaryPaths = len(c.boundaries)
	c.boundOutput()
	return &c.report, nil
}

func (c *Collector) finishRelationships(ctx context.Context) error {
	gitlinks := map[string]int{}
	for i := range c.report.Gitlinks {
		gitlinks[c.report.Gitlinks[i].Path] = i
	}
	for i := range c.report.Submodules {
		if j, ok := gitlinks[c.report.Submodules[i].Path]; ok {
			c.report.Submodules[i].HasGitlink = true
			c.report.Gitlinks[j].Declared = true
			c.report.Counts.MatchedSubmodules++
		}
	}
	boundaries := sortedHeap(c.boundaries)
	refs := sortedHeap(c.references)
	for _, ref := range refs {
		if c.report.Coverage.CorrelationWork&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if c.report.Coverage.CorrelationWork >= c.opts.CorrelationWork {
			c.report.Coverage.OmittedCorrelations++
			c.partial("correlation_work_limit", 1)
			c.report.References = append(c.report.References, ReferenceCorrelation{MissingReference: ref.MissingReference, Qualification: "unknown"})
			continue
		}
		out := ReferenceCorrelation{MissingReference: ref.MissingReference, Qualification: "unqualified"}
		for _, boundary := range boundaries {
			if c.report.Coverage.CorrelationWork&255 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			if c.report.Coverage.CorrelationWork >= c.opts.CorrelationWork {
				c.report.Coverage.OmittedCorrelations++
				c.partial("correlation_work_limit", 1)
				out.Qualification = "unknown"
				break
			}
			c.report.Coverage.CorrelationWork++
			matched := ref.Target == boundary.path
			if boundary.kind == "gitlink" || boundary.kind == "skip_worktree" || boundary.kind == "sparse_directory" {
				matched = matched || strings.HasPrefix(ref.Target, strings.TrimSuffix(boundary.path, "/")+"/")
			}
			if matched {
				out.Qualification, out.BoundaryKind, out.BoundaryPath = "established_boundary", boundary.kind, boundary.path
				c.report.Counts.QualifiedMissingReferences++
				break
			}
		}
		checkoutUnknown := c.report.Coverage.CheckoutMetadataInspected && !c.report.Coverage.CheckoutMetadataComplete
		if out.Qualification == "unqualified" && (!c.report.Coverage.SelectedInventoryComplete || checkoutUnknown || c.report.Coverage.OmittedBoundaryPaths > 0 || c.report.Coverage.OmittedPointerFiles > 0) {
			out.Qualification = "unknown"
		}
		c.report.References = append(c.report.References, out)
	}
	return nil
}

func retainLFS(report *Report, value LFSObservation, limit int) bool {
	if len(report.LFS) < limit {
		report.LFS = append(report.LFS, value)
		return true
	}
	// Files are processed lexically, so every later observation is a lexical tail.
	return false
}

func sortedHeap[T keyed](values maxHeap[T]) []T {
	out := slices.Clone(values)
	slices.SortFunc(out, func(a, b T) int { return strings.Compare(a.evidenceKey(), b.evidenceKey()) })
	return out
}

func unwrapGitlinks(values []gitlinkValue) []Gitlink {
	out := make([]Gitlink, len(values))
	for i := range values {
		out[i] = values[i].Gitlink
	}
	return out
}
func unwrapSubmodules(values []submoduleValue) []SubmoduleDeclaration {
	out := make([]SubmoduleDeclaration, len(values))
	for i := range values {
		out[i] = values[i].SubmoduleDeclaration
	}
	return out
}
func unwrapSparse(values []sparseValue) []SparseIndication {
	out := make([]SparseIndication, len(values))
	for i := range values {
		out[i] = values[i].SparseIndication
	}
	return out
}
func unwrapDiagnostics(values []diagnosticValue) []Diagnostic {
	out := make([]Diagnostic, len(values))
	for i := range values {
		out[i] = values[i].Diagnostic
	}
	return out
}

func validPath(value string, max int) bool {
	return value != "." && len(value) <= max && utf8.ValidString(value) && fs.ValidPath(value) && !strings.ContainsAny(value, "\\:") && !hasControl(value) && !strings.EqualFold(value, ".git") && !strings.HasPrefix(strings.ToLower(value), ".git/")
}

func validEvidencePath(value string, max int) bool {
	return value != "." && len(value) <= max && utf8.ValidString(value) && fs.ValidPath(value) && !strings.ContainsAny(value, "\\:") && !hasControl(value)
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validText(value string, max int) bool {
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func (c *Collector) boundOutput() {
	for {
		encoded, _ := json.Marshal(&c.report)
		if len(encoded) <= c.opts.OutputBytes {
			return
		}
		removed := int64(0)
		trim := func(n int, apply func(int)) {
			if n == 0 {
				return
			}
			cut := max(1, n/8)
			apply(n - cut)
			removed += int64(cut)
		}
		trim(len(c.report.References), func(n int) { c.report.References = c.report.References[:n] })
		trim(len(c.report.Sparse), func(n int) { c.report.Sparse = c.report.Sparse[:n] })
		trim(len(c.report.Submodules), func(n int) { c.report.Submodules = c.report.Submodules[:n] })
		trim(len(c.report.Gitlinks), func(n int) { c.report.Gitlinks = c.report.Gitlinks[:n] })
		trim(len(c.report.LFS), func(n int) { c.report.LFS = c.report.LFS[:n] })
		trim(len(c.report.Diagnostics), func(n int) { c.report.Diagnostics = c.report.Diagnostics[:n] })
		if removed == 0 {
			c.report.Status = "partial"
			c.report.Omissions["output_limit"]++
			return
		}
		c.report.Coverage.OmittedEvidence += removed
		c.partial("output_limit", removed)
	}
}
