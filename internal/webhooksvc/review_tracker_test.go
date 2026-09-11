package webhooksvc

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestReviewTrackerAcquireRelease 守护互斥语义：同 key 互斥、不同 key 并行、release 后可复用。
func TestReviewTrackerAcquireRelease(t *testing.T) {
	tr := &reviewTracker{}

	if !tr.acquire("repo#1#sha-a") {
		t.Fatal("首次登记应成功")
	}
	if tr.acquire("repo#1#sha-a") {
		t.Fatal("同 key 在途时应拒绝重复登记")
	}
	if !tr.acquire("repo#1#sha-b") {
		t.Fatal("不同 key 应允许并行登记")
	}

	tr.release("repo#1#sha-a")
	if !tr.acquire("repo#1#sha-a") {
		t.Fatal("释放后同 key 应可再次登记")
	}
}

// TestReviewTrackerWait 守护排空语义：在途任务清空前 wait 阻塞，清空后返回。
func TestReviewTrackerWait(t *testing.T) {
	tr := &reviewTracker{}
	_ = tr.acquire("repo#1#sha-a")

	waitDone := make(chan error, 1)
	go func() { waitDone <- tr.wait(context.Background()) }()

	// wait 尚未排空：短暂等待后任务应仍在途。
	select {
	case err := <-waitDone:
		t.Fatalf("在途任务未清空时 wait 不应返回，got %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	tr.release("repo#1#sha-a")
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("排空后 wait 应返回 nil，got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("排空后 wait 应返回")
	}
}

// TestReviewTrackerWaitContextCancel 守护带超时的排空等待：ctx 到期返回 ctx 错误。
func TestReviewTrackerWaitContextCancel(t *testing.T) {
	tr := &reviewTracker{}
	_ = tr.acquire("repo#1#sha-a")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := tr.wait(ctx); err == nil {
		t.Fatal("ctx 到期且仍有在途任务时 wait 应返回错误")
	}
}

// TestReviewTrackerStop 守护停机语义：stop 后拒绝登记新任务，且排空等待可在
// 「wait 进行中新任务被拒绝」的窗口内保持一致（Add-vs-Wait 竞态由同锁互斥消除）。
func TestReviewTrackerStop(t *testing.T) {
	tr := &reviewTracker{}
	tr.stop()
	if tr.acquire("repo#1#sha-a") {
		t.Fatal("停机后应拒绝登记新任务")
	}
	if err := tr.wait(context.Background()); err != nil {
		t.Fatalf("停机且无在途任务时 wait 应立即返回，got %v", err)
	}
}

// TestReviewTrackerConcurrentAddWhileWaiting 复现原 WaitGroup 的 Add-vs-Wait 竞态场景：
// 大量 goroutine 在 wait 观察空集合前后并发登记/释放，同锁互斥下不允许出现
// 数据竞争（配合 -race 运行）或漏排空。
func TestReviewTrackerConcurrentAddWhileWaiting(t *testing.T) {
	tr := &reviewTracker{}
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		key := "repo#1#sha-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !tr.acquire(key) {
				return
			}
			tr.release(key)
		}()
	}
	if err := tr.wait(context.Background()); err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	wg.Wait()
	// 全部登记/释放结束后集合必须为空。
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.inFlight) != 0 {
		t.Fatalf("expected empty inFlight, got %d", len(tr.inFlight))
	}
}

// TestReviewTrackerNilSafe 守护零值 nil 跟踪器的放行语义（测试直构 Service 未装配跟踪器）。
func TestReviewTrackerNilSafe(t *testing.T) {
	var tr *reviewTracker
	if !tr.acquire("k") {
		t.Fatal("nil 跟踪器应放行登记")
	}
	tr.release("k")
	tr.stop()
	if err := tr.wait(context.Background()); err != nil {
		t.Fatalf("nil 跟踪器 wait 应返回 nil，got %v", err)
	}
}
