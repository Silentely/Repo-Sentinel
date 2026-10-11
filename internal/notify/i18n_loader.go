package notify

import (
	"strings"
	"sync"
)

// I18nCatalog 多语言字典存储引擎
type I18nCatalog struct {
	mu           sync.RWMutex
	defaultLang  string
	translations map[string]map[string]string // lang -> key -> template
}

// NewI18nCatalog 创建默认字典引擎并预置基础词条
func NewI18nCatalog() *I18nCatalog {
	c := &I18nCatalog{
		defaultLang:  "zh-CN",
		translations: make(map[string]map[string]string),
	}

	// 注册默认内置词条
	c.Register("zh-CN", map[string]string{
		"event.ci_failure":    "🚨 仓库 {repo} 的工作流 {workflow} 执行失败！",
		"event.pr_opened":     "📌 新合并请求 #{number}: {title} (由 {author} 创建)",
		"event.release_ready": "🎉 新版本发布 {tag} 已就绪！",
	})

	c.Register("en-US", map[string]string{
		"event.ci_failure":    "🚨 Workflow {workflow} failed on repository {repo}!",
		"event.pr_opened":     "📌 New Pull Request #{number}: {title} (by {author})",
		"event.release_ready": "🎉 New Release {tag} is published!",
	})

	return c
}

// Register 批量注册指定语言的模板词条
func (c *I18nCatalog) Register(lang string, dict map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.translations[lang]; !ok {
		c.translations[lang] = make(map[string]string)
	}
	for k, v := range dict {
		c.translations[lang][k] = v
	}
}

// Format 按照指定语言渲染模板，若不存在则回退至默认语言
func (c *I18nCatalog) Format(lang, key string, vars map[string]string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var tmpl string
	if dict, ok := c.translations[lang]; ok {
		tmpl = dict[key]
	}
	if tmpl == "" {
		if dict, ok := c.translations[c.defaultLang]; ok {
			tmpl = dict[key]
		}
	}
	if tmpl == "" {
		return key
	}

	result := tmpl
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}
