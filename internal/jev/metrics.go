package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strings"
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

// ClassifyError 将 Jev 决策错误归类为标准 error_class。
// 可选枚举值：timeout / 4xx / 5xx / decode / breaker_open / unknown。
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrCircuitOpen) {
		return "breaker_open"
	}
	if errors.Is(err, ErrRateLimited) {
		return "rate_limited"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var upErr *UpstreamError
	if errors.As(err, &upErr) {
		if upErr.StatusCode >= 500 {
			return "5xx"
		}
		return "timeout"
	}
	var clErr *ClientHTTPError
	if errors.As(err, &clErr) {
		return "4xx"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return "timeout"
	}
	if errors.Is(err, ErrTruncatedState) {
		return "truncated"
	}
	if errors.Is(err, ErrMalformedDecision) || errors.Is(err, ErrStateTooLarge) {
		return "decode"
	}
	if strings.Contains(msg, "malformed") || strings.Contains(msg, "unmarshal") || strings.Contains(msg, "unexpected end of json") {
		return "decode"
	}
	if strings.Contains(msg, "client error 4") || strings.Contains(msg, " 400") || strings.Contains(msg, " 401") ||
		strings.Contains(msg, " 403") || strings.Contains(msg, " 404") || strings.Contains(msg, " 422") || strings.Contains(msg, " 429") {
		return "4xx"
	}
	if strings.Contains(msg, "server returned 5") || strings.Contains(msg, " 500") || strings.Contains(msg, " 502") ||
		strings.Contains(msg, " 503") || strings.Contains(msg, " 504") || strings.Contains(msg, " 529") {
		return "5xx"
	}
	return "unknown"
}

// StoredMetrics 描述在数据库中持久化的 Jev 运行时指标结构。
type StoredMetrics struct {
	Calls         map[string]uint64            `json:"calls,omitempty"`
	Successes     map[string]uint64            `json:"successes,omitempty"`
	Failures      map[string]uint64            `json:"failures,omitempty"`
	CacheHits     map[string]uint64            `json:"cache_hits,omitempty"`
	CacheMisses   map[string]uint64            `json:"cache_misses,omitempty"`
	CacheEvicts   map[string]uint64            `json:"cache_evicts,omitempty"`
	Fallbacks     map[string]uint64            `json:"fallbacks,omitempty"`
	FailureClass  map[string]map[string]uint64 `json:"failure_class,omitempty"`
	Suppressed    uint64                       `json:"suppressed,omitempty"`
	Skipped       uint64                       `json:"skipped,omitempty"`
	WouldSkip     uint64                       `json:"would_skip,omitempty"`
	WouldSuppress uint64                       `json:"would_suppress,omitempty"`
	UpdatedAt     time.Time                    `json:"updated_at,omitempty"`
}

type MetricsSnapshot struct {
	Calls         map[string]uint64
	Successes     map[string]uint64
	Failures      map[string]uint64
	CacheHits     map[string]uint64
	CacheMisses   map[string]uint64
	CacheEvicts   map[string]uint64
	Fallbacks     map[string]uint64
	FailureClass  map[string]map[string]uint64
	LatencyP50    map[string]float64
	LatencyP95    map[string]float64
	Suppressed    uint64
	Skipped       uint64
	WouldSkip     uint64
	WouldSuppress uint64
}

type latencyReservoir struct {
	mu      sync.Mutex
	samples []float64
	idx     int
}

func newLatencyReservoir(cap int) *latencyReservoir {
	return &latencyReservoir{
		samples: make([]float64, 0, cap),
	}
}

func (lr *latencyReservoir) record(val float64) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if len(lr.samples) < cap(lr.samples) {
		lr.samples = append(lr.samples, val)
		return
	}
	lr.samples[lr.idx] = val
	lr.idx = (lr.idx + 1) % cap(lr.samples)
}

func (lr *latencyReservoir) percentiles() (p50, p95 float64) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	n := len(lr.samples)
	if n == 0 {
		return 0, 0
	}
	cp := make([]float64, n)
	copy(cp, lr.samples)
	sort.Float64s(cp)

	p50Idx := n * 50 / 100
	if p50Idx >= n {
		p50Idx = n - 1
	}
	p95Idx := n * 95 / 100
	if p95Idx >= n {
		p95Idx = n - 1
	}
	return cp[p50Idx], cp[p95Idx]
}

type MetricsCollector struct {
	mu           sync.RWMutex
	calls        map[string]*atomic.Uint64
	successes    map[string]*atomic.Uint64
	failures     map[string]*atomic.Uint64
	cacheHits    map[string]*atomic.Uint64
	cacheMisses  map[string]*atomic.Uint64
	cacheEvicts  map[string]*atomic.Uint64
	fallbacks    map[string]*atomic.Uint64
	failureClass map[string]map[string]*atomic.Uint64
	latencies    map[string]*latencyReservoir
	suppressed   atomic.Uint64
	skipped      atomic.Uint64
	wouldSkip    atomic.Uint64
	wouldSupp    atomic.Uint64

	saveMu    sync.Mutex
	storeMu   sync.Mutex
	st        store.Store
	persistCh chan struct{}
}

var globalMetrics = newMetricsCollector()

func newMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		calls:        make(map[string]*atomic.Uint64),
		successes:    make(map[string]*atomic.Uint64),
		failures:     make(map[string]*atomic.Uint64),
		cacheHits:    make(map[string]*atomic.Uint64),
		cacheMisses:  make(map[string]*atomic.Uint64),
		cacheEvicts:  make(map[string]*atomic.Uint64),
		fallbacks:    make(map[string]*atomic.Uint64),
		failureClass: make(map[string]map[string]*atomic.Uint64),
		latencies:    make(map[string]*latencyReservoir),
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
	for plane, count := range stored.Successes {
		s, ok := m.successes[plane]
		if !ok {
			s = &atomic.Uint64{}
			m.successes[plane] = s
		}
		s.Store(count)
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
	for plane, count := range stored.CacheMisses {
		ms, ok := m.cacheMisses[plane]
		if !ok {
			ms = &atomic.Uint64{}
			m.cacheMisses[plane] = ms
		}
		ms.Store(count)
	}
	for plane, count := range stored.CacheEvicts {
		ev, ok := m.cacheEvicts[plane]
		if !ok {
			ev = &atomic.Uint64{}
			m.cacheEvicts[plane] = ev
		}
		ev.Store(count)
	}
	for plane, count := range stored.Fallbacks {
		fb, ok := m.fallbacks[plane]
		if !ok {
			fb = &atomic.Uint64{}
			m.fallbacks[plane] = fb
		}
		fb.Store(count)
	}
	for plane, classMap := range stored.FailureClass {
		pMap, ok := m.failureClass[plane]
		if !ok {
			pMap = make(map[string]*atomic.Uint64)
			m.failureClass[plane] = pMap
		}
		for cls, count := range classMap {
			c, ok := pMap[cls]
			if !ok {
				c = &atomic.Uint64{}
				pMap[cls] = c
			}
			c.Store(count)
		}
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
		Successes:     snap.Successes,
		Failures:      snap.Failures,
		CacheHits:     snap.CacheHits,
		CacheMisses:   snap.CacheMisses,
		CacheEvicts:   snap.CacheEvicts,
		Fallbacks:     snap.Fallbacks,
		FailureClass:  snap.FailureClass,
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

func (m *MetricsCollector) RecordSuccess(plane string) {
	m.getCounter(m.successes, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordFailure(plane string) {
	m.getCounter(m.failures, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordFailureClass(plane, errorClass string) {
	if errorClass == "" {
		errorClass = "unknown"
	}
	m.mu.Lock()
	pMap, ok := m.failureClass[plane]
	if !ok {
		pMap = make(map[string]*atomic.Uint64)
		m.failureClass[plane] = pMap
	}
	c, ok := pMap[errorClass]
	if !ok {
		c = &atomic.Uint64{}
		pMap[errorClass] = c
	}
	m.mu.Unlock()

	c.Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordCacheHit(plane string) {
	m.getCounter(m.cacheHits, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordCacheMiss(plane string) {
	m.getCounter(m.cacheMisses, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordCacheEviction(plane string) {
	m.getCounter(m.cacheEvicts, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordFallback(plane string) {
	m.getCounter(m.fallbacks, plane).Add(1)
	m.triggerPersistAsync()
}

func (m *MetricsCollector) RecordLatency(plane string, latencyMs float64) {
	m.mu.Lock()
	res, ok := m.latencies[plane]
	if !ok {
		res = newLatencyReservoir(1024)
		m.latencies[plane] = res
	}
	m.mu.Unlock()

	res.record(latencyMs)
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
		Successes:     make(map[string]uint64, len(m.successes)),
		Failures:      make(map[string]uint64, len(m.failures)),
		CacheHits:     make(map[string]uint64, len(m.cacheHits)),
		CacheMisses:   make(map[string]uint64, len(m.cacheMisses)),
		CacheEvicts:   make(map[string]uint64, len(m.cacheEvicts)),
		Fallbacks:     make(map[string]uint64, len(m.fallbacks)),
		FailureClass:  make(map[string]map[string]uint64, len(m.failureClass)),
		LatencyP50:    make(map[string]float64, len(m.latencies)),
		LatencyP95:    make(map[string]float64, len(m.latencies)),
		Suppressed:    m.suppressed.Load(),
		Skipped:       m.skipped.Load(),
		WouldSkip:     m.wouldSkip.Load(),
		WouldSuppress: m.wouldSupp.Load(),
	}

	for k, v := range m.calls {
		snap.Calls[k] = v.Load()
	}
	for k, v := range m.successes {
		snap.Successes[k] = v.Load()
	}
	for k, v := range m.failures {
		snap.Failures[k] = v.Load()
	}
	for k, v := range m.cacheHits {
		snap.CacheHits[k] = v.Load()
	}
	for k, v := range m.cacheMisses {
		snap.CacheMisses[k] = v.Load()
	}
	for k, v := range m.cacheEvicts {
		snap.CacheEvicts[k] = v.Load()
	}
	for k, v := range m.fallbacks {
		snap.Fallbacks[k] = v.Load()
	}
	for plane, classMap := range m.failureClass {
		pMap := make(map[string]uint64, len(classMap))
		for cls, v := range classMap {
			pMap[cls] = v.Load()
		}
		snap.FailureClass[plane] = pMap
	}
	for plane, res := range m.latencies {
		p50, p95 := res.percentiles()
		snap.LatencyP50[plane] = p50
		snap.LatencyP95[plane] = p95
	}

	return snap
}

// GlobalMetrics 返回全局指标收集器。
func GlobalMetrics() *MetricsCollector {
	return globalMetrics
}
