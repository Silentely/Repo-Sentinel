package httpapi

import (
	"bufio"
	"net"
	"net/http/httptest"

	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestSSEHub_SubscribeAndUnsubscribe(t *testing.T) {
	hub := NewSSEHub(nil)
	defer hub.Close()

	ch, id, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" || ch == nil {
		t.Fatalf("expected non-empty id and channel, got id=%s", id)
	}

	if hub.ActiveCount() != 1 {
		t.Fatalf("expected 1 active subscriber, got %d", hub.ActiveCount())
	}

	// Unsubscribe is idempotent
	hub.Unsubscribe(id)
	hub.Unsubscribe(id)

	if hub.ActiveCount() != 0 {
		t.Fatalf("expected 0 active subscribers, got %d", hub.ActiveCount())
	}
}

func TestSSEHub_Broadcast(t *testing.T) {
	hub := NewSSEHub(nil)
	defer hub.Close()

	ch1, id1, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("sub 1 error: %v", err)
	}
	defer hub.Unsubscribe(id1)

	ch2, id2, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("sub 2 error: %v", err)
	}
	defer hub.Unsubscribe(id2)

	evt := SSEEvent{
		ID:         "evt-1",
		Topic:      "events.created",
		Version:    1,
		OccurredAt: time.Now(),
		Resource:   "event",
		ResourceID: "row-123",
	}

	hub.Broadcast(evt)

	select {
	case msg := <-ch1:
		if msg.ID != "evt-1" || msg.Topic != "events.created" {
			t.Fatalf("unexpected event on ch1: %+v", msg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ch1 timed out waiting for event")
	}

	select {
	case msg := <-ch2:
		if msg.ID != "evt-1" || msg.ResourceID != "row-123" {
			t.Fatalf("unexpected event on ch2: %+v", msg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ch2 timed out waiting for event")
	}
}

func TestSSEHub_SlowClientProtectionAndDropCount(t *testing.T) {
	hub := NewSSEHub(nil)
	defer hub.Close()

	// Slow client: does not read from channel
	slowCh, slowID, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("sub slow error: %v", err)
	}
	defer hub.Unsubscribe(slowID)

	// Normal client: continuously drains channel
	normalCh, normalID, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("sub normal error: %v", err)
	}
	defer hub.Unsubscribe(normalID)

	var normalReceived int64
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-normalCh:
				if !ok {
					return
				}
				atomic.AddInt64(&normalReceived, 1)
			}
		}
	}()

	// Broadcast more events than the channel buffer (default 64)
	totalEvents := 100
	for i := 0; i < totalEvents; i++ {
		hub.Broadcast(SSEEvent{
			ID:         fmt.Sprintf("evt-%d", i),
			Topic:      "outbox.changed",
			Version:    1,
			OccurredAt: time.Now(),
			Resource:   "outbox",
			ResourceID: fmt.Sprintf("row-%d", i),
		})
		time.Sleep(500 * time.Microsecond)
	}

	// Wait briefly for normal client to receive
	time.Sleep(100 * time.Millisecond)

	// Slow channel should have buffer full (64) and dropped events > 0
	if len(slowCh) != defaultSSEChannelBuffer {
		t.Fatalf("expected slow channel to be full (%d), got %d", defaultSSEChannelBuffer, len(slowCh))
	}

	if hub.DroppedCount() == 0 {
		t.Fatalf("expected dropped count > 0 for slow client, got %d", hub.DroppedCount())
	}

	// Normal client should have received all 100
	if atomic.LoadInt64(&normalReceived) != int64(totalEvents) {
		t.Fatalf("expected normal client to receive %d events, got %d", totalEvents, atomic.LoadInt64(&normalReceived))
	}

	hub.Unsubscribe(normalID)
	cancel()
	<-done
}

