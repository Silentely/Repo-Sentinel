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

// TestDetermineMessageThemeWordBoundaries 守护词边界判定：
// 关键词必须按整词匹配，且连字符复合词视为一个整体词，
// 避免 "high" 命中 "highlight"/"high-level"、"success" 命中 "failureValue" 之外的良性词。
func TestDetermineMessageThemeWordBoundaries(t *testing.T) {
	tests := []struct {
		title       string
		body        string
		wantFeishu  string
		wantDiscord int
	}{
		{title: "New issue #12", body: "This is a highlight of the docs", wantFeishu: "blue", wantDiscord: 0x3B82F6},
		{title: "Release v1.2.3", body: "highlights: fixes", wantFeishu: "blue", wantDiscord: 0x3B82F6},
		{title: "release notes", body: "high-level overview of the change", wantFeishu: "blue", wantDiscord: 0x3B82F6},
		{title: "New release", body: "fixing typo in docs", wantFeishu: "blue", wantDiscord: 0x3B82F6},
		{title: "中文标题", body: "该方法返回 failureValue 用于异常兜底", wantFeishu: "blue", wantDiscord: 0x3B82F6},
		// 预期的强信号必须保留
		{title: "build passed", body: "the success criteria are met", wantFeishu: "turquoise", wantDiscord: 0x10B981},
		{title: "CI failure", body: "step 2 failed to install deps", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "PR 审查完成", body: "优先级：P1 High", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "merge risk", body: "high-risk change touching auth", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "Review done", body: "maintainer verdict: Block Risk", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "Dependabot medium alert", body: "moderate severity", wantFeishu: "orange", wantDiscord: 0xF59E0B},
		// 中文状态词条支持
		{title: "安全扫描预警", body: "发现高危漏洞需立即修复", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "PR 审查建议", body: "维护者裁决：阻断风险", wantFeishu: "carmine", wantDiscord: 0xEF4444},
		{title: "每日汇总报告", body: "所有健康检查均已通过且成功", wantFeishu: "turquoise", wantDiscord: 0x10B981},
		{title: "Issue 报告", body: "发现中危配置需要关注，包含警告", wantFeishu: "orange", wantDiscord: 0xF59E0B},
	}

	for _, tc := range tests {
		feishu, discord := determineMessageTheme(tc.title, tc.body)
		if feishu != tc.wantFeishu || discord != tc.wantDiscord {
			t.Errorf("determineMessageTheme(%q, %q) = (%s, %#x), want (%s, %#x)",
				tc.title, tc.body, feishu, discord, tc.wantFeishu, tc.wantDiscord)
		}
	}
}

func TestDetermineMessageTheme(t *testing.T) {
	tests := []struct {
		title       string
		body        string
		wantFeishu  string
		wantDiscord int
	}{
		{
			title:       "❌ CI Workflow 运行失败",
			body:        "build job failed with critical error",
			wantFeishu:  "carmine",
			wantDiscord: 0xEF4444,
		},
		{
			title:       "⚠️ PR 质量需关注",
			body:        "Maintainer verdict: Needs Manual Review",
			wantFeishu:  "orange",
			wantDiscord: 0xF59E0B,
		},
		{
			title:       "✅ CI 运行成功并已恢复",
			body:        "all checks passed, Ready to Merge",
			wantFeishu:  "turquoise",
			wantDiscord: 0x10B981,
		},
		{
			title:       "新提交推送通知",
			body:        "author pushed commit",
			wantFeishu:  "blue",
			wantDiscord: 0x3B82F6,
		},
	}

	for _, tc := range tests {
		feishu, discord := determineMessageTheme(tc.title, tc.body)
		if feishu != tc.wantFeishu {
			t.Errorf("determineMessageTheme(%q) feishu = %v, want %v", tc.title, feishu, tc.wantFeishu)
		}
		if discord != tc.wantDiscord {
			t.Errorf("determineMessageTheme(%q) discord = %x, want %x", tc.title, discord, tc.wantDiscord)
		}
	}
}

func TestFormatTelegramExpandableBlocks(t *testing.T) {
	plain := "PR #100 opened by alice"
	if got := formatTelegramExpandableBlocks(plain); got != plain {
		t.Errorf("expected plain text unchanged, got %s", got)
	}

	msgWithAI := "PR #100 opened by alice\n────────────────\n🤖 故障诊断\n第 2 步 npm test 失败，找不到依赖"
	got := formatTelegramExpandableBlocks(msgWithAI)
	if !strings.Contains(got, "<blockquote expandable>") {
		t.Errorf("expected <blockquote expandable> in %s", got)
	}
	if !strings.Contains(got, "<b>🤖 故障诊断</b>") {
		t.Errorf("expected bold title in %s", got)
	}
	if !strings.Contains(got, "第 2 步 npm test 失败") {
		t.Errorf("expected body in %s", got)
	}
}

// TestFormatQuotedAISections 守护企业微信/钉钉的引用块渲染：
// AI 段落的标题与正文都必须落在引用内，不能只把分隔线换成 "> 🤖" 而让正文裸露在引用之外。
func TestFormatQuotedAISections(t *testing.T) {
	noAI := "🔔 测试通知\n────────────────\n来自 RepoSentinel 的测试消息"
	if got := formatQuotedAISections(noAI); got != noAI {
		t.Errorf("无 AI 段落时不应改写正文，got %q", got)
	}

	msgWithAI := "Issue #7 opened by alice\n────────────────\n🤖 Issue 智能分析与回复建议\n类别：Bug Report\n缺失要素：复现步骤"
	got := formatQuotedAISections(msgWithAI)
	for _, want := range []string{"> 🤖 Issue 智能分析与回复建议", "> 类别：Bug Report", "> 缺失要素：复现步骤"} {
		if !strings.Contains(got, want) {
			t.Errorf("引用块缺少 %q，实际: %s", want, got)
		}
	}
	if strings.Contains(got, "────") {
		t.Errorf("分隔线应已被引用块替换，实际: %s", got)
	}
}

// TestSplitAISections 守护多个 AI 段落的拆分与 CRLF 归一。
func TestSplitAISections(t *testing.T) {
	multi := "主体\r\n────────────────\r\n🤖 告警分析\r\n风险说明\r\n────────────────\r\n🤖 更新速览\r\n要点一"
	prefix, sections := splitAISections(multi)
	if prefix != "主体" {
		t.Errorf("prefix = %q, want 主体", prefix)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d: %v", len(sections), sections)
	}
	if sections[0][0] != "告警分析" || sections[0][1] != "风险说明" {
		t.Errorf("unexpected first section: %v", sections[0])
	}
	if sections[1][0] != "更新速览" || sections[1][1] != "要点一" {
		t.Errorf("unexpected second section: %v", sections[1])
	}

	if prefix, sections := splitAISections("无分隔线正文"); prefix != "无分隔线正文" || len(sections) != 0 {
		t.Errorf("无 AI 段落时应原样返回，got prefix=%q sections=%v", prefix, sections)
	}
}
