package buildinfo

import (
	"testing"
)

func Test未注入构建信息不会伪装正式版本(t *testing.T) {
	info := Current()
	if info.BuildChannel == "release" {
		t.Fatal("本地默认值不得是 release")
	}
	if info.GitSHA == "" || info.BuildTime == "" {
		t.Fatal("开发回退字段必须明确")
	}
}
