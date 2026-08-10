package rules

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// newRulesLogger 返回写入内存缓冲的 DEBUG 级 slog 记录器，供分诊参与度日志断言。
func newRulesLogger(t *testing.T) (*bytes.Buffer, *slog.Logger) {
	t.Helper()
	var buf bytes.Buffer
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelDebug)
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lv}))
	return &buf, logger
}

// TestShouldNotifyRealtime 表驱动守护实时通知判定：Issue/PR 按 action 白名单，
// Actions 按结论失败与恢复，安全告警一律实时。
func TestShouldNotifyRealtime(t *testing.T) {
	cases := []struct {
		name string
		ev   store.Event
		want bool
	}{
		{"issue opened 实时", store.Event{Kind: store.WorkItemKindIssue, Action: "opened"}, true},
		{"issue closed 实时", store.Event{Kind: store.WorkItemKindIssue, Action: "closed"}, true},
		{"issue edited 摘要", store.Event{Kind: store.WorkItemKindIssue, Action: "edited"}, false},
		{"pr merged 实时", store.Event{Kind: store.WorkItemKindPR, Action: "merged"}, true},
		{"pr ready_for_review 实时", store.Event{Kind: store.WorkItemKindPR, Action: "ready_for_review"}, true},
		{"pr converted_to_draft 摘要", store.Event{Kind: store.WorkItemKindPR, Action: "converted_to_draft"}, false},
		{"workflow 失败实时", store.Event{Kind: "workflow_run", WorkflowConclusion: "failure"}, true},
		{"workflow 成功摘要", store.Event{Kind: "workflow_run", WorkflowConclusion: "success"}, false},
		{"workflow 恢复实时", store.Event{Kind: "workflow_run", Action: "recovered"}, true},
		{"dependabot 创建实时", store.Event{Kind: store.AlertKindDependabot, Action: "created"}, true},
		{"code_scanning 严重度变化实时", store.Event{Kind: store.AlertKindCodeScanning, Action: "changed"}, true},
		{"secret_scanning 忽略实时", store.Event{Kind: store.AlertKindSecretScanning, Action: "dismissed"}, true},
		{"未知 kind 摘要", store.Event{Kind: "unknown", Action: "opened"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldNotifyRealtime(&c.ev); got != c.want {
				t.Fatalf("shouldNotifyRealtime(%+v) = %v, want %v", c.ev, got, c.want)
			}
		})
	}
}

// TestShouldNotifyRealtimeRelease 守护 release 事件实时通知判定。
func TestShouldNotifyRealtimeRelease(t *testing.T) {
	if !shouldNotifyRealtime(&store.Event{Kind: store.ReleaseKind, Action: "published"}) {
		t.Fatal("release published 应实时通知")
	}
	if shouldNotifyRealtime(&store.Event{Kind: store.ReleaseKind, Action: "edited"}) {
		t.Fatal("release edited 不应通知")
	}
}

// TestReleaseAnalysis 覆盖成功、未启用、无渠道、AI 失败各分支。
func TestReleaseAnalysis(t *testing.T) {
	ev := &store.Event{
		Kind: store.ReleaseKind, Action: "published", Title: "Hello-World v2.0.0",
		HTMLURL: "https://github.com/o/r/releases/tag/v2.0.0",
		PayloadSummary: map[string]any{
			"tag_name": "v2.0.0", "prerelease": false, "notes": "Some English notes",
		},
	}
	subscribed := []store.NotificationChannel{{Enabled: true}}
	t.Run("启用返回总结", func(t *testing.T) {
		client := aiStub(t, `{"choices":[{"message":{"content":"新增 X，修复 Y；⚠️ 破坏性变更：Z。"}}]}`)
		client.ReleaseSummaryEnabled = true
		e := &Engine{AI: client}
		got := e.releaseAnalysis(t.Context(), ev, "o/r", subscribed)
		if !strings.Contains(got, "新增 X") {
			t.Fatalf("期望包含总结，实际: %q", got)
		}
	})
	t.Run("未启用返回空", func(t *testing.T) {
		// aiStub 构造的 Client 未开 ReleaseSummaryEnabled。
		e := &Engine{AI: aiStub(t, `{"choices":[{"message":{"content":"不应调用"}}]}`)}
		if got := e.releaseAnalysis(t.Context(), ev, "o/r", subscribed); got != "" {
			t.Fatalf("未启用应返回空，实际: %q", got)
		}
	})
	t.Run("无订阅渠道返回空", func(t *testing.T) {
		client := aiStub(t, `{"choices":[{"message":{"content":"不应调用"}}]}`)
		client.ReleaseSummaryEnabled = true
		e := &Engine{AI: client}
		if got := e.releaseAnalysis(t.Context(), ev, "o/r", nil); got != "" {
			t.Fatalf("无渠道应返回空，实际: %q", got)
		}
	})
	t.Run("AI 失败返回空", func(t *testing.T) {
		client := aiStub(t, `internal error`)
		client.ReleaseSummaryEnabled = true
		e := &Engine{AI: client}
		if got := e.releaseAnalysis(t.Context(), ev, "o/r", subscribed); got != "" {
			t.Fatalf("AI 失败应返回空，实际: %q", got)
		}
	})
	t.Run("非 release 事件返回空", func(t *testing.T) {
		client := aiStub(t, `{"choices":[{"message":{"content":"x"}}]}`)
		client.ReleaseSummaryEnabled = true
		issue := &store.Event{Kind: store.WorkItemKindIssue, Action: "opened", Title: "t"}
		if got := (&Engine{AI: client}).releaseAnalysis(t.Context(), issue, "o/r", subscribed); got != "" {
			t.Fatalf("issue 不应触发 release 总结，实际: %q", got)
		}
	})
}

