package plugin

import (
	"context"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Capability 插件声明的能力。
type Capability string

const (
	CapDataSource   Capability = "datasource"
	CapNotification Capability = "notification"
	CapChatOps      Capability = "chatops"
)

// Manifest 插件元数据与能力清单。
type Manifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description"`
	Author       string       `json:"author"`
	Capabilities []Capability `json:"capabilities"`
}

// HealthStatus 插件健康状态。
type HealthStatus string

const (
	HealthOK       HealthStatus = "healthy"
	HealthDegraded HealthStatus = "degraded"
	HealthDown     HealthStatus = "down"
)

// PluginHealth 插件健康心跳。
type PluginHealth struct {
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message"`
	CheckedAt time.Time    `json:"checked_at"`
}

// Provider 插件提供者接口契约。
type Provider interface {
	Name() string
	Manifest() Manifest
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) (PluginHealth, error)
	DispatchEvent(ctx context.Context, ev store.Event) error
}

// PluginInfo 系统暴露的插件运行时信息。
type PluginInfo struct {
	Manifest Manifest     `json:"manifest"`
	Health   PluginHealth `json:"health"`
}
