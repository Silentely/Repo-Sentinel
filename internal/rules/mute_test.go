package rules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type dummySettingsStore struct {
	settings []store.SystemSetting
}

func (d *dummySettingsStore) Get(ctx context.Context, key string) (store.SystemSetting, error) {
	for _, s := range d.settings {
		if s.Key == key {
			return s, nil
		}
	}
	return store.SystemSetting{}, store.ErrNotFound
}

func (d *dummySettingsStore) GetMany(ctx context.Context, keys ...string) ([]store.SystemSetting, error) {
	var res []store.SystemSetting
	for _, k := range keys {
		for _, s := range d.settings {
			if s.Key == k {
				res = append(res, s)
			}
		}
	}
	return res, nil
}

func (d *dummySettingsStore) Upsert(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	return s, nil
}

func (d *dummySettingsStore) Create(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	return s, nil
}

func (d *dummySettingsStore) Delete(ctx context.Context, key string) error { return nil }

func (d *dummySettingsStore) UpdateAIBudgetUsageAtomic(ctx context.Context, todayKey string, tokens int, costCents int, budgetLimitCents int) (bool, error) {
	return false, nil
}

type dummyStore struct {
	store.Store
	settings dummySettingsStore
}

func (d *dummyStore) Settings() store.SettingsStore {
	return &d.settings
}

func TestCheckEmergencyMuteWithExpiry(t *testing.T) {
	now := time.Now().UTC()
	exp := now.Add(2 * time.Hour)
	payload, _ := json.Marshal(mutePayload{
		Muted:     true,
		Reason:    "排障期",
		ExpiresAt: exp,
	})

	mockStore := &dummyStore{
		settings: dummySettingsStore{
			settings: []store.SystemSetting{
				{Key: store.MuteAllSettingKey(), ValueJSON: payload},
			},
		},
	}

	muted, reason, gotExp := CheckEmergencyMuteWithExpiry(context.Background(), mockStore, "")
	if !muted {
		t.Fatal("expected muted to be true")
	}
	if !gotExp.Equal(exp) {
		t.Fatalf("expected expires_at %v, got %v", exp, gotExp)
	}
	if !strings.Contains(reason, "自动解冻") {
		t.Fatalf("reason 应包含自动解冻时间提示，实际: %s", reason)
	}
}
