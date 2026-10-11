package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"repo-sentinel/internal/notify"
)

func TestFilterDeadLetters(t *testing.T) {
	now := time.Now()
	items := []DeadLetterItem{
		{
			ID:               "dl-1",
			Channel:          "telegram",
			ErrorCode:        "timeout",
			Category:         notify.DeadCauseTransient,
			RetryRecommended: true,
			Timestamp:        now.Add(-10 * time.Minute),
		},
		{
			ID:               "dl-2",
			Channel:          "feishu",
			ErrorCode:        "invalid_token",
			Category:         notify.DeadCauseConfiguration,
			RetryRecommended: false,
			Timestamp:        now.Add(-5 * time.Minute),
		},
		{
			ID:               "dl-3",
			Channel:          "telegram",
			ErrorCode:        "message_too_long",
			Category:         notify.DeadCausePayload,
			RetryRecommended: false,
			Timestamp:        now.Add(-1 * time.Minute),
		},
	}

	// 按 channel 筛选
	filteredTelegram := FilterDeadLetters(items, DeadLetterFilterParams{Channel: "telegram"})
	if len(filteredTelegram) != 2 {
		t.Fatalf("expected 2 telegram items, got %d", len(filteredTelegram))
	}

	// 按 category 筛选
	filteredTransient := FilterDeadLetters(items, DeadLetterFilterParams{Category: "transient"})
	if len(filteredTransient) != 1 || filteredTransient[0].ID != "dl-1" {
		t.Fatalf("expected 1 transient item (dl-1), got %d", len(filteredTransient))
	}
}

func TestFilterDeadLettersHandler(t *testing.T) {
	items := []DeadLetterItem{
		{
			ID:               "dl-1",
			Channel:          "slack",
			ErrorCode:        "timeout",
			Category:         notify.DeadCauseTransient,
			RetryRecommended: true,
		},
	}

	handler := FilterDeadLettersHandler(func() []DeadLetterItem {
		return items
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dead-letters?channel=slack", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp struct {
		Count int              `json:"count"`
		Items []DeadLetterItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Count != 1 || resp.Items[0].Channel != "slack" {
		t.Errorf("unexpected response body: %+v", resp)
	}
}
