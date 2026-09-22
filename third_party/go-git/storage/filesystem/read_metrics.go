// Modified for dircue: opt-in loose-object and index byte counters.

package filesystem

import (
	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
)

type metricFileKind uint8

const (
	metricLoose metricFileKind = iota
	metricIndex
)

type metricsFile struct {
	billy.File
	metrics *packfile.ReadMetrics
	kind    metricFileKind
}

func (f *metricsFile) record(n int) {
	if f.kind == metricIndex {
		f.metrics.RecordIndexBytes(n)
	} else {
		f.metrics.RecordLooseBytes(n)
	}
}

func (f *metricsFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.record(n)
	return n, err
}

func (f *metricsFile) ReadAt(p []byte, offset int64) (int, error) {
	n, err := f.File.ReadAt(p, offset)
	f.record(n)
	return n, err
}

func instrumentFile(file billy.File, metrics *packfile.ReadMetrics, kind metricFileKind) billy.File {
	if metrics == nil {
		return file
	}
	return &metricsFile{File: file, metrics: metrics, kind: kind}
}
