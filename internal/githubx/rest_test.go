package githubx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseNextAfterEmptyOrInvalid(t *testing.T) {
	if got := parseNextAfter(""); got != "" {
		t.Fatalf("parseNextAfter empty got %q, want empty", got)
	}
	if got := parseNextAfter("   "); got != "" {
		t.Fatalf("parseNextAfter spaces got %q, want empty", got)
	}
	if got := parseNextAfter("invalid-link"); got != "" {
		t.Fatalf("parseNextAfter invalid got %q, want empty", got)
	}
}

func TestInstallationRepoImplementsRepoSource(t *testing.T) {
	var _ RepoSource = (*InstallationRepo)(nil)
}

type RepoSource interface {
	GetFullName() string
	GetID() int64
	GetName() string
	GetHTMLURL() string
	GetArchived() bool
	GetPrivate() bool
	GetDefaultBranch() string
	GetOwnerLogin() string
}

func TestInstallationRepoGetMethods(t *testing.T) {
	r := &InstallationRepo{
		ID: 1, Name: "demo", FullName: "acme/demo", Private: false,
		HTMLURL: "https://github.com/acme/demo", Archived: false, DefaultBranch: "main",
		Owner: struct {
			Login string `json:"login"`
		}{Login: "acme"},
	}
	if r.GetFullName() != "acme/demo" {
		t.Fatalf("GetFullName: %s", r.GetFullName())
	}
	if r.GetID() != 1 {
		t.Fatalf("GetID: %d", r.GetID())
	}
	if r.GetName() != "demo" {
		t.Fatalf("GetName: %s", r.GetName())
	}
	if r.GetHTMLURL() != "https://github.com/acme/demo" {
		t.Fatalf("GetHTMLURL: %s", r.GetHTMLURL())
	}
	if r.GetArchived() != false {
		t.Fatalf("GetArchived: %v", r.GetArchived())
	}
	if r.GetPrivate() != false {
		t.Fatalf("GetPrivate: %v", r.GetPrivate())
	}
	if r.GetDefaultBranch() != "main" {
		t.Fatalf("GetDefaultBranch: %s", r.GetDefaultBranch())
	}
	if r.GetOwnerLogin() != "acme" {
		t.Fatalf("GetOwnerLogin: %s", r.GetOwnerLogin())
	}
}

// TestGetRepositoryParsesStargazers 验证 GET /repos/{owner}/{repo} 的元数据解析与剩余配额透传。
func TestGetRepositoryParsesStargazers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Write([]byte(`{"full_name":"o/r","stargazers_count":123,"forks_count":4,"open_issues_count":2}`))
	}))
	defer srv.Close()
	c := &AppClient{HTTP: srv.Client(), BaseURL: srv.URL}
	meta, remaining, err := c.GetRepository(context.Background(), "tok", "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if meta.StargazersCount != 123 || meta.ForksCount != 4 || meta.OpenIssuesCount != 2 {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if remaining != 4999 {
		t.Fatalf("remaining = %d", remaining)
	}
}

// TestListReleasesConditional 验证 release 拉取、ETag 条件请求与 304 判定。
func TestListReleasesConditional(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/repos/o/r/releases" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != fmt.Sprintf("%d", ReleaseListPerPage) {
			t.Fatalf("per_page = %s, want %d（补拉需覆盖多条新 release）", got, ReleaseListPerPage)
		}
		w.Header().Set("ETag", `"rel-1"`)
		w.Header().Set("X-RateLimit-Remaining", "4999")
		if r.Header.Get("If-None-Match") == `"rel-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte(`[{"id":42,"tag_name":"v1.0","name":"First","draft":false,"prerelease":false,"published_at":"2026-08-01T00:00:00Z","html_url":"https://github.com/o/r/releases/tag/v1.0","body":"notes"}]`))
	}))
	defer srv.Close()
	c := &AppClient{HTTP: srv.Client(), BaseURL: srv.URL}

	items, etag, modified, _, err := c.ListReleases(context.Background(), "tok", "o", "r", 1, "")
	if err != nil || len(items) != 1 || !modified || etag != `"rel-1"` {
		t.Fatalf("first: items=%v etag=%q modified=%v err=%v", items, etag, modified, err)
	}
	if items[0].ID != 42 || items[0].TagName != "v1.0" || items[0].Prerelease {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	items, _, modified, _, err = c.ListReleases(context.Background(), "tok", "o", "r", 1, etag)
	if err != nil || modified || len(items) != 0 {
		t.Fatalf("conditional: modified=%v items=%v err=%v", modified, items, err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
}

// TestListReleasesNotFound 验证 404 归类为 HTTPStatusError，供调用方标记仓库不可用。
func TestListReleasesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := &AppClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, _, _, _, err := c.ListReleases(context.Background(), "tok", "o", "r", 1, "")
	var stErr *HTTPStatusError
	if !errors.As(err, &stErr) || stErr.StatusCode != 404 {
		t.Fatalf("want HTTPStatusError 404, got %v", err)
	}
}

// TestListUserStarred 验证匿名枚举用户公开 star 的分页与字段解析。
func TestListUserStarred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/octocat/starred" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("匿名请求不应带 Authorization: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-RateLimit-Remaining", "59")
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`[]`))
			return
		}
		w.Header().Set("Link", `<https://api.github.com/user/starred?page=2>; rel="next"`)
		w.Write([]byte(`[{"full_name":"octocat/Hello-World","private":false,"fork":false,"archived":false},{"full_name":"o/f","fork":true}]`))
	}))
	defer srv.Close()
	p := &PublicClient{HTTP: srv.Client(), BaseURL: srv.URL}

	items, link, remaining, err := p.ListUserStarred(context.Background(), "octocat", 1)
	if err != nil || len(items) != 2 || items[0].FullName != "octocat/Hello-World" {
		t.Fatalf("page1: items=%v link=%q remaining=%d err=%v", items, link, remaining, err)
	}
	if !strings.Contains(link, "page=2") {
		t.Fatalf("应有 next link: %q", link)
	}
	if items[1].Fork != true {
		t.Fatalf("fork 标记应解析: %+v", items[1])
	}
	items2, link2, _, err := p.ListUserStarred(context.Background(), "octocat", 2)
	if err != nil || len(items2) != 0 || link2 != "" {
		t.Fatalf("page2: items=%v link=%q err=%v", items2, link2, err)
	}
}

