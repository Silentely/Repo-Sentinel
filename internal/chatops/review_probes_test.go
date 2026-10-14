package chatops

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/sideeffect"
)

type reviewLegacyExecutor struct{}

func (reviewLegacyExecutor) Execute(context.Context, ActionData) error {
	return context.DeadlineExceeded
}

func TestReviewUnknownErrors(t *testing.T) {
	for _, e := range []error{io.EOF, context.Canceled} {
		if !sideeffect.IsNetworkUnknownError(e) {
			t.Errorf("uncertain in-flight error classified safe: %v", e)
		}
	}
}

func TestReviewReceiptPersistFailure(t *testing.T) {
	e := &GitHubChatOpsExecutor{}
	o, _ := e.mapReceiptOutcome("issue_reply_draft", "ok", "id", errors.New("side_effect succeeded externally but receipt persist failed: database unavailable"))
	if o.CanReleaseToken() {
		t.Fatalf("externally succeeded receipt failure releases token: %s", o.Status)
	}
}

func TestReviewLegacyTimeoutRetainsToken(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	id, err := ts.Create(t.Context(), ActionData{Action: "issue_reply_draft"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(Dependencies{Store: st, TokenStore: ts, Executor: reviewLegacyExecutor{}})
	o, _ := s.HandleSlack(t.Context(), SlackCallbackRequest{TokenID: id, UserID: "u"})
	if _, err := ts.Consume(t.Context(), id); err == nil {
		t.Fatalf("legacy timeout token reconsumed, status=%s", o.Status)
	}
}
