package scanner

import (
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
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
