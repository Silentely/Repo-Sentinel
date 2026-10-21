package chatops_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func ptrFloat(f float64) *float64 { return &f }
func ptrString(s string) *string  { return &s }

type mockChatOpsJevClient struct {
	calls     atomic.Int64
	judgeFunc func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
}

func (m *mockChatOpsJevClient) Judge(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
	m.calls.Add(1)
	if m.judgeFunc != nil {
		return m.judgeFunc(ctx, state, questions)
	}
	return jev.Result{}, nil
}

type recordingExecutor struct {
	executedActions []chatops.ActionData
	returnErr       error
	returnOutcome   *chatops.Outcome
	mu              sync.Mutex
}

func (r *recordingExecutor) Execute(ctx context.Context, action chatops.ActionData) error {
	r.mu.Lock()
	r.executedActions = append(r.executedActions, action)
	r.mu.Unlock()
	return r.returnErr
}

func (r *recordingExecutor) ExecuteWithOutcome(ctx context.Context, action chatops.ActionData) (*chatops.Outcome, error) {
	r.mu.Lock()
	r.executedActions = append(r.executedActions, action)
	r.mu.Unlock()
	if r.returnOutcome != nil {
		return r.returnOutcome, r.returnErr
	}
	if r.returnErr != nil {
		return &chatops.Outcome{Status: chatops.StatusRetryableFailure, Action: action.Action, Message: "failed", Err: r.returnErr}, r.returnErr
	}
	return &chatops.Outcome{Status: chatops.StatusExecuted, Action: action.Action, Message: "success"}, nil
}

func (r *recordingExecutor) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.executedActions)
}

func newTestChatOpsStore(t *testing.T) store.Store {
	t.Helper()
	tempDir := t.TempDir()
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + tempDir + "/chatops_jev_test.db",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = opened.Close()
	})
	return opened
}

func TestJevActionMatrix(t *testing.T) {
	expected := map[string]string{
		"pr_snooze":                 "low",
		"issue_snooze":              "low",
		"workflow_rerun":            "medium",
		"issue_apply_labels":        "medium",
		"issue_request_details":     "medium",
		"issue_reply_draft":         "high",
		"issue_mark_spam_and_close": "high",
		"issue_mark_duplicate":      "high",
		"pr_quick_approve":          "critical",
	}
	for action, want := range expected {
		got, ok := chatops.ActionRisk(action)
		if !ok || got != want {
			t.Fatalf("%s 分类错误: got %q, %v, want %q", action, got, ok, want)
		}
	}
	if _, ok := chatops.ActionRisk("unknown_action"); ok {
		t.Fatal("未知动作必须拒绝")
	}
	if !(chatops.Outcome{Status: chatops.StatusGuardrailUnavailable}).CanReleaseToken() {
		t.Fatal("副作用前不可用应释放")
	}
	if (chatops.Outcome{Status: chatops.StatusGuardrailDenied}).CanReleaseToken() {
		t.Fatal("明确拒绝不得重新抽样")
	}
	if (chatops.Outcome{Status: chatops.StatusIndeterminate}).CanReleaseToken() {
		t.Fatal("在途不确定不得释放")
	}
}

