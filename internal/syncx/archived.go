package syncx

import (
	"context"
	"log/slog"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// collapseArchived 收口「GitHub 侧已归档但本地状态未联动」的仓：
// UpdateSettings 会一并关闭监控与全部能力开关，避免平台继续轮询并通知一个已归档的仓。
// 返回 bool 表示更新是否成功。
//
// 失败必须留痕而不向上返回错误：收口是对账/轮询的顺带动作，单个仓写失败不应中断整轮；
// 但静默丢弃会让本地仓停留在「监控开启 + 能力开启」，运维无从判断为何仍在收到归档仓的通知。
//
// 对账、外部轮询与 Installation 同步三条路径共用（日志文案由 caller 区分）。
func collapseArchived(ctx context.Context, data store.Store, logger *slog.Logger, logMsg string, repo store.Repository) bool {
	archived := true
	if err := data.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{IsArchived: &archived}); err != nil {
		if logger != nil {
			logger.Warn(logMsg,
				"repo", repo.FullName, "error_code", "repo_state_update_failed", "error", err.Error())
		}
		return false
	}
	return true
}