// TestEvaluateReleaseOutbox 验证 release 事件经 Evaluate 写入 Outbox，且 feature 关闭时静默。
func TestEvaluateReleaseOutbox(t *testing.T) {
	data := openEngineStore(t)
	ctx := t.Context()
	ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: "telegram", Name: "tg", Target: "123",
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	releaseID := 42
	ev := store.Event{
		ID: ulid.Make().String(), Kind: store.ReleaseKind, Action: "published",
		Title: "Hello-World v2.0.0", SubjectNumber: &releaseID,
		OccurredAt: now, HTMLURL: "https://github.com/o/r/releases/tag/v2.0.0",
		PayloadSummary: map[string]any{"tag_name": "v2.0.0"},
	}
	e := &Engine{Store: data}
	if err := e.Evaluate(ctx, normalizer.Result{Event: &ev}, "o/r"); err != nil {
		t.Fatal(err)
	}
	outbox, _, err := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 || outbox[0].ChannelID != ch.ID {
		t.Fatalf("应写入 1 条 outbox: %+v", outbox)
	}
	if !strings.Contains(outbox[0].Title, "🚀") {
		t.Fatalf("outbox 标题应含 🚀: %s", outbox[0].Title)
	}
	// 关闭全局开关后不再投递
	raw, _ := json.Marshal(false)
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: store.SettingFeatureStarredReleases,
		ValueJSON: raw, UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	ev2 := ev
	ev2.ID = ulid.Make().String()
	if err := e.Evaluate(ctx, normalizer.Result{Event: &ev2}, "o/r"); err != nil {
		t.Fatal(err)
	}
	outbox, _, _ = data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if len(outbox) != 1 {
		t.Fatalf("开关关闭后不应新增投递，got %d", len(outbox))
	}
}

// TestEvaluateReleaseAIInjection 验证 AI 总结注入正文与失败降级（不丢通知）。
func TestEvaluateReleaseAIInjection(t *testing.T) {
	data := openEngineStore(t)
	ctx := t.Context()
	if _, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: "telegram", Name: "tg", Target: "123",
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	releaseID := 42
	ev := store.Event{
		ID: ulid.Make().String(), Kind: store.ReleaseKind, Action: "published",
		Title: "Hello-World v2.0.0", SubjectNumber: &releaseID,
		OccurredAt: now, HTMLURL: "https://github.com/o/r/releases/tag/v2.0.0",
		PayloadSummary: map[string]any{"tag_name": "v2.0.0", "notes": "English notes"},
	}
	t.Run("AI 成功注入总结", func(t *testing.T) {
		client := aiStub(t, `{"choices":[{"message":{"content":"新增 X 功能，修复 Y。"}}]}`)
		client.ReleaseSummaryEnabled = true
		e := &Engine{Store: data, AI: client}
		if err := e.Evaluate(ctx, normalizer.Result{Event: &ev}, "o/r"); err != nil {
			t.Fatal(err)
		}
		outbox, _, _ := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
		if len(outbox) == 0 {
			t.Fatal("应写入 outbox")
		}
		if !strings.Contains(outbox[len(outbox)-1].BodyText, "🤖 AI 总结") {
			t.Fatalf("正文应含 AI 总结段: %s", outbox[len(outbox)-1].BodyText)
		}
	})
	t.Run("AI 失败仍通知", func(t *testing.T) {
		client := aiStub(t, `internal error`)
		client.ReleaseSummaryEnabled = true
		ev2 := ev
		ev2.ID = ulid.Make().String()
		e := &Engine{Store: data, AI: client}
		if err := e.Evaluate(ctx, normalizer.Result{Event: &ev2}, "o/r"); err != nil {
			t.Fatal(err)
		}
		outbox, _, _ := data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
		// 按内容断言（不依赖 outbox 排序）：AI 失败时通知仍在且不带总结段。
		found := false
		for _, o := range outbox {
			if strings.Contains(o.BodyText, "🔗 在 GitHub 中查看") && !strings.Contains(o.BodyText, "🤖 AI 总结") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("AI 失败时应有原文链接兜底且无 AI 段: %+v", outbox)
		}
	})
}