func setupChatOpsTestService(
	t *testing.T,
	st store.Store,
	mode string,
	mockJev *mockChatOpsJevClient,
	exec chatops.ActionExecutor,
) chatops.Service {
	t.Helper()
	jevCfg := config.JevConfig{
		Enabled:                 true,
		ChatOpsGuardrailEnabled: true,
		Mode:                    mode,
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	if mockJev != nil {
		rt.SetClientForTest("chatops", mockJev)
	}

	return chatops.NewService(chatops.Dependencies{
		Store:      st,
		TokenStore: chatops.NewTokenStore(st),
		Executor:   exec,
		JevRuntime: rt,
	})
}

func TestChatOps_9ActionsDecisionMatrix(t *testing.T) {
	actions := []string{
		"pr_snooze",
		"issue_snooze",
		"workflow_rerun",
		"issue_apply_labels",
		"issue_request_details",
		"issue_reply_draft",
		"issue_mark_spam_and_close",
		"issue_mark_duplicate",
		"pr_quick_approve",
	}

	decisions := []struct {
		name      string
		judgeFunc func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
	}{
		{
			name: "allow",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("allow"), Confidence: ptrFloat(0.95)},
					},
				}, nil
			},
		},
		{
			name: "deny",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("deny"), Confidence: ptrFloat(0.95)},
					},
				}, nil
			},
		},
		{
			name: "uncertain",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("uncertain"), Confidence: ptrFloat(0.95)},
					},
				}, nil
			},
		},
		{
			name: "low_confidence",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("allow"), Confidence: ptrFloat(0.80)},
					},
				}, nil
			},
		},
		{
			name: "timeout_error",
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{}, errors.New("timeout connecting to jev")
			},
		},
	}

	for _, act := range actions {
		risk, _ := chatops.ActionRisk(act)
		for _, dec := range decisions {
			t.Run(act+"_"+dec.name+"_active", func(t *testing.T) {
				st := newTestChatOpsStore(t)
				mockJev := &mockChatOpsJevClient{judgeFunc: dec.judgeFunc}
				exec := &recordingExecutor{}
				svc := setupChatOpsTestService(t, st, "active", mockJev, exec)
				ctx := t.Context()

				tok, err := svc.CreateToken(ctx, chatops.ActionData{
					Action:  act,
					RepoID:  "repo-1",
					ActorID: "user-1",
				}, time.Hour)
				if err != nil {
					t.Fatalf("create token: %v", err)
				}

				outcome, handleErr := svc.HandleFeishu(ctx, chatops.FeishuCallbackRequest{
					TokenID:        tok,
					OperatorOpenID: "user-1",
				})

				switch risk {
				case "low":
					// low: 本地放行，0 Jev，1 Executor
					if mockJev.calls.Load() != 0 {
						t.Fatalf("expected 0 Jev calls for low action, got %d", mockJev.calls.Load())
					}
					if exec.callCount() != 1 {
						t.Fatalf("expected 1 executor call for low action, got %d", exec.callCount())
					}
					if handleErr != nil || outcome.Status != chatops.StatusExecuted {
						t.Fatalf("expected executed for low action, got %+v, %v", outcome, handleErr)
					}

				case "medium":
					// medium: advisory，1 Jev，1 Executor
					if mockJev.calls.Load() != 1 {
						t.Fatalf("expected 1 Jev call for medium action, got %d", mockJev.calls.Load())
					}
					if exec.callCount() != 1 {
						t.Fatalf("expected 1 executor call for medium advisory action, got %d", exec.callCount())
					}
					if handleErr != nil || outcome.Status != chatops.StatusExecuted {
						t.Fatalf("expected executed for medium advisory action, got %+v, %v", outcome, handleErr)
					}

				case "high", "critical":
					// high/critical: 明确 allow 才能执行，其余均阻断且 Executor=0
					if mockJev.calls.Load() != 1 {
						t.Fatalf("expected 1 Jev call, got %d", mockJev.calls.Load())
					}
					if dec.name == "allow" {
						if exec.callCount() != 1 {
							t.Fatalf("expected 1 executor call on explicit allow, got %d", exec.callCount())
						}
						if handleErr != nil || outcome.Status != chatops.StatusExecuted {
							t.Fatalf("expected executed, got %+v, %v", outcome, handleErr)
						}
					} else {
						if exec.callCount() != 0 {
							t.Fatalf("expected 0 executor calls on non-allow %s, got %d", dec.name, exec.callCount())
						}
						if handleErr == nil {
							t.Fatalf("expected error on non-allow %s", dec.name)
						}
						if dec.name == "deny" {
							if outcome.Status != chatops.StatusGuardrailDenied {
								t.Fatalf("expected StatusGuardrailDenied, got %s", outcome.Status)
							}
						} else {
							if outcome.Status != chatops.StatusGuardrailUnavailable {
								t.Fatalf("expected StatusGuardrailUnavailable, got %s", outcome.Status)
							}
						}
					}
				}
			})

			// Shadow mode: 即使 high/critical 且 Jev 返回 deny/error，也仅审计，不拦截 (Executor=1)
			if risk == "high" || risk == "critical" {
				t.Run(act+"_"+dec.name+"_shadow", func(t *testing.T) {
					st := newTestChatOpsStore(t)
					mockJev := &mockChatOpsJevClient{judgeFunc: dec.judgeFunc}
					exec := &recordingExecutor{}
					svc := setupChatOpsTestService(t, st, "shadow", mockJev, exec)
					ctx := t.Context()

					tok, err := svc.CreateToken(ctx, chatops.ActionData{
						Action:  act,
						RepoID:  "repo-1",
						ActorID: "user-1",
					}, time.Hour)
					if err != nil {
						t.Fatalf("create token: %v", err)
					}

					outcome, handleErr := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
						TokenID: tok,
						UserID:  "user-1",
					})

					if mockJev.calls.Load() != 1 {
						t.Fatalf("expected 1 Jev call in shadow mode, got %d", mockJev.calls.Load())
					}
					if exec.callCount() != 1 {
						t.Fatalf("expected 1 executor call in shadow mode, got %d", exec.callCount())
					}
					if handleErr != nil || outcome.Status != chatops.StatusExecuted {
						t.Fatalf("expected executed in shadow mode, got %+v, %v", outcome, handleErr)
					}
				})
			}
		}
	}
}

