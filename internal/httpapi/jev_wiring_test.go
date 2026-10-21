package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
	"github.com/oklog/ulid/v2"
)

func TestJevDependencyFields(t *testing.T) {
	var rt *jev.RuntimeConfig
	_ = Dependencies{JevRuntime: rt}
	_ = rules.Engine{JevRuntime: rt}
	_ = rules.Aggregator{JevRuntime: rt}
	_ = chatops.Dependencies{JevRuntime: rt}
	_ = webhooksvc.Service{JevRuntime: rt}
}

type trackingJevClient struct {
	plane   string
	counter *atomic.Int64
}

func (c *trackingJevClient) Judge(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
	c.counter.Add(1)
	answers := make(map[string]jev.Answer)
	for qKey := range questions {
		switch qKey {
		case "noise_type":
			answers[qKey] = jev.Answer{Type: "choice", Choice: httpPtrString("actionable"), Confidence: httpPtrFloat(0.95)}
		case "severity_tier":
			answers[qKey] = jev.Answer{Type: "choice", Choice: httpPtrString("medium"), Confidence: httpPtrFloat(0.95)}
		case "incident_tag":
			answers[qKey] = jev.Answer{Type: "choice", Choice: httpPtrString("general"), Confidence: httpPtrFloat(0.95)}
		case "needs_deep_review":
			answers[qKey] = jev.Answer{Type: "noul", Noul: httpPtrFloat(0.01)}
		case "risk_surface":
			answers[qKey] = jev.Answer{Type: "choice", Choice: httpPtrString("documentation_only"), Confidence: httpPtrFloat(0.95)}
		case "safety_decision":
			answers[qKey] = jev.Answer{Type: "choice", Choice: httpPtrString("allow"), Confidence: httpPtrFloat(0.95)}
		}
	}
	return jev.Result{
		Model:   "jev-1.14.0",
		Answers: answers,
	}, nil
}

func TestJevWiring_DryRunZeroSideEffects(t *testing.T) {
	var jevCalls atomic.Int64
	jevCfg := config.JevConfig{
		Enabled:                 true,
		NoiseFilterEnabled:      true,
		ChatOpsGuardrailEnabled: true,
		Mode:                    "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	rt.SetClientForTest("notification", &trackingJevClient{plane: "notification", counter: &jevCalls})

	fixture := newHTTPTestFixture(t, httpTestOptions{
		jevRuntime: rt,
	})
	ctx := t.Context()

	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	auditsBefore, err := fixture.store.Audits().List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list audits before: %v", err)
	}

	dryReq := map[string]any{
		"event_type": "issues",
		"action":     "opened",
		"repository": "acme/demo",
		"payload": map[string]any{
			"action": "opened",
			"repository": map[string]any{
				"id":        99,
				"name":      "demo",
				"full_name": "acme/demo",
				"owner":     map[string]any{"login": "acme"},
			},
			"issue": map[string]any{
				"number":     42,
				"title":      "Bug report in dryrun",
				"body":       "Some reproduction details",
				"state":      "open",
				"html_url":   "https://github.com/acme/demo/issues/42",
				"user":       map[string]any{"login": "alice"},
				"updated_at": time.Now().UTC().Format(time.RFC3339),
				"labels":     []map[string]any{{"name": "bug"}},
			},
		},
	}
	raw, _ := json.Marshal(dryReq)

	w := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/rules/dry-run",
		string(raw),
		"127.0.0.1:45100",
		cookies,
		map[string]string{
			CSRFHeaderName: csrf.Value,
			"Content-Type": "application/json",
		},
	)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from dryrun, got %d: %s", w.Code, w.Body.String())
	}

	// 断言 0 远程 Jev 调用
	if jevCalls.Load() != 0 {
		t.Fatalf("expected 0 remote Jev calls during dryrun, got %d", jevCalls.Load())
	}

	// 断言 0 Outbox 写入
	outboxes, _, err := fixture.store.Outbox().List(ctx, store.ListFilter{})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if len(outboxes) != 0 {
		t.Fatalf("expected 0 outbox entries from dryrun, got %d", len(outboxes))
	}

	// 断言 dryrun 本身 0 审计日志写入
	auditsAfter, err := fixture.store.Audits().List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list audits after: %v", err)
	}
	if len(auditsAfter) != len(auditsBefore) {
		t.Fatalf("expected 0 audit logs from dryrun, before=%d, after=%d", len(auditsBefore), len(auditsAfter))
	}
}

