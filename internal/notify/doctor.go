package notify

import (
	"net/url"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// ChannelHealthStatus 渠道健康诊断状态。
type ChannelHealthStatus string

const (
	ChannelHealthOK      ChannelHealthStatus = "healthy"
	ChannelHealthWarning ChannelHealthStatus = "warning"
	ChannelHealthError   ChannelHealthStatus = "misconfigured"
)

// ChannelDiagnostic 渠道诊断结论。
// ChannelHealthTracker 内存中的运行时渠道健康追踪器（并发安全）。
type ChannelHealthTracker struct {
	mu       sync.RWMutex
	failures map[string]int
	lastErr  map[string]string
	lastTime map[string]time.Time
}

// NewChannelHealthTracker 创建健康追踪器实例。
func NewChannelHealthTracker() *ChannelHealthTracker {
	return &ChannelHealthTracker{
		failures: make(map[string]int),
		lastErr:  make(map[string]string),
		lastTime: make(map[string]time.Time),
	}
}

// DefaultHealthTracker 全局默认渠道运行时健康追踪器。
var DefaultHealthTracker = NewChannelHealthTracker()

func (t *ChannelHealthTracker) RecordSuccess(channelID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, channelID)
	delete(t.lastErr, channelID)
}

func (t *ChannelHealthTracker) RecordFailure(channelID, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures[channelID]++
	t.lastErr[channelID] = reason
	t.lastTime[channelID] = time.Now().UTC()
}

func (t *ChannelHealthTracker) GetHealth(channelID string) (int, string, time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.failures[channelID], t.lastErr[channelID], t.lastTime[channelID]
}

type ChannelDiagnostic struct {
	Status  ChannelHealthStatus `json:"status"`
	Message string              `json:"message"`
}

// DiagnoseChannel 对通知渠道的配置与目标进行静态健康诊断，提前指出潜在配置错误。
// DiagnoseChannel 对通知渠道进行配置与运行时综合健康诊断。
func DiagnoseChannel(ch store.NotificationChannel) ChannelDiagnostic {
	return DiagnoseChannelWithRuntime(ch, DefaultHealthTracker)
}

// DiagnoseChannelWithRuntime 支持传入自定义运行时健康追踪器。
func DiagnoseChannelWithRuntime(ch store.NotificationChannel, tracker *ChannelHealthTracker) ChannelDiagnostic {
	if tracker != nil && ch.ID != "" {
		fails, lastErr, _ := tracker.GetHealth(ch.ID)
		if fails >= 3 {
			return ChannelDiagnostic{
				Status:  ChannelHealthError,
				Message: fmt.Sprintf("运行时连续投递失败 %d 次 (最近错误: %s)", fails, lastErr),
			}
		}
	}

	if !ch.Enabled {
		return ChannelDiagnostic{
			Status:  ChannelHealthWarning,
			Message: "渠道已禁用，不会投递任何通知",
		}
	}

	target := strings.TrimSpace(ch.Target)
	if target == "" {
		return ChannelDiagnostic{
			Status:  ChannelHealthError,
			Message: "目标地址或 Chat ID 为空",
		}
	}

	switch ch.ChannelType {
	case store.ChannelTelegram:
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			return ChannelDiagnostic{
				Status:  ChannelHealthError,
				Message: "Telegram 目标应为数字 Chat ID（如 123456789），而非 URL",
			}
		}
		if ch.SecretEnvelope == "" {
			return ChannelDiagnostic{
				Status:  ChannelHealthWarning,
				Message: "未配置专属 Bot Token，将尝试回退使用系统全局默认 Token",
			}
		}
	case store.ChannelHTTPWebhook, store.ChannelWeCom, store.ChannelDingTalk, store.ChannelFeishu, store.ChannelDiscord, store.ChannelSlack:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ChannelDiagnostic{
				Status:  ChannelHealthError,
				Message: "Webhook 目标必须为以 http:// 或 https:// 开头的有效完整 URL",
			}
		}
		// 私网地址保护检查
		if (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || strings.HasPrefix(u.Hostname(), "192.168.") || strings.HasPrefix(u.Hostname(), "10.")) && !ch.AllowPrivate {
			return ChannelDiagnostic{
				Status:  ChannelHealthWarning,
				Message: "目标为本地或局域网私有地址，需勾选「允许私网地址」方可投递",
			}
		}
	case store.ChannelBark:
		if !strings.HasPrefix(target, "http") && len(target) < 10 {
			return ChannelDiagnostic{
				Status:  ChannelHealthWarning,
				Message: "Bark Device Key 长度偏短，请核对是否为有效的 Bark 推送密钥",
			}
		}
	}

	return ChannelDiagnostic{
		Status:  ChannelHealthOK,
		Message: "配置完整有效",
	}
}
