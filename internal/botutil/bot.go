package botutil

import "strings"

// IsBotUser 判断指定 GitHub 用户名或类型是否为机器人账户。
// 规则：
// 1. GitHub API 用户类型为 "Bot"（忽略大小写）；
// 2. 登录名以 "[bot]" 结尾（如 dependabot[bot], renovate[bot], github-actions[bot] 等）；
// 3. 常见自动化机器人名匹配（如 dependabot, renovate, github-actions, greenkeeper, snyk-bot, codecov, copilot）。
func IsBotUser(login, userType string) bool {
	if strings.EqualFold(strings.TrimSpace(userType), "Bot") {
		return true
	}
	loginLower := strings.ToLower(strings.TrimSpace(login))
	if strings.HasSuffix(loginLower, "[bot]") {
		return true
	}
	switch loginLower {
	case "dependabot", "renovate", "github-actions", "greenkeeper", "snyk-bot", "codecov", "copilot":
		return true
	}
	return false
}