func TestChatOps_UnavailableTokenRetryableVsDenied(t *testing.T) {
	st := newTestChatOpsStore(t)
	ctx := t.Context()

	// 1. Unavailable: 令牌释放成功后，同 token 可以重试
	t.Run("unavailable_releases_token_for_retry", func(t *testing.T) {
		var callCount atomic.Int64
		mockJev := &mockChatOpsJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				if callCount.Add(1) == 1 {
					return jev.Result{}, errors.New("temporary timeout")
				}
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("allow"), Confidence: ptrFloat(0.95)},
					},
				}, nil
			},
		}
		exec := &recordingExecutor{}
		svc := setupChatOpsTestService(t, st, "active", mockJev, exec)

		tok, err := svc.CreateToken(ctx, chatops.ActionData{
			Action:  "issue_reply_draft",
			RepoID:  "repo-1",
			ActorID: "user-1",
		}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}

		// 第一次调用：返回 Unavailable，令牌被释放
		out1, err1 := svc.HandleFeishu(ctx, chatops.FeishuCallbackRequest{
			TokenID:        tok,
			OperatorOpenID: "user-1",
		})
		if !errors.Is(err1, chatops.ErrGuardrailUnavailable) || out1.Status != chatops.StatusGuardrailUnavailable {
			t.Fatalf("expected guardrail unavailable on first call, got %+v, %v", out1, err1)
		}
		if exec.callCount() != 0 {
			t.Fatalf("executor should not be called on unavailable")
		}

		// 第二次调用（重试同 token）：Jev 返回 allow，执行成功
		out2, err2 := svc.HandleFeishu(ctx, chatops.FeishuCallbackRequest{
			TokenID:        tok,
			OperatorOpenID: "user-1",
		})
		if err2 != nil || out2.Status != chatops.StatusExecuted {
			t.Fatalf("expected retry with same token to succeed, got %+v, %v", out2, err2)
		}
		if exec.callCount() != 1 {
			t.Fatalf("executor should be called exactly once after retry")
		}
	})

	// 2. Denied: 令牌不得释放，同 token 再次调用直接 invalid_token
	t.Run("denied_does_not_release_token", func(t *testing.T) {
		mockJev := &mockChatOpsJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"safety_decision": {Type: "choice", Choice: ptrString("deny"), Confidence: ptrFloat(0.95)},
					},
				}, nil
			},
		}
		exec := &recordingExecutor{}
		svc := setupChatOpsTestService(t, st, "active", mockJev, exec)

		tok, err := svc.CreateToken(ctx, chatops.ActionData{
			Action:  "issue_mark_spam_and_close",
			RepoID:  "repo-1",
			ActorID: "user-1",
		}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}

		// 第一次调用：返回 Denied，令牌未释放
		out1, err1 := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
			TokenID: tok,
			UserID:  "user-1",
		})
		if !errors.Is(err1, chatops.ErrGuardrailDenied) || out1.Status != chatops.StatusGuardrailDenied {
			t.Fatalf("expected guardrail denied on first call, got %+v, %v", out1, err1)
		}

		// 第二次调用：同 token 已被 consume 且未 release，判定为 invalid_token
		out2, err2 := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
			TokenID: tok,
			UserID:  "user-1",
		})
		if err2 == nil || out2.Status != chatops.StatusInvalidToken {
			t.Fatalf("expected invalid_token on replay of denied token, got %+v, %v", out2, err2)
		}
		if exec.callCount() != 0 {
			t.Fatalf("executor should never be called for denied token")
		}
	})
}