// TestShouldNotifyRealtimeStarWatch 守护 star/watch 实时通知判定：创建/删除/关注
// 一律实时；未知 action 不通知（与 Issue/PR 的白名单语义一致）。
func TestShouldNotifyRealtimeStarWatch(t *testing.T) {
	star := &store.Event{Kind: store.StarKind, Action: "created"}
	if !shouldNotifyRealtime(star) {
		t.Fatal("star created should notify realtime")
	}
	starDeleted := &store.Event{Kind: store.StarKind, Action: "deleted"}
	if !shouldNotifyRealtime(starDeleted) {
		t.Fatal("star deleted should notify realtime")
	}
	watch := &store.Event{Kind: store.WatchKind, Action: "started"}
	if !shouldNotifyRealtime(watch) {
		t.Fatal("watch started should notify realtime")
	}
	// 未知 action 不通知。
	if shouldNotifyRealtime(&store.Event{Kind: store.StarKind, Action: "unknown"}) {
		t.Fatal("unknown star action should not notify")
	}
}

func TestRenderMessage_IssueOpened(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 15, 0, 0, time.UTC)
	num := 8
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "[BUG] 脚本一直在跑",
		Actor: "testuser", SubjectNumber: &num, OccurredAt: now, HTMLURL: "https://github.com/test/repo/issues/8",
		PayloadSummary: map[string]any{"state": "open", "draft": false, "labels": []any{"bug", "urgent"}, "assignees": []any{"user1"}, "milestone": "v1.0"},
	}
	title, body, htmlURL := renderMessage(ev, "Silentely/xserver-vps-renew")

	if !strings.Contains(title, "已打开") {
		t.Errorf("期望标题含「已打开」，标题: %s", title)
	}
	if !strings.Contains(body, "状态：已打开") {
		t.Errorf("期望正文状态置顶，实际: %s", body)
	}
	if !strings.Contains(body, "Silentely/xserver-vps-renew") {
		t.Error("期望包含仓库名")
	}
	if !strings.Contains(body, "#8") {
		t.Error("期望包含编号")
	}
	if !strings.Contains(body, "bug, urgent") {
		t.Errorf("期望包含标签，实际: %s", body)
	}
	if !strings.Contains(body, "user1") {
		t.Errorf("期望包含指派人，实际: %s", body)
	}
	if !strings.Contains(body, "v1.0") {
		t.Errorf("期望包含里程碑，实际: %s", body)
	}
	if !strings.Contains(body, "2026-07-30 09:15 UTC") {
		t.Errorf("期望包含时间，实际: %s", body)
	}
	if htmlURL != "https://github.com/test/repo/issues/8" {
		t.Errorf("期望返回 htmlURL，实际: %s", htmlURL)
	}
}

func TestRenderMessage_PRMerged(t *testing.T) {
	num := 42
	ev := &store.Event{
		Kind: store.WorkItemKindPR, Action: "merged", Title: "feat: 新功能",
		Actor: "dev", SubjectNumber: &num, OccurredAt: time.Now().UTC(), HTMLURL: "https://github.com/test/repo/pull/42",
		PayloadSummary: map[string]any{"state": "closed", "draft": false},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🟣") {
		t.Errorf("期望 🟣 emoji 表示 merged，标题: %s", title)
	}
	if !strings.Contains(title, "已合并") {
		t.Errorf("期望标题含「已合并」，标题: %s", title)
	}
	if !strings.Contains(body, "状态：已合并") {
		t.Errorf("期望包含「状态：已合并」，实际: %s", body)
	}
}

// aiStub 返回指向桩服务器的 AI 客户端，reply 为完整 HTTP 响应体。
func aiStub(t *testing.T, reply string) *ai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &ai.Client{BaseURL: srv.URL, APIKey: "sk-test", Enabled: true, DigestEnabled: true, TriageEnabled: true}
}

func openEngineStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "rules.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func TestIsNewSecurityAlert(t *testing.T) {
	cases := []struct {
		name string
		ev   store.Event
		want bool
	}{
		{"dependabot 创建", store.Event{Kind: store.AlertKindDependabot, Action: "created"}, true},
		{"code_scanning 打开", store.Event{Kind: store.AlertKindCodeScanning, Action: "opened"}, true},
		{"secret_scanning 重新打开", store.Event{Kind: store.AlertKindSecretScanning, Action: "reopened"}, true},
		{"dependabot 修复", store.Event{Kind: store.AlertKindDependabot, Action: "fixed"}, false},
		{"dependabot 忽略", store.Event{Kind: store.AlertKindDependabot, Action: "dismissed"}, false},
		{"issue 打开不分诊", store.Event{Kind: store.WorkItemKindIssue, Action: "opened"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNewSecurityAlert(&tc.ev); got != tc.want {
				t.Fatalf("isNewSecurityAlert(%+v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}

func TestTriageAnalysis(t *testing.T) {
	ev := &store.Event{
		Kind: store.AlertKindDependabot, Action: "created", Title: "lodash 漏洞", Severity: "high",
		HTMLURL:        "https://github.com/acme/web/security/dependabot/3",
		PayloadSummary: map[string]any{"rule_or_dependency": "lodash"},
	}
	// 订阅全部类型的渠道（EventKinds 为 nil），使分诊有接收方。
	subscribed := []store.NotificationChannel{{Enabled: true}}
	t.Run("新告警返回分析", func(t *testing.T) {
		e := &Engine{AI: aiStub(t, `{"choices":[{"message":{"content":"影响：攻击面扩大。\n建议：升级依赖。"}}]}`)}
		got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed)
		if !strings.Contains(got, "影响：") {
			t.Fatalf("期望包含分析，实际: %q", got)
		}
	})
	t.Run("非新告警返回空", func(t *testing.T) {
		closed := &store.Event{Kind: store.AlertKindDependabot, Action: "fixed", Title: "x"}
		e := &Engine{AI: aiStub(t, `{"choices":[{"message":{"content":"ignored"}}]}`)}
		if got := e.triageAnalysis(t.Context(), closed, "acme/web", subscribed); got != "" {
			t.Fatalf("终态告警不应分诊，实际: %q", got)
		}
	})
	t.Run("无订阅渠道返回空", func(t *testing.T) {
		e := &Engine{AI: aiStub(t, `{"choices":[{"message":{"content":"不应调用"}}]}`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", nil); got != "" {
			t.Fatalf("无订阅渠道不应分诊，实际: %q", got)
		}
	})
	t.Run("AI 失败返回空", func(t *testing.T) {
		e := &Engine{AI: aiStub(t, `internal error`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatalf("AI 失败应返回空串，实际: %q", got)
		}
	})
	t.Run("格式不达标返回空", func(t *testing.T) {
		e := &Engine{AI: aiStub(t, `{"choices":[{"message":{"content":"分析：存在风险。"}}]}`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatalf("缺「影响：」前缀应返回空串，实际: %q", got)
		}
	})
	t.Run("未配置 AI 返回空", func(t *testing.T) {
		if got := (&Engine{}).triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatalf("未配置 AI 应返回空串，实际: %q", got)
		}
	})
	t.Run("分诊开关关闭返回空", func(t *testing.T) {
		e := &Engine{AI: &ai.Client{APIKey: "k", Enabled: true, TriageEnabled: false}}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatalf("分诊关闭应返回空串，实际: %q", got)
		}
	})
}

// TestTriageAnalysisLogs 验证分诊参与度留痕：used / skipped（三种原因）/ fallback。
func TestTriageAnalysisLogs(t *testing.T) {
	subscribed := []store.NotificationChannel{{Enabled: true}}
	ev := &store.Event{
		ID: ulid.Make().String(), Kind: store.AlertKindDependabot, Action: "created",
		Title: "lodash 漏洞", Severity: "high",
		PayloadSummary: map[string]any{"rule_or_dependency": "lodash"},
	}

	t.Run("成功 → triage ai used", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		e := &Engine{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"影响：攻击面扩大。\n建议：升级依赖。"}}]}`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got == "" {
			t.Fatal("期望分诊成功")
		}
		if !strings.Contains(buf.String(), `msg="triage ai used"`) {
			t.Fatalf("期望 triage ai used 日志，实际: %s", buf.String())
		}
	})
	t.Run("未启用 → skipped triage_not_enabled", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		e := &Engine{Logger: logger}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatal("未配置 AI 应返回空")
		}
		if !strings.Contains(buf.String(), `msg="triage ai skipped"`) || !strings.Contains(buf.String(), "reason=triage_not_enabled") {
			t.Fatalf("期望 skipped triage_not_enabled，实际: %s", buf.String())
		}
	})
	t.Run("非告警事件静默不产生日志", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		issue := &store.Event{ID: ev.ID, Kind: store.WorkItemKindIssue, Action: "opened", Title: "普通问题"}
		e := &Engine{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"不应调用"}}]}`)}
		if got := e.triageAnalysis(t.Context(), issue, "acme/web", subscribed); got != "" {
			t.Fatal("非告警事件应返回空")
		}
		if buf.Len() != 0 {
			t.Fatalf("非告警事件不应产生分诊日志，实际: %s", buf.String())
		}
	})
	t.Run("非新告警 → skipped not_new_alert", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		closed := &store.Event{ID: ev.ID, Kind: store.AlertKindDependabot, Action: "fixed", Title: "x"}
		e := &Engine{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"ignored"}}]}`)}
		if got := e.triageAnalysis(t.Context(), closed, "acme/web", subscribed); got != "" {
			t.Fatal("终态告警应返回空")
		}
		if !strings.Contains(buf.String(), `msg="triage ai skipped"`) || !strings.Contains(buf.String(), "reason=not_new_alert") {
			t.Fatalf("期望 skipped not_new_alert，实际: %s", buf.String())
		}
	})
	t.Run("无订阅渠道 → skipped no_subscribed_channel", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		e := &Engine{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"不应调用"}}]}`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", nil); got != "" {
			t.Fatal("无订阅渠道应返回空")
		}
		if !strings.Contains(buf.String(), `msg="triage ai skipped"`) || !strings.Contains(buf.String(), "reason=no_subscribed_channel") {
			t.Fatalf("期望 skipped no_subscribed_channel，实际: %s", buf.String())
		}
	})
	t.Run("AI 失败 → fallback ai_error", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		e := &Engine{Logger: logger, AI: aiStub(t, `internal error`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatal("AI 失败应返回空")
		}
		out := buf.String()
		if !strings.Contains(out, `msg="triage ai fallback"`) || !strings.Contains(out, "reason=ai_error") {
			t.Fatalf("期望 fallback ai_error，实际: %s", out)
		}
	})
	t.Run("格式不达标 → fallback format_invalid", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		e := &Engine{Logger: logger, AI: aiStub(t, `{"choices":[{"message":{"content":"分析：存在风险。"}}]}`)}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got != "" {
			t.Fatal("格式不达标应返回空")
		}
		out := buf.String()
		if !strings.Contains(out, `msg="triage ai fallback"`) || !strings.Contains(out, "reason=format_invalid") {
			t.Fatalf("期望 fallback format_invalid，实际: %s", out)
		}
	})
	t.Run("used 与 ai 层日志 req_id 一致", func(t *testing.T) {
		buf, logger := newRulesLogger(t)
		aiClient := aiStub(t, `{"choices":[{"message":{"content":"影响：攻击面扩大。\n建议：升级依赖。"}}]}`)
		// 同一 Logger 注入 AI 客户端，使参与度日志与 ai 层调用日志写入同一缓冲。
		aiClient.Logger = logger
		e := &Engine{Logger: logger, AI: aiClient}
		if got := e.triageAnalysis(t.Context(), ev, "acme/web", subscribed); got == "" {
			t.Fatal("期望分诊成功")
		}
		re := regexp.MustCompile(`req_id=([0-9a-f]+)`)
		ids := re.FindAllStringSubmatch(buf.String(), -1)
		if len(ids) < 2 {
			t.Fatalf("期望 triage 与 ai 层日志均携带 req_id，实际: %s", buf.String())
		}
		if ids[0][1] != ids[1][1] {
			t.Fatalf("triage 与 ai 层 req_id 应一致，实际: %s", buf.String())
		}
	})
}

