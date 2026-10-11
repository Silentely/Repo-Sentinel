package notify

import (
	"regexp"
)

var (
	reGitHubPAT         = regexp.MustCompile(`\bghp_[A-Za-z0-9_]{20,}\b`)
	reGitHubFineGrained = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{30,}\b`)
	reGitHubOAuth       = regexp.MustCompile(`\bgho_[A-Za-z0-9_]{20,}\b`)
	reOpenAIKey         = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)
	reAnthropicKey      = regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)
	reAWSAccessKey      = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	reSlackBotToken     = regexp.MustCompile(`\bxoxb-[0-9]{8,}-[0-9]{8,}-[A-Za-z0-9]{20,}\b`)
	// 私钥必须整块遮盖：只替换 BEGIN 行会让 base64 密钥正文继续外泄。
	// 完整块（有 END 标记）整段替换；缺 END 时至少吃掉紧随其后的 base64 正文，避免吞掉后续正文。
	rePrivateKeyBlock  = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	rePrivateKeyOrphan = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----[A-Za-z0-9+/=\s]{8,}`)
	// 兜底：残缺到连 base64 正文都不完整的 BEGIN 标记本身也不应外泄。
	rePrivateKeyMarker = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	// 传入 Webhook 的凭据内嵌在 URL 路径里（Slack services/T/B/X、Discord webhooks/id/token）。
	reSlackWebhookURL   = regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/_-]+`)
	reDiscordWebhookURL = regexp.MustCompile(`https://(?:ptb\.|canary\.)?discord(?:app)?\.com/api/webhooks/[0-9]+/[A-Za-z0-9_-]+`)
	// URL query 与请求头里的凭据值（只匹配明确表示凭据的参数名，避免误伤普通查询参数）。
	reURLCredential = regexp.MustCompile(`(?i)([?&](?:access_token|token|secret|signature|sign|auth|apikey|api_key|api_token|bearer)=)[^&\s#]+`)
	reAuthHeader    = regexp.MustCompile(`(?i)((?:proxy-)?authorization\s*:\s*(?:bearer|token|basic)?\s*)[^\r\n]+`)
	reCookieHeader  = regexp.MustCompile(`(?i)((?:set-)?cookie\s*:\s*)[^\r\n]+`)
)

// RedactSensitiveSecrets 对通知标题与正文进行敏感凭据防御掩码脱敏，防止凭证在公共群聊中泄露。
func RedactSensitiveSecrets(text string) string {
	if text == "" {
		return ""
	}
	text = reGitHubPAT.ReplaceAllString(text, "ghp_***")
	text = reGitHubFineGrained.ReplaceAllString(text, "github_pat_***")
	text = reGitHubOAuth.ReplaceAllString(text, "gho_***")
	text = reOpenAIKey.ReplaceAllString(text, "sk-***")
	text = reAnthropicKey.ReplaceAllString(text, "sk-ant-***")
	text = reAWSAccessKey.ReplaceAllString(text, "AKIA***")
	text = reSlackBotToken.ReplaceAllString(text, "xoxb-***")
	text = rePrivateKeyBlock.ReplaceAllString(text, "[REDACTED_PRIVATE_KEY]")
	text = rePrivateKeyOrphan.ReplaceAllString(text, "[REDACTED_PRIVATE_KEY]")
	text = rePrivateKeyMarker.ReplaceAllString(text, "[REDACTED_PRIVATE_KEY]")
	text = reSlackWebhookURL.ReplaceAllString(text, "https://hooks.slack.com/services/***")
	text = reDiscordWebhookURL.ReplaceAllString(text, "https://discord.com/api/webhooks/***")
	text = reURLCredential.ReplaceAllString(text, "${1}***")
	text = reAuthHeader.ReplaceAllString(text, "${1}***")
	text = reCookieHeader.ReplaceAllString(text, "${1}***")
	return text
}