// TestListUserStarredWithPAT 验证配置了 PAT 时请求头携带 Authorization Bearer。
func TestListUserStarredWithPAT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/octocat/starred" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ghp_secret_123" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer ghp_secret_123")
		}
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Write([]byte(`[{"full_name":"octocat/Hello-World","private":false,"fork":false,"archived":false}]`))
	}))
	defer srv.Close()
	p := &PublicClient{PAT: "ghp_secret_123", HTTP: srv.Client(), BaseURL: srv.URL}

	items, _, remaining, err := p.ListUserStarred(context.Background(), "octocat", 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListUserStarred error: %v", err)
	}
	if remaining != 4999 {
		t.Fatalf("remaining = %d, want 4999", remaining)
	}
}

func TestListWorkflowJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/demo/actions/runs/12345/jobs" {
			http.NotFound(w, r)
			return
		}
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-tok" {
			t.Errorf("expected bearer token, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if page == "1" {
			w.Header().Set("Link", `<https://api.github.com/repos/acme/demo/actions/runs/12345/jobs?page=2>; rel="next"`)
		}
		w.WriteHeader(http.StatusOK)
		if page == "2" {
			w.Write([]byte(`{"total_count":2,"jobs":[{"id":2,"name":"lint","conclusion":"success"}]}`))
			return
		}
		w.Write([]byte(`{
			"total_count": 2,
			"jobs": [
				{
					"id": 1,
					"name": "build-and-test",
					"conclusion": "failure",
					"steps": [
						{"name": "checkout", "conclusion": "success", "number": 1},
						{"name": "run test", "conclusion": "failure", "number": 2}
					]
				}
			]
		}`))
	}))
	defer srv.Close()

	client := NewAppClient(100, "")
	client.BaseURL = srv.URL

	jobs, err := client.ListWorkflowJobs(context.Background(), "test-tok", "acme", "demo", 12345)
	if err != nil {
		t.Fatalf("ListWorkflowJobs failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs across pages, got %d", len(jobs))
	}
	if jobs[0].Name != "build-and-test" || jobs[0].Conclusion != "failure" {
		t.Fatalf("unexpected job: %+v", jobs[0])
	}
	if len(jobs[0].Steps) != 2 || jobs[0].Steps[1].Name != "run test" || jobs[0].Steps[1].Conclusion != "failure" {
		t.Fatalf("unexpected steps: %+v", jobs[0].Steps)
	}
}

// TestValidGitHubUsername 验证 GitHub 用户名字符集校验：含 "/"、空格、控制字符、
// 超长、以连字符起止的值必须拒绝（它们会拼出错误 API 路径且用户侧无反馈）。
func TestValidGitHubUsername(t *testing.T) {
	valid := []string{"a", "octocat", "Ab-1", "a-b-c", "012345678901234567890123456789012345678"}
	for _, v := range valid {
		if !ValidGitHubUsername(v) {
			t.Fatalf("%q 应判为合法", v)
		}
	}
	invalid := []string{"", "octo cat", "octo/cat", "octo cat/../x", "-octocat", "octocat-", "octo@cat", "octo.cat",
		"0123456789012345678901234567890123456789", "octocat\n", " octocat", "octocat ", "octo:cat", "café"}
	for _, v := range invalid {
		if ValidGitHubUsername(v) {
			t.Fatalf("%q 应判为非法", v)
		}
	}
}

// TestListUserStarredEscapesUsername 验证用户名进入路径前被转义：含 "/" 的脏值
// 不再拼出越界路径（/users/a/b/starred），而是作为单一路径段转义后请求。
func TestListUserStarredEscapesUsername(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath 才是线上真实发出的路径（URL.Path 已被解码）。
		gotPath = r.URL.EscapedPath()
		w.Header().Set("X-RateLimit-Remaining", "59")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	p := &PublicClient{HTTP: srv.Client(), BaseURL: srv.URL}

	if _, _, _, err := p.ListUserStarred(context.Background(), "../evil", 1); err != nil {
		t.Fatalf("ListUserStarred: %v", err)
	}
	if gotPath != "/users/..%2Fevil/starred" {
		t.Fatalf("path = %q, 用户名应被转义为单一路径段", gotPath)
	}
}
