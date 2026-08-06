package githubx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