func TestJevWiring_AllEntrypointsIntegration(t *testing.T) {
	var notifCalls atomic.Int64
	var reviewCalls atomic.Int64
	var chatopsCalls atomic.Int64

	jevCfg := config.JevConfig{
		Enabled:                 true,
		NoiseFilterEnabled:      true,
		CascadeGateEnabled:      true,
		ChatOpsGuardrailEnabled: true,
		Mode:                    "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	rt.SetClientForTest("notification", &trackingJevClient{plane: "notification", counter: &notifCalls})
	rt.SetClientForTest("review", &trackingJevClient{plane: "review", counter: &reviewCalls})
	rt.SetClientForTest("chatops", &trackingJevClient{plane: "chatops", counter: &chatopsCalls})

	fixture := newHTTPTestFixture(t, httpTestOptions{
		jevRuntime: rt,
	})
	ctx := t.Context()

	// 1. 验证 ChatOps 路由到 chatops 面
	_, _ = fixture.store.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "chatops.feishu.allowed_user_ids",
		ValueJSON: []byte(`"user-1"`),
	})
	tok, err := chatops.NewTokenStore(fixture.store).Create(ctx, chatops.ActionData{
		Action:  "issue_reply_draft",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatalf("create chatops token: %v", err)
	}

	fsBody := map[string]any{
		"open_id": "user-1",
		"action": map[string]any{
			"value": map[string]any{
				"token": tok,
			},
		},
	}
	rawFs, _ := json.Marshal(fsBody)
	fsW := fixture.request(
		t,
		http.MethodPost,
		"/chatops/feishu/callback",
		string(rawFs),
		"127.0.0.1:45101",
		nil,
		map[string]string{"Content-Type": "application/json"},
	)
	if fsW.Code != http.StatusOK {
		t.Fatalf("expected 200 from feishu callback, got %d: %s", fsW.Code, fsW.Body.String())
	}
	if chatopsCalls.Load() != 1 {
		t.Fatalf("expected 1 call to chatops plane, got %d", chatopsCalls.Load())
	}

	// 2. 验证 Evaluator == nil 回退引擎使用 notification 面
	_, _ = fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-test-1",
		FullName:       "acme/demo",
		Owner:          "acme",
		Name:           "demo",
		MonitorEnabled: true,
		IssuesEnabled:  true,
		SyncStatus:     "synced",
	})
	_, _ = fixture.store.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          "ch-1",
		ChannelType: store.ChannelTelegram,
		Target:      "123",
		EventKinds:  []string{store.WorkItemKindIssue},
		Enabled:     true,
	})

	fallbackSvc := &webhooksvc.Service{
		Store:      fixture.store,
		Evaluator:  nil, // 显式 nil 回退
		JevRuntime: rt,
		Background: ctx,
	}

	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":     7,
			"title":      "hello",
			"state":      "open",
			"html_url":   "https://github.com/acme/demo/issues/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": map[string]any{
			"id":             99,
			"name":           "demo",
			"full_name":      "acme/demo",
			"private":        false,
			"html_url":       "https://github.com/acme/demo",
			"default_branch": "main",
			"owner":          map[string]any{"login": "acme"},
		},
	})

	deliv, _ := fixture.store.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:         "deliv-test-1",
		DeliveryID: "gh-deliv-1",
		EventType:  "issues",
		Payload:    payload,
		Status:     "received",
		ReceivedAt: time.Now().UTC(),
	})

	// 运行 Process，触发 nil Evaluator 回退 Engine
	fallbackSvc.Process(deliv.ID, "issues", "gh-deliv-1", "", payload)

	if notifCalls.Load() < 1 {
		t.Fatalf("expected at least 1 call to notification plane via fallback Engine, got %d", notifCalls.Load())
	}
}

func TestJevWiring_HotUpdateGeneration(t *testing.T) {
	initialCfg := config.JevConfig{
		Enabled: true,
		Model:   "jev-1.14.0",
		Mode:    "shadow",
	}
	rt := jev.RuntimeFromEnv(initialCfg)
	snap1 := rt.Snapshot()
	if snap1.Model != "jev-1.14.0" {
		t.Fatalf("expected model jev-1.14.0, got %s", snap1.Model)
	}

	// 构造下一代配置并 Replace
	nextCfg := initialCfg
	nextCfg.Model = "jev-1.15.0"
	nextRt := jev.RuntimeFromEnv(nextCfg)
	rt.Replace(nextRt)

	snap2 := rt.Snapshot()
	if snap2.Model != "jev-1.15.0" {
		t.Fatalf("expected replaced model jev-1.15.0, got %s", snap2.Model)
	}
}
