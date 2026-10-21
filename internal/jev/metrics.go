package jev

import (
	"sync"
	"sync/atomic"
)

type MetricsSnapshot struct {
	Calls         map[string]uint64
	Failures      map[string]uint64
	CacheHits     map[string]uint64
	Fallbacks     map[string]uint64
	Suppressed    uint64
	Skipped       uint64
	WouldSkip     uint64
	WouldSuppress uint64
}

type MetricsCollector struct {
	mu         sync.RWMutex
	calls      map[string]*atomic.Uint64
	failures   map[string]*atomic.Uint64
	cacheHits  map[string]*atomic.Uint64
	fallbacks  map[string]*atomic.Uint64
	suppressed atomic.Uint64
	skipped    atomic.Uint64
	wouldSkip  atomic.Uint64
	wouldSupp  atomic.Uint64
}

var globalMetrics = newMetricsCollector()

func newMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		calls:     make(map[string]*atomic.Uint64),
		failures:  make(map[string]*atomic.Uint64),
		cacheHits: make(map[string]*atomic.Uint64),
		fallbacks: make(map[string]*atomic.Uint64),
	}
}

func (m *MetricsCollector) getCounter(mMap map[string]*atomic.Uint64, plane string) *atomic.Uint64 {
	m.mu.RLock()
	c, ok := mMap[plane]
	m.mu.RUnlock()
	if ok {
		return c
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := mMap[plane]; ok {
		return c
	}
	c = &atomic.Uint64{}
	mMap[plane] = c
	return c
}

func (m *MetricsCollector) RecordCall(plane string) {
	m.getCounter(m.calls, plane).Add(1)
}

func (m *MetricsCollector) RecordFailure(plane string) {
	m.getCounter(m.failures, plane).Add(1)
}

func (m *MetricsCollector) RecordCacheHit(plane string) {
	m.getCounter(m.cacheHits, plane).Add(1)
}

func (m *MetricsCollector) RecordFallback(plane string) {
	m.getCounter(m.fallbacks, plane).Add(1)
}

func (m *MetricsCollector) RecordSuppressed() {
	m.suppressed.Add(1)
}

func (m *MetricsCollector) RecordSkipped() {
	m.skipped.Add(1)
}

func (m *MetricsCollector) RecordWouldSkip() {
	m.wouldSkip.Add(1)
}

func (m *MetricsCollector) RecordWouldSuppress() {
	m.wouldSupp.Add(1)
}

func (m *MetricsCollector) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := MetricsSnapshot{
		Calls:         make(map[string]uint64, len(m.calls)),
		Failures:      make(map[string]uint64, len(m.failures)),
		CacheHits:     make(map[string]uint64, len(m.cacheHits)),
		Fallbacks:     make(map[string]uint64, len(m.fallbacks)),
		Suppressed:    m.suppressed.Load(),
		Skipped:       m.skipped.Load(),
		WouldSkip:     m.wouldSkip.Load(),
		WouldSuppress: m.wouldSupp.Load(),
	}

	for k, v := range m.calls {
		snap.Calls[k] = v.Load()
	}
	for k, v := range m.failures {
		snap.Failures[k] = v.Load()
	}
	for k, v := range m.cacheHits {
		snap.CacheHits[k] = v.Load()
	}
	for k, v := range m.fallbacks {
		snap.Fallbacks[k] = v.Load()
	}

	return snap
}

// GlobalMetrics 返回全局指标收集器。
func GlobalMetrics() *MetricsCollector {
	return globalMetrics
}
