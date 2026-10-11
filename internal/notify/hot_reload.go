package notify

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// ChannelConfig 单个渠道的配置描述
type ChannelConfig struct {
	Channel  string `json:"channel"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Enabled  bool   `json:"enabled"`
}

// HotReloadEngine 全渠道通知配置并发安全热重载引擎
type HotReloadEngine struct {
	mu          sync.RWMutex
	configs     map[string]ChannelConfig
	revision    int64
	lastUpdated time.Time
}

// NewHotReloadEngine 创建配置热重载引擎
func NewHotReloadEngine() *HotReloadEngine {
	return &HotReloadEngine{
		configs:     make(map[string]ChannelConfig),
		lastUpdated: time.Now(),
	}
}

// ValidateConfig 校验新配置的基本合法性
func ValidateConfig(cfg ChannelConfig) error {
	ch := strings.TrimSpace(cfg.Channel)
	if ch == "" {
		return errors.New("channel name cannot be empty")
	}
	if cfg.Enabled {
		if strings.TrimSpace(cfg.Endpoint) == "" && strings.TrimSpace(cfg.Token) == "" {
			return errors.New("enabled channel must have either endpoint or token configured")
		}
	}
	return nil
}

// ApplyUpdate 动态热更新指定渠道配置
func (e *HotReloadEngine) ApplyUpdate(cfg ChannelConfig) (bool, error) {
	if err := ValidateConfig(cfg); err != nil {
		return false, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	ch := strings.ToLower(cfg.Channel)
	existing, ok := e.configs[ch]
	if ok && existing == cfg {
		return false, nil // 无变更
	}

	e.configs[ch] = cfg
	e.revision++
	e.lastUpdated = time.Now()
	return true, nil
}

// GetConfig 获取当前渠道配置副本
func (e *HotReloadEngine) GetConfig(channel string) (ChannelConfig, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	cfg, ok := e.configs[strings.ToLower(channel)]
	return cfg, ok
}

// Revision 获取当前全局配置修订版本号
func (e *HotReloadEngine) Revision() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.revision
}
