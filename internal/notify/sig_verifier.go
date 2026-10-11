package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
)

// SignDingTalk 计算钉钉机器人加签签名。
func SignDingTalk(timestampMs int64, secret string) string {
	nowMs := strconv.FormatInt(timestampMs, 10)
	stringToSign := nowMs + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// VerifyDingTalk 校验钉钉签名是否一致。
func VerifyDingTalk(timestampMs int64, secret, sign string) bool {
	expected := SignDingTalk(timestampMs, secret)
	return expected == sign
}

// SignFeishu 计算飞书机器人加签签名。
func SignFeishu(timestampSec int64, secret string) string {
	ts := strconv.FormatInt(timestampSec, 10)
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyFeishu 校验飞书签名是否一致。
func VerifyFeishu(timestampSec int64, secret, sign string) bool {
	expected := SignFeishu(timestampSec, secret)
	return expected == sign
}
