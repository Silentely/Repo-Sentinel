package jev

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	// MetricsSettingKey 是 system_settings 中 Jev 运行指标持久化的键。
	MetricsSettingKey = "jev.metrics.v1"
)

// StoredMetrics 描述在数据库中持久化的 Jev 运行时指标结构。
type StoredMetrics struct {
	Calls         map[string]uint64 `json:"calls,omitempty"`
	Failures      map[string]uint64 `json:"failures,omitempty"`
	CacheHits     map[string]uint64 `json:"cache_hits,omitempty"`
	Fallbacks     map[string]uint64 `json:"fallbacks,omitempty"`
	Suppressed    uint64            `json:"suppressed,omitempty"`
	Skipped       uint64            `json:"skipped,omitempty"`
	WouldSkip     uint64            `json:"would_skip,omitempty"`
	WouldSuppress uint64            `json:"would_suppress,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
}

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

	saveMu    sync.Mutex
	storeMu   sync.Mutex
	st        store.Store
	persistCh chan struct{}
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

// BindStore 绑定存储实例，并立即从数据库恢复既往累计指标。
func (m *MetricsCollector) BindStore(ctx context.Context, st store.Store) error {
	if st == nil {
		return nil
	}
	m.storeMu.Lock()
	m.st = st
	if m.persistCh == nil {
		m.persistCh = make(chan struct{}, 1)
		go m.persistWorker()
	}
	m.storeMu.Unlock()

	return m.LoadFromStore(ctx, st)
}

func (m *MetricsCollector) persistWorker() {
	for range m.persistCh {
		time.Sleep(30 * time.Millisecond)
		select {
		case <-m.persistCh:
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = m.SaveToStore(ctx)
		cancel()
	}
}

// LoadFromStore 从持久化存储加载指标数据。
func (m *MetricsCollector) LoadFromStore(ctx context.Context, st store.Store) error {
	if st == nil {
		return nil
	}
	setting, err := st.Settings().Get(ctx, MetricsSettingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	var stored StoredMetrics
	if err := json.Unmarshal(setting.ValueJSON, &stored); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for plane, count := range stored.Calls {
		c, ok := m.calls[plane]
		if !ok {
			c = &atomic.Uint64{}
			m.calls[plane] = c
		}
		c.Store(count)
	}
	for plane, count := range stored.Failures {
		f, ok := m.failures[plane]
		if !ok {
			f = &atomic.Uint64{}
			m.failures[plane] = f
		}
		f.Store(count)
	}
	for plane, count := range stored.CacheHits {
		h, ok := m.cacheHits[plane]
		if !ok {
			h = &atomic.Uint64{}
			m.cacheHits[plane] = h
		}
		h.Store(count)
	}
	for plane, count := range stored.Fallbacks {
		fb, ok := m.fallbacks[plane]
		if !ok {
			fb = &atomic.Uint64{}
			m.fallbacks[plane] = fb
		}
		fb.Store(count)
	}
	m.suppressed.Store(stored.Suppressed)
	m.skipped.Store(stored.Skipped)
	m.wouldSkip.Store(stored.WouldSkip)
	m.wouldSupp.Store(stored.WouldSuppress)

	return nil
}

// SaveToStore 将当前指标快照持久化进系统设置。
func (m *MetricsCollector) SaveToStore(ctx context.Context) error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()

	m.storeMu.Lock()
	st := m.st
	m.storeMu.Unlock()

	if st == nil {
		return nil
	}

	snap := m.Snapshot()
	stored := StoredMetrics{
		Calls:         snap.Calls,
		Failures:      snap.Failures,
		CacheHits:     snap.CacheHits,
		Fallbacks:     snap.Fallbacks,
		Suppressed:    snap.Suppressed,
		Skipped:       snap.Skipped,
		WouldSkip:     snap.WouldSkip,
		WouldSuppress: snap.WouldSuppress,
		UpdatedAt:     time.Now().UTC(),
	}

	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}

	_, err = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       MetricsSettingKey,
		ValueJSON: raw,
		UpdatedAt: stored.UpdatedAt,
		UpdatedBy: "jev_metrics",
	})
	return err
}

func (m *MetricsCollector) triggerPersistAsync() {
	m.storeMu.Lock()
	ch := m.persistCh
	m.storeMu.Unlock()
	if ch == nil {
		return
	}

	select {
	case ch <- struct{}{}:
	default:
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
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordFailure(plane string) {
	m.getCounter(m.failures, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordCacheHit(plane string) {
	m.getCounter(m.cacheHits, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordFallback(plane string) {
	m.getCounter(m.fallbacks, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordSuppressed() {
	m.suppressed.Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordSkipped() {
	m.skipped.Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordWouldSkip() {
	m.wouldSkip.Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordWouldSuppress() {
	m.wouldSupp.Add(1)
	m.triggerPersistAsync()
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
