package normalizer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// repoSource 抽象 GitHub 仓库数据源，使 NormalizeRepository 不依赖具体响应结构。
type repoSource interface {
	GetFullName() string
	GetID() int64
	GetName() string
	GetHTMLURL() string
	GetArchived() bool
	GetPrivate() bool
	GetDefaultBranch() string
	GetOwnerLogin() string
}

// ghRepository 实现 repoSource（Webhook 路径）。
func (g *ghRepository) GetFullName() string      { return g.FullName }
func (g *ghRepository) GetID() int64             { return g.ID }
func (g *ghRepository) GetName() string          { return g.Name }
func (g *ghRepository) GetHTMLURL() string       { return g.HTMLURL }
func (g *ghRepository) GetArchived() bool        { return g.Archived }
func (g *ghRepository) GetPrivate() bool         { return g.Private }
func (g *ghRepository) GetDefaultBranch() string { return g.DefaultBranch }
func (g *ghRepository) GetOwnerLogin() string    { return g.Owner.Login }

// InstallationRepo 在 internal/githubx 定义；本包仅通过 repoSource 接口使用。

// NormalizeRepository 将 GitHub 仓库数据规范化为本地仓库记录并写入存储。
// 处理 owner/name 解析、归档联动、基线状态等逻辑，供 Webhook 处理器与 HTTP handler 共用。
// logger 可选：归档联动写失败（GitHub 已归档但本地能力开关未收口）时 Warn 留痕。
func NormalizeRepository(ctx context.Context, s store.Store, gh repoSource, installationID *string, logger *slog.Logger) (store.Repository, error) {
	if gh.GetFullName() == "" {
		return store.Repository{}, errors.New("missing repository")
	}

	fullName := gh.GetFullName()
	owner, name := gh.GetOwnerLogin(), gh.GetName()
	if idx := strings.IndexByte(fullName, '/'); idx >= 0 {
		if owner == "" {
			owner = fullName[:idx]
		}
		if name == "" {
			name = fullName[idx+1:]
		}
	}

	repoID := gh.GetID()
	htmlURL := strings.TrimSpace(gh.GetHTMLURL())
	if htmlURL == "" && gh.GetFullName() != "" {
		htmlURL = "https://github.com/" + gh.GetFullName()
	}

	in := store.Repository{
		Type:           store.RepositoryTypeInstallation,
		SyncStatus:     store.SyncStatusBaseline,
		GitHubRepoID:   &repoID,
		Owner:          owner,
		Name:           name,
		FullName:       gh.GetFullName(),
		InstallationID: installationID,
		IsArchived:     gh.GetArchived(),
		IsPrivate:      gh.GetPrivate(),
		HTMLURL:        htmlURL,
		DefaultBranch:  gh.GetDefaultBranch(),
	}

	existing, err := s.Repositories().GetByFullName(ctx, gh.GetFullName())
	if err == nil {
		in.ID = existing.ID
		in.SyncStatus = existing.SyncStatus
		if existing.SyncStatus == "" {
			in.SyncStatus = store.SyncStatusBaseline
		}
		// payload 显示 GitHub 侧已归档而本地未归档：联动收口归档状态与能力开关
		// （与设置页手动归档语义一致，均由 UpdateSettings 统一处理）。
		if gh.GetArchived() && existing.SyncStatus != store.SyncStatusArchived {
			archived := true
			if uerr := s.Repositories().UpdateSettings(ctx, existing.ID, store.RepositorySettings{IsArchived: &archived}); uerr == nil {
				in.SyncStatus = store.SyncStatusArchived
			} else if logger != nil {
				// 联动失败会留下「GitHub 已归档但本地能力开关未收口」的不一致，必须留痕。
				logger.Warn("repository archive link update failed",
					"repo", gh.GetFullName(), "error_code", "archive_link_failed", "error", uerr.Error())
			}
		}
		// 本地归档标记不被单条 payload 抹掉：取消归档仅经 unarchived 事件或设置页操作。
		if existing.IsArchived && !gh.GetArchived() {
			in.IsArchived = true
		}
		// 类型不由 webhook 单方面改写：外部公开仓（匿名轮询）若因普通事件被改判为
		// installation 而载荷又没有可解析的安装绑定，该行会退出匿名轮询却仍在
		// resolveInstallationToken 处失败，sync_status 永远停在 baseline_sync——
		// 与本次修复的静默失联同源。携带真实绑定时允许升级为 installation。
		if in.Type != existing.Type && (in.InstallationID == nil || *in.InstallationID == "") {
			if logger != nil {
				logger.Warn("repository type preserved on webhook upsert",
					"repo", in.FullName, "existing_type", existing.Type,
					"payload_type", in.Type, "error_code", "repo_type_reclassify_skipped")
			}
			in.Type = existing.Type
		}
		// 已存在仓库保持状态，仅更新元数据
		return s.Repositories().Upsert(ctx, in)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Repository{}, err
	}

	now := time.Now().UTC()
	in.BaselineStartedAt = &now
	// 安装绑定缺失留痕：新建的 installation 行若无 installation_id，将永远无法换取令牌
	// （resolveInstallationToken 直接 missing_installation），sync_status 永远停在
	// baseline_sync，其事件又被 baseline 抑制通知——平台静默失去对该仓库的可见性。
	// 正常路径由 Processor.resolveInstallation 从信封补回绑定；走到这里说明载荷未携带
	// installation 或本地尚未同步该安装，按 webhook 投递语义留痕而非阻断（投递行仍会被
	// GitHub 重试，届时通常已能解析）。恢复手段：SyncInstallations 或重新授权。
	if in.Type == store.RepositoryTypeInstallation &&
		(in.InstallationID == nil || *in.InstallationID == "") && logger != nil {
		logger.Warn("installation repository created without installation binding",
			"repo", in.FullName, "error_code", "repo_missing_installation_binding")
	}
	return s.Repositories().Upsert(ctx, in)
}
