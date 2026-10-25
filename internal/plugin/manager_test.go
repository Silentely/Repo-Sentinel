package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

type mockProvider struct {
	name         string
	capabilities []Capability
	started      bool
	stopped      bool
	healthErr    error
	receivedEvs  []store.Event
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Manifest() Manifest {
	return Manifest{
		Name:         m.name,
		Version:      "1.0.0",
		Description:  "Mock Plugin for testing",
		Author:       "Sentinel Team",
		Capabilities: m.capabilities,
	}
}
func (m *mockProvider) Start(ctx context.Context) error {
	m.started = true
	return nil
}
func (m *mockProvider) Stop(ctx context.Context) error {
	m.stopped = true
	return nil
}
func (m *mockProvider) Health(ctx context.Context) (PluginHealth, error) {
	if m.healthErr != nil {
		return PluginHealth{}, m.healthErr
	}
	return PluginHealth{
		Status:    HealthOK,
		Message:   "ok",
		CheckedAt: time.Now().UTC(),
	}, nil
}
func (m *mockProvider) DispatchEvent(ctx context.Context, ev store.Event) error {
	m.receivedEvs = append(m.receivedEvs, ev)
	return nil
}

func TestPluginManager_LifecycleAndDispatch(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(nil)

	p1 := &mockProvider{
		name:         "notification-plugin",
		capabilities: []Capability{CapNotification},
	}
	p2 := &mockProvider{
		name:         "datasource-plugin",
		capabilities: []Capability{CapDataSource},
	}

	// 1. 注册插件
	if err := mgr.Register(ctx, p1); err != nil {
		t.Fatalf("register p1: %v", err)
	}
	if err := mgr.Register(ctx, p2); err != nil {
		t.Fatalf("register p2: %v", err)
	}
	if !p1.started || !p2.started {
		t.Errorf("expected providers to be started")
	}

	// 重复注册应报错
	if err := mgr.Register(ctx, p1); err == nil {
		t.Errorf("expected error on duplicate register")
	}

	// 2. 健康与元数据列表
	list := mgr.List(ctx)
	if len(list) != 2 {
		t.Fatalf("expected 2 plugins in list, got %d", len(list))
	}
	for _, info := range list {
		if info.Health.Status != HealthOK {
			t.Errorf("expected healthy status, got %s", info.Health.Status)
		}
	}

	// 3. 事件广播：仅具有 CapNotification 的 p1 应该收到事件
	ev := store.Event{ID: "test-ev-1", Kind: "issue", Title: "test"}
	if err := mgr.DispatchEvent(ctx, ev); err != nil {
		t.Fatalf("dispatch error: %v", err)
	}

	if len(p1.receivedEvs) != 1 || p1.receivedEvs[0].ID != "test-ev-1" {
		t.Errorf("p1 should have received event, got %+v", p1.receivedEvs)
	}
	if len(p2.receivedEvs) != 0 {
		t.Errorf("p2 should not have received event because it lacks CapNotification")
	}

	// 4. 关闭管理器
	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	if !p1.stopped || !p2.stopped {
		t.Errorf("expected providers to be stopped")
	}
}
