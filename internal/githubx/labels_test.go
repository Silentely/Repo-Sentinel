package githubx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
)

func TestFilterAndMapLabels(t *testing.T) {
	cases := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "Valid labels mapped without sentinel prefix",
			input:    []string{"bug", "enhancement"},
			expected: []string{"bug", "enhancement"},
		},
		{
			name:     "Non-whitelisted labels stripped",
			input:    []string{"bug", "custom-junk", "crypto-miner", "help wanted"},
			expected: []string{"bug", "help wanted"},
		},
		{
			name:     "Case-insensitive matching and deduplication",
			input:    []string{"BUG", "bug", "Security", "security"},
			expected: []string{"bug", "security"},
		},
		{
			name:     "Empty input returns empty slice",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := githubx.FilterAndMapLabels(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected len %d, got %d: %v", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestFilterAndMapLabels_UsesIssueTriageCategories(t *testing.T) {
	got := githubx.FilterAndMapLabels([]string{"Bug Report", "Feature Request"})
	want := []string{"bug", "enhancement"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped labels = %#v, want %#v", got, want)
	}
}

func TestShouldAutoLabelIssue(t *testing.T) {
	// If triage category is invalid or spam, should NOT auto-label
	if githubx.ShouldAutoLabelIssue("invalid", []string{"bug"}) {
		t.Error("expected false for category 'invalid'")
	}
	if githubx.ShouldAutoLabelIssue("spam", []string{"bug"}) {
		t.Error("expected false for category 'spam'")
	}
	if githubx.ShouldAutoLabelIssue("valid", []string{}) {
		t.Error("expected false for empty labels")
	}
	if !githubx.ShouldAutoLabelIssue("valid", []string{"bug"}) {
		t.Error("expected true for valid category with mapped labels")
	}
}

func TestHasLabel(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		target   string
		expected bool
	}{
		{
			name:     "exact match",
			existing: []string{"bug"},
			target:   "bug",
			expected: true,
		},
		{
			name:     "case-insensitive match",
			existing: []string{"Bug"},
			target:   "bug",
			expected: true,
		},
		{
			name:     "legacy sentinel prefix match",
			existing: []string{"sentinel:bug"},
			target:   "bug",
			expected: true,
		},
		{
			name:     "target with prefix matches clean existing",
			existing: []string{"bug"},
			target:   "sentinel:bug",
			expected: true,
		},
		{
			name:     "dash and space variation match",
			existing: []string{"help-wanted"},
			target:   "help wanted",
			expected: true,
		},
		{
			name:     "category alias match (bug report matches bug)",
			existing: []string{"bug report"},
			target:   "bug",
			expected: true,
		},
		{
			name:     "not found",
			existing: []string{"documentation", "enhancement"},
			target:   "bug",
			expected: false,
		},
		{
			name:     "empty existing",
			existing: []string{},
			target:   "bug",
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := githubx.HasLabel(tc.existing, tc.target)
			if got != tc.expected {
				t.Fatalf("expected HasLabel(%v, %q) = %v, got %v", tc.existing, tc.target, tc.expected, got)
			}
		})
	}
}

func TestFilterUnappliedLabels(t *testing.T) {
	cases := []struct {
		name     string
		targets  []string
		existing []string
		expected []string
	}{
		{
			name:     "all already exist",
			targets:  []string{"bug"},
			existing: []string{"bug"},
			expected: []string{},
		},
		{
			name:     "partial existing",
			targets:  []string{"bug", "enhancement"},
			existing: []string{"bug"},
			expected: []string{"enhancement"},
		},
		{
			name:     "none exist",
			targets:  []string{"bug", "enhancement"},
			existing: []string{"documentation"},
			expected: []string{"bug", "enhancement"},
		},
		{
			name:     "empty existing returns all targets",
			targets:  []string{"bug"},
			existing: []string{},
			expected: []string{"bug"},
		},
		{
			name:     "empty targets returns empty",
			targets:  []string{},
			existing: []string{"bug"},
			expected: []string{},
		},
		{
			name:     "case insensitive and prefix matching removes target",
			targets:  []string{"bug"},
			existing: []string{"sentinel:bug"},
			expected: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := githubx.FilterUnappliedLabels(tc.targets, tc.existing)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected len %d, got %d: %v", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestExtractLabelNames(t *testing.T) {
	raw := []any{"bug", map[string]any{"name": "enhancement"}, "  documentation  ", 123}
	got := githubx.ExtractLabelNames(raw)
	want := []string{"bug", "enhancement", "documentation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppClient_AddIssueLabels(t *testing.T) {
	t.Run("Success 200 OK", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/repos/owner/repo/issues/42/labels" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
			}
			var body struct {
				Labels []string `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			if len(body.Labels) != 1 || body.Labels[0] != "bug" {
				t.Fatalf("unexpected labels in request: %v", body.Labels)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"bug"}]`))
		}))
		defer ts.Close()

		client := githubx.NewAppClient(1, "")
		client.BaseURL = ts.URL
		client.HTTP = ts.Client()

		err := client.AddIssueLabels(t.Context(), "test-token", "owner", "repo", 42, []string{"bug"})
		if err != nil {
			t.Fatalf("AddIssueLabels failed: %v", err)
		}
	})

	t.Run("422 Unprocessable Entity - Handled gracefully", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Label does not exist"}`))
		}))
		defer ts.Close()

		client := githubx.NewAppClient(1, "")
		client.BaseURL = ts.URL
		client.HTTP = ts.Client()

		err := client.AddIssueLabels(t.Context(), "test-token", "owner", "repo", 42, []string{"nonexistent"})
		if err == nil {
			t.Fatal("expected error on 422, got nil")
		}
		if !githubx.IsLabelNotFoundError(err) {
			t.Fatalf("expected IsLabelNotFoundError, got %v", err)
		}
	})
}
