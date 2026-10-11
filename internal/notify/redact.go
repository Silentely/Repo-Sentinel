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
	rePrivateKey        = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
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
	text = rePrivateKey.ReplaceAllString(text, "[REDACTED_PRIVATE_KEY]")
	return text
}
