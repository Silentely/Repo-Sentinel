package rules_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestEvaluateQuietHours_CrossMidnightAndSameDay(t *testing.T) {
	ch := store.NotificationChannel{
		QuietHoursEnabled: true,
		QuietHoursStart:   "22:00",
		QuietHoursEnd:     "08:00",
		QuietHoursTZ:      "Asia/Shanghai", // UTC+8
	}

	// 23:30 in Shanghai = 15:30 UTC
	t2330 := time.Date(2026, 10, 2, 15, 30, 0, 0, time.UTC)
	dec := rules.EvaluateQuietHours(ch, &store.Event{Kind: store.WorkItemKindIssue}, t2330)
	if dec.Action != rules.ActionDeferQuietHours {
		t.Fatalf("expected ActionDeferQuietHours at 23:30, got %v", dec.Action)
	}
	// Expected resume at 08:00 next day in Shanghai = 00:00 UTC on 2026-10-03
	expectedResume := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	if !dec.ResumeAt.Equal(expectedResume) {
		t.Fatalf("expected resumeAt %v, got %v", expectedResume, dec.ResumeAt)
	}

	// 03:00 in Shanghai = 19:00 UTC previous day (2026-10-02 19:00 UTC)
	t0300 := time.Date(2026, 10, 2, 19, 0, 0, 0, time.UTC)
	dec0300 := rules.EvaluateQuietHours(ch, &store.Event{Kind: store.WorkItemKindIssue}, t0300)
	if dec0300.Action != rules.ActionDeferQuietHours {
		t.Fatalf("expected ActionDeferQuietHours at 03:00, got %v", dec0300.Action)
	}
	// Resume at 08:00 Shanghai on 2026-10-03 = 00:00 UTC on 2026-10-03
	if !dec0300.ResumeAt.Equal(expectedResume) {
		t.Fatalf("expected resumeAt %v, got %v", expectedResume, dec0300.ResumeAt)
	}

	// 10:00 in Shanghai = 02:00 UTC
	t1000 := time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)
	dec1000 := rules.EvaluateQuietHours(ch, &store.Event{Kind: store.WorkItemKindIssue}, t1000)
	if dec1000.Action != rules.ActionDeliverNow {
		t.Fatalf("expected ActionDeliverNow at 10:00, got %v", dec1000.Action)
	}

	// Same-day window: 13:00 - 15:00 UTC
	chSameDay := store.NotificationChannel{
		QuietHoursEnabled: true,
		QuietHoursStart:   "13:00",
		QuietHoursEnd:     "15:00",
		QuietHoursTZ:      "UTC",
	}
	t1400 := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	decSameDay := rules.EvaluateQuietHours(chSameDay, &store.Event{Kind: store.WorkItemKindIssue}, t1400)
	if decSameDay.Action != rules.ActionDeferQuietHours {
		t.Fatalf("expected ActionDeferQuietHours at 14:00, got %v", decSameDay.Action)
	}
	if !decSameDay.ResumeAt.Equal(time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected resumeAt: %v", decSameDay.ResumeAt)
	}
}

