package githubx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// IssueItem GitHub issues API 条目（含 PR）。
type IssueItem struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Draft  bool `json:"draft"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

// WorkflowRunItem Actions run。
type WorkflowRunItem struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	WorkflowID int64     `json:"workflow_id"`
	RunNumber  int       `json:"run_number"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`
	Conclusion *string   `json:"conclusion"`
	HTMLURL    string    `json:"html_url"`
	HeadBranch string    `json:"head_branch"`
	HeadSHA    string    `json:"head_sha"`
	RunAttempt int       `json:"run_attempt"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedAt  time.Time `json:"created_at"`
	Actor      struct {
		Login string `json:"login"`
	} `json:"actor"`
}

// AlertItem 安全告警通用结构。
type AlertItem struct {
	Number          int       `json:"number"`
	State           string    `json:"state"`
	HTMLURL         string    `json:"html_url"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Severity        string    `json:"severity"`
	DismissedReason string    `json:"dismissed_reason"`
	Dependency      *struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
	} `json:"dependency"`
	SecurityAdvisory *struct {
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
	} `json:"security_advisory"`
	Rule *struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
	} `json:"rule"`
	SecretType            string `json:"secret_type"`
	SecretTypeDisplayName string `json:"secret_type_display_name"`
}

// RepositoryMeta 仓库元数据（star 快照等用途）。
type RepositoryMeta struct {
	StargazersCount int64 `json:"stargazers_count"`
	ForksCount      int64 `json:"forks_count"`
	OpenIssuesCount int   `json:"open_issues_count"`
}

// ReleaseItem GitHub release 条目（star 仓库 release 追踪用）。
type ReleaseItem struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

// ListReleases 拉取仓库最新 release（per_page=1）。
// ifNoneMatch 非空时带 If-None-Match 条件请求；304 时 modified=false、items 为空。
// 返回响应 ETag 供下次条件请求；错误分类与 doJSON 一致（限流 / HTTPStatusError）。
func (c *AppClient) ListReleases(ctx context.Context, token, owner, repo, ifNoneMatch string) ([]ReleaseItem, string, bool, int, error) {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://api.github.com"
	}
	full := c.BaseURL + fmt.Sprintf("/repos/%s/%s/releases?per_page=1", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, "", false, 0, err
	}
	// 出站标识与鉴权头与 doJSON 保持一致。
	req.Header.Set("User-Agent", githubClientUA)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", false, 0, err
	}
	defer resp.Body.Close()
	var remaining int
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		remaining, _ = strconv.Atoi(v)
	}
	etag := resp.Header.Get("ETag")
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, false, remaining, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, etag, false, remaining, &RateLimitError{RetryAfter: parseRetryAfterHeader(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode == http.StatusForbidden {
		// 403 需区分限流与权限/功能未开启：主限流响应携带 X-RateLimit-Remaining: 0。
		if resp.Header.Get("X-RateLimit-Remaining") == "0" || bytes.Contains(body, []byte("rate limit")) {
			return nil, etag, false, remaining, &RateLimitError{RetryAfter: resetDelta(resp.Header.Get("X-RateLimit-Reset"))}
		}
		return nil, etag, false, remaining, statusError(resp.StatusCode, body)
	}
	if resp.StatusCode >= 300 {
		return nil, etag, false, remaining, statusError(resp.StatusCode, body)
	}
	var items []ReleaseItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, etag, false, remaining, err
	}
	return items, etag, true, remaining, nil
}

// ListIssues 分页拉取 issues（含 PR）。
func (c *AppClient) ListIssues(ctx context.Context, token, owner, repo string, since *time.Time, page int) ([]IssueItem, int, error) {
	q := url.Values{}
	q.Set("state", "all")
	q.Set("per_page", "100")
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	if since != nil {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	path := fmt.Sprintf("/repos/%s/%s/issues?%s", owner, repo, q.Encode())
	var items []IssueItem
	remaining, err := c.DoJSON(ctx, "GET", path, token, &items)
	return items, remaining, err
}

// ListWorkflowRuns 拉取 workflow runs。
func (c *AppClient) ListWorkflowRuns(ctx context.Context, token, owner, repo string, page int) ([]WorkflowRunItem, int, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=50&page=%d", owner, repo, page)
	var payload struct {
		WorkflowRuns []WorkflowRunItem `json:"workflow_runs"`
	}
	remaining, err := c.DoJSON(ctx, "GET", path, token, &payload)
	return payload.WorkflowRuns, remaining, err
}

// GetRepository 拉取仓库元数据（含 stargazers_count）。
func (c *AppClient) GetRepository(ctx context.Context, token, owner, repo string) (RepositoryMeta, int, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, repo)
	var meta RepositoryMeta
	remaining, err := c.DoJSON(ctx, "GET", path, token, &meta)
	return meta, remaining, err
}

// ListDependabotAlerts 拉取 dependabot alerts（游标分页）。
// Dependabot 端点不接收 page 参数（GitHub 返回 400），必须用 after 游标翻页；
// cursor 为空表示第一页，返回的 next 为下一页游标（来自 Link header rel="next"），空表示已到最后一页。
func (c *AppClient) ListDependabotAlerts(ctx context.Context, token, owner, repo, cursor string) ([]AlertItem, string, int, error) {
	path := fmt.Sprintf("/repos/%s/%s/dependabot/alerts?per_page=50", owner, repo)
	if cursor != "" {
		path += "&after=" + url.QueryEscape(cursor)
	}
	var items []AlertItem
	remaining, link, err := c.DoJSONPage(ctx, "GET", path, token, &items)
	if err != nil {
		return nil, "", remaining, err
	}
	return items, parseNextAfter(link), remaining, nil
}

// parseNextAfter 从 Link header 提取 rel="next" 指向 URL 中的 after 游标参数。
// GitHub 游标分页端点（如 dependabot alerts）的 Link 形如：
// <https://api.github.com/repos/o/r/dependabot/alerts?after=xxx&per_page=50>; rel="next"
func parseNextAfter(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		seg := strings.SplitN(part, ";", 2)
		if len(seg) != 2 || !strings.Contains(seg[1], `rel="next"`) {
			continue
		}
		u, err := url.Parse(strings.Trim(strings.TrimSpace(seg[0]), "<>"))
		if err != nil {
			continue
		}
		if after := u.Query().Get("after"); after != "" {
			return after
		}
	}
	return ""
}

// ListCodeScanningAlerts 拉取 code scanning alerts。
func (c *AppClient) ListCodeScanningAlerts(ctx context.Context, token, owner, repo string, page int) ([]AlertItem, int, error) {
	path := fmt.Sprintf("/repos/%s/%s/code-scanning/alerts?per_page=50&page=%d", owner, repo, page)
	var items []AlertItem
	remaining, err := c.DoJSON(ctx, "GET", path, token, &items)
	return items, remaining, err
}

// ListSecretScanningAlerts 拉取 secret scanning alerts。
func (c *AppClient) ListSecretScanningAlerts(ctx context.Context, token, owner, repo string, page int) ([]AlertItem, int, error) {
	path := fmt.Sprintf("/repos/%s/%s/secret-scanning/alerts?per_page=50&page=%d", owner, repo, page)
	var items []AlertItem
	remaining, err := c.DoJSON(ctx, "GET", path, token, &items)
	return items, remaining, err
}

// PRReviewItem PR 审核条目。
type PRReviewItem struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
	User  struct {
		Login string `json:"login"`
	} `json:"user"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// ListPRReviews 拉取 PR 的审核列表。
func (c *AppClient) ListPRReviews(ctx context.Context, token, owner, repo string, prNumber int) ([]PRReviewItem, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber)
	var items []PRReviewItem
	_, err := c.DoJSON(ctx, "GET", path, token, &items)
	return items, err
}

