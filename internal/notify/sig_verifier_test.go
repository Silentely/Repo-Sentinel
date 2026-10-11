package notify

import (
	"testing"
	"time"
)

func TestDingTalkSignature(t *testing.T) {
	ts := time.Now().UnixMilli()
	secret := "SEC12345678"
	sign := SignDingTalk(ts, secret)
	if sign == "" {
		t.Fatal("sign should not be empty")
	}

	if !VerifyDingTalk(ts, secret, sign) {
		t.Fatal("signature should verify successfully")
	}

	if VerifyDingTalk(ts+1, secret, sign) {
		t.Fatal("different timestamp should fail verification")
	}
}

func TestFeishuSignature(t *testing.T) {
	ts := time.Now().Unix()
	secret := "FEISHU_SEC_XYZ"
	sign := SignFeishu(ts, secret)
	if sign == "" {
		t.Fatal("feishu sign should not be empty")
	}

	if !VerifyFeishu(ts, secret, sign) {
		t.Fatal("feishu signature should verify successfully")
	}

	if VerifyFeishu(ts, "wrong_secret", sign) {
		t.Fatal("wrong secret should fail verification")
	}
}
