package githubx

import (
	"testing"
)

func TestClassifyCIFailure_Categories(t *testing.T) {
	tests := []struct {
		name       string
		conclusion string
		detail     string
		wantCat    FailureCategory
		wantRetry  bool
	}{
		{
			name:       "network timeout",
			conclusion: "failure",
			detail:     "Get https://proxy.golang.org/x: dial tcp timeout\nexit code 1",
			wantCat:    CategoryFlakyNetwork,
			wantRetry:  true,
		},
		{
			name:       "github runner oom killed",
			conclusion: "failure",
			detail:     "Process completed with exit code 137.\nrunner lost communication",
			wantCat:    CategoryFlakyRunner,
			wantRetry:  true,
		},
		{
			name:       "unit test failed",
			conclusion: "failure",
			detail:     "--- FAIL: TestUserLogin (0.05s)\nassertionerror: expected 200 got 401",
			wantCat:    CategoryTestFailure,
			wantRetry:  false,
		},
		{
			name:       "linter error",
			conclusion: "failure",
			detail:     "running golangci-lint run\nmain.go:12: missing return",
			wantCat:    CategoryLintFailure,
			wantRetry:  false,
		},
		{
			name:       "compilation error",
			conclusion: "failure",
			detail:     "src/app.ts(12,5): error TS2322: Type 'string' is not assignable to type 'number'",
			wantCat:    CategoryBuildFailure,
			wantRetry:  false,
		},
		{
			name:       "workflow timed out",
			conclusion: "timed_out",
			detail:     "",
			wantCat:    CategoryTimeout,
			wantRetry:  true,
		},
		{
			name:       "unknown generic failure",
			conclusion: "failure",
			detail:     "process exited with status 1",
			wantCat:    CategoryUnknown,
			wantRetry:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyCIFailure(tt.conclusion, tt.detail)
			if got.Category != tt.wantCat {
				t.Errorf("got category %s, want %s", got.Category, tt.wantCat)
			}
			if got.CanRetry != tt.wantRetry {
				t.Errorf("got can_retry %v, want %v", got.CanRetry, tt.wantRetry)
			}
		})
	}
}
