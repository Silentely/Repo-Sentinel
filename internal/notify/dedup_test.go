package notify

import (
	"testing"
	"time"
)

func TestContentDeduplicator(t *testing.T) {
	d := NewContentDeduplicator(10 * time.Second)
	now := time.Now()

	ch := "tg-1"
	title := "🚨 高危安全告警"
	body := "发现漏洞 CVE-2026-9999"

	// 1. 首次出现不应被抑制
	if d.ShouldSuppress(ch, title, body, now) {
		t.Fatal("首次消息不应被抑制")
	}

	// 2. 5 秒内再次出现相同内容应被抑制
	if !d.ShouldSuppress(ch, title, body, now.Add(5*time.Second)) {
		t.Fatal("短期内完全相同的内容应被抑制去重")
	}

	// 3. 相同时间不同渠道不应被抑制
	if d.ShouldSuppress("tg-2", title, body, now.Add(5*time.Second)) {
		t.Fatal("不同渠道不应互相抑制")
	}

	// 4. 相同渠道不同内容不应被抑制
	if d.ShouldSuppress(ch, "不同标题", body, now.Add(5*time.Second)) {
		t.Fatal("不同标题不应被抑制")
	}

	// 5. 超过 TTL（15 秒后）不应再被抑制
	if d.ShouldSuppress(ch, title, body, now.Add(15*time.Second)) {
		t.Fatal("超过 TTL 后应该允许重新发送")
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	fp1 := Fingerprint("ch1", "t1", "b1")
	fp2 := Fingerprint("ch1", "t1", "b1")
	if fp1 != fp2 {
		t.Errorf("相同内容指纹应相同: %s != %s", fp1, fp2)
	}
	if len(fp1) != 16 {
		t.Errorf("指纹长度应为 16, got %d", len(fp1))
	}
}
