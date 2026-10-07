package declarations

import "slices"

// ProjectRecord preserves parser eligibility for consumers of a finished
// collection. Complete concerns per-document retention, not build evaluation.
// The ordinary declaration report intentionally retains its existing shape.
type ProjectRecord struct {
	Project  Project
	Parsed   bool
	Complete bool
	// WorkspaceDeclared is parser-private metadata for explicit empty groups.
	WorkspaceDeclared bool
	// PythonBuildSystemSeen preserves an empty pyproject build-system table,
	// which has no corresponding requirement observation.
	PythonBuildSystemSeen bool
}

// EnableProjectRecords retains bounded eligibility metadata when Finish runs.
// Call it before Finish; ordinary collections do not allocate these records.
func (c *Collector) EnableProjectRecords() {
	if !c.finished {
		c.retainRecords = true
	}
}

// ProjectRecords returns retained projects after a successful Finish. Missing
// records are not evidence that no other projects exist; inspect report coverage.
func (c *Collector) ProjectRecords() []ProjectRecord {
	if !c.finished || c.finishErr != nil || !c.retainRecords {
		return nil
	}
	out := slices.Clone(c.records)
	for i := range out {
		out[i].Project.Requirements = slices.Clone(out[i].Project.Requirements)
		out[i].Project.References = slices.Clone(out[i].Project.References)
		out[i].Project.Interfaces = slices.Clone(out[i].Project.Interfaces)
	}
	return out
}
