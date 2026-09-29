package httpapi

import (
	"net/http"
	"net/http/httptest"

	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestSSEHub_HTTPStream_Connection(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/events/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.handler.ServeHTTP(w, req)
	}()

	select {
	case <-time.After(500 * time.Millisecond):
		cancel()
	case <-done:
	}
	<-done

	t.Logf("Response code: %d, body: %q, headers: %v", w.Code, w.Body.String(), w.Header())
}
