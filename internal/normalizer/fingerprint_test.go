package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// oldFingerprint 旧版实现，用于对照测试
func oldFingerprint(source, repo, resourceKind, resourceID, action string, sourceUpdatedAt time.Time, stateHash string) string {
	raw := strings.Join([]string{
		source,
		repo,
		resourceKind,
		resourceID,
		action,
		sourceUpdatedAt.UTC().Format(time.RFC3339),
		stateHash,
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// oldStateHash 旧版实现，用于对照测试
func oldStateHash(parts ...string) string {
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

func TestFingerprintMatchesOldImplementation(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		source, repo, kind, id, action string
		updated                        time.Time
		stateHash                      string
	}{
		{"webhook", "Silentely/Repo-Sentinel", "issue", "issue:42", "opened", now, "abc123hash"},
		{"sync", "org/repo", "pull_request", "pull_request:100", "closed", now.Add(time.Hour), "state_hash_value"},
		{"", "", "", "", "", time.Time{}, ""},
	}

	for _, tc := range cases {
		got := Fingerprint(tc.source, tc.repo, tc.kind, tc.id, tc.action, tc.updated, tc.stateHash)
		expected := oldFingerprint(tc.source, tc.repo, tc.kind, tc.id, tc.action, tc.updated, tc.stateHash)
		if got != expected {
			t.Fatalf("Fingerprint 不一致: got %s, expected %s", got, expected)
		}
	}
}

func TestStateHashMatchesOldImplementation(t *testing.T) {
	cases := [][]string{
		{"issue", "open", "bug title", "alice", "false"},
		{"pull_request", "closed", "feature", "bob", "milestone 1", "true"},
		{"alert", "open", "high", "rule-1", ""},
		{},
		{""},
	}

	for _, parts := range cases {
		got := StateHash(parts...)
		expected := oldStateHash(parts...)
		if got != expected {
			t.Fatalf("StateHash 不一致: got %s, expected %s", got, expected)
		}
	}
}

func TestResourceIdentity(t *testing.T) {
	if got := ResourceIdentity("issue", 42, 0); got != "issue:42" {
		t.Fatalf("expected issue:42, got %s", got)
	}
	if got := ResourceIdentity("workflow_run", 0, 999); got != "run:999" {
		t.Fatalf("expected run:999, got %s", got)
	}
}

func BenchmarkFingerprint(b *testing.B) {
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Fingerprint("webhook", "Silentely/Repo-Sentinel", "issue", "issue:42", "opened", now, "abc123hash")
	}
}

func BenchmarkStateHash(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = StateHash("issue", "open", "bug title", "alice", "false")
	}
}
