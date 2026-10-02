package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestSendSlack_BlockKitPayload(t *testing.T) {
	var receivedPayload map[string]any

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedPayload); err != nil {
			t.Errorf("unmarshal slack payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	w := &Worker{Client: srv.Client()}
	ch := store.NotificationChannel{
		Target:       srv.URL,
		AllowPrivate: true,
	}
	item := store.NotificationOutbox{
		Title:    "🚨 [Critical] Security Vulnerability Found",
		BodyText: "<b>Alert</b>: A critical leak was detected in repo <code>main</code> branch.",
		HTMLURL:  "https://github.com/org/repo/security/advisories/1",
	}

	if err := w.sendSlack(t.Context(), ch, "", item); err != nil {
		t.Fatalf("sendSlack error: %v", err)
	}

	if receivedPayload == nil {
		t.Fatal("expected payload to be received")
	}

	blocks, ok := receivedPayload["blocks"].([]any)
	if !ok || len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks (header, section, actions), got %v", blocks)
	}

	// Block 0: Header
	headerBlock := blocks[0].(map[string]any)
	if headerBlock["type"] != "header" {
		t.Errorf("expected header block type, got %v", headerBlock["type"])
	}
	headerText := headerBlock["text"].(map[string]any)["text"].(string)
	if !strings.Contains(headerText, "Security Vulnerability Found") {
		t.Errorf("unexpected header text: %s", headerText)
	}

	// Block 1: Section
	sectionBlock := blocks[1].(map[string]any)
	if sectionBlock["type"] != "section" {
		t.Errorf("expected section block type, got %v", sectionBlock["type"])
	}

	// Block 2: Actions with GitHub button
	actionBlock := blocks[2].(map[string]any)
	if actionBlock["type"] != "actions" {
		t.Errorf("expected actions block type, got %v", actionBlock["type"])
	}
	elements := actionBlock["elements"].([]any)
	button := elements[0].(map[string]any)
	if button["type"] != "button" || button["url"] != item.HTMLURL {
		t.Errorf("unexpected button element: %+v", button)
	}
}

func TestSendSlack_DeliverRouter(t *testing.T) {
	w, ch, item := newChannelTestHarness(t, store.ChannelSlack, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
	})

	channelType, err := w.deliver(t.Context(), item, map[string]store.NotificationChannel{ch.ID: ch}, make(map[string]string))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelType != store.ChannelSlack {
		t.Fatalf("expected channel type slack, got %s", channelType)
	}
}

func TestSendSlack_ErrorClassification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate_limited"))
	}))
	defer srv.Close()

	w := &Worker{Client: srv.Client()}
	ch := store.NotificationChannel{
		Target:       srv.URL,
		AllowPrivate: true,
	}
	item := store.NotificationOutbox{
		Title:    "Test alert",
		BodyText: "Test body",
	}

	err := w.sendSlack(t.Context(), ch, "", item)
	if err == nil {
		t.Fatal("expected error for 429")
	}
	code := deliveryErrorCode(err)
	if code != "slack_retry_after" {
		t.Fatalf("expected error code slack_retry_after, got %s (err: %v)", code, err)
	}
}
