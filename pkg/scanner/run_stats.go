// Package scanner includes RunCounters, a lightweight opt-in harness for
// deterministic cost counters. Counters are updated atomically and produce
// identical values for the same input and settings regardless of worker count.
// They must never enter a deterministic report payload.
package scanner

import "sync/atomic"

// RunCounters accumulates deterministic cost counters during a scan.
// All fields are updated atomically; they are safe to read after Scan returns.
// Physical metrics (cache hits, delta resolutions, bytes inflated from pack
// data) depend on worker scheduling and object-cache state, so they are NOT
// included here; use GitReadMetrics for those observations.
//
// Additional deterministic counts (files_enumerated, limit_hits.*) are
// derived from profile.Report.Summary and Report.Warnings after the scan.
type RunCounters struct {
	// FilesContentRead is the number of files whose content was actually
	// fetched from the source for classification and analysis.
	FilesContentRead atomic.Int64
	// BytesRequested is the sum of logical file sizes for files whose content
	// was fetched. For Git sources this is the uncompressed object size; for
	// directory sources this is the stat size. It does not count bytes that
	// were skipped because of vendor rules.
	BytesRequested atomic.Int64
}
