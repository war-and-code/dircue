package scanner

import (
	"github.com/war-and-code/dircue/third_party/go-git/plumbing"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/cache"
	"github.com/war-and-code/dircue/third_party/go-git/plumbing/format/packfile"
)

// GitReadMetrics is an opt-in measurement hook for the maintained Git reader.
// It is not included in reports or CLI output. Physical read and cache counters
// can vary with scheduling and cache state.
type GitReadMetrics = packfile.ReadMetrics

// GitReadMetricsSnapshot is one observation of GitReadMetrics.
type GitReadMetricsSnapshot = packfile.ReadMetricsSnapshot

type metricsObjectCache struct {
	cache.Object
	metrics *packfile.ReadMetrics
}

func (c *metricsObjectCache) Put(object plumbing.EncodedObject) {
	c.metrics.RecordObjectCachePut()
	c.Object.Put(object)
}

func (c *metricsObjectCache) Get(hash plumbing.Hash) (plumbing.EncodedObject, bool) {
	object, ok := c.Object.Get(hash)
	c.metrics.RecordObjectCacheGet(ok)
	return object, ok
}