func TestChatOps_ParentContextCanceledStillReleases(t *testing.T) {
	st := newTestChatOpsStore(t)
	cancelCtx, cancel := context.WithCancel(context.Background())
	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			cancel()
			return jev.Result{}, cancelCtx.Err()
		},
	}
	exec := &recordingExecutor{}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)

	tok, err := svc.CreateToken(context.Background(), chatops.ActionData{
		Action:  "pr_quick_approve",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleFeishu(cancelCtx, chatops.FeishuCallbackRequest{
		TokenID:        tok,
		OperatorOpenID: "user-1",
	})
	if !errors.Is(err, chatops.ErrGuardrailUnavailable) || out.Status != chatops.StatusGuardrailUnavailable {
		t.Fatalf("expected guardrail unavailable, got %+v, %v", out, err)
	}

	// 验证 token 成功释放：即使父 context 已在执行中被取消，清理仍成功完成，可再次消费
	consumeAction, err := chatops.NewTokenStore(st).Consume(context.Background(), tok)
	if err != nil {
		t.Fatalf("expected token to be released even when parent context was canceled: %v", err)
	}
	if consumeAction.Action != "pr_quick_approve" {
		t.Fatalf("expected pr_quick_approve, got %s", consumeAction.Action)
	}
}

type failingReleaseTokenStore struct {
	chatops.TokenStore
}

func (f *failingReleaseTokenStore) Release(ctx context.Context, id string) error {
	return errors.New("simulated database lock / release error")
}

func TestChatOps_ReleaseFailedCannotReportRetryable(t *testing.T) {
	st := newTestChatOpsStore(t)
	realTS := chatops.NewTokenStore(st)
	failingTS := &failingReleaseTokenStore{TokenStore: realTS}

	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{}, errors.New("timeout")
		},
	}

	jevCfg := config.JevConfig{
		Enabled:                 true,
		ChatOpsGuardrailEnabled: true,
		Mode:                    "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	rt.SetClientForTest("chatops", mockJev)

	svc := chatops.NewService(chatops.Dependencies{
		Store:      st,
		TokenStore: failingTS,
		Executor:   &recordingExecutor{},
		JevRuntime: rt,
	})

	ctx := t.Context()
	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "issue_reply_draft",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
		TokenID: tok,
		UserID:  "user-1",
	})

	if !errors.Is(err, chatops.ErrGuardrailUnavailable) || out.Status != chatops.StatusGuardrailUnavailable {
		t.Fatalf("expected guardrail unavailable, got %+v, %v", out, err)
	}
	// 释放失败不得谎报可重试，必须提示重新生成操作
	if !strings.Contains(out.Message, "请重新生成操作") {
		t.Fatalf("expected message to mention regenerating action when release fails, got: %s", out.Message)
	}
}

func TestChatOps_ActorMismatchFailsBeforeJev(t *testing.T) {
	st := newTestChatOpsStore(t)
	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			t.Fatal("Jev should not be called when Actor verification fails")
			return jev.Result{}, nil
		},
	}
	exec := &recordingExecutor{}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)
	ctx := t.Context()

	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "issue_reply_draft",
		RepoID:  "repo-1",
		ActorID: "authorized-user",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleFeishu(ctx, chatops.FeishuCallbackRequest{
		TokenID:        tok,
		OperatorOpenID: "unauthorized-user",
	})
	if !errors.Is(err, chatops.ErrActorMismatch) || out.Status != chatops.StatusForbidden {
		t.Fatalf("expected actor mismatch forbidden, got %+v, %v", out, err)
	}
	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls on actor mismatch, got %d", mockJev.calls.Load())
	}
	if exec.callCount() != 0 {
		t.Fatalf("expected 0 executor calls on actor mismatch, got %d", exec.callCount())
	}
}

