package webhooksvc

import (
	"context"
	"sync"
)

// reviewTracker 跟踪在途 PR 审查任务，承担两项职责：
//  1. 互斥：同一 PR 同一提交只允许一个审查任务在途（acquire 返回 false）。
//  2. 停机排空：Close 先 stop 拒绝登记新任务，再 wait 等待在途任务清空，
//     保证排空期间启动的任务不会写已关闭的数据库。
//
// 登记与等待共用同一互斥锁：WaitGroup 的「计数为零时的正数 Add 必须先于 Wait」
// 契约在排空窗口内无法保证（在途 webhook 处理可能恰在此时登记新任务），
// 同锁互斥从根本上消除该竞态。
type reviewTracker struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inFlight map[string]struct{}
	stopped  bool
}

// acquire 登记任务；key 已在途或已进入停机状态时返回 false。
// 未装配（零值 nil，测试直构 Service）时放行：不去重、不排空。
func (t *reviewTracker) acquire(key string) bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return false
	}
	if t.inFlight == nil {
		t.inFlight = make(map[string]struct{})
	}
	if _, exists := t.inFlight[key]; exists {
		return false
	}
	t.inFlight[key] = struct{}{}
	return true
}

// release 释放任务并唤醒排空等待者；在途任务清空时广播一次。
func (t *reviewTracker) release(key string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inFlight, key)
	if t.cond != nil && len(t.inFlight) == 0 {
		t.cond.Broadcast()
	}
}

// stop 进入停机状态：拒绝登记新任务（App.Close 在 wait 之前调用）。
func (t *reviewTracker) stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

// wait 阻塞直到在途任务清空或 ctx 超时/取消。
func (t *reviewTracker) wait(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cond == nil {
		t.cond = sync.NewCond(&t.mu)
	}
	// ctx 结束时广播唤醒等待者检查退出条件；wait 返回后经 wake 通知该 goroutine 退出。
	wake := make(chan struct{})
	defer close(wake)
	go func() {
		select {
		case <-ctx.Done():
			t.cond.Broadcast()
		case <-wake:
		}
	}()
	for len(t.inFlight) > 0 {
		t.cond.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}
