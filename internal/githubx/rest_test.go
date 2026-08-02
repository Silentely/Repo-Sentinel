package githubx

import (
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
