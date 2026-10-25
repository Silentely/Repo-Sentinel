package notify

import (
	"context"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type mockChannelStore struct {
	store.ChannelStore
	channels map[string]store.NotificationChannel
}

func (m *mockChannelStore) Get(ctx context.Context, id string) (store.NotificationChannel, error) {
	ch, ok := m.channels[id]
	if !ok {
		return store.NotificationChannel{}, store.ErrNotFound
	}
	return ch, nil
}

func (m *mockChannelStore) GetEnabledByType(ctx context.Context, channelType string) (store.NotificationChannel, error) {
	for _, ch := range m.channels {
		if ch.ChannelType == channelType && ch.Enabled {
			return ch, nil
		}
	}
	return store.NotificationChannel{}, store.ErrNotFound
}

type mockStoreForFailover struct {
	store.Store
	chStore *mockChannelStore
}

func (m *mockStoreForFailover) Channels() store.ChannelStore {
	return m.chStore
}

func TestFailoverRouter_Lifecycle(t *testing.T) {
	ctx := context.Background()
	mockChs := &mockChannelStore{
		channels: map[string]store.NotificationChannel{
			"primary-tg": {
				ID:          "primary-tg",
				ChannelType: store.ChannelTelegram,
				Name:        "Main Telegram",
				Enabled:     true,
			},
			"backup-feishu": {
				ID:          "backup-feishu",
				ChannelType: store.ChannelFeishu,
				Name:        "Backup Feishu",
				Enabled:     true,
			},
		},
	}
	mockSt := &mockStoreForFailover{chStore: mockChs}
	tracker := NewChannelHealthTracker()

	cfg := FailoverConfig{
		Enabled:     true,
		MaxFailures: 3,
		RouteMap: map[string]string{
			store.ChannelTelegram: store.ChannelFeishu,
		},
	}
	router := NewFailoverRouter(mockSt, cfg, tracker, nil)

	primary := mockChs.channels["primary-tg"]

	// 1. Initial healthy state: should not engage failover
	res, engaged, err := router.ResolveChannel(ctx, primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engaged || res.ID != "primary-tg" {
		t.Errorf("expected primary-tg, got engaged=%v, id=%s", engaged, res.ID)
	}

	// 2. 2 failures (under threshold of 3): still no failover
	tracker.RecordFailure("primary-tg", "network_timeout")
	tracker.RecordFailure("primary-tg", "network_timeout")
	res, engaged, err = router.ResolveChannel(ctx, primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engaged || res.ID != "primary-tg" {
		t.Errorf("expected primary-tg, got engaged=%v, id=%s", engaged, res.ID)
	}

	// 3. 3rd failure (hits threshold): failover engaged, routes to backup-feishu
	tracker.RecordFailure("primary-tg", "rate_limit_429")
	res, engaged, err = router.ResolveChannel(ctx, primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !engaged || res.ID != "backup-feishu" {
		t.Errorf("expected failover to backup-feishu, got engaged=%v, id=%s", engaged, res.ID)
	}

	// 4. Primary recovers (RecordSuccess): failover disengages
	tracker.RecordSuccess("primary-tg")
	res, engaged, err = router.ResolveChannel(ctx, primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engaged || res.ID != "primary-tg" {
		t.Errorf("expected failover to disengage, got engaged=%v, id=%s", engaged, res.ID)
	}

	// 5. Disabled config: never engages
	tracker.RecordFailure("primary-tg", "err")
	tracker.RecordFailure("primary-tg", "err")
	tracker.RecordFailure("primary-tg", "err")
	router.UpdateConfig(FailoverConfig{Enabled: false})
	res, engaged, err = router.ResolveChannel(ctx, primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engaged || res.ID != "primary-tg" {
		t.Errorf("expected disabled config to never engage, got engaged=%v, id=%s", engaged, res.ID)
	}
}

func TestFailoverRouter_MultiHop(t *testing.T) {
	ctx := context.Background()
	mockChs := &mockChannelStore{
		channels: map[string]store.NotificationChannel{
			"tg-1": {
				ID:          "tg-1",
				ChannelType: store.ChannelTelegram,
				Enabled:     true,
			},
			"feishu-1": {
				ID:          "feishu-1",
				ChannelType: store.ChannelFeishu,
				Enabled:     true,
			},
			"wecom-1": {
				ID:          "wecom-1",
				ChannelType: store.ChannelWeCom,
				Enabled:     true,
			},
		},
	}
	mockSt := &mockStoreForFailover{chStore: mockChs}
	tracker := NewChannelHealthTracker()
	router := NewFailoverRouter(mockSt, FailoverConfig{
		Enabled:     true,
		MaxFailures: 3,
		RouteMap: map[string]string{
			store.ChannelTelegram: store.ChannelFeishu,
			store.ChannelFeishu:   store.ChannelWeCom,
		},
	}, tracker, nil)

	primary := mockChs.channels["tg-1"]
	// 1. tg fails 3 times -> routes to feishu
	tracker.RecordFailure("tg-1", "err")
	tracker.RecordFailure("tg-1", "err")
	tracker.RecordFailure("tg-1", "err")
	res, engaged, err := router.ResolveChannel(ctx, primary)
	if err != nil || !engaged || res.ID != "feishu-1" {
		t.Fatalf("expected failover to feishu-1, got engaged=%v, id=%s, err=%v", engaged, res.ID, err)
	}

	// 2. feishu also fails 3 times -> multi-hop routes to wecom
	tracker.RecordFailure("feishu-1", "err")
	tracker.RecordFailure("feishu-1", "err")
	tracker.RecordFailure("feishu-1", "err")
	res, engaged, err = router.ResolveChannel(ctx, primary)
	if err != nil || !engaged || res.ID != "wecom-1" {
		t.Fatalf("expected multi-hop failover to wecom-1, got engaged=%v, id=%s, err=%v", engaged, res.ID, err)
	}
}
