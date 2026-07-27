package buildinfo

import (
	"strings"
	"testing"
)

func Test读取版本文件会去除空白并校验SemVer(t *testing.T) {
	got, err := ReadVersionFile(strings.NewReader("0.1.0\n"))
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if got != "0.1.0" {
		t.Fatalf("版本=%q，期望 0.1.0", got)
	}
}

func Test读取非法版本会失败(t *testing.T) {
	if _, err := ReadVersionFile(strings.NewReader("v0.1\n")); err == nil {
		t.Fatal("非法 SemVer 未返回错误")
	}
}

func Test未注入构建信息不会伪装正式版本(t *testing.T) {
	info := Current()
	if info.BuildChannel == "release" {
		t.Fatal("本地默认值不得是 release")
	}
	if info.GitSHA == "" || info.BuildTime == "" {
		t.Fatal("开发回退字段必须明确")
	}
}
