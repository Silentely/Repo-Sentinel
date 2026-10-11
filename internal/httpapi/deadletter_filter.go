package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/notify"
)

// DeadLetterFilterParams 死信查询筛选条件
type DeadLetterFilterParams struct {
	Channel   string    `json:"channel"`
	Category  string    `json:"category"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Limit     int       `json:"limit"`
}

// DeadLetterItem 描述单条死信展示模型
type DeadLetterItem struct {
	ID               string                   `json:"id"`
	Channel          string                   `json:"channel"`
	ErrorCode        string                   `json:"error_code"`
	Category         notify.DeadCauseCategory `json:"category"`
	RetryRecommended bool                     `json:"retry_recommended"`
	Hint             string                   `json:"hint"`
	Timestamp        time.Time                `json:"timestamp"`
	PayloadSummary   string                   `json:"payload_summary"`
}

// FilterDeadLetters 在死信切片中执行多维度过滤
func FilterDeadLetters(records []DeadLetterItem, params DeadLetterFilterParams) []DeadLetterItem {
	if params.Limit <= 0 {
		params.Limit = 50
	}

	var matched []DeadLetterItem
	for _, rec := range records {
		if params.Channel != "" && !strings.EqualFold(rec.Channel, params.Channel) {
			continue
		}
		if params.Category != "" && !strings.EqualFold(string(rec.Category), params.Category) {
			continue
		}
		if !params.StartTime.IsZero() && rec.Timestamp.Before(params.StartTime) {
			continue
		}
		if !params.EndTime.IsZero() && rec.Timestamp.After(params.EndTime) {
			continue
		}

		matched = append(matched, rec)
		if len(matched) >= params.Limit {
			break
		}
	}

	return matched
}

// FilterDeadLettersHandler 返回死信过滤 HTTP Handler
func FilterDeadLettersHandler(provider func() []DeadLetterItem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		params := DeadLetterFilterParams{
			Channel:  q.Get("channel"),
			Category: q.Get("category"),
		}
		var err error
		if raw := q.Get("start_time"); raw != "" {
			params.StartTime, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, "invalid start_time", http.StatusBadRequest)
				return
			}
		}
		if raw := q.Get("end_time"); raw != "" {
			params.EndTime, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, "invalid end_time", http.StatusBadRequest)
				return
			}
		}
		if raw := q.Get("limit"); raw != "" {
			params.Limit, err = strconv.Atoi(raw)
			if err != nil || params.Limit <= 0 || params.Limit > 500 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
		}

		var items []DeadLetterItem
		if provider != nil {
			items = provider()
		}

		filtered := FilterDeadLetters(items, params)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count": len(filtered),
			"items": filtered,
		})
	}
}
