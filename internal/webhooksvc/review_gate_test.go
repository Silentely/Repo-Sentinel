package webhooksvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

type mockReviewJevClient struct {
	judgeFunc func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
}

func (m *mockReviewJevClient) Judge(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
	if m.judgeFunc != nil {
		return m.judgeFunc(ctx, state, questions)
	}
	return jev.Result{}, nil
}

func ptrFloat(f float64) *float64 { return &f }
func ptrString(s string) *string  { return &s }

func TestShouldSkipByJev(t *testing.T) {
	ctx := context.Background()

	t.Run("nil client returns error", func(t *testing.T) {
		skip, err := shouldSkipByJev(ctx, nil, []jev.FileFact{{Path: "docs/a.txt", Documentation: true}})
		if err == nil || skip {
			t.Fatalf("expected error and skip=false, got err=%v, skip=%v", err, skip)
		}
	})

	t.Run("client returns error", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{}, errors.New("timeout or network failure")
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true}})
		if err == nil || skip {
			t.Fatalf("expected error and skip=false, got err=%v, skip=%v", err, skip)
		}
	})

	t.Run("state exceeds 8KiB returns ErrStateTooLarge", func(t *testing.T) {
		var largeFiles []jev.FileFact
		for i := 0; i < 500; i++ {
			largeFiles = append(largeFiles, jev.FileFact{
				Path:          fmt.Sprintf("docs/very/long/nested/directory/path/to/documentation/file_%d.md", i),
				Documentation: true,
			})
		}
		cli := &mockReviewJevClient{}
		skip, err := shouldSkipByJev(ctx, cli, largeFiles)
		if !errors.Is(err, jev.ErrStateTooLarge) || skip {
			t.Fatalf("expected ErrStateTooLarge and skip=false, got err=%v, skip=%v", err, skip)
		}
	})

	t.Run("satisfies all criteria returns skip true", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Model: "jev-1.14.0",
					Answers: map[string]jev.Answer{
						"needs_deep_review": {Type: "noul", Noul: ptrFloat(0.05)},
						"risk_surface": {
							Type:       "choice",
							Choice:     ptrString("documentation_only"),
							Confidence: ptrFloat(0.95),
						},
					},
				}, nil
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true, Ambiguous: true}})
		if err != nil || !skip {
			t.Fatalf("expected skip=true, err=nil; got skip=%v, err=%v", skip, err)
		}
	})

	t.Run("needs_deep_review exceeds 0.10 returns skip false", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Answers: map[string]jev.Answer{
						"needs_deep_review": {Type: "noul", Noul: ptrFloat(0.15)},
						"risk_surface": {
							Type:       "choice",
							Choice:     ptrString("documentation_only"),
							Confidence: ptrFloat(0.95),
						},
					},
				}, nil
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true, Ambiguous: true}})
		if err != nil || skip {
			t.Fatalf("expected skip=false, err=nil; got skip=%v, err=%v", skip, err)
		}
	})

	t.Run("risk_surface is not documentation_only returns skip false", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Answers: map[string]jev.Answer{
						"needs_deep_review": {Type: "noul", Noul: ptrFloat(0.05)},
						"risk_surface": {
							Type:       "choice",
							Choice:     ptrString("business_logic"),
							Confidence: ptrFloat(0.95),
						},
					},
				}, nil
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true, Ambiguous: true}})
		if err != nil || skip {
			t.Fatalf("expected skip=false, err=nil; got skip=%v, err=%v", skip, err)
		}
	})

	t.Run("risk_surface confidence is below 0.90 returns skip false", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Answers: map[string]jev.Answer{
						"needs_deep_review": {Type: "noul", Noul: ptrFloat(0.05)},
						"risk_surface": {
							Type:       "choice",
							Choice:     ptrString("documentation_only"),
							Confidence: ptrFloat(0.85),
						},
					},
				}, nil
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true, Ambiguous: true}})
		if err != nil || skip {
			t.Fatalf("expected skip=false, err=nil; got skip=%v, err=%v", skip, err)
		}
	})

	t.Run("missing answers returns skip false", func(t *testing.T) {
		cli := &mockReviewJevClient{
			judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
				return jev.Result{
					Answers: map[string]jev.Answer{},
				}, nil
			},
		}
		skip, err := shouldSkipByJev(ctx, cli, []jev.FileFact{{Path: "docs/a.txt", Documentation: true, Ambiguous: true}})
		if err != nil || skip {
			t.Fatalf("expected skip=false, err=nil; got skip=%v, err=%v", skip, err)
		}
	})
}
