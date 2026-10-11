package notify

import (
	"testing"
)

func TestI18nCatalog(t *testing.T) {
	catalog := NewI18nCatalog()

	// 格式化英文 CI 失败
	enMsg := catalog.Format("en-US", "event.ci_failure", map[string]string{
		"repo":     "my-org/core",
		"workflow": "deploy-prod",
	})
	expectedEn := "🚨 Workflow deploy-prod failed on repository my-org/core!"
	if enMsg != expectedEn {
		t.Errorf("expected %q, got %q", expectedEn, enMsg)
	}

	// 格式化中文 PR
	zhMsg := catalog.Format("zh-CN", "event.pr_opened", map[string]string{
		"number": "42",
		"title":  "优化连接池",
		"author": "alice",
	})
	expectedZh := "📌 新合并请求 #42: 优化连接池 (由 alice 创建)"
	if zhMsg != expectedZh {
		t.Errorf("expected %q, got %q", expectedZh, zhMsg)
	}

	// 未知语言回退到 zh-CN
	fallbackMsg := catalog.Format("fr-FR", "event.release_ready", map[string]string{
		"tag": "v1.2.0",
	})
	expectedFallback := "🎉 新版本发布 v1.2.0 已就绪！"
	if fallbackMsg != expectedFallback {
		t.Errorf("expected %q, got %q", expectedFallback, fallbackMsg)
	}
}
