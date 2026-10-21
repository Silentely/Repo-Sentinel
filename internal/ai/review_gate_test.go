package ai

import (
	"errors"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

func TestLocalReviewGate(t *testing.T) {
	cases := []struct {
		files []jev.FileFact
		want  string
	}{
		{nil, "deep"},
		{[]jev.FileFact{{Path: "docs/guide.md", Documentation: true}}, "skip"},
		{[]jev.FileFact{{Path: ".github/workflows/ci.yml", Sensitive: true}}, "deep"},
		{[]jev.FileFact{{Path: "internal/auth.go"}}, "deep"},
		{[]jev.FileFact{{Path: "docs/guide.md", Documentation: true, Ambiguous: true}}, "consult"},
	}
	for _, tc := range cases {
		if got := LocalReviewGate(tc.files); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

func TestLocalReviewGate_Invariants(t *testing.T) {
	// 任何包含空路径、敏感文件或非文档文件的，均强制 deep
	t.Run("empty path", func(t *testing.T) {
		got := LocalReviewGate([]jev.FileFact{{Path: "", Documentation: true}})
		if got != "deep" {
			t.Fatalf("expected deep, got %s", got)
		}
	})
	t.Run("empty slice", func(t *testing.T) {
		got := LocalReviewGate([]jev.FileFact{})
		if got != "deep" {
			t.Fatalf("expected deep, got %s", got)
		}
	})
	t.Run("mixed docs and sensitive", func(t *testing.T) {
		got := LocalReviewGate([]jev.FileFact{
			{Path: "README.md", Documentation: true},
			{Path: "go.sum", Sensitive: true},
		})
		if got != "deep" {
			t.Fatalf("expected deep, got %s", got)
		}
	})
	t.Run("mixed docs and code", func(t *testing.T) {
		got := LocalReviewGate([]jev.FileFact{
			{Path: "README.md", Documentation: true},
			{Path: "main.go", Documentation: false},
		})
		if got != "deep" {
			t.Fatalf("expected deep, got %s", got)
		}
	})
	t.Run("multiple docs with one ambiguous", func(t *testing.T) {
		got := LocalReviewGate([]jev.FileFact{
			{Path: "README.md", Documentation: true},
			{Path: "docs/config.txt", Documentation: true, Ambiguous: true},
		})
		if got != "consult" {
			t.Fatalf("expected consult, got %s", got)
		}
	})
}

func TestAnalyzeReviewFiles(t *testing.T) {
	t.Run("empty diff", func(t *testing.T) {
		_, err := AnalyzeReviewFiles("")
		if !errors.Is(err, ErrEmptyDiff) {
			t.Fatalf("expected ErrEmptyDiff, got %v", err)
		}
	})

	t.Run("unrecognized diff format", func(t *testing.T) {
		_, err := AnalyzeReviewFiles("random plain text without diff header")
		if !errors.Is(err, ErrUnrecognizedDiff) {
			t.Fatalf("expected ErrUnrecognizedDiff, got %v", err)
		}
	})

	t.Run("pure docs diff", func(t *testing.T) {
		diff := `diff --git a/docs/guide.md b/docs/guide.md
index 1111111..2222222 100644
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1,3 +1,4 @@
 # User Guide
+This is an update to documentation.
`
		facts, err := AnalyzeReviewFiles(diff)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		f := facts[0]
		if f.Path != "docs/guide.md" || !f.Documentation || f.Sensitive || f.Ambiguous {
			t.Fatalf("unexpected fact: %+v", f)
		}
		if gate := LocalReviewGate(facts); gate != "skip" {
			t.Fatalf("expected skip gate for pure doc, got %s", gate)
		}
	})

	t.Run("sensitive CI workflow diff", func(t *testing.T) {
		diff := `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
index 1111111..2222222 100644
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,3 +1,4 @@
 name: CI
+run: echo hello
`
		facts, err := AnalyzeReviewFiles(diff)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		if !facts[0].Sensitive {
			t.Fatalf("expected workflow to be sensitive: %+v", facts[0])
		}
		if gate := LocalReviewGate(facts); gate != "deep" {
			t.Fatalf("expected deep gate, got %s", gate)
		}
	})

	t.Run("production code diff", func(t *testing.T) {
		diff := `diff --git a/pkg/service.go b/pkg/service.go
index 1111111..2222222 100644
--- a/pkg/service.go
+++ b/pkg/service.go
@@ -1,3 +1,4 @@
 package pkg
+func New() {}
`
		facts, err := AnalyzeReviewFiles(diff)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		if facts[0].Documentation {
			t.Fatalf("expected prod code to not be documentation: %+v", facts[0])
		}
		if gate := LocalReviewGate(facts); gate != "deep" {
			t.Fatalf("expected deep gate, got %s", gate)
		}
	})

	t.Run("ambiguous txt doc diff", func(t *testing.T) {
		diff := `diff --git a/notes.txt b/notes.txt
index 1111111..2222222 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1,3 +1,4 @@
 notes
+extra note
`
		facts, err := AnalyzeReviewFiles(diff)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(facts) != 1 {
			t.Fatalf("expected 1 fact, got %d", len(facts))
		}
		if !facts[0].Documentation || !facts[0].Ambiguous {
			t.Fatalf("expected documentation=true and ambiguous=true: %+v", facts[0])
		}
		if gate := LocalReviewGate(facts); gate != "consult" {
			t.Fatalf("expected consult gate, got %s", gate)
		}
	})
}