// Evaluate 级测试：新安全告警通知正文附带 AI 分析；Issue 不受影响。
func TestEvaluateWithAITriage(t *testing.T) {
	data := openEngineStore(t)
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: data, AI: aiStub(t, `{"choices":[{"message":{"content":"影响：<b>高危</b>依赖。\n建议：升级。"}}]}`)}

	alert := normalizer.Result{Event: &store.Event{
		Kind: store.AlertKindDependabot, Action: "created", Title: "lodash 漏洞", Severity: "high",
		HTMLURL:        "https://github.com/acme/web/security/dependabot/3",
		PayloadSummary: map[string]any{"rule_or_dependency": "lodash"},
	}}
	if err := e.Evaluate(t.Context(), alert, "acme/web"); err != nil {
		t.Fatal(err)
	}

	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应生成 1 条通知，got %d", len(items))
	}
	if !strings.Contains(items[0].BodyText, "🤖 AI 分析") {
		t.Fatalf("正文应含 AI 分析段，实际: %s", items[0].BodyText)
	}
	// AI 输出必须转义，避免破坏 Telegram HTML。
	if strings.Contains(items[0].BodyText, "<b>高危</b>") {
		t.Fatalf("AI 输出必须转义，实际: %s", items[0].BodyText)
	}
	if !strings.Contains(items[0].BodyText, "&lt;b&gt;高危&lt;/b&gt;") {
		t.Fatalf("期望转义后的 HTML，实际: %s", items[0].BodyText)
	}
}

