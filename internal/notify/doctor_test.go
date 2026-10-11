package notify

import (
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestDiagnoseChannel(t *testing.T) {
	// 1. 禁用渠道
	dDisabled := DiagnoseChannel(store.NotificationChannel{Enabled: false})
	if dDisabled.Status != ChannelHealthWarning {
		t.Errorf("禁用渠道状态应为 warning, got %v", dDisabled.Status)
	}

	// 2. 空目标
	dEmpty := DiagnoseChannel(store.NotificationChannel{Enabled: true, Target: ""})
	if dEmpty.Status != ChannelHealthError {
		t.Errorf("空目标状态应为 misconfigured, got %v", dEmpty.Status)
	}

	// 3. Telegram 错误的目标 URL
	dTgErr := DiagnoseChannel(store.NotificationChannel{
		Enabled:     true,
		ChannelType: store.ChannelTelegram,
		Target:      "https://t.me/mybot",
	})
	if dTgErr.Status != ChannelHealthError {
		t.Errorf("Telegram 目标填 URL 应报错, got %v", dTgErr.Status)
	}

	// 4. Webhook 局域网且未开 allow_private
	dLocal := DiagnoseChannel(store.NotificationChannel{
		Enabled:      true,
		ChannelType:  store.ChannelHTTPWebhook,
		Target:       "http://127.0.0.1:8080/hook",
		AllowPrivate: false,
	})
	if dLocal.Status != ChannelHealthWarning {
		t.Errorf("内网 Webhook 未开 allow_private 应提示 warning, got %v", dLocal.Status)
	}

	// 5. 正常 Webhook
	dOk := DiagnoseChannel(store.NotificationChannel{
		Enabled:      true,
		ChannelType:  store.ChannelHTTPWebhook,
		Target:       "https://webhook.site/abc",
		AllowPrivate: false,
	})
	if dOk.Status != ChannelHealthOK {
		t.Errorf("正常 Webhook 状态应为 healthy, got %v", dOk.Status)
	}
}
