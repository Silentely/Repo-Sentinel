package textutil

import (
	"strings"
	"testing"
)

func TestInterpolateSafeTemplate(t *testing.T) {
	tpl := "仓库: {{repo}}, 标题: {{title}}, 作者: {{author}}"
	vars := map[string]string{
		"repo":   "Silentely/Repo-Sentinel",
		"title":  "<Bug> 登录崩溃 & 闪退",
		"author": "alice",
	}

	// 开启 HTML 转义
	got := InterpolateSafeTemplate(tpl, vars, true)
	if !strings.Contains(got, "&lt;Bug&gt; 登录崩溃 &amp; 闪退") {
		t.Fatalf("变量内容应被 HTML 安全转义，实际: %s", got)
	}
	if !strings.Contains(got, "仓库: Silentely/Repo-Sentinel") {
		t.Fatalf("常规变量未正确插值，实际: %s", got)
	}

	// 未知占位符保留
	tplUnknown := "值: {{unknown}}"
	if gotUnk := InterpolateSafeTemplate(tplUnknown, vars, true); gotUnk != tplUnknown {
		t.Fatalf("未知占位符应保留原样，got: %s", gotUnk)
	}
}