func TestEvaluateQuietHours_SecurityBypass(t *testing.T) {
	ch := store.NotificationChannel{
		QuietHoursEnabled: true,
		QuietHoursStart:   "22:00",
		QuietHoursEnd:     "08:00",
		QuietHoursTZ:      "UTC",
	}
	midnight := time.Date(2026, 10, 2, 23, 0, 0, 0, time.UTC)

	// Secret scanning: always bypasses
	secretEv := &store.Event{Kind: store.AlertKindSecretScanning, Severity: ""}
	if dec := rules.EvaluateQuietHours(ch, secretEv, midnight); dec.Action != rules.ActionDeliverNow || dec.Reason != "security_alert_bypass" {
		t.Fatalf("secret scanning must bypass quiet hours, got %+v", dec)
	}

	// Code scanning critical/high: bypasses
	for _, sev := range []string{"critical", "Critical", "high", "HIGH"} {
		ev := &store.Event{Kind: store.AlertKindCodeScanning, Severity: sev}
		if dec := rules.EvaluateQuietHours(ch, ev, midnight); dec.Action != rules.ActionDeliverNow || dec.Reason != "security_alert_bypass" {
			t.Fatalf("code scanning %s must bypass quiet hours, got %+v", sev, dec)
		}
	}

	// Dependabot critical/high: bypasses
	for _, sev := range []string{"critical", "high"} {
		ev := &store.Event{Kind: store.AlertKindDependabot, Severity: sev}
		if dec := rules.EvaluateQuietHours(ch, ev, midnight); dec.Action != rules.ActionDeliverNow || dec.Reason != "security_alert_bypass" {
			t.Fatalf("dependabot %s must bypass quiet hours, got %+v", sev, dec)
		}
	}

	// Medium/Low/Info: deferred
	for _, sev := range []string{"medium", "low", "info", ""} {
		ev := &store.Event{Kind: store.AlertKindCodeScanning, Severity: sev}
		if dec := rules.EvaluateQuietHours(ch, ev, midnight); dec.Action != rules.ActionDeferQuietHours {
			t.Fatalf("code scanning %s must be deferred during quiet hours, got %+v", sev, dec)
		}
	}
}

func TestValidateQuietHours(t *testing.T) {
	valid := []struct {
		name       string
		start, end string
		tz         string
	}{
		{"同日区间", "13:00", "15:00", "UTC"},
		{"跨午夜区间", "22:00", "08:00", "Asia/Shanghai"},
		{"单数字小时可接受", "8:00", "09:00", "UTC"},
	}
	for _, tc := range valid {
		if err := rules.ValidateQuietHours(tc.start, tc.end, tc.tz); err != nil {
			t.Fatalf("%s 应通过校验，got %v", tc.name, err)
		}
	}

	invalid := []struct {
		name       string
		start, end string
		tz         string
	}{
		{"起始小时越界", "24:00", "08:00", "UTC"},
		{"起始为空", "", "08:00", "UTC"},
		{"结束非数字", "22:00", "ab:00", "UTC"},
		{"结束分钟越界", "22:00", "08:75", "UTC"},
		{"起止相同", "08:00", "08:00", "UTC"},
		{"时区为空", "22:00", "08:00", ""},
		{"时区不存在", "22:00", "08:00", "Not/AZone"},
	}
	for _, tc := range invalid {
		if err := rules.ValidateQuietHours(tc.start, tc.end, tc.tz); err == nil {
			t.Fatalf("%s 应被拒绝（start=%q end=%q tz=%q）", tc.name, tc.start, tc.end, tc.tz)
		}
	}
}

func TestEngineEvaluate_QuietHoursOutboxScheduling(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(ctx, config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "rules_qh.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	// Channel with quiet hours 22:00 - 08:00 UTC
	ch, err := st.Channels().Upsert(ctx, store.NotificationChannel{
		ID:                ulid.Make().String(),
		ChannelType:       store.ChannelTelegram,
		Name:              "ops-qh",
		Enabled:           true,
		Target:            "12345",
		QuietHoursEnabled: true,
		QuietHoursStart:   "22:00",
		QuietHoursEnd:     "08:00",
		QuietHoursTZ:      "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	eng := &rules.Engine{Store: st}
	now := time.Now().UTC()

	// 1. Regular issue event: should be deferred if created during quiet hours
	// Note: in Engine.Evaluate, time.Now().UTC() is used.
	// We test that Evaluate succeeds and Outbox item is created.
	evID := ulid.Make().String()
	num := int64(10)
	res := normalizer.Result{
		Event: &store.Event{
			ID:            evID,
			Source:        "github",
			Kind:          store.WorkItemKindIssue,
			Action:        "opened",
			Title:         "Non-urgent bug",
			SubjectNumber: &num,
			OccurredAt:    now,
			CreatedAt:     now,
		},
	}
	if err := eng.Evaluate(ctx, res, "acme/repo"); err != nil {
		t.Fatalf("evaluate error: %v", err)
	}

	items, _, err := st.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ChannelID != ch.ID {
		t.Fatalf("expected 1 outbox item for ch, got %d", len(items))
	}
}
