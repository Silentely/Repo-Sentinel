package notify

import (
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestInterpretPlatformError(t *testing.T) {
	// 1. WeCom 93000
	wecomExp := InterpretPlatformError(store.ChannelWeCom, "wecom api error errcode: 93000")
	if !strings.Contains(wecomExp.Meaning, "移出群聊") {
		t.Fatalf("WeCom 93000 释义不符: %+v", wecomExp)
	}

	// 2. Feishu 19001
	feishuExp := InterpretPlatformError(store.ChannelFeishu, "code: 19001, sign match fail")
	if !strings.Contains(feishuExp.Meaning, "加签密钥验证失败") {
		t.Fatalf("Feishu 19001 释义不符: %+v", feishuExp)
	}
}
