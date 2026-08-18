package syncx

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// 仓库清单同步的配置级错误，由 HTTP 层映射为具体状态码。
var (
	// ErrAppNotConfigured GitHub App 未配置或不可用。
	// 统一复用 githubx 的 sentinel，保证 errors.Is 判定跨包一致
	//（githubx.AppJWT 与 syncx.ReconcileRepository 返回同一错误）。
	ErrAppNotConfigured = githubx.ErrAppNotConfigured
	// ErrNoInstallation 本地尚无任何 GitHub App 安装。
	ErrNoInstallation = errors.New("github_no_installation")
)

// SyncInstallationResult 一次安装仓库清单同步的结果。
type SyncInstallationResult struct {
	Installations int    `json:"installations"`
	Imported      int    `json:"imported_or_updated"`
	LastError     string `json:"last_error,omitempty"`
}

// SyncInstallations 用 Installation Token 拉取 GitHub 上已授权仓库并写入本地（基线状态）。
// 用于补救「installation 事件已到但旧版本未解析 repositories」或主动刷新授权范围。
// 返回结果与配置级错误（App 未配置 / 无安装）；单安装级失败不中断，记入 result.LastError。
func (r *Reconciler) SyncInstallations(ctx context.Context, maxPages int) (SyncInstallationResult, error) {
	if r.GitHub == nil || !r.GitHub.Configured() {
		return SyncInstallationResult{}, ErrAppNotConfigured
	}
	installations, err := r.Store.Installations().List(ctx)
	if err != nil {
		return SyncInstallationResult{}, err
	}
	if len(installations) == 0 {
		return SyncInstallationResult{}, ErrNoInstallation
	}
	if maxPages <= 0 {
		maxPages = 20
	}
	result := SyncInstallationResult{Installations: len(installations)}
	// 一次性加载本地仓库建 full_name→repo 映射，避免对每个安装仓库 GetByFullName 的
	// N+1 查询（安装仓库多时一轮同步数百次单查）。加载失败降级逐仓单查并留痕。
	existingByFullName := make(map[string]store.Repository)
	for page := 1; ; page++ {
		repos, res, err := r.Store.Repositories().List(ctx, store.ListFilter{Page: page, PerPage: 100})
		if err != nil {
			r.warn("load local repositories failed", 0, err)
			break
		}
		for _, repo := range repos {
			existingByFullName[repo.FullName] = repo
		}
		if page*res.PerPage >= res.Total || len(repos) == 0 {
			break
		}
	}
	for _, inst := range installations {
		token, err := r.GitHub.InstallationToken(ctx, inst.InstallationID)
		if err != nil {
			result.LastError = err.Error()
			r.warn("installation token failed", inst.InstallationID, err)
			continue
		}
		instID := inst.ID
		for page := 1; page <= maxPages; page++ {
			repos, _, err := r.GitHub.ListInstallationRepositories(ctx, token, page)
			if err != nil {
				result.LastError = err.Error()
				r.warn("list installation repositories failed", inst.InstallationID, err)
				break
			}
			if len(repos) == 0 {
				break
			}
			for _, gr := range repos {
				fullName := strings.TrimSpace(gr.FullName)
				if fullName == "" {
					continue
				}
				owner, name := gr.Owner.Login, gr.Name
				parts := strings.SplitN(fullName, "/", 2)
				if len(parts) == 2 {
					if owner == "" {
						owner = parts[0]
					}
					if name == "" {
						name = parts[1]
					}
				}
				htmlURL := strings.TrimSpace(gr.HTMLURL)
				if htmlURL == "" {
					htmlURL = "https://github.com/" + fullName
				}
				repoID := gr.ID
				in := store.Repository{
					Type:           store.RepositoryTypeInstallation,
					SyncStatus:     store.SyncStatusBaseline,
					GitHubRepoID:   &repoID,
					Owner:          owner,
					Name:           name,
					FullName:       fullName,
					InstallationID: &instID,
					IsArchived:     gr.Archived,
					IsPrivate:      gr.Private,
					HTMLURL:        htmlURL,
					DefaultBranch:  gr.DefaultBranch,
				}
				existing, ok := existingByFullName[fullName]
				if ok {
					in.ID = existing.ID
					in.SyncStatus = existing.SyncStatus
					if existing.SyncStatus == "" {
						in.SyncStatus = store.SyncStatusBaseline
					}
					// GitHub 侧已归档而本地未归档：联动收口归档状态与能力开关
					//（与 normalizer 的 webhook 侧处理同一语义）。
					if gr.Archived && existing.SyncStatus != store.SyncStatusArchived {
						archived := true
						if uerr := r.Store.Repositories().UpdateSettings(ctx, existing.ID, store.RepositorySettings{IsArchived: &archived}); uerr == nil {
							in.SyncStatus = store.SyncStatusArchived
						}
					}
					// 本地归档标记不因清单数据抹掉：取消归档仅经 unarchived 事件或设置页操作。
					if existing.IsArchived && !gr.Archived {
						in.IsArchived = true
					}
					if _, err := r.Store.Repositories().Upsert(ctx, in); err != nil {
						result.LastError = err.Error()
						continue
					}
					result.Imported++
					continue
				}
				// map 不命中 = 本地尚无该仓库：按新建入库（语义与 GetByFullName 的
				// ErrNotFound 分支一致；映射加载失败降级时同样走此路径）。
				now := time.Now().UTC()
				in.BaselineStartedAt = &now
				if _, err := r.Store.Repositories().Upsert(ctx, in); err != nil {
					result.LastError = err.Error()
					continue
				}
				result.Imported++
			}
			if len(repos) < 100 {
				break
			}
		}
	}
	return result, nil
}

func (r *Reconciler) warn(msg string, installationID int64, err error) {
	if r.Logger != nil {
		r.Logger.Warn(msg, "installation_id", installationID, "error", err.Error())
	}
}
