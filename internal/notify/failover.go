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
	RouteMap    map[string]string `json:"route_map"`    // primaryChannelType -> secondaryChannelType (例如 "telegram" -> "feishu")
}

// Threshold 获取有效的失败阈值。
func (c FailoverConfig) Threshold() int {
	if c.MaxFailures <= 0 {
		return 3
	}
	return c.MaxFailures
}

// FailoverRouter 负责监测渠道健康度并在主渠道熔断时调度至备用渠道。
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
// 支持带环检测的多级故障自动转移（最多 3 跳）。
func (r *FailoverRouter) ResolveChannel(ctx context.Context, primary store.NotificationChannel) (store.NotificationChannel, bool, error) {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	if !cfg.Enabled || r.store == nil {
		return primary, false, nil
	}

	curr := primary
	visited := map[string]bool{primary.ID: true}
	var lastEngaged *store.NotificationChannel

	for hop := 0; hop < 3; hop++ {
		failures, _, _ := r.healthTracker.GetHealth(curr.ID)
		if failures < cfg.Threshold() {
			if lastEngaged != nil {
				if r.logger != nil {
					r.logger.Warn(
						"notification channel failover engaged",
						"primary_channel_id", primary.ID,
						"primary_type", primary.ChannelType,
						"fallback_channel_id", curr.ID,
						"fallback_type", curr.ChannelType,
						"hops", hop,
					)
				}
				return curr, true, nil
			}
			return primary, false, nil
		}

		fallbackType, ok := cfg.RouteMap[curr.ChannelType]
		if !ok || fallbackType == "" || fallbackType == curr.ChannelType {
			break
		}

		fallback, err := r.store.Channels().GetEnabledByType(ctx, fallbackType)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				if r.logger != nil {
					r.logger.Warn("failover channel not found or disabled", "from_type", curr.ChannelType, "fallback_type", fallbackType)
				}
				break
			}
			return primary, false, err
		}

		if visited[fallback.ID] {
			break // 避免自环或循环路由
		}
		visited[fallback.ID] = true
		curr = fallback
		lastEngaged = &fallback
	}

	if lastEngaged != nil {
		if r.logger != nil {
			r.logger.Warn(
				"notification channel failover engaged",
				"primary_channel_id", primary.ID,
				"primary_type", primary.ChannelType,
				"fallback_channel_id", curr.ID,
				"fallback_type", curr.ChannelType,
			)
		}
		return curr, true, nil
	}

	return primary, false, nil
}
