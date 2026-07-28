package app

import (
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
)

func newRuntimeFromEnv(cfg config.Config, client *githubx.AppClient) *githubx.RuntimeConfig {
	rt := &githubx.RuntimeConfig{
		AppID:                 cfg.GitHub.AppID,
		ClientID:              strings.TrimSpace(cfg.GitHub.ClientID),
		PrivateKeyPath:        strings.TrimSpace(cfg.GitHub.PrivateKeyPath),
		WebhookSecret:         cfg.GitHub.WebhookSecret.Reveal(),
		WebhookPreviousSecret: cfg.GitHub.WebhookPreviousSecret.Reveal(),
		ExternalPAT:           cfg.GitHub.ExternalPAT.Reveal(),
		PublicBaseURL:         strings.TrimSpace(cfg.HTTP.PublicBaseURL),
		Client:                client,
	}
	rt.AppIDSource = sourceIf(rt.AppID > 0, "env")
	rt.ClientIDSource = sourceIf(rt.ClientID != "", "env")
	rt.PrivateKeySource = sourceIf(rt.PrivateKeyPath != "", "env")
	rt.WebhookSecretSource = sourceIf(strings.TrimSpace(rt.WebhookSecret) != "", "env")
	rt.PublicBaseURLSource = sourceIf(rt.PublicBaseURL != "", "env")
	rt.ExternalPATSource = sourceIf(strings.TrimSpace(rt.ExternalPAT) != "", "env")
	rt.ApplyToClient()
	return rt
}

func sourceIf(ok bool, source string) string {
	if ok {
		return source
	}
	return "unset"
}
