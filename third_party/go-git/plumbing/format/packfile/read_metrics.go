// Modified for dircue: opt-in Git read-path work counters.

package packfile

import (
	"io"
	"sync/atomic"

	billy "github.com/go-git/go-billy/v5"
)

// ReadMetrics records work performed while reading Git objects. The zero value
// is ready for use. Counters describing physical reads and cache behavior may
// vary with scheduling and cache state; callers must not treat them as output
// invariants.
type ReadMetrics struct {
	inflatersStarted   atomic.Uint64
	inflatedBytes      atomic.Uint64
	deltaResolutions   atomic.Uint64
	deltaBaseBytesRead atomic.Uint64
	packBytesRead      atomic.Uint64
	looseBytesRead     atomic.Uint64
	indexBytesRead     atomic.Uint64
	objectCacheHits    atomic.Uint64
	objectCacheMisses  atomic.Uint64
	objectCachePuts    atomic.Uint64
	activeDeltaReaders atomic.Int64
}

// ReadMetricsSnapshot is one observation of ReadMetrics. A snapshot is final
// only when ActiveDeltaReaders is zero and the owning scan has stopped issuing
// reads.
type ReadMetricsSnapshot struct {
	InflatersStarted   uint64 `json:"inflaters_started"`
	InflatedBytes      uint64 `json:"inflated_bytes"`
	DeltaResolutions   uint64 `json:"delta_resolutions"`
	DeltaBaseBytesRead uint64 `json:"delta_base_bytes_read"`
	PackBytesRead      uint64 `json:"pack_bytes_read"`
	LooseBytesRead     uint64 `json:"loose_bytes_read"`
	IndexBytesRead     uint64 `json:"index_bytes_read"`
	ObjectCacheHits    uint64 `json:"object_cache_hits"`
	ObjectCacheMisses  uint64 `json:"object_cache_misses"`
	ObjectCachePuts    uint64 `json:"object_cache_puts"`
	ActiveDeltaReaders int64  `json:"active_delta_readers"`
}

// Snapshot returns the current counters without waiting for asynchronous delta
// readers. ActiveDeltaReaders reports whether those counters can still change.
func (m *ReadMetrics) Snapshot() ReadMetricsSnapshot {
	if m == nil {
		return ReadMetricsSnapshot{}
	}
	// Producers publish all counter increments before their final active-reader
	// decrement. Read activity first: zero is a finality signal only when the
	// owner has already stopped issuing new reads; a stale nonzero is safely
	// conservative.
	active := m.activeDeltaReaders.Load()
	return ReadMetricsSnapshot{
		InflatersStarted:   m.inflatersStarted.Load(),
		InflatedBytes:      m.inflatedBytes.Load(),
		DeltaResolutions:   m.deltaResolutions.Load(),
		DeltaBaseBytesRead: m.deltaBaseBytesRead.Load(),
		PackBytesRead:      m.packBytesRead.Load(),
		LooseBytesRead:     m.looseBytesRead.Load(),
		IndexBytesRead:     m.indexBytesRead.Load(),
		ObjectCacheHits:    m.objectCacheHits.Load(),
		ObjectCacheMisses:  m.objectCacheMisses.Load(),
		ObjectCachePuts:    m.objectCachePuts.Load(),
		ActiveDeltaReaders: active,
	}
}

// Reset clears a completed run. The caller must exclusively own the metrics and
// must have stopped issuing new reads. It returns false without changing
// counters if an asynchronous delta reader is still active at the initial check.
func (m *ReadMetrics) Reset() bool {
	if m == nil {
		return true
	}
	if m.activeDeltaReaders.Load() != 0 {
		return false
	}
	m.inflatersStarted.Store(0)
	m.inflatedBytes.Store(0)
	m.deltaResolutions.Store(0)
	m.deltaBaseBytesRead.Store(0)
	m.packBytesRead.Store(0)
	m.looseBytesRead.Store(0)
	m.indexBytesRead.Store(0)
	m.objectCacheHits.Store(0)
	m.objectCacheMisses.Store(0)
	m.objectCachePuts.Store(0)
	return m.activeDeltaReaders.Load() == 0
}

func (m *ReadMetrics) recordInflater() {
	if m != nil {
		m.inflatersStarted.Add(1)
	}
}

func (m *ReadMetrics) recordInflatedBytes(n int) {
	if m != nil && n > 0 {
		m.inflatedBytes.Add(uint64(n))
	}
}

func (m *ReadMetrics) recordDeltaResolution() {
	if m != nil {
		m.deltaResolutions.Add(1)
	}
}

func (m *ReadMetrics) recordDeltaBaseBytes(n int) {
	if m != nil && n > 0 {
		m.deltaBaseBytesRead.Add(uint64(n))
	}
}

// RecordPackBytes records bytes returned from a pack file Read or ReadAt.
func (m *ReadMetrics) RecordPackBytes(n int) {
	if m != nil && n > 0 {
		m.packBytesRead.Add(uint64(n))
	}
}

// RecordLooseBytes records bytes returned from a loose-object file read.
func (m *ReadMetrics) RecordLooseBytes(n int) {
	if m != nil && n > 0 {
		m.looseBytesRead.Add(uint64(n))
	}
}

// RecordIndexBytes records bytes returned from a pack-index file read.
func (m *ReadMetrics) RecordIndexBytes(n int) {
	if m != nil && n > 0 {
		m.indexBytesRead.Add(uint64(n))
	}
}

// RecordObjectCacheGet records one observed object-cache lookup.
func (m *ReadMetrics) RecordObjectCacheGet(hit bool) {
	if m == nil {
		return
	}
	if hit {
		m.objectCacheHits.Add(1)
	} else {
		m.objectCacheMisses.Add(1)
	}
}

// RecordObjectCachePut records one object-cache insertion attempt.
func (m *ReadMetrics) RecordObjectCachePut() {
	if m != nil {
		m.objectCachePuts.Add(1)
	}
}

type inflatedCountingReadCloser struct {
	io.ReadCloser
	metrics *ReadMetrics
}

func (r inflatedCountingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.metrics.recordInflatedBytes(n)
	return n, err
}

type inflatedCountingWriter struct {
	writer  io.Writer
	metrics *ReadMetrics
}

type packMetricsFile struct {
	billy.File
	metrics *ReadMetrics
}

func (f *packMetricsFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.metrics.RecordPackBytes(n)
	return n, err
}

func (f *packMetricsFile) ReadAt(p []byte, offset int64) (int, error) {
	n, err := f.File.ReadAt(p, offset)
	f.metrics.RecordPackBytes(n)
	return n, err
}

func (w inflatedCountingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.metrics.recordInflatedBytes(n)
	return n, err
}