func TestChatOps_UnknownActionLocalReject(t *testing.T) {
	st := newTestChatOpsStore(t)
	mockJev := &mockChatOpsJevClient{}
	exec := &recordingExecutor{}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)
	ctx := t.Context()

	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "dangerous_unknown_action",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
		TokenID: tok,
		UserID:  "user-1",
	})
	if !errors.Is(err, chatops.ErrGuardrailDenied) || out.Status != chatops.StatusGuardrailDenied {
		t.Fatalf("expected guardrail denied for unknown action, got %+v, %v", out, err)
	}
	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls for unknown action, got %d", mockJev.calls.Load())
	}
	if exec.callCount() != 0 {
		t.Fatalf("expected 0 executor calls for unknown action, got %d", exec.callCount())
	}
}

func TestChatOps_Concurrent100ClicksAtMostOneExecution(t *testing.T) {
	st := newTestChatOpsStore(t)
	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{
				Model: "jev-1.14.0",
				Answers: map[string]jev.Answer{
					"safety_decision": {Type: "choice", Choice: ptrString("allow"), Confidence: ptrFloat(0.95)},
				},
			}, nil
		},
	}
	exec := &recordingExecutor{}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)
	ctx := t.Context()

	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "issue_reply_draft",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	const concurrency = 100
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var successCount atomic.Int64
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			out, err := svc.HandleFeishu(ctx, chatops.FeishuCallbackRequest{
				TokenID:        tok,
				OperatorOpenID: "user-1",
			})
			if err == nil && out != nil && out.Status == chatops.StatusExecuted {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful execution across 100 concurrent requests, got %d", successCount.Load())
	}
	if exec.callCount() != 1 {
		t.Fatalf("expected exactly 1 executor invocation, got %d", exec.callCount())
	}
}

func TestChatOps_IndeterminateDoesNotReleaseToken(t *testing.T) {
	st := newTestChatOpsStore(t)
	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{
				Model: "jev-1.14.0",
				Answers: map[string]jev.Answer{
					"safety_decision": {Type: "choice", Choice: ptrString("allow"), Confidence: ptrFloat(0.95)},
				},
			}, nil
		},
	}
	// Executor 返回网络超时 / 在途不确定
	exec := &recordingExecutor{
		returnOutcome: &chatops.Outcome{
			Status:  chatops.StatusIndeterminate,
			Action:  "workflow_rerun",
			Message: "timeout during execution",
			Err:     context.DeadlineExceeded,
		},
		returnErr: context.DeadlineExceeded,
	}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)
	ctx := t.Context()

	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "workflow_rerun",
		RepoID:  "repo-1",
		ActorID: "user-1",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
		TokenID: tok,
		UserID:  "user-1",
	})
	if out.Status != chatops.StatusIndeterminate {
		t.Fatalf("expected StatusIndeterminate, got %s", out.Status)
	}

	// 令牌不确定态严禁释放，重新调用应判定为 invalid_token
	out2, err2 := svc.HandleSlack(ctx, chatops.SlackCallbackRequest{
		TokenID: tok,
		UserID:  "user-1",
	})
	if err2 == nil || out2.Status != chatops.StatusInvalidToken {
		t.Fatalf("expected invalid_token because indeterminate token cannot be released, got %+v, %v", out2, err2)
	}
}

func TestChatOps_Telegram_GuardrailFeedback(t *testing.T) {
	st := newTestChatOpsStore(t)
	ctx := t.Context()

	// 1. 白名单配置
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(`"12345"`),
		UpdatedAt: time.Now().UTC(),
	})

	mockJev := &mockChatOpsJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{
				Model: "jev-1.14.0",
				Answers: map[string]jev.Answer{
					"safety_decision": {Type: "choice", Choice: ptrString("deny"), Confidence: ptrFloat(0.95)},
				},
			}, nil
		},
	}
	exec := &recordingExecutor{}
	svc := setupChatOpsTestService(t, st, "active", mockJev, exec)

	tok, err := svc.CreateToken(ctx, chatops.ActionData{
		Action:  "issue_reply_draft",
		RepoID:  "repo-1",
		ActorID: "12345",
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.HandleTelegram(ctx, chatops.TelegramCallbackRequest{
		CallbackQueryID: "cb-tg-1",
		Data:            "act:" + tok,
		FromID:          12345,
	})
	if !errors.Is(err, chatops.ErrGuardrailDenied) || out.Status != chatops.StatusGuardrailDenied {
		t.Fatalf("expected guardrail denied, got %+v, %v", out, err)
	}
	if exec.callCount() != 0 {
		t.Fatalf("executor should not be called")
	}
}
