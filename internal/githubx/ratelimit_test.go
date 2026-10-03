package githubx

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestParseRateLimitError(t *testing.T) {
	// Case 1: nil response
	if ok, err := parseRateLimitError(nil, nil); ok || err != nil {
		t.Fatalf("expected (false, nil) for nil response, got (%v, %v)", ok, err)
	}

	// Case 2: 200 OK
	okResp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	if ok, err := parseRateLimitError(okResp, []byte("ok")); ok || err != nil {
		t.Fatalf("expected (false, nil) for 200 OK, got (%v, %v)", ok, err)
	}

	// Case 3: 429 Too Many Requests with Retry-After header
	resp429 := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"42"}},
		Body:       io.NopCloser(bytes.NewBufferString("too many requests")),
	}
	if ok, err := parseRateLimitError(resp429, []byte("too many requests")); !ok || err == nil || err.RetryAfter != 42*time.Second {
		t.Fatalf("expected 429 with 42s RetryAfter, got ok=%v, err=%v", ok, err)
	}

	// Case 4: 403 Secondary Rate Limit without Retry-After (fallback to 60s)
	respSecondary := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     make(http.Header),
	}
	secondaryBody := []byte(`{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`)
	if ok, err := parseRateLimitError(respSecondary, secondaryBody); !ok || err == nil || err.RetryAfter != 60*time.Second {
		t.Fatalf("expected secondary rate limit with 60s fallback, got ok=%v, err=%v", ok, err)
	}

	// Case 5: 403 Abuse Detection with Retry-After header
	respAbuse := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Retry-After": []string{"15"}},
	}
	abuseBody := []byte(`{"message":"Please wait before submitting more requests (abuse detection mechanism)."}`)
	if ok, err := parseRateLimitError(respAbuse, abuseBody); !ok || err == nil || err.RetryAfter != 15*time.Second {
		t.Fatalf("expected abuse detection with 15s RetryAfter, got ok=%v, err=%v", ok, err)
	}

	// Case 6: 403 Primary Rate Limit with X-RateLimit-Remaining: 0 and X-RateLimit-Reset
	resetUnix := time.Now().Add(120 * time.Second).Unix()
	respPrimary := &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{
			"X-Ratelimit-Remaining": []string{"0"},
			"X-Ratelimit-Reset":     []string{strconv.FormatInt(resetUnix, 10)},
		},
	}
	if ok, err := parseRateLimitError(respPrimary, []byte(`{"message":"API rate limit exceeded"}`)); !ok || err == nil {
		t.Fatalf("expected primary rate limit parsed, got ok=%v, err=%v", ok, err)
	} else if err.RetryAfter < 110*time.Second || err.RetryAfter > 130*time.Second {
		t.Fatalf("expected ~120s RetryAfter, got %v", err.RetryAfter)
	}

	// Case 7: 403 Standard Permission Denied (Not a rate limit)
	respForbidden := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"X-Ratelimit-Remaining": []string{"4999"}},
	}
	if ok, err := parseRateLimitError(respForbidden, []byte(`{"message":"Resource not accessible by integration"}`)); ok || err != nil {
		t.Fatalf("expected (false, nil) for permission forbidden, got ok=%v, err=%v", ok, err)
	}
}
