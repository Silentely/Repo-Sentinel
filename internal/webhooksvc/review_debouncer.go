package webhooksvc

import (
	"sync"
	"time"
)

const defaultReviewDebounceDelay = 60 * time.Second

type reviewDebounceEntry struct {
	timer *time.Timer
	req   prReviewRequest
	key   string
}

// reviewDebouncer 维护 PR 审查防抖桶。
// 当连续 push 多个 commit 到同一 PR 时，重置 60s 防抖计时器，最终仅触发一次最新 commit 的审查。
type reviewDebouncer struct {
	mu      sync.Mutex
	entries map[string]*reviewDebounceEntry
	stopped bool
}

func newReviewDebouncer() *reviewDebouncer {
	return &reviewDebouncer{
		entries: make(map[string]*reviewDebounceEntry),
	}
}

func (d *reviewDebouncer) schedule(prefix, key string, req prReviewRequest, delay time.Duration, onFire func(key string, req prReviewRequest)) {
	if d == nil {
		onFire(key, req)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	if d.entries == nil {
		d.entries = make(map[string]*reviewDebounceEntry)
	}

	if existing, ok := d.entries[prefix]; ok {
		existing.timer.Stop()
	}

	entry := &reviewDebounceEntry{
		key: key,
		req: req,
	}
	entry.timer = time.AfterFunc(delay, func() {
		d.mu.Lock()
		if d.stopped {
			d.mu.Unlock()
			return
		}
		curr, ok := d.entries[prefix]
		if !ok || curr != entry {
			d.mu.Unlock()
			return
		}
		delete(d.entries, prefix)
		d.mu.Unlock()

		onFire(entry.key, entry.req)
	})
	d.entries[prefix] = entry
}

func (d *reviewDebouncer) cancel(prefix string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, ok := d.entries[prefix]; ok {
		entry.timer.Stop()
		delete(d.entries, prefix)
	}
}

func (d *reviewDebouncer) stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	for _, entry := range d.entries {
		entry.timer.Stop()
	}
	d.entries = make(map[string]*reviewDebounceEntry)
}

func (d *reviewDebouncer) pendingCount() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}
