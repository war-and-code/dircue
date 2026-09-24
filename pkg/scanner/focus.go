package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/war-and-code/dircue/pkg/declarations"
	"github.com/war-and-code/dircue/pkg/focus"
	"github.com/war-and-code/dircue/pkg/profile"
)

// focusAccumulator retains the selected-source jobs needed by the only initial
// deep consumer. Readers and resolved attributes stay bound to this Scan call.
type focusAccumulator struct {
	request           focus.Request
	opts              Options
	snapshot          *gitSnapshot
	jobs              map[string]job
	inventory         []focus.File
	ownershipBarriers []string
	inventoryComplete bool
	treeLimitOmission bool
}

func newFocusAccumulator(opts Options, snapshot *gitSnapshot) (*focusAccumulator, error) {
	if opts.Focus == nil {
		return nil, nil
	}
	if opts.MaxTreeSize > focus.DefaultMaxInventoryPaths {
		return nil, fmt.Errorf("focus tree size limit must not exceed %d", focus.DefaultMaxInventoryPaths)
	}
	request := *opts.Focus
	request.Related = slices.Clone(request.Related)
	if request.AffectedBy != "" && opts.Metrics != nil {
		return nil, errors.New("affected-by focus does not support metrics")
	}
	return &focusAccumulator{request: request, opts: opts, snapshot: snapshot, jobs: make(map[string]job), inventory: []focus.File{}, ownershipBarriers: []string{}, inventoryComplete: true}, nil
}

func (a *focusAccumulator) add(value result) error {
	for _, warning := range value.warnings {
		if warning.Code == "tree_size_limit" {
			a.inventoryComplete = false
			a.treeLimitOmission = true
		}
	}
	if value.selectedJob == nil {
		if focus.IsProjectManifestCandidate(value.path) {
			a.ownershipBarriers = append(a.ownershipBarriers, value.path)
		}
		return nil
	}
	item := *value.selectedJob
	if item.path == "" || item.size < 0 {
		return nil
	}
	if _, exists := a.jobs[item.path]; exists {
		return fmt.Errorf("focus inventory contains duplicate path %q", item.path)
	}
	if len(a.jobs) >= focus.DefaultMaxInventoryPaths {
		a.inventoryComplete = false
		return errors.New("focus inventory path limit reached")
	}
	a.jobs[item.path] = item
	a.inventory = append(a.inventory, focus.File{Path: item.path, Size: item.size})
	return nil
}

func (a *focusAccumulator) finish(ctx context.Context, root *os.Root, declarationsCollector *declarations.Collector, report *profile.Report) error {
	if a.treeLimitOmission {
		return errors.New("focus inventory omitted: tree size limit reached")
	}
	if declarationsCollector == nil || report.Declarations == nil {
		return errors.New("focus requires a finished declaration prepass")
	}
	records := declarationsCollector.ProjectRecords()
	projects := make([]focus.ProjectRecord, 0, len(records))
	for _, record := range records {
		projects = append(projects, focus.ProjectRecord{Project: record.Project, Parsed: record.Parsed && record.Complete})
	}
	declarationReport := report.Declarations
	declarationsComplete := declarationReport.Status == "complete" && declarationReport.Coverage.OmittedFiles == 0 && declarationReport.Coverage.OmittedDiagnostics == 0
	plan, err := focus.Build(ctx, focus.Input{
		Source:               declarationReport.Source,
		Tree:                 declarationReport.Tree,
		Inventory:            a.inventory,
		InventoryComplete:    a.inventoryComplete,
		DeclarationsComplete: declarationsComplete,
		OmittedProjects:      declarationReport.Coverage.OmittedFiles,
		OwnershipBarriers:    a.ownershipBarriers,
		Projects:             projects,
	}, a.request, focus.Limits{})
	if err != nil {
		return fmt.Errorf("plan focus: %w", err)
	}
	report.Focus = plan.Report
	if a.opts.Metrics == nil {
		return nil
	}

	focused := &profile.FocusedMetrics{ScopeID: plan.Report.Scope.ID, Related: []profile.FocusedProjectMetrics{}}
	focused.Primary, err = a.measure(ctx, root, plan.PrimaryPathList(), report)
	if err != nil {
		return fmt.Errorf("measure primary project %q: %w", a.request.Project, err)
	}
	relatedIDs := slices.Clone(a.request.Related)
	slices.Sort(relatedIDs)
	relatedIDs = slices.Compact(relatedIDs)
	for _, projectID := range relatedIDs {
		paths := sortedPathSet(plan.RelatedPaths[projectID])
		metrics, measureErr := a.measure(ctx, root, paths, report)
		if measureErr != nil {
			return fmt.Errorf("measure related project %q: %w", projectID, measureErr)
		}
		focused.Related = append(focused.Related, profile.FocusedProjectMetrics{Project: projectID, Metrics: metrics})
	}
	if !plan.SelectionComplete {
		qualifyIncompleteFocusMetrics(focused.Primary)
		for i := range focused.Related {
			qualifyIncompleteFocusMetrics(focused.Related[i].Metrics)
		}
	}
	report.FocusedMetrics = focused
	return nil
}

func qualifyIncompleteFocusMetrics(metrics *profile.MetricsReport) {
	if metrics == nil || metrics.Status == "skipped" {
		return
	}
	metrics.Status = "partial"
	for _, skipped := range metrics.Skipped {
		if skipped.Reason == "focus_scope_partial" {
			return
		}
	}
	metrics.Skipped = append(metrics.Skipped, profile.MetricSkip{Reason: "focus_scope_partial"})
	slices.SortFunc(metrics.Skipped, func(a, b profile.MetricSkip) int { return strings.Compare(a.Reason, b.Reason) })
}

func (a *focusAccumulator) measure(ctx context.Context, root *os.Root, paths []string, report *profile.Report) (*profile.MetricsReport, error) {
	metricsReport := newMetricsReport(*a.opts.Metrics, a.snapshot)
	metrics := newMetricsAccumulator(metricsReport)
	analysisOptions := Options{MaxFileBytes: a.opts.MaxFileBytes, Metrics: a.opts.Metrics}
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, found := a.jobs[name]
		if !found {
			return nil, fmt.Errorf("selected path %q is unavailable from the source session", name)
		}
		value, err := analyzeFile(ctx, root, item, analysisOptions)
		if err != nil {
			return nil, err
		}
		metrics.add(value)
		report.Warnings = append(report.Warnings, value.warnings...)
	}
	metrics.finish()
	return metricsReport, nil
}

func sortedPathSet(set map[string]struct{}) []string {
	paths := make([]string, 0, len(set))
	for name := range set {
		paths = append(paths, name)
	}
	slices.Sort(paths)
	return paths
}