// CheckRunItem 检查运行条目。
type CheckRunItem struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Output     struct {
		Title string `json:"title"`
	} `json:"output"`
}

// ListCheckRuns 拉取 commit 的检查运行列表。
func (c *AppClient) ListCheckRuns(ctx context.Context, token, owner, repo, ref string) ([]CheckRunItem, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, repo, ref)
	var payload struct {
		CheckRuns []CheckRunItem `json:"check_runs"`
	}
	_, err := c.DoJSON(ctx, "GET", path, token, &payload)
	return payload.CheckRuns, err
}

// ListRequestedReviewers 拉取 PR 的请求审核人列表。
func (c *AppClient) ListRequestedReviewers(ctx context.Context, token, owner, repo string, prNumber int) ([]string, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/requested_reviewers", owner, repo, prNumber)
	var payload struct {
		Users []struct {
			Login string `json:"login"`
		} `json:"users"`
		Teams []struct {
			Slug string `json:"slug"`
		} `json:"teams"`
	}
	_, err := c.DoJSON(ctx, "GET", path, token, &payload)
	if err != nil {
		return nil, err
	}

	var reviewers []string
	for _, u := range payload.Users {
		reviewers = append(reviewers, u.Login)
	}
	for _, t := range payload.Teams {
		reviewers = append(reviewers, t.Slug)
	}
	return reviewers, nil
}

