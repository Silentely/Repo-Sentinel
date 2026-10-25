package notify

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// FailoverConfig 渠道故障转移策略配置。
type FailoverConfig struct {
	Enabled     bool              `json:"enabled"`
	MaxFailures int               `json:"max_failures"` // 触发故障转移的连续失败阈值，默认 3
	RouteMap    map[string]string `json:"route_map"`   // primaryChannelType -> secondaryChannelType (例如 "telegram" -> "feishu")
}

// Threshold 获取有效的失败阈值。
func (c FailoverConfig) Threshold() int {
	if c.MaxFailures <= 0 {
		return 3
	}
	return c.MaxFailures
}

// FailoverRouter 负责监测渠道健康并在主渠道熔断时调度至备用渠道。
type FailoverRouter struct {
	mu            sync.RWMutex
	cfg           FailoverConfig
	healthTracker *ChannelHealthTracker
	store         store.Store
	logger        *slog.Logger
}

// NewFailoverRouter 创建故障转移路由器。
func NewFailoverRouter(st store.Store, cfg FailoverConfig, tracker *ChannelHealthTracker, logger *slog.Logger) *FailoverRouter {
	if tracker == nil {
		tracker = DefaultHealthTracker
	}
	return &FailoverRouter{
		cfg:           cfg,
		healthTracker: tracker,
		store:         st,
		logger:        logger,
	}
}

// UpdateConfig 动态更新故障转移策略。
func (r *FailoverRouter) UpdateConfig(cfg FailoverConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
}

// ResolveChannel 评估主渠道健康度；若达到连续失败阈值且配置了备用渠道，则返回备用渠道与 engaged=true。
func (r *FailoverRouter) ResolveChannel(ctx context.Context, primary store.NotificationChannel) (store.NotificationChannel, bool, error) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	if !cfg.Enabled || r.store == nil {
		return primary, false, nil
	}

	failures, _, _ := r.healthTracker.GetHealth(primary.ID)
	if failures < cfg.Threshold() {
		return primary, false, nil
	}

	fallbackType, ok := cfg.RouteMap[primary.ChannelType]
	if !ok || fallbackType == "" || fallbackType == primary.ChannelType {
		return primary, false, nil
	}

	fallback, err := r.store.Channels().GetEnabledByType(ctx, fallbackType)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if r.logger != nil {
				r.logger.Warn("failover channel not found or disabled", "primary_type", primary.ChannelType, "fallback_type", fallbackType)
			}
			return primary, false, nil
		}
		return primary, false, err
	}

	if r.logger != nil {
		r.logger.Warn(
			"notification channel failover engaged",
			"primary_channel_id", primary.ID,
			"primary_type", primary.ChannelType,
			"fallback_channel_id", fallback.ID,
			"fallback_type", fallback.ChannelType,
			"failures", failures,
		)
	}

	return fallback, true, nil
}
