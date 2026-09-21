package focus

import (
	"context"
	"errors"
	"slices"
)

func queryLimit(options QueryOptions) (int, error) {
	if options.Limit == 0 {
		return DefaultQueryLimit, nil
	}
	if options.Limit < 0 || options.Limit > MaxQueryLimit {
		return 0, errors.New("focus query limit is outside supported bounds")
	}
	return options.Limit, nil
}

// ProjectContext returns retained direct context evidence for a selected
// primary or explicitly related project. It never traverses project relations.
func (r *Result) ProjectContext(ctx context.Context, projectID string, options QueryOptions) (ContextQuery, error) {
	if err := ctx.Err(); err != nil {
		return ContextQuery{}, err
	}
	limit, err := queryLimit(options)
	if err != nil {
		return ContextQuery{}, err
	}
	values, ok := r.contextsByProject[projectID]
	if !ok {
		return ContextQuery{}, errors.New("project is outside the focus selection")
	}
	q := ContextQuery{Status: "complete", ProjectID: projectID, Contexts: []Context{}}
	q.Contexts = append(q.Contexts, values[:min(len(values), limit)]...)
	q.Omitted = len(values) - len(q.Contexts)
	if q.Omitted > 0 || r.queryEvidenceIncomplete() {
		q.Status = "partial"
		if q.Omitted == 0 {
			q.Omitted = 1
		}
	}
	return q, nil
}

// AffectedProjects reports selected projects for which a shared input is
// candidate or declared context. It describes static declared impact only.
func (r *Result) AffectedProjects(ctx context.Context, contextPath string, options QueryOptions) (ImpactQuery, error) {
	if err := ctx.Err(); err != nil {
		return ImpactQuery{}, err
	}
	limit, err := queryLimit(options)
	if err != nil {
		return ImpactQuery{}, err
	}
	values := r.projectsByContext[contextPath]
	q := ImpactQuery{Status: "complete", Path: contextPath, Projects: []ProjectImpact{}}
	q.Projects = append(q.Projects, values[:min(len(values), limit)]...)
	q.Omitted = len(values) - len(q.Projects)
	if q.Omitted > 0 || r.queryEvidenceIncomplete() {
		q.Status = "partial"
		if q.Omitted == 0 {
			q.Omitted = 1
		}
	}
	return q, nil
}

func (r *Result) queryEvidenceIncomplete() bool {
	if r.Report.Coverage.OmittedProjects > 0 || r.Report.Coverage.OmittedRecords > 0 || r.Report.Coverage.OmittedRelations > 0 {
		return true
	}
	for _, omission := range r.Report.Omissions {
		switch omission.Reason {
		case "incomplete-inventory", "incomplete-declarations", "project-record-limit", "work-limit":
			return true
		}
	}
	return false
}

// PrimaryPathList is a deterministic adapter convenience for scanner gating.
func (r *Result) PrimaryPathList() []string {
	paths := make([]string, 0, len(r.PrimaryPaths))
	for p := range r.PrimaryPaths {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths
}
