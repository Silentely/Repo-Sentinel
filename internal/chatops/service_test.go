package chatops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

type recordingExecutor struct {
	executedActions []ActionData
	returnErr       error
	returnOutcome   *Outcome
}

func (r *recordingExecutor) Execute(ctx context.Context, action ActionData) error {
	r.executedActions = append(r.executedActions, action)
	return r.returnErr
}

func (r *recordingExecutor) ExecuteWithOutcome(ctx context.Context, action ActionData) (*Outcome, error) {
	r.executedActions = append(r.executedActions, action)
	if r.returnOutcome != nil {
		return r.returnOutcome, r.returnErr
	}
	if r.returnErr != nil {
		return &Outcome{Status: StatusRetryableFailure, Action: action.Action, Message: "failed", Err: r.returnErr}, r.returnErr
	}
	return &Outcome{Status: StatusExecuted, Action: action.Action, Message: "success"}, nil
}

type recordingFeedback struct {
	answeredCallbacks []string
	editedKeyboards   int
}

func (f *recordingFeedback) AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) {
	f.answeredCallbacks = append(f.answeredCallbacks, callbackID+":"+text)
}

func (f *recordingFeedback) AnswerActionCallback(ctx context.Context, callbackID, actionName string) {
	f.answeredCallbacks = append(f.answeredCallbacks, callbackID+":action:"+actionName)
}

func (f *recordingFeedback) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, clickedData, actionName string, keyboard [][]map[string]any) {
	f.editedKeyboards++
}

func TestService_HandleTelegram_Lifecycle(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action:  "workflow_rerun",
		RepoID:  "repo-1",
		ActorID: "12345",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	exec := &recordingExecutor{}
	fb := &recordingFeedback{}
	svc := NewService(Dependencies{
		Store:            st,
		TokenStore:       ts,
		Executor:         exec,
		TelegramFeedback: fb,
	})

	// 1. Whitelist guard
	_, _ = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "chatops.telegram.allowed_user_ids",
		ValueJSON: []byte(`"12345"`),
		UpdatedAt: time.Now().UTC(),
	})

	// Unauthorized user
	outcome, err := svc.HandleTelegram(ctx, TelegramCallbackRequest{
		CallbackQueryID: "cb-1",
		Data:            "act:" + tokenID,
		FromID:          99999,
	})
	if err == nil || outcome.Status != StatusForbidden {
		t.Fatalf("expected forbidden for unauthorized user, got: %+v, %v", outcome, err)
	}

	// Authorized user
	outcome, err = svc.HandleTelegram(ctx, TelegramCallbackRequest{
		CallbackQueryID: "cb-2",
		Data:            "act:" + tokenID,
		FromID:          12345,
		ChatID:          100,
		MessageID:       200,
		InlineKeyboard:  [][]map[string]any{{{"text": "Rerun", "callback_data": "act:" + tokenID}}},
	})
	if err != nil || outcome.Status != StatusExecuted {
		t.Fatalf("expected executed outcome, got: %+v, %v", outcome, err)
	}
	if len(exec.executedActions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(exec.executedActions))
	}
	if exec.executedActions[0].Action != "workflow_rerun" {
		t.Fatalf("unexpected executed action: %s", exec.executedActions[0].Action)
	}
}

func TestService_HandleFeishu_Lifecycle(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action:  "issue_mark_duplicate",
		RepoID:  "repo-1",
		ActorID: "ou_valid_user",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	exec := &recordingExecutor{}
	svc := NewService(Dependencies{
		Store:      st,
		TokenStore: ts,
		Executor:   exec,
	})

	// Actor mismatch
	outcome, err := svc.HandleFeishu(ctx, FeishuCallbackRequest{
		TokenID:        tokenID,
		OperatorOpenID: "ou_other_user",
	})
	if err == nil || outcome.Status != StatusForbidden {
		t.Fatalf("expected forbidden on actor mismatch, got: %+v, %v", outcome, err)
	}

	// Correct actor
	outcome, err = svc.HandleFeishu(ctx, FeishuCallbackRequest{
		TokenID:        tokenID,
		OperatorOpenID: "ou_valid_user",
	})
	if err != nil || outcome.Status != StatusExecuted {
		t.Fatalf("expected executed outcome, got: %+v, %v", outcome, err)
	}
}

func TestService_HandleSlack_Lifecycle(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action: "pr_quick_approve",
		RepoID: "repo-1",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	exec := &recordingExecutor{
		returnErr: errors.New("github api rate limit"),
		returnOutcome: &Outcome{
			Status:  StatusRetryableFailure,
			Action:  "pr_quick_approve",
			Message: "rate limited",
			Err:     errors.New("github api rate limit"),
		},
	}
	svc := NewService(Dependencies{
		Store:      st,
		TokenStore: ts,
		Executor:   exec,
	})

	// Execution fails with retryable failure -> token is released
	outcome, err := svc.HandleSlack(ctx, SlackCallbackRequest{
		TokenID: tokenID,
		UserID:  "U12345",
	})
	if err == nil || outcome.Status != StatusRetryableFailure {
		t.Fatalf("expected retryable failure, got: %+v, %v", outcome, err)
	}

	// Since token was released, subsequent consumption succeeds
	reconsumed, err := ts.Consume(ctx, tokenID)
	if err != nil || reconsumed == nil {
		t.Fatalf("token should be released and retryable after retryable failure, got: %v", err)
	}
}
