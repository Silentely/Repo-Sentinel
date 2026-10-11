package notify

import (
	"testing"
)

func TestBuildFeishuMentionAndLinks(t *testing.T) {
	atUser := BuildFeishuAtTag("ou_123456", "张三")
	if atUser["tag"] != "at" || atUser["user_id"] != "ou_123456" || atUser["user_name"] != "张三" {
		t.Errorf("unexpected atUser: %+v", atUser)
	}

	atAll := BuildFeishuAtAllTag()
	if atAll["tag"] != "at" || atAll["user_id"] != "all" {
		t.Errorf("unexpected atAll: %+v", atAll)
	}

	link := BuildFeishuLinkTag("查看报告", "https://example.com/report")
	if link["tag"] != "a" || link["text"] != "查看报告" || link["href"] != "https://example.com/report" {
		t.Errorf("unexpected link: %+v", link)
	}

	row := BuildFeishuParagraph(
		BuildFeishuTextTag("请 "),
		atUser,
		BuildFeishuTextTag(" 尽快核对 "),
		link,
	)

	if len(row) != 4 {
		t.Fatalf("expected 4 elements in paragraph, got %d", len(row))
	}
}
