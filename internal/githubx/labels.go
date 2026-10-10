package githubx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// AllowedSentinelLabels maps normalized category names to canonical GitHub issue labels.
// Values intentionally do not carry any "sentinel:" prefix to match standard repository labels.
var AllowedSentinelLabels = map[string]string{
	"bug":             "bug",
	"bug report":      "bug",
	"enhancement":     "enhancement",
	"feature":         "enhancement",
	"feature request": "enhancement",
	"question":        "question",
	"documentation":   "documentation",
	"help wanted":     "help wanted",
	"help-wanted":     "help wanted",
	"performance":     "performance",
	"security":        "security",
}

// ErrLabelNotFound indicates GitHub returned 422 Unprocessable Entity (e.g. label does not exist).
var ErrLabelNotFound = errors.New("github_label_not_found")

// IsLabelNotFoundError reports whether err is or wraps ErrLabelNotFound.
func IsLabelNotFoundError(err error) bool {
	return errors.Is(err, ErrLabelNotFound)
}

// FilterAndMapLabels normalizes, filters against the whitelist, and deduplicates issue labels.
func FilterAndMapLabels(rawLabels []string) []string {
	if len(rawLabels) == 0 {
		return []string{}
	}
	seen := make(map[string]bool)
	var result []string
	for _, l := range rawLabels {
		clean := strings.ToLower(strings.TrimSpace(l))
		if mapped, ok := AllowedSentinelLabels[clean]; ok {
			if !seen[mapped] {
				seen[mapped] = true
				result = append(result, mapped)
			}
		}
	}
	if len(result) == 0 {
		return []string{}
	}
	sort.Strings(result)
	return result
}

// ShouldAutoLabelIssue checks whether an issue triage outcome qualifies for labeling.
// Spam, invalid, or empty label lists are rejected to protect repository health.
func ShouldAutoLabelIssue(category string, labels []string) bool {
	if len(labels) == 0 {
		return false
	}
	cat := strings.ToLower(strings.TrimSpace(category))
	if cat == "invalid" || cat == "spam" || cat == "none" || cat == "wontfix" {
		return false
	}
	return true
}

// HasLabel checks whether existing labels already contain the target label,
// accounting for case insensitivity, optional "sentinel:" prefix, dash/space variations,
// and canonical whitelist mapping (e.g. "bug report" matches "bug").
func HasLabel(existing []string, target string) bool {
	normTarget := normalizeLabel(target)
	if normTarget == "" {
		return false
	}
	for _, l := range existing {
		normExisting := normalizeLabel(l)
		if normExisting == normTarget {
			return true
		}
		if mapped, ok := AllowedSentinelLabels[normExisting]; ok && normalizeLabel(mapped) == normTarget {
			return true
		}
	}
	return false
}

func normalizeLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "sentinel:")
	s = strings.ReplaceAll(s, "-", " ")
	return strings.TrimSpace(s)
}

// FilterUnappliedLabels returns only the labels in targetLabels that are not already
// present in existing labels.
func FilterUnappliedLabels(targetLabels []string, existing []string) []string {
	if len(targetLabels) == 0 {
		return []string{}
	}
	if len(existing) == 0 {
		return targetLabels
	}
	var unapplied []string
	for _, l := range targetLabels {
		if !HasLabel(existing, l) {
			unapplied = append(unapplied, l)
		}
	}
	if len(unapplied) == 0 {
		return []string{}
	}
	return unapplied
}

// ExtractLabelNames extracts label name strings from a raw labels slice (e.g. from WorkItem.LabelsJSON).
func ExtractLabelNames(raw []any) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var names []string
	for _, item := range raw {
		switch v := item.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				names = append(names, trimmed)
			}
		case map[string]any:
			if name, ok := v["name"].(string); ok {
				if trimmed := strings.TrimSpace(name); trimmed != "" {
					names = append(names, trimmed)
				}
			}
		}
	}
	return names
}

// AddIssueLabels calls GitHub API to attach labels to an issue.
func (c *AppClient) AddIssueLabels(ctx context.Context, token, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)

	bodyData, err := json.Marshal(map[string][]string{"labels": labels})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+path, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", githubClientUA)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		bodyStr := string(body)
		var ghErr struct {
			Message string `json:"message"`
			Errors  []struct {
				Resource string `json:"resource"`
				Field    string `json:"field"`
				Code     string `json:"code"`
				Message  string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(body, &ghErr) == nil {
			for _, e := range ghErr.Errors {
				if e.Code == "already_exists" {
					return nil
				}
				if e.Code == "missing_field" || e.Code == "invalid" ||
					strings.Contains(strings.ToLower(e.Message), "does not exist") ||
					strings.Contains(strings.ToLower(e.Message), "not found") {
					return fmt.Errorf("%w: issue %d on %s/%s (%s)", ErrLabelNotFound, number, owner, repo, e.Message)
				}
			}
			if strings.Contains(strings.ToLower(ghErr.Message), "label does not exist") ||
				strings.Contains(strings.ToLower(ghErr.Message), "not found") {
				return fmt.Errorf("%w: issue %d on %s/%s (%s)", ErrLabelNotFound, number, owner, repo, ghErr.Message)
			}
		}
		if strings.Contains(strings.ToLower(bodyStr), "does not exist") || strings.Contains(strings.ToLower(bodyStr), "not found") {
			return fmt.Errorf("%w: issue %d on %s/%s", ErrLabelNotFound, number, owner, repo)
		}
		return statusError(resp.StatusCode, body)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return statusError(resp.StatusCode, body)
	}

	return nil
}

// RerunWorkflow requests GitHub to rerun a workflow run.
func (c *AppClient) RerunWorkflow(ctx context.Context, token, owner, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/rerun", owner, repo, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", githubClientUA)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return statusError(resp.StatusCode, body)
	}
	return nil
}
