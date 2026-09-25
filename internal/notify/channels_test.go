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

func newChannelTestHarness(t *testing.T, channelType string, handler http.HandlerFunc) (*Worker, store.NotificationChannel, store.NotificationOutbox) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	ch := store.NotificationChannel{
		ID:           "test-ch-" + channelType,
		ChannelType:  channelType,
		Target:       srv.URL,
		AllowPrivate: true,
	}
	item := store.NotificationOutbox{
		ID:        "01JTESTCH00000000000000001",
		ChannelID: ch.ID,
		Title:     "测试标题",
		BodyText:  "<b>加粗文本</b> <a href=\"https://github.com/org/repo/issues/1\">Issue #1</a>",
		HTMLURL:   "https://github.com/org/repo/issues/1",
	}
	return &Worker{Client: srv.Client()}, ch, item
}

func TestSendFeishu(t *testing.T) {
	t.Run("SuccessWithoutSign", func(t *testing.T) {
		var receivedBody map[string]any
		w, ch, item := newChannelTestHarness(t, store.ChannelFeishu, func(rw http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedBody)
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":0,"msg":"success"}`))
		})

		_, err := w.deliver(t.Context(), item, map[string]store.NotificationChannel{ch.ID: ch}, make(map[string]string))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedBody["msg_type"] != "interactive" {
			t.Fatalf("expected msg_type interactive, got %v", receivedBody["msg_type"])
		}
	})

	t.Run("SuccessWithSign", func(t *testing.T) {
		var receivedBody map[string]any
		w, ch, item := newChannelTestHarness(t, store.ChannelFeishu, func(rw http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedBody)
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":0,"msg":"success"}`))
		})

		err := w.sendFeishu(t.Context(), ch, "test-secret-key", item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedBody["sign"] == nil || receivedBody["sign"] == "" {
			t.Fatal("expected sign in feishu payload")
		}
	})

	t.Run("ApiErrorCode", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelFeishu, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":19001,"msg":"param error"}`))
		})

		err := w.sendFeishu(t.Context(), ch, "", item)
		if err == nil {
			t.Fatal("expected error on code!=0")
		}
		code := deliveryErrorCode(err)
		if !isPermanentDeliveryError(code) {
			t.Fatalf("expected permanent delivery error, got code: %s", code)
		}
	})
}

func TestSendWeCom(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var receivedBody map[string]any
		w, ch, item := newChannelTestHarness(t, store.ChannelWeCom, func(rw http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &receivedBody)
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		})

		err := w.sendWeCom(t.Context(), ch, "", item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedBody["msgtype"] != "markdown" {
			t.Fatalf("expected markdown, got %v", receivedBody["msgtype"])
		}
	})

	t.Run("ApiErrorCode", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelWeCom, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
		})

		err := w.sendWeCom(t.Context(), ch, "", item)
		if err == nil {
			t.Fatal("expected error")
		}
		code := deliveryErrorCode(err)
		if !isPermanentDeliveryError(code) {
			t.Fatalf("expected permanent error, got %s", code)
		}
	})
}

func TestSendDingTalk(t *testing.T) {
	t.Run("SuccessWithSign", func(t *testing.T) {
		var querySign string
		w, ch, item := newChannelTestHarness(t, store.ChannelDingTalk, func(rw http.ResponseWriter, r *http.Request) {
			querySign = r.URL.Query().Get("sign")
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		})

		err := w.sendDingTalk(t.Context(), ch, "ding-secret", item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if querySign == "" {
			t.Fatal("expected query sign in url")
		}
	})
}

func TestSendDiscord(t *testing.T) {
	t.Run("Success204", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelDiscord, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusNoContent)
		})

		err := w.sendDiscord(t.Context(), ch, "", item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ClientError400", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelDiscord, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = rw.Write([]byte(`{"message":"invalid payload"}`))
		})

		err := w.sendDiscord(t.Context(), ch, "", item)
		if err == nil {
			t.Fatal("expected error")
		}
		code := deliveryErrorCode(err)
		if !isPermanentDeliveryError(code) {
			t.Fatalf("expected permanent error, got %s", code)
		}
	})
}

func TestSendBark(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		var received map[string]any
		w, ch, item := newChannelTestHarness(t, store.ChannelBark, func(rw http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &received)
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":200,"message":"success"}`))
		})

		err := w.sendBark(t.Context(), ch, "testkey", item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if received["group"] != "RepoSentinel" {
			t.Fatalf("expected group RepoSentinel, got %v", received["group"])
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		input    string
		maxRunes int
		want     string
	}{
		{"", 10, ""},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"hello", 5, "hello"},
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"hello   world", 5, "hello…"},
		{"你好世界，欢迎来到开源监控", 4, "你好世界…"},
	}
	for _, tc := range cases {
		got := truncateRunes(tc.input, tc.maxRunes)
		if got != tc.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.input, tc.maxRunes, got, tc.want)
		}
	}
}

func TestPostJSONChannelDetailFallback(t *testing.T) {
	t.Run("FeishuErrcodeFallbackToErrMsg", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelFeishu, func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"errcode":9999,"errmsg":"custom error detail"}`))
		})
		err := w.sendFeishu(t.Context(), ch, "", item)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "custom error detail") {
			t.Fatalf("expected error detail from errmsg, got %v", err)
		}
	})

	t.Run("FeishuCodeFallbackToRawBodyWhenEmptyMsg", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelFeishu, func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":50001}`))
		})
		err := w.sendFeishu(t.Context(), ch, "", item)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `{"code":50001}`) {
			t.Fatalf("expected raw body fallback, got %v", err)
		}
	})

	t.Run("BarkClientErrorCodeFallbackToRawBody", func(t *testing.T) {
		w, ch, item := newChannelTestHarness(t, store.ChannelBark, func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"code":400}`))
		})
		err := w.sendBark(t.Context(), ch, "testkey", item)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `{"code":400}`) {
			t.Fatalf("expected raw body fallback for bark, got %v", err)
		}
	})
}
