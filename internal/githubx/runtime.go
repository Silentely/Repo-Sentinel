package githubx

import (
	"strings"
	"sync"
)

// WebhookPath GitHub Webhook 挂载路径（唯一事实来源）。
const WebhookPath = "/webhooks/github"

// RuntimeConfig 持有进程内可热更新的 GitHub 相关配置。
// 环境变量在启动时写入后，管理台可在 env 未设置的字段上叠加数据库值。
type RuntimeConfig struct {
	mu sync.RWMutex

	AppID                 int64
	ClientID              string
	PrivateKeyPath        string
	PrivateKeyPEM         string
	WebhookSecret         string
	WebhookPreviousSecret string
	ExternalPAT           string
	PublicBaseURL         string

	// 字段来源：env | database | unset（仅状态展示，不回显秘密）。
	AppIDSource         string
	ClientIDSource      string
	PrivateKeySource    string
	WebhookSecretSource string
	PublicBaseURLSource string
	ExternalPATSource   string

	Client *AppClient
}

// Snapshot 返回当前只读副本，供 HTTP 处理使用。
func (r *RuntimeConfig) Snapshot() RuntimeConfig {
	if r == nil {
		return RuntimeConfig{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RuntimeConfig{
		AppID:                 r.AppID,
		ClientID:              r.ClientID,
		PrivateKeyPath:        r.PrivateKeyPath,
		PrivateKeyPEM:         r.PrivateKeyPEM,
		WebhookSecret:         r.WebhookSecret,
		WebhookPreviousSecret: r.WebhookPreviousSecret,
		ExternalPAT:           r.ExternalPAT,
		PublicBaseURL:         r.PublicBaseURL,
		AppIDSource:           r.AppIDSource,
		ClientIDSource:        r.ClientIDSource,
		PrivateKeySource:      r.PrivateKeySource,
		WebhookSecretSource:   r.WebhookSecretSource,
		PublicBaseURLSource:   r.PublicBaseURLSource,
		ExternalPATSource:     r.ExternalPATSource,
	}
}

// ApplyToClient 将当前 App 身份同步到 AppClient。
func (r *RuntimeConfig) ApplyToClient() {
	if r == nil || r.Client == nil {
		return
	}
	r.mu.RLock()
	appID := r.AppID
	path := r.PrivateKeyPath
	pem := r.PrivateKeyPEM
	r.mu.RUnlock()
	r.Client.Configure(appID, path, pem)
}

// sourceLabel 标记字段来源：有值记为 source，否则 unset。
func sourceLabel(ok bool, source string) string {
	if ok {
		return source
	}
	return "unset"
}

// RuntimeFromEnv 从环境变量配置构建运行时基线并标记来源。
// 供启动装配（app）与保存后的 env 重建（httpapi）复用，保证 env 优先语义唯一实现。
func RuntimeFromEnv(
	appID int64,
	clientID, privateKeyPath, webhookSecret, webhookPreviousSecret, externalPAT, publicBaseURL string,
	client *AppClient,
) *RuntimeConfig {
	rt := &RuntimeConfig{
		AppID:                 appID,
		ClientID:              strings.TrimSpace(clientID),
		PrivateKeyPath:        strings.TrimSpace(privateKeyPath),
		WebhookSecret:         webhookSecret,
		WebhookPreviousSecret: webhookPreviousSecret,
		ExternalPAT:           externalPAT,
		PublicBaseURL:         strings.TrimSpace(publicBaseURL),
		Client:                client,
	}
	rt.AppIDSource = sourceLabel(rt.AppID > 0, "env")
	rt.ClientIDSource = sourceLabel(rt.ClientID != "", "env")
	rt.PrivateKeySource = sourceLabel(rt.PrivateKeyPath != "", "env")
	rt.WebhookSecretSource = sourceLabel(strings.TrimSpace(rt.WebhookSecret) != "", "env")
	rt.PublicBaseURLSource = sourceLabel(rt.PublicBaseURL != "", "env")
	rt.ExternalPATSource = sourceLabel(strings.TrimSpace(rt.ExternalPAT) != "", "env")
	rt.ApplyToClient()
	return rt
}

// Replace 用完整快照替换可变字段（保留 Client 指针）。
func (r *RuntimeConfig) Replace(next *RuntimeConfig) {
	if r == nil || next == nil {
		return
	}
	r.mu.Lock()
	client := r.Client
	r.AppID = next.AppID
	r.ClientID = next.ClientID
	r.PrivateKeyPath = next.PrivateKeyPath
	r.PrivateKeyPEM = next.PrivateKeyPEM
	r.WebhookSecret = next.WebhookSecret
	r.WebhookPreviousSecret = next.WebhookPreviousSecret
	r.ExternalPAT = next.ExternalPAT
	r.PublicBaseURL = next.PublicBaseURL
	r.AppIDSource = next.AppIDSource
	r.ClientIDSource = next.ClientIDSource
	r.PrivateKeySource = next.PrivateKeySource
	r.WebhookSecretSource = next.WebhookSecretSource
	r.PublicBaseURLSource = next.PublicBaseURLSource
	r.ExternalPATSource = next.ExternalPATSource
	r.Client = client
	r.mu.Unlock()
	r.ApplyToClient()
}

// WebhookSecrets 返回当前与 previous Secret（非空才加入）。
func (r *RuntimeConfig) WebhookSecrets() []string {
	if r == nil {
		return nil
	}
	snap := r.Snapshot()
	out := make([]string, 0, 2)
	if v := strings.TrimSpace(snap.WebhookSecret); v != "" {
		out = append(out, v)
	}
	if v := strings.TrimSpace(snap.WebhookPreviousSecret); v != "" {
		out = append(out, v)
	}
	return out
}

// StatusFlags 供管理面展示，不包含秘密。
func (r *RuntimeConfig) StatusFlags() (appID, clientID, privateKey, webhook, previous, externalPAT bool, publicBaseURL, webhookPath string) {
	if r == nil {
		return false, false, false, false, false, false, "", WebhookPath
	}
	snap := r.Snapshot()
	privateKeyOK := strings.TrimSpace(snap.PrivateKeyPEM) != ""
	if !privateKeyOK && r.Client != nil {
		privateKeyOK = r.Client.HasPrivateKeyMaterial()
	}
	return snap.AppID > 0,
		strings.TrimSpace(snap.ClientID) != "",
		privateKeyOK,
		strings.TrimSpace(snap.WebhookSecret) != "",
		strings.TrimSpace(snap.WebhookPreviousSecret) != "",
		strings.TrimSpace(snap.ExternalPAT) != "",
		snap.PublicBaseURL,
		WebhookPath
}