// PRDetailItem PR 详情条目（用于获取 head SHA）。
type PRDetailItem struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// GetPRDetail 拉取 PR 详情（获取 head SHA）。
func (c *AppClient) GetPRDetail(ctx context.Context, token, owner, repo string, prNumber int) (PRDetailItem, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	var item PRDetailItem
	_, err := c.DoJSON(ctx, "GET", path, token, &item)
	return item, err
}

// InstallationRepo 是 GET /installation/repositories 的条目。
type InstallationRepo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	HTMLURL  string `json:"html_url"`
	Archived bool   `json:"archived"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	DefaultBranch string `json:"default_branch"`
}

// repoSource 使 InstallationRepo 满足 normalizer.repoSource 接口，
// 以便直接传给 NormalizeRepository 复用仓库规范化逻辑。
func (r *InstallationRepo) GetFullName() string      { return r.FullName }
func (r *InstallationRepo) GetID() int64             { return r.ID }
func (r *InstallationRepo) GetName() string          { return r.Name }
func (r *InstallationRepo) GetHTMLURL() string       { return r.HTMLURL }
func (r *InstallationRepo) GetArchived() bool        { return r.Archived }
func (r *InstallationRepo) GetPrivate() bool         { return r.Private }
func (r *InstallationRepo) GetDefaultBranch() string { return r.DefaultBranch }
func (r *InstallationRepo) GetOwnerLogin() string    { return r.Owner.Login }

// ListInstallationRepositories 分页列出当前 Installation 可访问的仓库。
func (c *AppClient) ListInstallationRepositories(ctx context.Context, token string, page int) ([]InstallationRepo, int, error) {
	if page <= 0 {
		page = 1
	}
	path := fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page)
	var payload struct {
		Repositories []InstallationRepo `json:"repositories"`
	}
	remaining, err := c.DoJSON(ctx, "GET", path, token, &payload)
	return payload.Repositories, remaining, err
}

// PublicClient 用于外部公开仓轮询（可选 PAT）。
type PublicClient struct {
	PAT     string
	HTTP    *http.Client
	BaseURL string
}

// ListPublicIssues 列出公开仓 issues。
func (c *PublicClient) ListPublicIssues(ctx context.Context, owner, repo string, since *time.Time, page int) ([]IssueItem, int, error) {
	app := &AppClient{
		HTTP:    c.HTTP,
		BaseURL: c.BaseURL,
	}
	if app.HTTP == nil {
		app.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if app.BaseURL == "" {
		app.BaseURL = "https://api.github.com"
	}
	return app.ListIssues(ctx, c.PAT, owner, repo, since, page)
}

// GetRepository 拉取公开仓元数据（可选 PAT）。
func (c *PublicClient) GetRepository(ctx context.Context, owner, repo string) (RepositoryMeta, int, error) {
	app := &AppClient{
		HTTP:    c.HTTP,
		BaseURL: c.BaseURL,
	}
	if app.HTTP == nil {
		app.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if app.BaseURL == "" {
		app.BaseURL = "https://api.github.com"
	}
	return app.GetRepository(ctx, c.PAT, owner, repo)
}