// Issue 通知不应触发分诊。
func TestEvaluateSkipsTriageForIssue(t *testing.T) {
	data := openEngineStore(t)
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: data, AI: aiStub(t, `{"choices":[{"message":{"content":"不应出现"}}]}`)}
	res := normalizer.Result{Event: &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "普通问题",
	}}
	if err := e.Evaluate(t.Context(), res, "acme/web"); err != nil {
		t.Fatal(err)
	}
	items, _, err := data.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应生成 1 条通知，got %d", len(items))
	}
	if strings.Contains(items[0].BodyText, "AI 分析") {
		t.Fatalf("Issue 通知不应含 AI 分析，实际: %s", items[0].BodyText)
	}
}

// TestEvaluateLogsNotifySkipped 事件已入库但未产生实时通知的三类静默路径
// （抑制 / 能力关闭 / 非实时范围）应 Debug 留痕并带 reason，排查漏通知不再盲猜。
func TestEvaluateLogsNotifySkipped(t *testing.T) {
	cases := []struct {
		name   string
		res    normalizer.Result
		reason string
	}{
		{
			name: "suppressed",
			res: normalizer.Result{Event: &store.Event{
				Kind: store.WorkItemKindIssue, Action: "opened", Title: "x",
			}, SuppressNotify: true},
			reason: "suppressed",
		},
		{
			name: "not_realtime",
			res: normalizer.Result{Event: &store.Event{
				Kind: store.WorkItemKindIssue, Action: "edited", Title: "x",
			}},
			reason: "not_realtime",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, logger := newRulesLogger(t)
			e := &Engine{Logger: logger}
			if err := e.Evaluate(t.Context(), tc.res, "acme/web"); err != nil {
				t.Fatal(err)
			}
			out := buf.String()
			if !strings.Contains(out, "notification skipped") || !strings.Contains(out, "reason="+tc.reason) {
				t.Fatalf("应留痕 notification skipped reason=%s，实际: %s", tc.reason, out)
			}
		})
	}
}

