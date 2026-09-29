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
var AllowedSentinelLabels = map[string]string{
	"bug":             "sentinel:bug",
	"bug report":      "sentinel:bug",
	"enhancement":     "sentinel:enhancement",
	"feature":         "sentinel:feature",
	"feature request": "sentinel:enhancement",
	"question":        "sentinel:question",
	"documentation":   "sentinel:documentation",
	"help wanted":     "sentinel:help-wanted",
	"help-wanted":     "sentinel:help-wanted",
	"performance":     "sentinel:performance",
	"security":        "sentinel:security",
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

// AddIssueLabels calls GitHub API to attach labels to an issue.
func (c *AppClient) AddIssueLabels(ctx context.Context, token, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	fullURL := path
	if strings.HasPrefix(path, "/") {
		fullURL = c.baseURL() + path
	}

	bodyData, err := json.Marshal(map[string][]string{"labels": labels})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(bodyData))
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
		_, _ = io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: issue %d on %s/%s", ErrLabelNotFound, number, owner, repo)
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