func TestSSEHub_MaxSubscribersLimit(t *testing.T) {
	hub := NewSSEHub(nil)
	hub.maxClients = 5
	defer hub.Close()

	var ids []string
	for i := 0; i < 5; i++ {
		_, id, err := hub.Subscribe()
		if err != nil {
			t.Fatalf("failed to subscribe %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// 6th subscriber should be rejected
	_, _, err := hub.Subscribe()
	if err == nil {
		t.Fatal("expected error on exceeding max subscribers, got nil")
	}

	// After one leaves, can subscribe again
	hub.Unsubscribe(ids[0])
	_, newID, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("expected success after unsubscribe, got %v", err)
	}
	hub.Unsubscribe(newID)
}

func TestSSEHub_CloseClosesSubscribers(t *testing.T) {
	hub := NewSSEHub(nil)
	ch, _, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	if err := hub.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, but was open")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for subscriber channel close")
	}

	// Further subscribes should fail
	_, _, err = hub.Subscribe()
	if err == nil {
		t.Fatal("expected error subscribing to closed hub")
	}
}

func TestSSEHub_ConcurrentBroadcastAndChurn(t *testing.T) {
	hub := NewSSEHub(nil)
	defer hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Subscriber churn goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					ch, id, err := hub.Subscribe()
					if err == nil {
						// Read a few messages or yield
						select {
						case <-ch:
						case <-time.After(5 * time.Millisecond):
						}
						hub.Unsubscribe(id)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Broadcaster goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			seq := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					hub.Broadcast(SSEEvent{
						ID:         fmt.Sprintf("evt-%d-%d", workerID, seq),
						Topic:      "events.created",
						Version:    1,
						OccurredAt: time.Now(),
						Resource:   "event",
						ResourceID: fmt.Sprintf("res-%d", seq),
					})
					seq++
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestSSEHub_HTTPStream_EndToEnd 走真实路由与完整中间件链（含 chi Compress 与
// accessLog/recovery 包装层），验证 SSE 首字节与广播事件确实送达客户端。
// 历史上该链路曾因包装层缺失 Flush 被静默吞掉（客户端零字节），此测试防止回归。
func TestSSEHub_HTTPStream_EndToEnd(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	hub := NewSSEHub(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	handler := New(Dependencies{
		Config:         config.Config{HTTP: config.HTTPConfig{PublicBaseURL: "https://reposentinel.example"}},
		Store:          fixture.store,
		AdminService:   fixture.adminService,
		SessionService: fixture.sessionService,
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		SSEHub:         hub,
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer conn.Close()

	var cookieHeader []string
	for _, c := range cookies {
		cookieHeader = append(cookieHeader, c.Name+"="+c.Value)
	}
	_, _ = fmt.Fprintf(conn, "GET /api/v1/events/stream HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\nAccept: text/event-stream\r\n\r\n",
		addr, strings.Join(cookieHeader, "; "))

	reader := bufio.NewReader(conn)
	// 首字节必须在超时内到达：任一包装层吞掉 flush 时会在此超时。
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var head strings.Builder
	for !strings.Contains(head.String(), ": ok") {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("SSE 首字节未在超时内送达（flush 链失效？）: %v", err)
		}
		head.WriteString(line)
	}
	if !strings.HasPrefix(head.String(), "HTTP/1.1 200 OK") {
		t.Fatalf("意外响应头: %q", head.String())
	}
	if !strings.Contains(head.String(), "Content-Type: text/event-stream") {
		t.Fatalf("响应 Content-Type 异常: %q", head.String())
	}

	// 广播事件必须能到达客户端。
	hub.Broadcast(SSEEvent{
		ID:         "evt-e2e-1",
		Topic:      "events.created",
		Version:    1,
		OccurredAt: time.Now().UTC(),
		Resource:   "event",
		ResourceID: "row-1",
	})
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var payload strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		payload.WriteString(line)
		// 事件帧为 id/event/data + 结尾空行：读到 event 行即已覆盖帧内关键字段。
		if strings.Contains(payload.String(), "event: events.created") {
			break
		}
	}
	if !strings.Contains(payload.String(), "evt-e2e-1") {
		t.Fatalf("广播事件未送达客户端，实际=%q", payload.String())
	}
	if !strings.Contains(payload.String(), "event: events.created") {
		t.Fatalf("缺少事件名行，实际=%q", payload.String())
	}
}
