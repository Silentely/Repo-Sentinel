package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsJSONContentType(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/json; charset=UTF-8", true},
		{"APPLICATION/JSON", true},
		{"  application/json  ", true},
		{"application/json; foo=bar", true},
		{"application/json ; charset=utf-8", true},
		{"application/json;@@", false},
		{"application/json; charset", false},
		{"application/json; =utf-8", false},
		{"text/html", false},
		{"text/plain", false},
		{"application/xml", false},
		{"", false},
		{"application/json-patch+json", false},
	}

	for _, tc := range cases {
		got := isJSONContentType(tc.contentType)
		if got != tc.want {
			t.Errorf("isJSONContentType(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}

func TestDecodeJSONFastPath(t *testing.T) {
	type samplePayload struct {
		Name string `json:"name"`
	}

	t.Run("ValidJSONWithFastPathHeader", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"}`))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		rw := httptest.NewRecorder()

		var payload samplePayload
		err := decodeJSON(rw, req, &payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if payload.Name != "test" {
			t.Fatalf("expected payload name test, got %s", payload.Name)
		}
	})

	t.Run("RejectUnsupportedMediaType", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"}`))
		req.Header.Set("Content-Type", "text/plain")
		rw := httptest.NewRecorder()

		var payload samplePayload
		err := decodeJSON(rw, req, &payload)
		if err == nil {
			t.Fatal("expected error for text/plain")
		}
		var decodeErr jsonDecodeError
		if !strings.Contains(err.Error(), "invalid JSON request") {
			decodeErr, _ = err.(jsonDecodeError)
			if decodeErr.status != http.StatusUnsupportedMediaType {
				t.Fatalf("expected status 415, got %d", decodeErr.status)
			}
		}
	})

	t.Run("RejectTrailingData", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test"} {"extra": 1}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()

		var payload samplePayload
		err := decodeJSON(rw, req, &payload)
		if err == nil {
			t.Fatal("expected error for multiple JSON objects")
		}
		if !strings.Contains(err.Error(), "请求体只允许包含单个 JSON 对象") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("RejectUnknownFields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test","unexpected":true}`))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()

		var payload samplePayload
		err := decodeJSON(rw, req, &payload)
		if err == nil {
			t.Fatal("expected error for unknown fields")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("RejectOversizedBody", func(t *testing.T) {
		oversized := `{"name":"` + strings.Repeat("a", int(maxJSONBodyBytes)+100) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(oversized)))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()

		var payload samplePayload
		err := decodeJSON(rw, req, &payload)
		if err == nil {
			t.Fatal("expected error for oversized body")
		}
		decodeErr, ok := err.(jsonDecodeError)
		if !ok || decodeErr.status != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 Payload Too Large, got %+v", err)
		}
	})
}
