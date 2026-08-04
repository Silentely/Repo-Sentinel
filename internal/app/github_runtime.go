package app

import (
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
)

// newRuntimeFromEnv 从环境变量构建 GitHub 运行时基线。
// 实际构建逻辑在 githubx.RuntimeFromEnv（httpapi 保存后重建 env 基线时复用同一实现）。
func newRuntimeFromEnv(cfg config.Config, client *githubx.AppClient) *githubx.RuntimeConfig {
	return githubx.RuntimeFromEnv(
		cfg.GitHub.AppID,
		cfg.GitHub.ClientID,
		cfg.GitHub.PrivateKeyPath,
		cfg.GitHub.WebhookSecret.Reveal(),
		cfg.GitHub.WebhookPreviousSecret.Reveal(),
		cfg.GitHub.ExternalPAT.Reveal(),
		cfg.HTTP.PublicBaseURL,
		client,
	)
}
