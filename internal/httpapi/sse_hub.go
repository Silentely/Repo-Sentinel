package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	defaultSSEChannelBuffer = 64
	defaultMaxClients       = 1024
	sseHeartbeatInterval    = 15 * time.Second
	sseWriteTimeout         = 10 * time.Second
)


const (
	DeliveryStageAccepted         = "accepted"
	DeliveryStageProcessing       = "processing"
	DeliveryStageRulesEvaluated   = "rules_evaluated"
	DeliveryStageOutboxQueued     = "outbox_queued"
	DeliveryStageChannelDelivered = "channel_delivered"
)

var (
	// ErrHubClosed is returned when subscribing to a closed hub.
	ErrHubClosed = errors.New("sse hub is closed")
	// ErrMaxClientsExceeded is returned when subscriber capacity is full.
	ErrMaxClientsExceeded = errors.New("max sse clients limit reached")
)

// SSEEvent represents a sanitized, read-only event sent to frontend subscribers.
// Sensitive data (e.g. raw diffs, webhook payloads, tokens) are never broadcast.
type SSEEvent struct {
	ID         string    `json:"id"`
	Topic      string    `json:"topic"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Stage      string    `json:"stage,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	Data       any       `json:"data,omitempty"`
}

// SSEHub manages active Server-Sent Events subscriber connections.
type SSEHub struct {
	mu           sync.RWMutex
	subscribers  map[string]chan SSEEvent
	maxClients   int
	closed       bool
	droppedCount uint64
	logger       *slog.Logger
}

// NewSSEHub creates a new in-process SSE broadcast hub.
func NewSSEHub(logger *slog.Logger) *SSEHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &SSEHub{
		subscribers: make(map[string]chan SSEEvent),
		maxClients:  defaultMaxClients,
		logger:      logger,
	}
}

// Subscribe registers a new subscriber and returns a channel and client ID.
func (h *SSEHub) Subscribe() (<-chan SSEEvent, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, "", ErrHubClosed
	}

	if len(h.subscribers) >= h.maxClients {
		return nil, "", ErrMaxClientsExceeded
	}

	id := generateClientID()
	ch := make(chan SSEEvent, defaultSSEChannelBuffer)
	h.subscribers[id] = ch

	return ch, id, nil
}

// Unsubscribe removes a subscriber by ID.
// The channel is deleted from the active registry; garbage collection cleans it up
// without risking concurrent send-on-closed-channel panics.
func (h *SSEHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.subscribers, id)
}

// Broadcast sends an event to all active subscribers non-blockingly.
// Uses non-blocking select under read lock. Because delivery is non-blocking (buffered channel),
// read lock is held for less than a few microseconds, eliminating lock contention while
// completely preventing send-on-closed-channel races during Close().
func (h *SSEHub) Broadcast(event SSEEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed || len(h.subscribers) == 0 {
		return
	}

	var dropped uint64
	for _, ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			dropped++
		}
	}

	if dropped > 0 {
		atomic.AddUint64(&h.droppedCount, dropped)
	}
}

// Close shuts down the hub and disconnects all clients.
func (h *SSEHub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}
	h.closed = true

	for id, ch := range h.subscribers {
		delete(h.subscribers, id)
		close(ch)
	}
	return nil
}

// ActiveCount returns current number of active subscribers.
func (h *SSEHub) ActiveCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// DroppedCount returns total number of dropped events due to buffer full.
func (h *SSEHub) DroppedCount() uint64 {
	return atomic.LoadUint64(&h.droppedCount)
}

func generateClientID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sub-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// broadcastResource 向 SSE 订阅者广播资源变更事件；未装配 hub 时静默忽略。
// 资源变更接口（忽略标记、批量操作等）在写库成功后调用，让其他标签页局部失效缓存。
func (s *server) broadcastResource(topic, resource string, resourceIDs ...string) {
	if s.sseHub == nil {
		return
	}
	occurredAt := time.Now().UTC()
	for _, id := range resourceIDs {
		s.sseHub.Broadcast(SSEEvent{
			ID:         ulid.Make().String(),
			Topic:      topic,
			Version:    1,
			OccurredAt: occurredAt,
			Resource:   resource,
			ResourceID: id,
		})
	}
}

func (s *server) broadcastDeliveryStage(stage, deliveryID string, durationMS int64, detail string, data any) {
	if s.sseHub == nil {
		return
	}
	s.sseHub.Broadcast(SSEEvent{
		ID:         ulid.Make().String(),
		Topic:      "delivery.stage",
		Version:    1,
		OccurredAt: time.Now().UTC(),
		Resource:   "webhook_delivery",
		ResourceID: deliveryID,
		Stage:      stage,
		DurationMS: durationMS,
		Detail:     detail,
		Data:       data,
	})
}

// handleEventStream handles GET /api/v1/events/stream.
func (s *server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if s.sseHub == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}

	ch, clientID, err := s.sseHub.Subscribe()
	if err != nil {
		if errors.Is(err, ErrMaxClientsExceeded) {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, map[string]any{"reason": "max_clients"})
			return
		}
		s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		return
	}
	defer s.sseHub.Unsubscribe(clientID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	w.WriteHeader(http.StatusOK)

	// Send initial connection comment
	_, _ = fmt.Fprint(w, ": ok\n\n")
	_ = rc.Flush()
	_ = rc.SetWriteDeadline(time.Time{})

	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-ticker.C:
			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			_ = rc.SetWriteDeadline(time.Time{})

		case event, ok := <-ch:
			if !ok {
				// Hub closed
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}

			_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
			if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Topic, payload); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			_ = rc.SetWriteDeadline(time.Time{})
		}
	}
}
