package notify

import (
	"testing"
)

func TestFormatWeComColoredText(t *testing.T) {
	txt := FormatWeComColoredText("安全警告", WeComColorWarning)
	expected := `<font color="warning">安全警告</font>`
	if txt != expected {
		t.Errorf("expected %q, got %q", expected, txt)
	}
}

func TestFormatWeComStatusBadge(t *testing.T) {
	succ := FormatWeComStatusBadge("success")
	if succ != `<font color="info">● 成功</font>` {
		t.Errorf("expected info badge, got %s", succ)
	}

	fail := FormatWeComStatusBadge("critical")
	if fail != `<font color="warning">● 失败</font>` {
		t.Errorf("expected warning badge, got %s", fail)
	}

	unknown := FormatWeComStatusBadge("other")
	if unknown != `<font color="comment">● other</font>` {
		t.Errorf("expected comment badge, got %s", unknown)
	}
}
