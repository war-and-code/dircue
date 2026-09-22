package scanner

import (
	"context"
	"dircue/pkg/profile"
	"dircue/pkg/structure"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
)

func newStructureReport(opts Options, snapshot *gitSnapshot) *profile.StructureReport {
	r := &profile.StructureReport{Engine: "big-code-analysis", EngineVersion: "2.2.0", Status: "complete", Scope: "source", Source: "directory", MaxFileBytes: opts.Structure.MaxFileBytes(), Omissions: map[string]int64{}, Observations: map[string]uint64{}, SupportedLanguages: structure.Capabilities(), ObservationFiles: map[string]int64{}}
	if snapshot != nil {
		r.Source = "git"
		r.Tree = snapshot.tree.Hash.String()
	}
	if opts.Structure.HotspotsEnabled() {
		r.Hotspots = structure.NewHotspotReport()
	}
	if opts.Structure.FunctionMetricsEnabled() {
		r.Functions = newFunctionReport()
	}
	if opts.StructureFiles {
		f := []structure.File{}
		r.Files = &f
	}
	return r
}
func countStructure(ctx context.Context, root *os.Root, item job, opts Options, value *result, language string, prefix []byte, size int64, included bool) error {
	f := structure.File{Path: item.path, Language: language, SourceBytes: size, Status: "skipped"}
	value.structural = &f
	if !included {
		f.Reason = "outside_scope"
		return nil
	}
	if !structure.Supports(language) {
		f.Reason = "unsupported_language"
		return nil
	}
	limit := opts.Structure.MaxFileBytes()
	if size > limit {
		f.Reason = "file_too_large"
		return nil
	}
	select {
	case opts.structureGate <- struct{}{}:
		defer func() { <-opts.structureGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	data := prefix
	if int64(len(data)) != size {
		var actual int64
		var err error
		if item.read != nil {
			data, actual, err = item.read(limit + 1)
		} else {
			data, _, actual, err = readBoundedSize(root, item.path, limit)
		}
		if err != nil {
			return err
		}
		if actual > limit || int64(len(data)) > limit {
			f.Reason = "file_too_large"
			return nil
		}
		if actual != size || int64(len(data)) != size {
			f.Reason = "file_changed"
			return nil
		}
	}
	observed, err := opts.Structure.Analyze(ctx, item.path, language, data)
	if err != nil {
		// Per-file worker conditions (deadline, crash) become skipped omissions
		// only under an explicit --on-error continue. Protocol violations
		// (identity, provenance, single-parse, malformed responses) never carry
		// these sentinels, so they remain fatal in every mode. The default
		// error policy still aborts on any worker failure, matching 0.8.0.
		if opts.ErrorPolicy == ErrorPolicyContinue && ctx.Err() == nil {
			if reason := recoverableStructureReason(err); reason != "" {
				f.Reason = reason
				// The worker's error can contain arbitrary stderr. Continue-mode
				// warnings are persisted in successful JSON reports, so retain only
				// a fixed explanation; the default fatal path still returns the
				// original error text for compatibility.
				message := "structural worker process exited unsuccessfully"
				if reason == "structural_timeout" {
					message = "structural worker exceeded the per-file timeout"
				}
				value.warnings = append(value.warnings, profile.Warning{Path: item.path, Code: reason, Message: message})
				return nil
			}
		}
		return fmt.Errorf("structure %s: %w", item.path, err)
	}
	value.structural = &observed
	return nil
}

// recoverableStructureReason classifies a structure-worker error for the
// per-file continue policy. Timeouts and process failures map to distinct
// omission reasons so the structure report attributes each cause; protocol
// violations (which never wrap these sentinels) map to no reason and stay
// fatal.
func recoverableStructureReason(err error) string {
	switch {
	case errors.Is(err, structure.ErrWorkerTimeout):
		return "structural_timeout"
	case errors.Is(err, structure.ErrWorkerFailure):
		return "structural_worker_failure"
	default:
		return ""
	}
}
func addStructure(r *profile.StructureReport, value result) error {
	for _, w := range value.warnings {
		if w.Code == "tree_size_limit" {
			r.Status = "skipped"
			r.Omissions["tree_size_limit"]++
			return nil
		}
	}
	if value.path == "" {
		return nil
	}
	f := value.structural
	if f == nil {
		reason := omissionReason(value, "outside_scope")
		for _, warning := range value.warnings {
			if warning.Code == "file_too_large" || warning.Code == "non_regular_file" {
				reason = warning.Code
			}
		}
		f = &structure.File{Path: value.path, Status: "skipped", Reason: reason, SourceBytes: value.size}
	}
	if f.Status == "skipped" {
		r.Omissions[f.Reason]++
	} else {
		r.AnalyzedFiles++
		r.ParseCount += int64(f.ParseCount)
		if f.Status == "partial" {
			r.PartialFiles++
		}
		for k, v := range f.Observations {
			r.Observations[k] += v
			r.ObservationFiles[k]++
		}
	}
	if r.Hotspots != nil && f.Hotspots != nil {
		if err := r.Hotspots.Add(*f); err != nil {
			return err
		}
	}
	if r.Functions != nil && f.Functions != nil {
		if err := addFunctions(r.Functions, *f); err != nil {
			return err
		}
	}
	if r.Files != nil {
		retained := *f
		retained.Hotspots = nil
		retained.Functions = nil
		retained.SourceSHA256 = ""
		*r.Files = append(*r.Files, retained)
	}
	return nil
}
func finishStructure(r *profile.StructureReport) {
	defer finishFunctions(r)
	defer func() {
		if r.Hotspots != nil {
			r.Hotspots.Finish(r.Status, r.Omissions)
			if r.Hotspots.Status == "partial" {
				r.Status = "partial"
			}
		}
	}()
	if r.Status == "skipped" {
		return
	}
	if r.PartialFiles > 0 {
		r.Status = "partial"
	}
	for reason := range r.Omissions {
		if reason != "outside_scope" {
			r.Status = "partial"
		}
	}
	if r.AnalyzedFiles == 0 && r.Status == "complete" {
		r.Status = "skipped"
	}
	if r.Files != nil {
		slices.SortFunc(*r.Files, func(a, b structure.File) int { return strings.Compare(a.Path, b.Path) })
	}
}

const functionReportLimit = 1024

func newFunctionReport() *profile.FunctionReport {
	return &profile.FunctionReport{Provider: "big-code-analysis@2.2.0", Rule: "space-kind-function", RuleVersion: "1.0.0", Scope: "selected-source-files", Status: "complete", ParentStatus: "complete", MetricScope: "includes_nested_spaces", MetricGroups: structure.FunctionMetricGroups(), Order: "path_then_provider_preorder", Limit: functionReportLimit, PerFileLimit: structure.FunctionLimit, NameMaxBytes: structure.FunctionNameMaxBytes, Omissions: map[string]int64{}, Entries: []profile.FunctionEvidence{}}
}

func compareFunction(a, b profile.FunctionEvidence) int {
	if order := strings.Compare(a.Path, b.Path); order != 0 {
		return order
	}
	if a.Index < b.Index {
		return -1
	}
	if a.Index > b.Index {
		return 1
	}
	return 0
}

func addFunctions(r *profile.FunctionReport, file structure.File) error {
	f := file.Functions
	// Every other space counter is a nonnegative subset of the total. Check
	// total first so a rejected response leaves the function aggregate unchanged.
	if f.TotalSpaces > math.MaxUint64-r.TotalSpaces {
		return fmt.Errorf("structure function-space counter overflow for %q", file.Path)
	}
	r.AnalyzedFiles++
	if f.Status == "partial" {
		r.PartialFiles++
	}
	r.TotalSpaces += f.TotalSpaces
	r.PerFileOmittedSpaces += f.OmittedSpaces
	r.OmittedSpaces += f.OmittedSpaces
	r.InvalidSpanSpaces += f.InvalidSpanSpaces
	for _, entry := range f.Entries {
		value := profile.FunctionEvidence{Path: file.Path, Language: file.Language, SourceSHA256: file.SourceSHA256, FunctionEntry: entry}
		index, _ := slices.BinarySearchFunc(r.Entries, value, compareFunction)
		if len(r.Entries) < functionReportLimit {
			r.Entries = slices.Insert(r.Entries, index, value)
		} else {
			r.ReportOmittedSpaces++
			r.OmittedSpaces++
			if index < functionReportLimit {
				copy(r.Entries[index+1:], r.Entries[index:functionReportLimit-1])
				r.Entries[index] = value
			}
		}
	}
	return nil
}

func finishFunctions(parent *profile.StructureReport) {
	r := parent.Functions
	if r == nil {
		return
	}
	r.ParentStatus = parent.Status
	for reason, count := range parent.Omissions {
		r.Omissions[reason] = count
	}
	r.Status = parent.Status
	if r.Status != "skipped" && (r.PartialFiles > 0 || r.OmittedSpaces > 0 || r.InvalidSpanSpaces > 0) {
		r.Status = "partial"
	}
	if r.Status == "partial" {
		parent.Status = "partial"
	}
}