// 无渠道订阅该告警类型时，AI 分诊不应被调用（避免无效的外部调用）。
func TestEvaluateSkipsTriageWithoutSubscriber(t *testing.T) {
	data := openEngineStore(t)
	// 渠道存在但显式不订阅实时通知（EventKinds 空数组）。
	_, err := data.Channels().Upsert(t.Context(), store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg",
		Enabled: true, Target: "1", EventKinds: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"不应调用"}}]}`))
	}))
	t.Cleanup(srv.Close)
	e := &Engine{Store: data, AI: &ai.Client{BaseURL: srv.URL, APIKey: "sk-test", Enabled: true, TriageEnabled: true}}
	res := normalizer.Result{Event: &store.Event{
		Kind: store.AlertKindDependabot, Action: "created", Title: "lodash 漏洞", Severity: "high",
		PayloadSummary: map[string]any{"rule_or_dependency": "lodash"},
	}}
	if err := e.Evaluate(t.Context(), res, "acme/web"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("无订阅渠道时不应调用 AI 分诊，实际调用 %d 次", calls.Load())
	}
}

func TestRenderMessage_PRDraft(t *testing.T) {
	num := 50
	ev := &store.Event{
		Kind: store.WorkItemKindPR, Action: "opened", Title: "WIP: draft pr",
		SubjectNumber: &num, OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "draft": true},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "📝") {
		t.Errorf("期望 📝 emoji 表示 draft，标题: %s", title)
	}
	if !strings.Contains(body, "状态：草稿") {
		t.Errorf("期望包含「状态：草稿」，实际: %s", body)
	}
}

func TestRenderMessage_SecurityAlert_Critical(t *testing.T) {
	ev := &store.Event{
		Kind: store.AlertKindDependabot, Action: "opened", Title: "lodash prototype pollution",
		Severity: "critical", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "severity": "critical", "rule_or_dependency": "lodash"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🚨") {
		t.Errorf("期望 🚨 emoji 表示 critical，标题: %s", title)
	}
	if !strings.Contains(title, "新告警") {
		t.Errorf("期望标题含「新告警」，标题: %s", title)
	}
	if !strings.Contains(body, "🚨 严重度：严重 (critical)") {
		t.Errorf("期望中文严重度，实际: %s", body)
	}
	if !strings.Contains(body, "lodash") {
		t.Errorf("期望包含规则名，实际: %s", body)
	}
}

func TestRenderMessage_CodeScanning_Error(t *testing.T) {
	ev := &store.Event{
		Kind: store.AlertKindCodeScanning, Action: "opened", Title: "SQL injection",
		Severity: "error", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "severity": "error", "rule_or_dependency": "sql-injection"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🔴") {
		t.Errorf("期望 🔴 emoji 表示 error 级别 code_scanning，标题: %s", title)
	}
	if !strings.Contains(body, "状态：新告警") {
		t.Errorf("期望状态新告警，实际: %s", body)
	}
}

func TestRenderMessage_WorkflowFailure(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "completed", Title: "CI",
		WorkflowConclusion: "failure", Actor: "bot", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"head_branch": "main", "workflow_name": "CI Pipeline"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "❌") {
		t.Errorf("期望 ❌ emoji 表示 failure，标题: %s", title)
	}
	if !strings.Contains(title, "失败") {
		t.Errorf("期望标题含「失败」，标题: %s", title)
	}
	if !strings.Contains(body, "main") {
		t.Errorf("期望包含分支名，实际: %s", body)
	}
	if !strings.Contains(body, "CI Pipeline") {
		t.Errorf("期望包含工作流名，实际: %s", body)
	}
}

func TestRenderMessage_WorkflowRecovered(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "recovered", Title: "CI",
		WorkflowConclusion: "success", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"head_branch": "main"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🟢") {
		t.Errorf("期望 🟢 emoji 表示 recovered，标题: %s", title)
	}
	if !strings.Contains(title, "已恢复") {
		t.Errorf("期望标题含「已恢复」，标题: %s", title)
	}
	if !strings.Contains(body, "状态：已恢复") {
		t.Errorf("期望正文状态已恢复，实际: %s", body)
	}
}

func TestRenderMessage_WorkflowCancelled(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "completed", Title: "Deploy",
		WorkflowConclusion: "cancelled", OccurredAt: time.Now().UTC(),
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "⏹️") {
		t.Errorf("期望 ⏹️ emoji 表示 cancelled，标题: %s", title)
	}
	if !strings.Contains(body, "状态：已取消") {
		t.Errorf("期望状态已取消，实际: %s", body)
	}
	if strings.Contains(body, "结论：") {
		t.Errorf("结论已并入状态行，不应再单独输出，实际: %s", body)
	}
}

func TestStatusDisplay_Table(t *testing.T) {
	cases := []struct {
		name      string
		ev        store.Event
		wantEmoji string
		wantLabel string
	}{
		{"issue opened", store.Event{Kind: store.WorkItemKindIssue, Action: "opened"}, "🟢", "已打开"},
		{"issue closed", store.Event{Kind: store.WorkItemKindIssue, Action: "closed"}, "⚫", "已关闭"},
		{"pr merged", store.Event{Kind: store.WorkItemKindPR, Action: "merged"}, "🟣", "已合并"},
		{"pr draft", store.Event{Kind: store.WorkItemKindPR, Action: "opened", PayloadSummary: map[string]any{"draft": true}}, "📝", "草稿"},
		{"workflow fail", store.Event{Kind: "workflow_run", Action: "completed", WorkflowConclusion: "failure"}, "❌", "失败"},
		{"dependabot created", store.Event{Kind: store.AlertKindDependabot, Action: "created", Severity: "medium"}, "🟠", "新告警"},
		{"dependabot fixed", store.Event{Kind: store.AlertKindDependabot, Action: "fixed"}, "✅", "已修复"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emoji, label := statusDisplay(&tc.ev)
			if emoji != tc.wantEmoji || label != tc.wantLabel {
				t.Fatalf("statusDisplay = %s %s, want %s %s", emoji, label, tc.wantEmoji, tc.wantLabel)
			}
		})
	}
}

// TestStatusDisplayStarWatch 守护 star/watch 状态文案：收藏/取消收藏/已关注。
func TestStatusDisplayStarWatch(t *testing.T) {
	emoji, label := statusDisplay(&store.Event{Kind: store.StarKind, Action: "created"})
	if emoji != "⭐" || label != "已收藏" {
		t.Fatalf("star created: %q %q", emoji, label)
	}
	emoji, label = statusDisplay(&store.Event{Kind: store.StarKind, Action: "deleted"})
	if emoji != "💔" || label != "取消收藏" {
		t.Fatalf("star deleted: %q %q", emoji, label)
	}
	emoji, label = statusDisplay(&store.Event{Kind: store.WatchKind, Action: "started"})
	if emoji != "👀" || label != "已关注" {
		t.Fatalf("watch started: %q %q", emoji, label)
	}
}

func TestRenderMessage_NoRepo(t *testing.T) {
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "test",
		OccurredAt: time.Now().UTC(),
	}
	_, body, _ := renderMessage(ev, "")

	if strings.Contains(body, "仓库") {
		t.Errorf("无仓库时不应包含仓库字段，实际: %s", body)
	}
}

func TestRenderMessage_NoHTMLURL(t *testing.T) {
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "test",
		OccurredAt: time.Now().UTC(), HTMLURL: "",
	}
	_, body, htmlURL := renderMessage(ev, "test/repo")

	if htmlURL != "" {
		t.Errorf("无链接时 htmlURL 应为空，实际: %s", htmlURL)
	}
	if strings.Contains(body, "在 GitHub 中查看") {
		t.Errorf("无链接时不应包含查看链接，实际: %s", body)
	}
}

func TestEventEmoji_SecretScanning(t *testing.T) {
	ev := &store.Event{Kind: store.AlertKindSecretScanning, Action: "opened"}
	if got := eventEmoji(ev); got != "🔑" {
		t.Errorf("期望 🔑，实际: %s", got)
	}
}

func TestSeverityEmoji_All(t *testing.T) {
	cases := []struct {
		severity, want string
	}{
		{"critical", "🚨"}, {"high", "🔴"}, {"error", "🔴"},
		{"medium", "🟠"}, {"warning", "🟠"}, {"low", "🟡"}, {"note", "🟡"},
		{"unknown", "⚠️"}, {"", "⚠️"},
	}
	for _, tc := range cases {
		if got := severityEmoji(tc.severity); got != tc.want {
			t.Errorf("severity=%q: 期望 %s，实际 %s", tc.severity, tc.want, got)
		}
	}
}

func TestWorkflowConclusionEmoji_All(t *testing.T) {
	cases := []struct {
		conclusion, want string
	}{
		{"success", "✅"}, {"failure", "❌"}, {"cancelled", "⏹️"},
		{"timed_out", "⏱️"}, {"action_required", "🔔"}, {"skipped", "⏭️"},
		// startup_failure 与 failure 同属失败类，展示同一 ❌（与通知标题语义一致）；
		// 未知结论在 IsFailureConclusion 兜底后仍失败类，否则用中性 📊。
		{"startup_failure", "❌"}, {"unknown", "📊"},
	}
	for _, tc := range cases {
		if got := workflowConclusionEmoji(tc.conclusion); got != tc.want {
			t.Errorf("conclusion=%q: 期望 %s，实际 %s", tc.conclusion, tc.want, got)
		}
	}
}

// TestStorePayloadString 验证共享的 PayloadSummary 字符串读取辅助（rules 与 ai 共用）。
func TestStorePayloadString(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42}
	if got := store.PayloadString(m, "key"); got != "value" {
		t.Errorf("期望 value，实际: %s", got)
	}
	if got := store.PayloadString(m, "num"); got != "" {
		t.Errorf("非字符串应返回空，实际: %s", got)
	}
	if got := store.PayloadString(nil, "key"); got != "" {
		t.Errorf("nil map 应返回空，实际: %s", got)
	}
	if got := store.PayloadString(m, "missing"); got != "" {
		t.Errorf("缺失 key 应返回空，实际: %s", got)
	}
}

func TestPayloadStringSlice(t *testing.T) {
	m := map[string]any{"labels": []any{"a", "b"}, "bad": []any{1, 2}}
	labels := payloadStringSlice(m, "labels")
	if len(labels) != 2 || labels[0] != "a" || labels[1] != "b" {
		t.Errorf("期望 [a b]，实际: %v", labels)
	}
	if got := payloadStringSlice(m, "bad"); len(got) != 0 {
		t.Errorf("非字符串元素应返回空切片，实际: %v", got)
	}
	if got := payloadStringSlice(nil, "key"); got != nil {
		t.Errorf("nil map 应返回 nil，实际: %v", got)
	}
}
