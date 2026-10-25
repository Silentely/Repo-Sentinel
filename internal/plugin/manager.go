package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Manager 管理插件的生命周期、健康探测与事件广播。
type Manager struct {
	mu        sync.RWMutex
	providers map[string]Provider
	logger    *slog.Logger
}

// NewManager 实例化插件管理器。
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		providers: make(map[string]Provider),
		logger:    logger,
	}
}

// Register 注册并启动一个 Provider 插件。
func (m *Manager) Register(ctx context.Context, p Provider) error {
	if p == nil {
		return fmt.Errorf("plugin: nil provider")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("plugin: empty provider name")
	}

	m.mu.Lock()
	if _, exists := m.providers[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("plugin: provider %q already registered", name)
	}
	m.mu.Unlock()

	// 锁外调用外部插件 Start，避免持锁阻塞网络或死锁
	if err := p.Start(ctx); err != nil {
		return fmt.Errorf("plugin %q start failed: %w", name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[name]; exists {
		_ = p.Stop(ctx)
		return fmt.Errorf("plugin: provider %q already registered", name)
	}

	m.providers[name] = p
	if m.logger != nil {
		m.logger.Info("plugin registered successfully", "plugin", name, "capabilities", p.Manifest().Capabilities)
	}
	return nil
}

// List 返回所有已注册插件的元数据与最新健康状态。
func (m *Manager) List(ctx context.Context) []PluginInfo {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	m.mu.RUnlock()

	list := make([]PluginInfo, 0, len(providers))
	for _, p := range providers {
		info := PluginInfo{
			Manifest: p.Manifest(),
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					info.Health = PluginHealth{
						Status:    HealthDown,
						Message:   fmt.Sprintf("plugin health check panicked: %v", r),
						CheckedAt: time.Now().UTC(),
					}
				}
			}()
			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			health, err := p.Health(healthCtx)
			if err != nil {
				health = PluginHealth{
					Status:    HealthDown,
					Message:   err.Error(),
					CheckedAt: time.Now().UTC(),
				}
			}
			info.Health = health
		}()
		list = append(list, info)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Manifest.Name < list[j].Manifest.Name
	})
	return list
}

// DispatchEvent 将规范化事件广播给所有具有 notification 能力的插件。
func (m *Manager) DispatchEvent(ctx context.Context, ev store.Event) error {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		for _, cap := range p.Manifest().Capabilities {
			if cap == CapNotification {
				providers = append(providers, p)
				break
			}
		}
	}
	m.mu.RUnlock()

	for _, p := range providers {
		func(prov Provider) {
			defer func() {
				if r := recover(); r != nil {
					if m.logger != nil {
						m.logger.Error("plugin panic recovered in DispatchEvent", "plugin", prov.Name(), "panic", r)
					}
				}
			}()
			if err := prov.DispatchEvent(ctx, ev); err != nil {
				if m.logger != nil {
					m.logger.Warn("plugin dispatch event failed", "plugin", prov.Name(), "error", err)
				}
			}
		}(p)
	}
	return nil
}

// Shutdown 优雅关闭所有插件。
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	m.providers = make(map[string]Provider)
	m.mu.Unlock()

	var firstErr error
	for _, p := range providers {
		func(prov Provider) {
			defer func() {
				if r := recover(); r != nil {
					if m.logger != nil {
						m.logger.Error("plugin panic during stop", "plugin", prov.Name(), "panic", r)
					}
				}
			}()
			stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := prov.Stop(stopCtx); err != nil {
				if m.logger != nil {
					m.logger.Error("plugin stop error", "plugin", prov.Name(), "error", err)
				}
				if firstErr == nil {
					firstErr = err
				}
			}
		}(p)
	}
	return firstErr
}
