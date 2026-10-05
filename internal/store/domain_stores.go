package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/event"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/githubinstallation"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/notificationchannel"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/notificationoutbox"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/predicate"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/repository"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/repostatsnapshot"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/securityalert"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/synccursor"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/systemlease"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/systemsetting"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/webhookdelivery"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/workflowrun"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/workitem"
	"github.com/oklog/ulid/v2"
)

func newID() string { return ulid.Make().String() }

// --- installations ---

type installationStore struct{ client *entclient.Client }

func (s *installationStore) Upsert(ctx context.Context, in GitHubInstallation) (GitHubInstallation, error) {
	now := time.Now().UTC()
	existing, err := s.client.GitHubInstallation.Query().
		Where(githubinstallation.InstallationIDEQ(in.InstallationID)).
		Only(ctx)
	if err == nil {
		entity, err := s.client.GitHubInstallation.UpdateOneID(existing.ID).
			SetAccountLogin(in.AccountLogin).
			SetAccountType(in.AccountType).
			SetTargetType(in.TargetType).
			SetPermissionsJSON(in.PermissionsJSON).
			SetSuspended(in.Suspended).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return GitHubInstallation{}, mapStoreError(err)
		}
		return installationFromEntity(entity), nil
	}
	if mapStoreError(err) != ErrNotFound {
		return GitHubInstallation{}, mapStoreError(err)
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	entity, err := s.client.GitHubInstallation.Create().
		SetID(in.ID).
		SetInstallationID(in.InstallationID).
		SetAccountLogin(in.AccountLogin).
		SetAccountType(in.AccountType).
		SetTargetType(in.TargetType).
		SetPermissionsJSON(in.PermissionsJSON).
		SetSuspended(in.Suspended).
		SetCreatedAt(in.CreatedAt.UTC()).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return GitHubInstallation{}, mapStoreError(err)
	}
	return installationFromEntity(entity), nil
}

func (s *installationStore) Get(ctx context.Context, id string) (GitHubInstallation, error) {
	entity, err := s.client.GitHubInstallation.Get(ctx, id)
	if err != nil {
		return GitHubInstallation{}, mapStoreError(err)
	}
	return installationFromEntity(entity), nil
}

func (s *installationStore) GetByInstallationID(ctx context.Context, id int64) (GitHubInstallation, error) {
	entity, err := s.client.GitHubInstallation.Query().Where(githubinstallation.InstallationIDEQ(id)).Only(ctx)
	if err != nil {
		return GitHubInstallation{}, mapStoreError(err)
	}
	return installationFromEntity(entity), nil
}

func (s *installationStore) List(ctx context.Context) ([]GitHubInstallation, error) {
	rows, err := s.client.GitHubInstallation.Query().All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]GitHubInstallation, 0, len(rows))
	for _, row := range rows {
		out = append(out, installationFromEntity(row))
	}
	return out, nil
}

func installationFromEntity(e *entclient.GitHubInstallation) GitHubInstallation {
	return GitHubInstallation{
		ID: e.ID, InstallationID: e.InstallationID, AccountLogin: e.AccountLogin,
		AccountType: e.AccountType, TargetType: e.TargetType, PermissionsJSON: e.PermissionsJSON,
		Suspended: e.Suspended, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- repositories ---

type repositoryStore struct {
	client *entclient.Client
	// idsCache 与 storeImpl 共享：写路径成功后失效，保证活跃/归档集合立即可见。
	idsCache *ttlValueCache[cachedRepoIDs]
	// settingsCache 与 storeImpl 共享：级联删除直删 ai.pr_review.* 等设置键后
	// 须逐键失效，否则 5s TTL 内 Settings().Get 仍可能返回已删除的数据。nil 安全。
	settingsCache *settingsCache
}

func (s *repositoryStore) Upsert(ctx context.Context, in Repository) (Repository, error) {
	now := time.Now().UTC()
	existing, err := s.client.Repository.Query().Where(repository.FullNameEQ(in.FullName)).Only(ctx)
	if err == nil {
		// 类型降级守卫：安装仓改判为外部仓会退出安装对账并改走匿名轮询，私有仓随即被
		// 404 钉成 unavailable 并永久跳过（ingestGate / RepoAllowsKind / ReconcileAll /
		// PollAll 全部跳过），属于监控能力的静默丢失。采集方式由安装事件与设置页决定，
		// 任何写路径都不得单方面改写它。
		if in.Type != existing.Type && existing.Type == RepositoryTypeInstallation {
			return Repository{}, fmt.Errorf(
				"store: cannot reclassify installation repository %q as %s", existing.FullName, in.Type)
		}
		// 能力开关由 UpdateSettings 单独管理；Upsert 仅同步元数据，保留用户配置。
		upd := s.client.Repository.UpdateOneID(existing.ID).
			SetType(in.Type).
			SetSyncStatus(in.SyncStatus).
			SetOwner(in.Owner).
			SetName(in.Name).
			SetFullName(in.FullName).
			SetIsArchived(in.IsArchived).
			SetIsPrivate(in.IsPrivate).
			SetHTMLURL(in.HTMLURL).
			SetDefaultBranch(in.DefaultBranch).
			SetLastSyncErrorCode(in.LastSyncErrorCode).
			SetUpdatedAt(now)
		if in.GitHubRepoID != nil {
			upd.SetGithubRepoID(*in.GitHubRepoID)
		}
		if in.InstallationID != nil {
			upd.SetInstallationID(*in.InstallationID)
		}
		if in.BaselineStartedAt != nil {
			upd.SetBaselineStartedAt(*in.BaselineStartedAt)
		}
		if in.BaselineFinishedAt != nil {
			upd.SetBaselineFinishedAt(*in.BaselineFinishedAt)
		}
		if in.LastSyncedAt != nil {
			upd.SetLastSyncedAt(*in.LastSyncedAt)
		}
		entity, err := upd.Save(ctx)
		if err != nil {
			return Repository{}, mapStoreError(err)
		}
		s.idsCache.Invalidate()
		return repositoryFromEntity(entity), nil
	}
	if mapStoreError(err) != ErrNotFound {
		return Repository{}, mapStoreError(err)
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	if in.SyncStatus == "" {
		in.SyncStatus = SyncStatusBaseline
	}
	c := s.client.Repository.Create().
		SetID(in.ID).
		SetType(in.Type).
		SetSyncStatus(in.SyncStatus).
		SetOwner(in.Owner).
		SetName(in.Name).
		SetFullName(in.FullName).
		SetIsArchived(in.IsArchived).
		SetIsPrivate(in.IsPrivate).
		SetHTMLURL(in.HTMLURL).
		SetDefaultBranch(in.DefaultBranch).
		SetLastSyncErrorCode(in.LastSyncErrorCode).
		SetCreatedAt(in.CreatedAt.UTC()).
		SetUpdatedAt(now)
	if in.GitHubRepoID != nil {
		c.SetGithubRepoID(*in.GitHubRepoID)
	}
	if in.InstallationID != nil {
		c.SetInstallationID(*in.InstallationID)
	}
	// 与更新路径保持一致：可选时间字段在创建时同样要落库。
	if in.BaselineStartedAt != nil {
		c.SetBaselineStartedAt(*in.BaselineStartedAt)
	}
	if in.BaselineFinishedAt != nil {
		c.SetBaselineFinishedAt(*in.BaselineFinishedAt)
	}
	if in.LastSyncedAt != nil {
		c.SetLastSyncedAt(*in.LastSyncedAt)
	}
	entity, err := c.Save(ctx)
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	s.idsCache.Invalidate()
	return repositoryFromEntity(entity), nil
}

func (s *repositoryStore) Get(ctx context.Context, id string) (Repository, error) {
	entity, err := s.client.Repository.Get(ctx, id)
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromEntity(entity), nil
}

func (s *repositoryStore) GetByFullName(ctx context.Context, fullName string) (Repository, error) {
	entity, err := s.client.Repository.Query().Where(repository.FullNameEQ(fullName)).Only(ctx)
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromEntity(entity), nil
}

func (s *repositoryStore) GetByGitHubRepoID(ctx context.Context, githubID int64) (Repository, error) {
	entity, err := s.client.Repository.Query().Where(repository.GithubRepoIDEQ(githubID)).Only(ctx)
	if err != nil {
		return Repository{}, mapStoreError(err)
	}
	return repositoryFromEntity(entity), nil
}

func (s *repositoryStore) List(ctx context.Context, f ListFilter) ([]Repository, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.Repository.Query()
	if f.Kind != "" {
		q = q.Where(repository.TypeEQ(f.Kind))
	}
	if f.Status != "" {
		q = q.Where(repository.SyncStatusEQ(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []Repository{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(repository.FieldUpdatedAt), entclient.Asc(repository.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]Repository, 0, len(rows))
	for _, row := range rows {
		out = append(out, repositoryFromEntity(row))
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *repositoryStore) ListSyncCandidates(ctx context.Context, repoType string, limit int) ([]Repository, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.client.Repository.Query().
		Where(repository.TypeEQ(repoType)).
		Order(repository.ByLastSyncedAt(entsql.OrderAsc(), entsql.OrderNullsFirst())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]Repository, 0, len(rows))
	for _, row := range rows {
		out = append(out, repositoryFromEntity(row))
	}
	return out, nil
}

func (s *repositoryStore) UpdateSyncStatus(ctx context.Context, id, status string) error {
	err := s.client.Repository.UpdateOneID(id).
		SetSyncStatus(status).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
	if err == nil {
		s.idsCache.Invalidate()
	}
	return mapStoreError(err)
}

// DeleteRepository 级联删除仓库及其全部关联数据，整体在一个事务内完成。
// 删除顺序：先取该仓库的事件 ID，删除引用这些事件的 Outbox 投递（通知正文虽已快照，
// 但事件与仓库行即将消失，继续投递没有意义）；再删事件、工作项、运行、告警、游标与
// 指标快照；最后删仓库行本身。仓库不存在时返回 ErrNotFound，调用方可按幂等语义忽略。
func (s *repositoryStore) DeleteRepository(ctx context.Context, id string) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return mapStoreError(err)
	}
	defer func() { _ = tx.Rollback() }()

	eventIDs, err := tx.Event.Query().Where(event.RepositoryIDEQ(id)).IDs(ctx)
	if err != nil {
		return mapStoreError(err)
	}
	if len(eventIDs) > 0 {
		if _, err := tx.NotificationOutbox.Delete().
			Where(notificationoutbox.EventIDIn(eventIDs...)).Exec(ctx); err != nil {
			return mapStoreError(err)
		}
	}
	if _, err := tx.Event.Delete().Where(event.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	// 清理该仓库所有 WorkItem 关联的 PR 审查等系统设置
	workItemIDs, err := tx.WorkItem.Query().Where(workitem.RepositoryIDEQ(id)).IDs(ctx)
	if err != nil {
		return mapStoreError(err)
	}
	var reviewKeys []string
	if len(workItemIDs) > 0 {
		reviewKeys = make([]string, 0, len(workItemIDs)*2)
		for _, wid := range workItemIDs {
			reviewKeys = append(reviewKeys, "ai.pr_review."+wid)
			reviewKeys = append(reviewKeys, "ai.issue_triage."+wid)
		}
		if _, err := tx.SystemSetting.Delete().Where(systemsetting.KeyIn(reviewKeys...)).Exec(ctx); err != nil {
			return mapStoreError(err)
		}
	}
	if _, err := tx.WorkItem.Delete().Where(workitem.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if _, err := tx.WorkflowRun.Delete().Where(workflowrun.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if _, err := tx.SecurityAlert.Delete().Where(securityalert.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if _, err := tx.SyncCursor.Delete().Where(synccursor.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if _, err := tx.RepoStatSnapshot.Delete().Where(repostatsnapshot.RepositoryIDEQ(id)).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if err := tx.Repository.DeleteOneID(id).Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return mapStoreError(err)
	}
	s.idsCache.Invalidate()
	// 事务内直删的设置键绕过了 settingsStore.Upsert 的缓存失效路径，这里逐键失效，
	// 保证删除后立即不可见（而非等待 5s TTL 过期）。
	for _, key := range reviewKeys {
		s.settingsCache.Invalidate(key)
	}
	return nil
}

func (s *repositoryStore) UpdateSettings(ctx context.Context, id string, settings RepositorySettings) error {
	upd := s.client.Repository.UpdateOneID(id).SetUpdatedAt(time.Now().UTC())
	if settings.MonitorEnabled != nil {
		upd.SetMonitorEnabled(*settings.MonitorEnabled)
	}
	if settings.IssuesEnabled != nil {
		upd.SetIssuesEnabled(*settings.IssuesEnabled)
	}
	if settings.PrEnabled != nil {
		upd.SetPrEnabled(*settings.PrEnabled)
	}
	if settings.ActionsEnabled != nil {
		upd.SetActionsEnabled(*settings.ActionsEnabled)
	}
	if settings.AlertsEnabled != nil {
		upd.SetAlertsEnabled(*settings.AlertsEnabled)
	}
	if settings.StarsEnabled != nil {
		upd.SetStarsEnabled(*settings.StarsEnabled)
	}
	if settings.WatchesEnabled != nil {
		upd.SetWatchesEnabled(*settings.WatchesEnabled)
	}
	if settings.IsArchived != nil {
		upd.SetIsArchived(*settings.IsArchived)
		if *settings.IsArchived {
			// 归档时联动关闭所有能力开关，避免界面显示矛盾。
			upd.SetSyncStatus(SyncStatusArchived)
			upd.SetMonitorEnabled(false)
			upd.SetIssuesEnabled(false)
			upd.SetPrEnabled(false)
			upd.SetActionsEnabled(false)
			upd.SetAlertsEnabled(false)
			upd.SetStarsEnabled(false)
			upd.SetWatchesEnabled(false)
		} else {
			// 取消归档时恢复所有能力开关。
			upd.SetSyncStatus(SyncStatusActive)
			upd.SetMonitorEnabled(true)
			upd.SetIssuesEnabled(true)
			upd.SetPrEnabled(true)
			upd.SetActionsEnabled(true)
			upd.SetAlertsEnabled(true)
			upd.SetStarsEnabled(true)
			upd.SetWatchesEnabled(true)
		}
	}
	if err := upd.Exec(ctx); err != nil {
		return mapStoreError(err)
	}
	s.idsCache.Invalidate()
	return nil
}

func (s *repositoryStore) CountByType(ctx context.Context, repoType string) (int, error) {
	n, err := s.client.Repository.Query().Where(repository.TypeEQ(repoType)).Count(ctx)
	return n, mapStoreError(err)
}

func repositoryFromEntity(e *entclient.Repository) Repository {
	return Repository{
		ID: e.ID, Type: e.Type, SyncStatus: e.SyncStatus, GitHubRepoID: e.GithubRepoID,
		Owner: e.Owner, Name: e.Name, FullName: e.FullName, InstallationID: e.InstallationID,
		IsArchived: e.IsArchived, IsPrivate: e.IsPrivate,
		MonitorEnabled: e.MonitorEnabled, IssuesEnabled: e.IssuesEnabled,
		PrEnabled: e.PrEnabled, ActionsEnabled: e.ActionsEnabled, AlertsEnabled: e.AlertsEnabled,
		StarsEnabled: e.StarsEnabled, WatchesEnabled: e.WatchesEnabled,
		HTMLURL: e.HTMLURL, DefaultBranch: e.DefaultBranch,
		BaselineStartedAt: e.BaselineStartedAt, BaselineFinishedAt: e.BaselineFinishedAt,
		LastSyncedAt: e.LastSyncedAt, LastSyncErrorCode: e.LastSyncErrorCode,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- webhook deliveries ---

type webhookDeliveryStore struct {
	client *entclient.Client
	driver dialect.Driver
}

func (s *webhookDeliveryStore) Create(ctx context.Context, in WebhookDelivery) (WebhookDelivery, error) {
	if in.ID == "" {
		in.ID = newID()
	}
	if in.ReceivedAt.IsZero() {
		in.ReceivedAt = time.Now().UTC()
	}
	if in.Status == "" {
		in.Status = DeliveryAccepted
	}
	createOp := s.client.WebhookDelivery.Create().
		SetID(in.ID).
		SetDeliveryID(in.DeliveryID).
		SetEventType(in.EventType).
		SetAction(in.Action).
		SetRepositoryFullName(in.RepositoryFullName).
		SetStatus(in.Status).
		SetErrorCode(in.ErrorCode).
		SetPayload(in.Payload).
		SetReceivedAt(in.ReceivedAt.UTC()).
		SetClaimToken(in.ClaimToken).
		SetClaimVersion(in.ClaimVersion).
		SetClaimedBy(in.ClaimedBy).
		SetAttemptCount(in.AttemptCount).
		SetLastErrorCode(in.LastErrorCode)
	if in.ClaimedUntil != nil {
		createOp.SetClaimedUntil(*in.ClaimedUntil)
	}
	if in.ProcessedAt != nil {
		createOp.SetProcessedAt(*in.ProcessedAt)
	}
	entity, err := createOp.Save(ctx)
	if err != nil {
		return WebhookDelivery{}, mapStoreError(err)
	}
	return webhookDeliveryFromEntity(entity), nil
}

func (s *webhookDeliveryStore) Get(ctx context.Context, id string) (WebhookDelivery, error) {
	entity, err := s.client.WebhookDelivery.Query().Where(webhookdelivery.IDEQ(id)).Only(ctx)
	if err != nil {
		return WebhookDelivery{}, mapStoreError(err)
	}
	return webhookDeliveryFromEntity(entity), nil
}

func (s *webhookDeliveryStore) List(ctx context.Context, f ListFilter) ([]WebhookDelivery, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.WebhookDelivery.Query()
	if f.Status != "" {
		q = q.Where(webhookdelivery.StatusEQ(f.Status))
	}
	if f.Kind != "" {
		q = q.Where(webhookdelivery.EventTypeEQ(f.Kind))
	}
	if f.RepositoryID != "" {
		q = q.Where(webhookdelivery.RepositoryFullNameContainsFold(f.RepositoryID))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []WebhookDelivery{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(webhookdelivery.FieldReceivedAt), entclient.Desc(webhookdelivery.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]WebhookDelivery, 0, len(rows))
	for _, row := range rows {
		out = append(out, webhookDeliveryFromEntity(row))
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *webhookDeliveryStore) GetByDeliveryID(ctx context.Context, deliveryID string) (WebhookDelivery, error) {
	entity, err := s.client.WebhookDelivery.Query().Where(webhookdelivery.DeliveryIDEQ(deliveryID)).Only(ctx)
	if err != nil {
		return WebhookDelivery{}, mapStoreError(err)
	}
	return webhookDeliveryFromEntity(entity), nil
}

func (s *webhookDeliveryStore) getDB() *sql.DB {
	if s.driver == nil {
		return nil
	}
	if sqlDriver, ok := s.driver.(*entsql.Driver); ok {
		return sqlDriver.DB()
	}
	return nil
}

func scanWebhookDelivery(scanner interface{ Scan(...any) error }) (*WebhookDelivery, error) {
	var d WebhookDelivery
	var processedAt sql.NullTime
	var claimedUntil sql.NullTime
	err := scanner.Scan(
		&d.ID,
		&d.DeliveryID,
		&d.EventType,
		&d.Action,
		&d.RepositoryFullName,
		&d.Status,
		&d.ErrorCode,
		&d.Payload,
		&d.ReceivedAt,
		&processedAt,
		&d.ClaimToken,
		&d.ClaimVersion,
		&d.ClaimedBy,
		&claimedUntil,
		&d.AttemptCount,
		&d.LastErrorCode,
	)
	if err != nil {
		return nil, err
	}
	if processedAt.Valid {
		t := processedAt.Time.UTC()
		d.ProcessedAt = &t
	}
	if claimedUntil.Valid {
		t := claimedUntil.Time.UTC()
		d.ClaimedUntil = &t
	}
	d.ReceivedAt = d.ReceivedAt.UTC()
	return &d, nil
}

func (s *webhookDeliveryStore) ClaimWebhookForProcessing(ctx context.Context, id string, workerID string, ttl time.Duration) (string, bool, error) {
	if ttl == 0 {
		ttl = 3 * time.Minute
	}
	ttlSecs := int(ttl.Seconds())
	claimToken := newID()
	now := time.Now().UTC()
	claimedUntil := now.Add(ttl)

	db := s.getDB()
	if db != nil {
		var query string
		var args []any
		if s.driver != nil && s.driver.Dialect() == dialect.Postgres {
			query = `UPDATE webhook_deliveries
SET status = 'processing',
    claim_token = $1,
    claim_version = claim_version + 1,
    claimed_by = $2,
    claimed_until = now() + make_interval(secs => $3),
    attempt_count = attempt_count + 1
WHERE id = $4 AND (status = 'accepted' OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < now())))`
			args = []any{claimToken, workerID, ttlSecs, id}
		} else {
			query = `UPDATE webhook_deliveries
SET status = 'processing',
    claim_token = ?,
    claim_version = claim_version + 1,
    claimed_by = ?,
    claimed_until = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds'),
    attempt_count = attempt_count + 1
WHERE id = ? AND (status = 'accepted' OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < CURRENT_TIMESTAMP)))`
			args = []any{claimToken, workerID, ttlSecs, id}
		}

		res, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return "", false, mapStoreError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return "", false, mapStoreError(err)
		}
		if rows == 0 {
			return "", false, nil
		}
		return claimToken, true, nil
	}

	pred := webhookdelivery.And(
		webhookdelivery.IDEQ(id),
		webhookdelivery.Or(
			webhookdelivery.StatusEQ(DeliveryAccepted),
			webhookdelivery.And(
				webhookdelivery.StatusEQ(DeliveryProcessing),
				webhookdelivery.Or(
					webhookdelivery.ClaimedUntilIsNil(),
					webhookdelivery.ClaimedUntilLT(now),
				),
			),
		),
	)
	n, err := s.client.WebhookDelivery.Update().
		Where(pred).
		SetStatus(DeliveryProcessing).
		SetClaimToken(claimToken).
		AddClaimVersion(1).
		SetClaimedBy(workerID).
		SetClaimedUntil(claimedUntil).
		AddAttemptCount(1).
		Save(ctx)
	if err != nil {
		return "", false, mapStoreError(err)
	}
	if n == 0 {
		return "", false, nil
	}
	return claimToken, true, nil
}

func (s *webhookDeliveryStore) ClaimDueOrphanWebhooks(ctx context.Context, workerID string, cutoff time.Time, limit int) ([]*WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	ttl := 3 * time.Minute
	ttlSecs := int(ttl.Seconds())
	batchToken := newID()
	now := time.Now().UTC()
	cutoffUTC := cutoff.UTC()

	db := s.getDB()
	if db != nil {
		isPG := s.driver != nil && s.driver.Dialect() == dialect.Postgres
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, mapStoreError(err)
		}
		defer func() {
			_ = tx.Rollback()
		}()

		const selectCols = "id, delivery_id, event_type, action, repository_full_name, status, error_code, payload, received_at, processed_at, claim_token, claim_version, claimed_by, claimed_until, attempt_count, last_error_code"

		if isPG {
			updateSQL := `UPDATE webhook_deliveries
SET status = 'processing',
    claim_token = $1,
    claim_version = claim_version + 1,
    claimed_by = $2,
    claimed_until = now() + make_interval(secs => $3),
    attempt_count = attempt_count + 1
WHERE id IN (
    SELECT id FROM webhook_deliveries
    WHERE (status = 'accepted' AND received_at < $4)
       OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < now()))
    ORDER BY received_at ASC
    LIMIT $5
    FOR UPDATE SKIP LOCKED
)
AND ((status = 'accepted' AND received_at < $4)
     OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < now())))`
			if _, err := tx.ExecContext(ctx, updateSQL, batchToken, workerID, ttlSecs, cutoffUTC, limit); err != nil {
				return nil, mapStoreError(err)
			}

			rows, err := tx.QueryContext(ctx, `SELECT `+selectCols+` FROM webhook_deliveries WHERE claim_token = $1 ORDER BY received_at ASC`, batchToken)
			if err != nil {
				return nil, mapStoreError(err)
			}
			defer rows.Close()

			var out []*WebhookDelivery
			for rows.Next() {
				d, err := scanWebhookDelivery(rows)
				if err != nil {
					return nil, mapStoreError(err)
				}
				out = append(out, d)
			}
			if err := rows.Err(); err != nil {
				return nil, mapStoreError(err)
			}
			if err := tx.Commit(); err != nil {
				return nil, mapStoreError(err)
			}
			return out, nil
		} else {
			updateSQL := `UPDATE webhook_deliveries
SET status = 'processing',
    claim_token = ?,
    claim_version = claim_version + 1,
    claimed_by = ?,
    claimed_until = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds'),
    attempt_count = attempt_count + 1
WHERE id IN (
    SELECT id FROM webhook_deliveries
    WHERE (status = 'accepted' AND received_at < ?)
       OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < CURRENT_TIMESTAMP))
    ORDER BY received_at ASC
    LIMIT ?
)
AND ((status = 'accepted' AND received_at < ?)
     OR (status = 'processing' AND (claimed_until IS NULL OR claimed_until < CURRENT_TIMESTAMP)))`
			if _, err := tx.ExecContext(ctx, updateSQL, batchToken, workerID, ttlSecs, cutoffUTC, limit, cutoffUTC); err != nil {
				return nil, mapStoreError(err)
			}

			rows, err := tx.QueryContext(ctx, `SELECT `+selectCols+` FROM webhook_deliveries WHERE claim_token = ? ORDER BY received_at ASC`, batchToken)
			if err != nil {
				return nil, mapStoreError(err)
			}
			defer rows.Close()

			var out []*WebhookDelivery
			for rows.Next() {
				d, err := scanWebhookDelivery(rows)
				if err != nil {
					return nil, mapStoreError(err)
				}
				out = append(out, d)
			}
			if err := rows.Err(); err != nil {
				return nil, mapStoreError(err)
			}
			if err := tx.Commit(); err != nil {
				return nil, mapStoreError(err)
			}
			return out, nil
		}
	}

	claimedUntil := now.Add(ttl)
	pred := webhookdelivery.Or(
		webhookdelivery.And(
			webhookdelivery.StatusEQ(DeliveryAccepted),
			webhookdelivery.ReceivedAtLT(cutoffUTC),
		),
		webhookdelivery.And(
			webhookdelivery.StatusEQ(DeliveryProcessing),
			webhookdelivery.Or(
				webhookdelivery.ClaimedUntilIsNil(),
				webhookdelivery.ClaimedUntilLT(now),
			),
		),
	)
	entities, err := s.client.WebhookDelivery.Query().
		Where(pred).
		Order(entclient.Asc(webhookdelivery.FieldReceivedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	var out []*WebhookDelivery
	for _, entity := range entities {
		n, err := s.client.WebhookDelivery.Update().
			Where(
				webhookdelivery.IDEQ(entity.ID),
				webhookdelivery.ClaimVersionEQ(entity.ClaimVersion),
				pred,
			).
			SetStatus(DeliveryProcessing).
			SetClaimToken(batchToken).
			AddClaimVersion(1).
			SetClaimedBy(workerID).
			SetClaimedUntil(claimedUntil).
			AddAttemptCount(1).
			Save(ctx)
		if err == nil && n > 0 {
			cloned := webhookDeliveryFromEntity(entity)
			cloned.Status = DeliveryProcessing
			cloned.ClaimToken = batchToken
			cloned.ClaimVersion = entity.ClaimVersion + 1
			cloned.ClaimedBy = workerID
			cloned.ClaimedUntil = &claimedUntil
			cloned.AttemptCount = entity.AttemptCount + 1
			out = append(out, &cloned)
		}
	}
	return out, nil
}

func (s *webhookDeliveryStore) MarkProcessed(ctx context.Context, id string, claimToken string) (TransitionResult, error) {
	now := time.Now().UTC()
	db := s.getDB()
	if db != nil {
		var query string
		var args []any
		if s.driver != nil && s.driver.Dialect() == dialect.Postgres {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'processed', claim_token = '', processed_at = $1, last_error_code = '' WHERE id = $2 AND claim_token = $3 AND status = 'processing'`
				args = []any{now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'processed', claim_token = '', processed_at = $1, last_error_code = '' WHERE id = $2 AND (claim_token = '' OR claim_token IS NULL) AND (status = 'accepted' OR status = 'processing')`
				args = []any{now, id}
			}
		} else {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'processed', claim_token = '', processed_at = ?, last_error_code = '' WHERE id = ? AND claim_token = ? AND status = 'processing'`
				args = []any{now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'processed', claim_token = '', processed_at = ?, last_error_code = '' WHERE id = ? AND (claim_token = '' OR claim_token IS NULL) AND (status = 'accepted' OR status = 'processing')`
				args = []any{now, id}
			}
		}
		res, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		if rows == 0 {
			return TransitionResult{Applied: false, Stale: true}, nil
		}
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	q := s.client.WebhookDelivery.Update().Where(webhookdelivery.IDEQ(id))
	if claimToken != "" {
		q = q.Where(webhookdelivery.ClaimTokenEQ(claimToken), webhookdelivery.StatusEQ(DeliveryProcessing))
	} else {
		q = q.Where(
			webhookdelivery.ClaimTokenEQ(""),
			webhookdelivery.Or(
				webhookdelivery.StatusEQ(DeliveryAccepted),
				webhookdelivery.StatusEQ(DeliveryProcessing),
			),
		)
	}
	n, err := q.
		SetStatus(DeliveryProcessed).
		SetClaimToken("").
		SetProcessedAt(now).
		SetLastErrorCode("").
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 0 {
		return TransitionResult{Applied: false, Stale: true}, nil
	}
	return TransitionResult{Applied: true, Stale: false}, nil
}

func (s *webhookDeliveryStore) MarkFailed(ctx context.Context, id string, claimToken string, errCode string) (TransitionResult, error) {
	now := time.Now().UTC()
	db := s.getDB()
	if db != nil {
		var query string
		var args []any
		if s.driver != nil && s.driver.Dialect() == dialect.Postgres {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'failed', claim_token = '', last_error_code = $1, error_code = $1, processed_at = $2 WHERE id = $3 AND claim_token = $4 AND status = 'processing'`
				args = []any{errCode, now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'failed', claim_token = '', last_error_code = $1, error_code = $1, processed_at = $2 WHERE id = $3 AND (claim_token = '' OR claim_token IS NULL) AND (status = 'accepted' OR status = 'processing')`
				args = []any{errCode, now, id}
			}
		} else {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'failed', claim_token = '', last_error_code = ?, error_code = ?, processed_at = ? WHERE id = ? AND claim_token = ? AND status = 'processing'`
				args = []any{errCode, errCode, now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'failed', claim_token = '', last_error_code = ?, error_code = ?, processed_at = ? WHERE id = ? AND (claim_token = '' OR claim_token IS NULL) AND (status = 'accepted' OR status = 'processing')`
				args = []any{errCode, errCode, now, id}
			}
		}
		res, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		if rows == 0 {
			return TransitionResult{Applied: false, Stale: true}, nil
		}
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	q := s.client.WebhookDelivery.Update().Where(webhookdelivery.IDEQ(id))
	if claimToken != "" {
		q = q.Where(webhookdelivery.ClaimTokenEQ(claimToken), webhookdelivery.StatusEQ(DeliveryProcessing))
	} else {
		q = q.Where(
			webhookdelivery.ClaimTokenEQ(""),
			webhookdelivery.Or(
				webhookdelivery.StatusEQ(DeliveryAccepted),
				webhookdelivery.StatusEQ(DeliveryProcessing),
			),
		)
	}
	n, err := q.
		SetStatus(DeliveryFailed).
		SetClaimToken("").
		SetLastErrorCode(errCode).
		SetErrorCode(errCode).
		SetProcessedAt(now).
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 0 {
		return TransitionResult{Applied: false, Stale: true}, nil
	}
	return TransitionResult{Applied: true, Stale: false}, nil
}

func (s *webhookDeliveryStore) MarkDeadLetter(ctx context.Context, id string, claimToken string, reason string) (TransitionResult, error) {
	now := time.Now().UTC()
	db := s.getDB()
	if db != nil {
		var query string
		var args []any
		if s.driver != nil && s.driver.Dialect() == dialect.Postgres {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'dead_letter', claim_token = '', last_error_code = $1, error_code = $1, processed_at = $2 WHERE id = $3 AND claim_token = $4 AND (status = 'accepted' OR status = 'processing')`
				args = []any{reason, now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'dead_letter', claim_token = '', last_error_code = $1, error_code = $1, processed_at = $2 WHERE id = $3 AND (status = 'accepted' OR status = 'processing')`
				args = []any{reason, now, id}
			}
		} else {
			if claimToken != "" {
				query = `UPDATE webhook_deliveries SET status = 'dead_letter', claim_token = '', last_error_code = ?, error_code = ?, processed_at = ? WHERE id = ? AND claim_token = ? AND (status = 'accepted' OR status = 'processing')`
				args = []any{reason, reason, now, id, claimToken}
			} else {
				query = `UPDATE webhook_deliveries SET status = 'dead_letter', claim_token = '', last_error_code = ?, error_code = ?, processed_at = ? WHERE id = ? AND (status = 'accepted' OR status = 'processing')`
				args = []any{reason, reason, now, id}
			}
		}
		res, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return TransitionResult{}, mapStoreError(err)
		}
		if rows == 0 {
			return TransitionResult{Applied: false, Stale: true}, nil
		}
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	q := s.client.WebhookDelivery.Update().Where(
		webhookdelivery.IDEQ(id),
		webhookdelivery.Or(
			webhookdelivery.StatusEQ(DeliveryAccepted),
			webhookdelivery.StatusEQ(DeliveryProcessing),
		),
	)
	if claimToken != "" {
		q = q.Where(webhookdelivery.ClaimTokenEQ(claimToken))
	}
	n, err := q.
		SetStatus(DeliveryDeadLetter).
		SetClaimToken("").
		SetLastErrorCode(reason).
		SetErrorCode(reason).
		SetProcessedAt(now).
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 0 {
		return TransitionResult{Applied: false, Stale: true}, nil
	}
	return TransitionResult{Applied: true, Stale: false}, nil
}

// retentionBatchSize 单批物理删除条数上限，避免超大事务长期独占写锁造成 busy_timeout。
const retentionBatchSize = 1000

func (s *webhookDeliveryStore) DehydrateWebhookPayloads(ctx context.Context, cutoff time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	cutoffUTC := cutoff.UTC()
	ids, err := s.client.WebhookDelivery.Query().
		Where(
			webhookdelivery.StatusEQ(DeliveryProcessed),
			webhookdelivery.ProcessedAtLTE(cutoffUTC),
			webhookdelivery.PayloadNotNil(),
		).
		Order(entclient.Asc(webhookdelivery.FieldReceivedAt)).
		Limit(batchSize).
		Select(webhookdelivery.FieldID).
		Strings(ctx)
	if err != nil {
		return 0, mapStoreError(err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.client.WebhookDelivery.Update().
		Where(webhookdelivery.IDIn(ids...)).
		ClearPayload().
		Save(ctx)
	if err != nil {
		return 0, mapStoreError(err)
	}
	return n, nil
}

func (s *webhookDeliveryStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	cutoffUTC := cutoff.UTC()
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		ids, err := s.client.WebhookDelivery.Query().
			Where(webhookdelivery.ReceivedAtLT(cutoffUTC)).
			Limit(retentionBatchSize).
			Select(webhookdelivery.FieldID).
			Strings(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		if len(ids) == 0 {
			break
		}
		n, err := s.client.WebhookDelivery.Delete().
			Where(webhookdelivery.IDIn(ids...)).
			Exec(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		total += n
		if len(ids) < retentionBatchSize {
			break
		}
		runtime.Gosched()
	}
	return total, nil
}

func webhookDeliveryFromEntity(e *entclient.WebhookDelivery) WebhookDelivery {
	return WebhookDelivery{
		ID: e.ID, DeliveryID: e.DeliveryID, EventType: e.EventType, Action: e.Action,
		RepositoryFullName: e.RepositoryFullName, Status: e.Status, ErrorCode: e.ErrorCode,
		Payload: e.Payload, ReceivedAt: e.ReceivedAt, ProcessedAt: e.ProcessedAt,
		ClaimToken: e.ClaimToken, ClaimVersion: e.ClaimVersion, ClaimedBy: e.ClaimedBy,
		ClaimedUntil: e.ClaimedUntil, AttemptCount: e.AttemptCount, LastErrorCode: e.LastErrorCode,
	}
}

// --- work items ---

type workItemStore struct {
	client *entclient.Client
	// idsCache 由 storeImpl 共享（见 repoIDSets）；测试直构时可为 nil。
	idsCache *ttlValueCache[cachedRepoIDs]
}

func (s *workItemStore) GetByRepoNumber(ctx context.Context, repoID string, number int) (WorkItem, error) {
	entity, err := s.client.WorkItem.Query().
		Where(workitem.RepositoryIDEQ(repoID), workitem.NumberEQ(number)).
		Only(ctx)
	if err != nil {
		return WorkItem{}, mapStoreError(err)
	}
	return workItemFromEntity(entity), nil
}

// UpsertIfNewer 按 source_updated_at / state_hash 防止陈旧回滚。返回 updated=是否写入。
// known 为调用方已持有的旧行（如对账 PR enrich 已查）：非 nil 且 ID 非空时直接复用，
// 避免同一行在同一调用内被二次 SELECT；known.ID 为空按未找到处理（走创建分支）。
func (s *workItemStore) UpsertIfNewer(ctx context.Context, in WorkItem, known *WorkItem) (WorkItem, bool, error) {
	now := time.Now().UTC()
	var existing WorkItem
	var err error
	if known != nil {
		existing = *known
		if known.ID == "" {
			err = ErrNotFound
		}
	} else {
		existing, err = s.GetByRepoNumber(ctx, in.RepositoryID, in.Number)
	}
	if err == nil {
		if in.SourceUpdatedAt.Before(existing.SourceUpdatedAt) {
			return existing, false, nil
		}
		if in.SourceUpdatedAt.Equal(existing.SourceUpdatedAt) && in.StateHash == existing.StateHash {
			return existing, false, nil
		}
		// 状态机校验：已处于 closed 终态的工作项，若入参试图将其重置为 open，但其更新时间未严格晚于 existing.SourceUpdatedAt，则拒绝状态倒流。
		if existing.State == "closed" && in.State == "open" && !in.SourceUpdatedAt.After(existing.SourceUpdatedAt) {
			return existing, false, nil
		}
		entity, err := s.client.WorkItem.UpdateOneID(existing.ID).
			SetKind(in.Kind).
			SetState(in.State).
			SetTitle(in.Title).
			SetAuthor(in.Author).
			SetAuthorIsBot(in.AuthorIsBot).
			SetLabelsJSON(in.LabelsJSON).
			SetAssigneesJSON(in.AssigneesJSON).
			SetMilestone(in.Milestone).
			SetDraft(in.Draft).
			// 已合并 PR 不会被后续事件回退为未合并。
			SetMerged(in.Merged || existing.Merged).
			SetHTMLURL(in.HTMLURL).
			SetSourceUpdatedAt(in.SourceUpdatedAt.UTC()).
			SetStateHash(in.StateHash).
			SetReviewState(in.ReviewState).
			SetReviewDecision(in.ReviewDecision).
			SetReviewers(in.Reviewers).
			SetCheckStatus(in.CheckStatus).
			SetCheckConclusion(in.CheckConclusion).
			SetChecksTotal(in.ChecksTotal).
			SetChecksPassed(in.ChecksPassed).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return WorkItem{}, false, mapStoreError(err)
		}
		return workItemFromEntity(entity), true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return WorkItem{}, false, err
	}
	if in.ID == "" {
		in.ID = newID()
	}
	entity, err := s.client.WorkItem.Create().
		SetID(in.ID).
		SetRepositoryID(in.RepositoryID).
		SetNumber(in.Number).
		SetKind(in.Kind).
		SetState(in.State).
		SetTitle(in.Title).
		SetAuthor(in.Author).
		SetAuthorIsBot(in.AuthorIsBot).
		SetLabelsJSON(in.LabelsJSON).
		SetAssigneesJSON(in.AssigneesJSON).
		SetMilestone(in.Milestone).
		SetDraft(in.Draft).
		SetMerged(in.Merged).
		SetHTMLURL(in.HTMLURL).
		SetSourceUpdatedAt(in.SourceUpdatedAt.UTC()).
		SetStateHash(in.StateHash).
		SetReviewState(in.ReviewState).
		SetReviewDecision(in.ReviewDecision).
		SetReviewers(in.Reviewers).
		SetCheckStatus(in.CheckStatus).
		SetCheckConclusion(in.CheckConclusion).
		SetChecksTotal(in.ChecksTotal).
		SetChecksPassed(in.ChecksPassed).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return WorkItem{}, false, mapStoreError(err)
	}
	return workItemFromEntity(entity), true, nil
}

// repoFullNameByID 批量查询仓库全名，返回 id→full_name 映射。
// 已改为从仓库 ID 集合缓存解析（见 repositoryNames）：列表页每页一次额外查询被
// 一次全表扫描（id/is_archived/full_name）取代，且该扫描本来就已发生。
func repositoryNames(ctx context.Context, client *entclient.Client, ids []string, cache *ttlValueCache[cachedRepoIDs]) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	// 缓存内已带全名（TTL 与列表查询同源，写入路径即时失效）：直接过滤构建，
	// 不产生额外查询；缓存未命中时回退按需直查。
	sets, setsErr := repoIDSets(ctx, client, cache)
	if setsErr == nil && sets.names != nil {
		out := make(map[string]string, len(ids))
		for _, id := range ids {
			if n, ok := sets.names[id]; ok {
				out[id] = n
			}
		}
		return out
	}
	seen := make(map[string]struct{}, len(ids))
	unique := ids[:0]
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	rows, err := client.Repository.Query().Where(repository.IDIn(unique...)).All(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.FullName
	}
	return out
}

// activeRepositoryIDs 返回未归档仓库的 ID 列表。
// repoIDSets 返回活跃/归档仓库 ID 集合：一次扫描分出两个集合，并经短 TTL 缓存
// 供列表/计数类查询复用（仓集合变化频率远低于列表查询频率）。缓存为 nil（测试直构
// 存储访问器）时等价于每次直查；repositoryStore 的写路径会使缓存即时失效。
// 事件类查询采用「排除归档」而非「仅含活跃」：repository_id 为空的孤儿事件保持可见，
// 避免历史数据因仓库行缺失而凭空消失。
func repoIDSets(ctx context.Context, client *entclient.Client, cache *ttlValueCache[cachedRepoIDs]) (cachedRepoIDs, error) {
	if sets, ok := cache.Get(); ok {
		return sets, nil
	}
	rows, err := client.Repository.Query().
		Select(repository.FieldID, repository.FieldIsArchived, repository.FieldFullName).
		All(ctx)
	if err != nil {
		return cachedRepoIDs{}, mapStoreError(err)
	}
	sets := cachedRepoIDs{names: make(map[string]string, len(rows))}
	for _, r := range rows {
		sets.names[r.ID] = r.FullName
		if r.IsArchived {
			sets.archived = append(sets.archived, r.ID)
		} else {
			sets.active = append(sets.active, r.ID)
		}
	}
	cache.Set(sets)
	return sets, nil
}

// activeRepositoryIDs 返回未归档仓库 ID 集合（缓存与排除语义见 repoIDSets）。
func activeRepositoryIDs(ctx context.Context, client *entclient.Client, cache *ttlValueCache[cachedRepoIDs]) ([]string, error) {
	sets, err := repoIDSets(ctx, client, cache)
	return sets.active, err
}

// archivedRepositoryIDs 返回已归档仓库 ID 集合（缓存与排除语义见 repoIDSets）。
func archivedRepositoryIDs(ctx context.Context, client *entclient.Client, cache *ttlValueCache[cachedRepoIDs]) ([]string, error) {
	sets, err := repoIDSets(ctx, client, cache)
	return sets.archived, err
}

func (s *workItemStore) Get(ctx context.Context, id string) (WorkItem, error) {
	entity, err := s.client.WorkItem.Get(ctx, id)
	if err != nil {
		return WorkItem{}, mapStoreError(err)
	}
	return workItemFromEntity(entity), nil
}

func (s *workItemStore) SetIgnored(ctx context.Context, id string, ignored bool) error {
	err := s.client.WorkItem.UpdateOneID(id).
		SetIgnored(ignored).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
	return mapStoreError(err)
}

// MarkMerged 仅置位 merged；GitHub 语义上已合并 PR 不会回退，允许重复调用。
func (s *workItemStore) MarkMerged(ctx context.Context, repoID string, number int) error {
	_, err := s.client.WorkItem.Update().
		Where(workitem.RepositoryIDEQ(repoID), workitem.NumberEQ(number)).
		SetMerged(true).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	return mapStoreError(err)
}

func (s *workItemStore) List(ctx context.Context, f ListFilter) ([]WorkItem, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.WorkItem.Query()
	if f.RepositoryID != "" {
		q = q.Where(workitem.RepositoryIDEQ(f.RepositoryID))
	} else if !f.IncludeArchivedRepos {
		ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
		if err != nil {
			return nil, PageResult{}, err
		}
		if len(ids) == 0 {
			return []WorkItem{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: 0}, nil
		}
		q = q.Where(workitem.RepositoryIDIn(ids...))
	}
	if f.Kind != "" {
		q = q.Where(workitem.KindEQ(f.Kind))
	}
	if f.State != "" {
		q = q.Where(workitem.StateEQ(f.State))
	}
	// PR 审核结论与检查状态在 SQL 层过滤，保证筛选结果与分页计数不被截断。
	if f.ReviewDecision != "" {
		if f.ReviewDecision == "pending" {
			// 「审核中」= 尚无审核结论（approved/changes_requested 之外的记录）。
			q = q.Where(workitem.ReviewDecisionEQ(""))
		} else {
			q = q.Where(workitem.ReviewDecisionEQ(f.ReviewDecision))
		}
	}
	if f.CheckStatus != "" {
		if f.CheckStatus == "pending" {
			// 尚无检查数据（空串）与检查进行中（pending）都视为待检查。
			q = q.Where(workitem.CheckStatusIn("", "pending"))
		} else {
			q = q.Where(workitem.CheckStatusEQ(f.CheckStatus))
		}
	}
	if f.Author != "" {
		q = q.Where(workitem.AuthorEqualFold(f.Author))
	}
	if f.AuthorIsBot != nil {
		q = q.Where(workitem.AuthorIsBotEQ(*f.AuthorIsBot))
	}
	if search := strings.TrimSpace(f.SearchText); search != "" {
		predicates := []predicate.WorkItem{
			workitem.TitleContainsFold(search),
			workitem.AuthorContainsFold(search),
		}
		if number, err := strconv.Atoi(search); err == nil {
			predicates = append(predicates, workitem.NumberEQ(number))
		}
		repositoryIDs, err := s.client.Repository.Query().
			Where(repository.Or(repository.NameContainsFold(search), repository.FullNameContainsFold(search))).
			IDs(ctx)
		if err != nil {
			return nil, PageResult{}, mapStoreError(err)
		}
		if len(repositoryIDs) > 0 {
			predicates = append(predicates, workitem.RepositoryIDIn(repositoryIDs...))
		}
		q = q.Where(workitem.Or(predicates...))
	}
	if f.OnlyIgnored {
		q = q.Where(workitem.IgnoredEQ(true))
	} else if !f.IncludeIgnored {
		q = q.Where(workitem.IgnoredEQ(false))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []WorkItem{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(workitem.FieldUpdatedAt), entclient.Asc(workitem.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]WorkItem, 0, len(rows))
	repoIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, workItemFromEntity(row))
		repoIDs = append(repoIDs, row.RepositoryID)
	}
	names := repositoryNames(ctx, s.client, repoIDs, s.idsCache)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *workItemStore) CountOpen(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.client.WorkItem.Query().
		Where(
			workitem.KindEQ(WorkItemKindIssue),
			workitem.StateEQ("open"),
			workitem.IgnoredEQ(false),
			workitem.RepositoryIDIn(ids...),
		).Count(ctx)
	return n, mapStoreError(err)
}

func workItemFromEntity(e *entclient.WorkItem) WorkItem {
	return WorkItem{
		ID: e.ID, RepositoryID: e.RepositoryID, Number: e.Number, Kind: e.Kind, State: e.State,
		Title: e.Title, Author: e.Author, AuthorIsBot: e.AuthorIsBot, LabelsJSON: e.LabelsJSON, AssigneesJSON: e.AssigneesJSON,
		Milestone: e.Milestone, Draft: e.Draft, Merged: e.Merged, HTMLURL: e.HTMLURL,
		SourceUpdatedAt: e.SourceUpdatedAt, StateHash: e.StateHash,
		ReviewState: e.ReviewState, ReviewDecision: e.ReviewDecision, Reviewers: e.Reviewers,
		CheckStatus: e.CheckStatus, CheckConclusion: e.CheckConclusion,
		ChecksTotal: e.ChecksTotal, ChecksPassed: e.ChecksPassed,
		Ignored: e.Ignored, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- workflow runs ---

type workflowRunStore struct {
	client *entclient.Client
	// idsCache 由 storeImpl 共享（见 repoIDSets）；测试直构时可为 nil。
	idsCache *ttlValueCache[cachedRepoIDs]
}

func (s *workflowRunStore) GetByRepoRunID(ctx context.Context, repoID string, runID int64) (WorkflowRun, error) {
	entity, err := s.client.WorkflowRun.Query().
		Where(workflowrun.RepositoryIDEQ(repoID), workflowrun.GithubRunIDEQ(runID)).
		Only(ctx)
	if err != nil {
		return WorkflowRun{}, mapStoreError(err)
	}
	return workflowRunFromEntity(entity), nil
}

func (s *workflowRunStore) UpsertIfNewer(ctx context.Context, in WorkflowRun) (WorkflowRun, bool, error) {
	now := time.Now().UTC()
	existing, err := s.GetByRepoRunID(ctx, in.RepositoryID, in.GitHubRunID)
	if err == nil {
		if in.RunAttempt < existing.RunAttempt {
			return existing, false, nil
		}
		if in.RunUpdatedAt.Before(existing.RunUpdatedAt) {
			return existing, false, nil
		}
		if in.RunUpdatedAt.Equal(existing.RunUpdatedAt) && in.StateHash == existing.StateHash {
			return existing, false, nil
		}
		prev := existing.Conclusion
		upd := s.client.WorkflowRun.UpdateOneID(existing.ID).
			SetGithubWorkflowID(in.GitHubWorkflowID).
			SetWorkflowName(in.WorkflowName).
			SetRunNumber(in.RunNumber).
			SetEvent(in.Event).
			SetHeadBranch(in.HeadBranch).
			SetHeadSha(in.HeadSHA).
			SetStatus(in.Status).
			SetActor(in.Actor).
			SetRunAttempt(in.RunAttempt).
			SetHTMLURL(in.HTMLURL).
			SetRunUpdatedAt(in.RunUpdatedAt.UTC()).
			SetStateHash(in.StateHash).
			SetUpdatedAt(now)
		if in.Conclusion != nil {
			upd.SetConclusion(*in.Conclusion)
			if prev != nil && *prev != *in.Conclusion {
				upd.SetPreviousConclusion(*prev)
			}
		}
		if in.RunStartedAt != nil {
			upd.SetRunStartedAt(*in.RunStartedAt)
		}
		if in.RunCompletedAt != nil {
			upd.SetRunCompletedAt(*in.RunCompletedAt)
		}
		entity, err := upd.Save(ctx)
		if err != nil {
			return WorkflowRun{}, false, mapStoreError(err)
		}
		return workflowRunFromEntity(entity), true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return WorkflowRun{}, false, err
	}
	if in.ID == "" {
		in.ID = newID()
	}
	c := s.client.WorkflowRun.Create().
		SetID(in.ID).
		SetRepositoryID(in.RepositoryID).
		SetGithubRunID(in.GitHubRunID).
		SetGithubWorkflowID(in.GitHubWorkflowID).
		SetWorkflowName(in.WorkflowName).
		SetRunNumber(in.RunNumber).
		SetEvent(in.Event).
		SetHeadBranch(in.HeadBranch).
		SetHeadSha(in.HeadSHA).
		SetStatus(in.Status).
		SetActor(in.Actor).
		SetRunAttempt(in.RunAttempt).
		SetHTMLURL(in.HTMLURL).
		SetRunUpdatedAt(in.RunUpdatedAt.UTC()).
		SetStateHash(in.StateHash).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if in.Conclusion != nil {
		c.SetConclusion(*in.Conclusion)
	}
	if in.RunStartedAt != nil {
		c.SetRunStartedAt(*in.RunStartedAt)
	}
	if in.RunCompletedAt != nil {
		c.SetRunCompletedAt(*in.RunCompletedAt)
	}
	entity, err := c.Save(ctx)
	if err != nil {
		return WorkflowRun{}, false, mapStoreError(err)
	}
	return workflowRunFromEntity(entity), true, nil
}

func (s *workflowRunStore) LatestCompleted(ctx context.Context, repoID string, workflowID int64, branch string) (WorkflowRun, error) {
	entity, err := s.client.WorkflowRun.Query().
		Where(
			workflowrun.RepositoryIDEQ(repoID),
			workflowrun.GithubWorkflowIDEQ(workflowID),
			workflowrun.HeadBranchEQ(branch),
			workflowrun.ConclusionNotNil(),
		).
		Order(entclient.Desc(workflowrun.FieldRunUpdatedAt)).
		First(ctx)
	if err != nil {
		return WorkflowRun{}, mapStoreError(err)
	}
	return workflowRunFromEntity(entity), nil
}

func (s *workflowRunStore) Get(ctx context.Context, id string) (WorkflowRun, error) {
	entity, err := s.client.WorkflowRun.Get(ctx, id)
	if err != nil {
		return WorkflowRun{}, mapStoreError(err)
	}
	return workflowRunFromEntity(entity), nil
}

func (s *workflowRunStore) SetIgnored(ctx context.Context, id string, ignored bool) error {
	err := s.client.WorkflowRun.UpdateOneID(id).
		SetIgnored(ignored).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
	return mapStoreError(err)
}

func (s *workflowRunStore) List(ctx context.Context, f ListFilter) ([]WorkflowRun, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.WorkflowRun.Query()
	if f.RepositoryID != "" {
		q = q.Where(workflowrun.RepositoryIDEQ(f.RepositoryID))
	} else if !f.IncludeArchivedRepos {
		ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
		if err != nil {
			return nil, PageResult{}, err
		}
		if len(ids) == 0 {
			return []WorkflowRun{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: 0}, nil
		}
		q = q.Where(workflowrun.RepositoryIDIn(ids...))
	}
	if f.Status != "" {
		q = q.Where(workflowrun.ConclusionEQ(f.Status))
	}
	if f.OnlyIgnored {
		q = q.Where(workflowrun.IgnoredEQ(true))
	} else if !f.IncludeIgnored {
		q = q.Where(workflowrun.IgnoredEQ(false))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []WorkflowRun{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(workflowrun.FieldRunUpdatedAt), entclient.Asc(workflowrun.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]WorkflowRun, 0, len(rows))
	repoIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowRunFromEntity(row))
		repoIDs = append(repoIDs, row.RepositoryID)
	}
	names := repositoryNames(ctx, s.client, repoIDs, s.idsCache)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *workflowRunStore) CountFailed(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.client.WorkflowRun.Query().Where(
		workflowrun.ConclusionIn(FailedConclusions()...),
		workflowrun.IgnoredEQ(false),
		workflowrun.RepositoryIDIn(ids...),
	).Count(ctx)
	return n, mapStoreError(err)
}

func workflowRunFromEntity(e *entclient.WorkflowRun) WorkflowRun {
	return WorkflowRun{
		ID: e.ID, RepositoryID: e.RepositoryID, GitHubRunID: e.GithubRunID, GitHubWorkflowID: e.GithubWorkflowID,
		WorkflowName: e.WorkflowName, RunNumber: e.RunNumber, Event: e.Event, HeadBranch: e.HeadBranch,
		HeadSHA: e.HeadSha, Status: e.Status, Conclusion: e.Conclusion, PreviousConclusion: e.PreviousConclusion,
		Actor: e.Actor, RunAttempt: e.RunAttempt, HTMLURL: e.HTMLURL, RunStartedAt: e.RunStartedAt,
		RunUpdatedAt: e.RunUpdatedAt, RunCompletedAt: e.RunCompletedAt, StateHash: e.StateHash,
		Ignored: e.Ignored, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- security alerts ---

type securityAlertStore struct {
	client *entclient.Client
	// idsCache 由 storeImpl 共享（见 repoIDSets）；测试直构时可为 nil。
	idsCache *ttlValueCache[cachedRepoIDs]
}

func (s *securityAlertStore) GetByIdentity(ctx context.Context, repoID, kind string, number int) (SecurityAlert, error) {
	entity, err := s.client.SecurityAlert.Query().
		Where(
			securityalert.RepositoryIDEQ(repoID),
			securityalert.AlertKindEQ(kind),
			securityalert.AlertNumberEQ(number),
		).Only(ctx)
	if err != nil {
		return SecurityAlert{}, mapStoreError(err)
	}
	return securityAlertFromEntity(entity), nil
}

func (s *securityAlertStore) UpsertIfNewer(ctx context.Context, in SecurityAlert) (SecurityAlert, bool, error) {
	now := time.Now().UTC()
	existing, err := s.GetByIdentity(ctx, in.RepositoryID, in.AlertKind, in.AlertNumber)
	if err == nil {
		if in.SourceUpdatedAt.Before(existing.SourceUpdatedAt) {
			return existing, false, nil
		}
		if in.SourceUpdatedAt.Equal(existing.SourceUpdatedAt) && in.StateHash == existing.StateHash {
			return existing, false, nil
		}
		entity, err := s.client.SecurityAlert.UpdateOneID(existing.ID).
			SetState(in.State).
			SetSeverity(in.Severity).
			SetRuleOrDependency(in.RuleOrDependency).
			SetDismissedReason(in.DismissedReason).
			SetHTMLURL(in.HTMLURL).
			SetSourceUpdatedAt(in.SourceUpdatedAt.UTC()).
			SetStateHash(in.StateHash).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return SecurityAlert{}, false, mapStoreError(err)
		}
		return securityAlertFromEntity(entity), true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SecurityAlert{}, false, err
	}
	if in.ID == "" {
		in.ID = newID()
	}
	entity, err := s.client.SecurityAlert.Create().
		SetID(in.ID).
		SetRepositoryID(in.RepositoryID).
		SetAlertKind(in.AlertKind).
		SetAlertNumber(in.AlertNumber).
		SetState(in.State).
		SetSeverity(in.Severity).
		SetRuleOrDependency(in.RuleOrDependency).
		SetDismissedReason(in.DismissedReason).
		SetHTMLURL(in.HTMLURL).
		SetSourceUpdatedAt(in.SourceUpdatedAt.UTC()).
		SetStateHash(in.StateHash).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return SecurityAlert{}, false, mapStoreError(err)
	}
	return securityAlertFromEntity(entity), true, nil
}

func (s *securityAlertStore) Get(ctx context.Context, id string) (SecurityAlert, error) {
	entity, err := s.client.SecurityAlert.Get(ctx, id)
	if err != nil {
		return SecurityAlert{}, mapStoreError(err)
	}
	return securityAlertFromEntity(entity), nil
}

func (s *securityAlertStore) SetIgnored(ctx context.Context, id string, ignored bool) error {
	err := s.client.SecurityAlert.UpdateOneID(id).
		SetIgnored(ignored).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)
	return mapStoreError(err)
}

func (s *securityAlertStore) List(ctx context.Context, f ListFilter) ([]SecurityAlert, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.SecurityAlert.Query()
	if f.RepositoryID != "" {
		q = q.Where(securityalert.RepositoryIDEQ(f.RepositoryID))
	} else if !f.IncludeArchivedRepos {
		ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
		if err != nil {
			return nil, PageResult{}, err
		}
		if len(ids) == 0 {
			return []SecurityAlert{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: 0}, nil
		}
		q = q.Where(securityalert.RepositoryIDIn(ids...))
	}
	if f.Kind != "" {
		q = q.Where(securityalert.AlertKindEQ(f.Kind))
	}
	if f.State != "" {
		q = q.Where(securityalert.StateEQ(f.State))
	}
	if f.OnlyIgnored {
		q = q.Where(securityalert.IgnoredEQ(true))
	} else if !f.IncludeIgnored {
		q = q.Where(securityalert.IgnoredEQ(false))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []SecurityAlert{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(securityalert.FieldSourceUpdatedAt), entclient.Asc(securityalert.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]SecurityAlert, 0, len(rows))
	repoIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, securityAlertFromEntity(row))
		repoIDs = append(repoIDs, row.RepositoryID)
	}
	names := repositoryNames(ctx, s.client, repoIDs, s.idsCache)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *securityAlertStore) CountOpen(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client, s.idsCache)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	n, err := s.client.SecurityAlert.Query().Where(
		securityalert.StateIn("open", "reopened"),
		securityalert.IgnoredEQ(false),
		securityalert.RepositoryIDIn(ids...),
	).Count(ctx)
	return n, mapStoreError(err)
}

// ListByRepoKind 返回某仓库某类型全量本地告警（不分状态、按编号升序）。
// 对账差集用：告警数量有界（GitHub 侧上限数百条），无需分页。
func (s *securityAlertStore) ListByRepoKind(ctx context.Context, repoID, kind string) ([]SecurityAlert, error) {
	rows, err := s.client.SecurityAlert.Query().
		Where(
			securityalert.RepositoryIDEQ(repoID),
			securityalert.AlertKindEQ(kind),
		).
		Order(entclient.Asc(securityalert.FieldAlertNumber)).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]SecurityAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, securityAlertFromEntity(row))
	}
	return out, nil
}

func securityAlertFromEntity(e *entclient.SecurityAlert) SecurityAlert {
	return SecurityAlert{
		ID: e.ID, RepositoryID: e.RepositoryID, AlertKind: e.AlertKind, AlertNumber: e.AlertNumber,
		State: e.State, Severity: e.Severity, RuleOrDependency: e.RuleOrDependency,
		DismissedReason: e.DismissedReason, HTMLURL: e.HTMLURL, SourceUpdatedAt: e.SourceUpdatedAt,
		StateHash: e.StateHash, Ignored: e.Ignored, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- events ---

type eventStore struct {
	client *entclient.Client
	// idsCache 由 storeImpl 共享（见 repoIDSets）；测试直构时可为 nil。
	idsCache *ttlValueCache[cachedRepoIDs]
}

func (s *eventStore) Create(ctx context.Context, in Event) (Event, error) {
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	c := s.client.Event.Create().
		SetID(in.ID).
		SetSource(in.Source).
		SetKind(in.Kind).
		SetAction(in.Action).
		SetTitle(in.Title).
		SetSeverity(in.Severity).
		SetActor(in.Actor).
		SetSenderIsBot(in.SenderIsBot).
		SetWorkflowConclusion(in.WorkflowConclusion).
		SetOccurredAt(in.OccurredAt.UTC()).
		SetHTMLURL(in.HTMLURL).
		SetPayloadSummary(in.PayloadSummary).
		SetSuppressNotification(in.SuppressNotification).
		SetDedupeFingerprint(in.DedupeFingerprint).
		SetStateHash(in.StateHash).
		SetCreatedAt(in.CreatedAt.UTC())
	if in.RepositoryID != nil {
		c.SetRepositoryID(*in.RepositoryID)
	}
	if in.SubjectNumber != nil {
		c.SetSubjectNumber(*in.SubjectNumber)
	}
	if in.WorkflowRunID != nil {
		c.SetWorkflowRunID(*in.WorkflowRunID)
	}
	if in.SourceUpdatedAt != nil {
		c.SetSourceUpdatedAt(in.SourceUpdatedAt.UTC())
	}
	entity, err := c.Save(ctx)
	if err != nil {
		return Event{}, mapStoreError(err)
	}
	return eventFromEntity(entity), nil
}

func (s *eventStore) GetByFingerprint(ctx context.Context, fp string) (Event, error) {
	entity, err := s.client.Event.Query().Where(event.DedupeFingerprintEQ(fp)).Only(ctx)
	if err != nil {
		return Event{}, mapStoreError(err)
	}
	return eventFromEntity(entity), nil
}

func (s *eventStore) List(ctx context.Context, f ListFilter) ([]Event, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.Event.Query()
	if f.RepositoryID != "" {
		q = q.Where(event.RepositoryIDEQ(f.RepositoryID))
	} else if !f.IncludeArchivedRepos {
		// 事件列表默认排除已归档仓库（与其他资源列表同一约定）；
		// 显式按仓库过滤（RepositoryID）或 IncludeArchivedRepos=true 时不受限。
		ids, err := archivedRepositoryIDs(ctx, s.client, s.idsCache)
		if err != nil {
			return nil, PageResult{}, err
		}
		if len(ids) > 0 {
			q = q.Where(event.Or(event.RepositoryIDIsNil(), event.RepositoryIDNotIn(ids...)))
		}
	}
	if f.Kind != "" {
		q = q.Where(event.KindEQ(f.Kind))
	}
	if f.SubjectNumber != nil {
		q = q.Where(event.SubjectNumberEQ(*f.SubjectNumber))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []Event{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(event.FieldOccurredAt), entclient.Asc(event.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, eventFromEntity(row))
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

// CountSince 统计指定时间之后的事件数。与 ListSince 同一归档约定：
// 已归档仓库的事件不计入，保证仪表盘「24h 事件」与其它指标口径一致。
func (s *eventStore) CountSince(ctx context.Context, since time.Time) (int, error) {
	ids, err := archivedRepositoryIDs(ctx, s.client, s.idsCache)
	if err != nil {
		return 0, mapStoreError(err)
	}
	return countEventsSince(ctx, s.client, since, ids)
}

// countEventsSince 带预取归档清单的计数实现：调用方已持有归档清单（如 Dashboard）时
// 免于在同一请求内重复扫描仓库表。
func countEventsSince(ctx context.Context, client *entclient.Client, since time.Time, archivedIDs []string) (int, error) {
	q := client.Event.Query().Where(event.OccurredAtGTE(since.UTC()))
	if len(archivedIDs) > 0 {
		q = q.Where(event.Or(event.RepositoryIDIsNil(), event.RepositoryIDNotIn(archivedIDs...)))
	}
	n, err := q.Count(ctx)
	return n, mapStoreError(err)
}

func (s *eventStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	cutoffUTC := cutoff.UTC()
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		ids, err := s.client.Event.Query().
			Where(event.CreatedAtLT(cutoffUTC)).
			Limit(retentionBatchSize).
			Select(event.FieldID).
			Strings(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		if len(ids) == 0 {
			break
		}
		n, err := s.client.Event.Delete().
			Where(event.IDIn(ids...)).
			Exec(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		total += n
		if len(ids) < retentionBatchSize {
			break
		}
		runtime.Gosched()
	}
	return total, nil
}

// ListSince 每日摘要专用：只取未被抑制（非基线/归档期间产生）且非已归档仓库的事件。
func (s *eventStore) ListSince(ctx context.Context, since time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	q := s.client.Event.Query().
		Where(event.OccurredAtGTE(since.UTC())).
		Where(event.SuppressNotificationEQ(false))
	ids, err := archivedRepositoryIDs(ctx, s.client, s.idsCache)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		q = q.Where(event.Or(event.RepositoryIDIsNil(), event.RepositoryIDNotIn(ids...)))
	}
	entList, err := q.
		Order(entclient.Desc(event.FieldOccurredAt), entclient.Asc(event.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]Event, len(entList))
	for i, e := range entList {
		out[i] = eventFromEntity(e)
	}
	return out, nil
}

func eventFromEntity(e *entclient.Event) Event {
	return Event{
		ID: e.ID, Source: e.Source, Kind: e.Kind, Action: e.Action, RepositoryID: e.RepositoryID,
		SubjectNumber: e.SubjectNumber, Title: e.Title, Severity: e.Severity, Actor: e.Actor,
		SenderIsBot:   e.SenderIsBot,
		WorkflowRunID: e.WorkflowRunID, WorkflowConclusion: e.WorkflowConclusion, OccurredAt: e.OccurredAt,
		SourceUpdatedAt: e.SourceUpdatedAt, HTMLURL: e.HTMLURL, PayloadSummary: e.PayloadSummary,
		SuppressNotification: e.SuppressNotification, DedupeFingerprint: e.DedupeFingerprint,
		StateHash: e.StateHash, CreatedAt: e.CreatedAt,
	}
}

// --- channels ---

type channelStore struct {
	client *entclient.Client
	// listCache 与 storeImpl 共享（见 storeImpl.channelsCache）；测试直构时可为 nil。
	listCache *ttlValueCache[[]NotificationChannel]
}

// Upsert 原样落库渠道配置：日/周/月报告子开关不做隐式回填。
// 由写入方显式给出取值（HTTP 处理层为新渠道补默认值、环境种子渠道显式开启），
// 否则「父开关开启 + 三个子开关全部关闭」这类合法状态会被静默改写。
func (s *channelStore) Upsert(ctx context.Context, in NotificationChannel) (NotificationChannel, error) {
	now := time.Now().UTC()
	if in.ID != "" {
		if _, err := s.client.NotificationChannel.Get(ctx, in.ID); err == nil {
			entity, err := s.client.NotificationChannel.UpdateOneID(in.ID).
				SetName(in.Name).
				SetEnabled(in.Enabled).
				SetTarget(in.Target).
				SetSecretEnvelope(in.SecretEnvelope).
				SetAllowPrivate(in.AllowPrivate).
				SetEventKinds(in.EventKinds).
				SetDigestEnabled(in.DigestEnabled).
				SetReceiveDailyDigest(in.ReceiveDailyDigest).
				SetReceiveWeeklyReport(in.ReceiveWeeklyReport).
				SetReceiveMonthlyReport(in.ReceiveMonthlyReport).
				SetQuietHoursEnabled(in.QuietHoursEnabled).
				SetQuietHoursStart(in.QuietHoursStart).
				SetQuietHoursEnd(in.QuietHoursEnd).
				SetQuietHoursTz(in.QuietHoursTZ).
				SetIgnoreBots(in.IgnoreBots).
				SetRepoPattern(in.RepoPattern).
				SetBranchFilter(in.BranchFilter).
				SetMinSeverity(in.MinSeverity).
				SetUpdatedAt(now).
				Save(ctx)
			if err != nil {
				return NotificationChannel{}, mapStoreError(err)
			}
			s.listCache.Invalidate()
			return channelFromEntity(entity), nil
		}
	}
	if in.ID == "" {
		in.ID = newID()
	}
	entity, err := s.client.NotificationChannel.Create().
		SetID(in.ID).
		SetChannelType(in.ChannelType).
		SetName(in.Name).
		SetEnabled(in.Enabled).
		SetTarget(in.Target).
		SetSecretEnvelope(in.SecretEnvelope).
		SetAllowPrivate(in.AllowPrivate).
		SetEventKinds(in.EventKinds).
		SetDigestEnabled(in.DigestEnabled).
		SetReceiveDailyDigest(in.ReceiveDailyDigest).
		SetReceiveWeeklyReport(in.ReceiveWeeklyReport).
		SetReceiveMonthlyReport(in.ReceiveMonthlyReport).
		SetQuietHoursEnabled(in.QuietHoursEnabled).
		SetQuietHoursStart(in.QuietHoursStart).
		SetQuietHoursEnd(in.QuietHoursEnd).
		SetQuietHoursTz(in.QuietHoursTZ).
		SetIgnoreBots(in.IgnoreBots).
		SetRepoPattern(in.RepoPattern).
		SetBranchFilter(in.BranchFilter).
		SetMinSeverity(in.MinSeverity).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return NotificationChannel{}, mapStoreError(err)
	}
	s.listCache.Invalidate()
	return channelFromEntity(entity), nil
}

func (s *channelStore) Get(ctx context.Context, id string) (NotificationChannel, error) {
	entity, err := s.client.NotificationChannel.Get(ctx, id)
	if err != nil {
		return NotificationChannel{}, mapStoreError(err)
	}
	return channelFromEntity(entity), nil
}

func (s *channelStore) GetEnabledByType(ctx context.Context, channelType string) (NotificationChannel, error) {
	entity, err := s.client.NotificationChannel.Query().
		Where(notificationchannel.ChannelTypeEQ(channelType), notificationchannel.EnabledEQ(true)).
		Only(ctx)
	if err != nil {
		return NotificationChannel{}, mapStoreError(err)
	}
	return channelFromEntity(entity), nil
}

func (s *channelStore) GetByType(ctx context.Context, channelType string) (NotificationChannel, error) {
	entity, err := s.client.NotificationChannel.Query().
		Where(notificationchannel.ChannelTypeEQ(channelType)).
		Order(entclient.Desc(notificationchannel.FieldEnabled), entclient.Desc(notificationchannel.FieldUpdatedAt)).
		First(ctx)
	if err != nil {
		return NotificationChannel{}, mapStoreError(err)
	}
	return channelFromEntity(entity), nil
}

func (s *channelStore) List(ctx context.Context) ([]NotificationChannel, error) {
	if cached, ok := s.listCache.Get(); ok {
		return cached, nil
	}
	rows, err := s.client.NotificationChannel.Query().All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]NotificationChannel, 0, len(rows))
	for _, row := range rows {
		out = append(out, channelFromEntity(row))
	}
	s.listCache.Set(out)
	return out, nil
}

func (s *channelStore) DisableOthersOfType(ctx context.Context, channelType, keepID string) error {
	_, err := s.client.NotificationChannel.Update().
		Where(
			notificationchannel.ChannelTypeEQ(channelType),
			notificationchannel.IDNEQ(keepID),
			notificationchannel.EnabledEQ(true),
		).
		SetEnabled(false).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err == nil {
		s.listCache.Invalidate()
	}
	return mapStoreError(err)
}

func (s *channelStore) Delete(ctx context.Context, id string) error {
	err := s.client.NotificationChannel.DeleteOneID(id).Exec(ctx)
	if err == nil {
		s.listCache.Invalidate()
	}
	return mapStoreError(err)
}

func (s *channelStore) ToggleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.client.NotificationChannel.UpdateOneID(id).
		SetEnabled(enabled).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	if err == nil {
		s.listCache.Invalidate()
	}
	return mapStoreError(err)
}

func channelFromEntity(e *entclient.NotificationChannel) NotificationChannel {
	return NotificationChannel{
		ID: e.ID, ChannelType: e.ChannelType, Name: e.Name, Enabled: e.Enabled,
		Target: e.Target, SecretEnvelope: e.SecretEnvelope, AllowPrivate: e.AllowPrivate,
		EventKinds: e.EventKinds, DigestEnabled: e.DigestEnabled,
		ReceiveDailyDigest: e.ReceiveDailyDigest, ReceiveWeeklyReport: e.ReceiveWeeklyReport, ReceiveMonthlyReport: e.ReceiveMonthlyReport,
		QuietHoursEnabled: e.QuietHoursEnabled, QuietHoursStart: e.QuietHoursStart, QuietHoursEnd: e.QuietHoursEnd, QuietHoursTZ: e.QuietHoursTz,
		IgnoreBots:  e.IgnoreBots,
		RepoPattern: e.RepoPattern, BranchFilter: e.BranchFilter, MinSeverity: e.MinSeverity,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- outbox ---

// outboxHTMLURLKey 是 HTMLURL 在 BodyJSON 中的存储键。
// HTMLURL 为虚拟字段（未建列）：写入时合并进 BodyJSON，读取时反解，存取统一走下面两个 helper。
const outboxHTMLURLKey = "html_url"

func withOutboxHTMLURL(bodyJSON map[string]any, htmlURL string) map[string]any {
	if htmlURL == "" {
		return bodyJSON
	}
	if bodyJSON == nil {
		bodyJSON = make(map[string]any)
	}
	bodyJSON[outboxHTMLURLKey] = htmlURL
	return bodyJSON
}

func outboxHTMLURLOf(bodyJSON map[string]any) string {
	if v, ok := bodyJSON[outboxHTMLURLKey].(string); ok {
		return v
	}
	return ""
}

// releaseRepositoryOf 从通知载荷派生 Release 类仓库名，供冗余列写入；
// 非 Release 类别或缺失仓库名时返回空串（该类通知不参与按仓库取消）。
func releaseRepositoryOf(bodyJSON map[string]any) string {
	if bodyJSON == nil {
		return ""
	}
	if kind, _ := bodyJSON["kind"].(string); kind != ReleaseKind {
		return ""
	}
	repo, _ := bodyJSON["repository"].(string)
	return strings.TrimSpace(repo)
}

type outboxStore struct {
	client *entclient.Client
	driver dialect.Driver
}

func (s *outboxStore) getDB() *sql.DB {
	if s.driver == nil {
		return nil
	}
	if sqlDriver, ok := s.driver.(*entsql.Driver); ok {
		return sqlDriver.DB()
	}
	return nil
}

func (s *outboxStore) Create(ctx context.Context, in NotificationOutbox) (NotificationOutbox, error) {
	if in.ID == "" {
		in.ID = newID()
	}
	now := time.Now().UTC()
	if in.NextAttemptAt.IsZero() {
		in.NextAttemptAt = now
	}
	if in.Status == "" {
		in.Status = OutboxPending
	}
	if in.ParseMode == "" {
		in.ParseMode = "HTML"
	}
	bodyJSON := withOutboxHTMLURL(in.BodyJSON, in.HTMLURL)
	c := s.client.NotificationOutbox.Create().
		SetID(in.ID).
		SetChannelID(in.ChannelID).
		SetAggregateKey(in.AggregateKey).
		SetIdempotencyKey(in.IdempotencyKey).
		SetStatus(in.Status).
		SetAttemptCount(in.AttemptCount).
		SetNextAttemptAt(in.NextAttemptAt.UTC()).
		SetLastErrorCode(in.LastErrorCode).
		SetTitle(in.Title).
		SetBodyText(in.BodyText).
		SetBodyJSON(bodyJSON).
		SetRepositoryFullName(releaseRepositoryOf(bodyJSON)).
		SetSuppressedReason(in.SuppressedReason).
		SetParseMode(in.ParseMode).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if in.EventID != nil {
		c.SetEventID(*in.EventID)
	}
	c.SetNillableLockedUntil(in.LockedUntil)
	c.SetNillableClaimToken(in.ClaimToken)
	entity, err := c.Save(ctx)
	if err != nil {
		return NotificationOutbox{}, mapStoreError(err)
	}
	return outboxFromEntity(entity), nil
}

func (s *outboxStore) ClaimDue(ctx context.Context, now time.Time, lockFor time.Duration, limit int) ([]NotificationOutbox, error) {
	if limit < 1 {
		limit = 10
	}
	now = now.UTC()
	lockUntil := now.Add(lockFor)
	rows, err := s.client.NotificationOutbox.Query().
		Where(
			notificationoutbox.StatusIn(OutboxPending, OutboxSending),
			notificationoutbox.NextAttemptAtLTE(now),
			notificationoutbox.Or(
				notificationoutbox.LockedUntilIsNil(),
				notificationoutbox.LockedUntilLTE(now),
			),
		).
		Order(entclient.Asc(notificationoutbox.FieldNextAttemptAt), entclient.Asc(notificationoutbox.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.LockedUntil != nil && row.LockedUntil.After(now) {
			continue
		}
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return []NotificationOutbox{}, nil
	}
	// 单条 UPDATE 批量领取（逐行 UpdateOneID.Save 积压时是 50 次往返/tick）；
	// 领取是单进程 worker 顺序投递（见 Worker 注释），不存在跨进程争用，语义与逐行一致：
	// 全部成功或整体失败（领取失败按批重试，不会半领取）。
	if _, err := s.client.NotificationOutbox.Update().
		Where(notificationoutbox.IDIn(ids...)).
		SetStatus(OutboxSending).
		SetLockedUntil(lockUntil).
		SetUpdatedAt(now).
		AddAttemptCount(1).
		Save(ctx); err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]NotificationOutbox, 0, len(ids))
	for _, row := range rows {
		if row.LockedUntil != nil && row.LockedUntil.After(now) {
			continue
		}
		// 以已执行的状态迁移回填返回体，免去重新 SELECT。
		row.Status = OutboxSending
		row.LockedUntil = &lockUntil
		row.AttemptCount++
		row.UpdatedAt = now
		out = append(out, outboxFromEntity(row))
	}
	return out, nil
}

// outboxTerminalGuard 是三个终态/重试标记共用的状态守卫谓词：只允许从在途状态
// （pending / sending）推进。缺少该守卫时，租约过期导致的两份在途投递会让迟到的失败
// 标记把已 sent 行改回 pending 再次投递，或让迟到的成功标记把 dead 行改成 sent。
// 与 RetryDead / RetryAllDead 的 StatusEQ(OutboxDead) 守卫保持同一强弱。
func (s *outboxStore) outboxTerminalGuard(id string) predicate.NotificationOutbox {
	return notificationoutbox.And(
		notificationoutbox.IDEQ(id),
		notificationoutbox.StatusIn(OutboxPending, OutboxSending),
	)
}

// outboxMarkTransition 执行带守卫的状态推进并回报是否真的写入了一行。
// Save 返回受影响行数：守卫拒绝（行已被并发投递推进过）时为 0 且无错误，
// 调用方必须能把它与「写入失败」区分开，否则会按已落库的口径记指标。
func (s *outboxStore) outboxMarkTransition(ctx context.Context, id string, apply func(*entclient.NotificationOutboxUpdate)) (bool, error) {
	update := s.client.NotificationOutbox.Update().Where(s.outboxTerminalGuard(id))
	apply(update)
	affected, err := update.Save(ctx)
	if err != nil {
		return false, mapStoreError(err)
	}
	return affected == 1, nil
}

func (s *outboxStore) MarkSent(ctx context.Context, id string) (bool, error) {
	now := time.Now().UTC()
	return s.outboxMarkTransition(ctx, id, func(u *entclient.NotificationOutboxUpdate) {
		u.SetStatus(OutboxSent).ClearLockedUntil().SetUpdatedAt(now)
	})
}

func (s *outboxStore) MarkRetry(ctx context.Context, id string, next time.Time, errorCode string) (bool, error) {
	now := time.Now().UTC()
	return s.outboxMarkTransition(ctx, id, func(u *entclient.NotificationOutboxUpdate) {
		u.SetStatus(OutboxPending).
			SetNextAttemptAt(next.UTC()).
			SetLastErrorCode(errorCode).
			ClearLockedUntil().
			SetUpdatedAt(now)
	})
}

func (s *outboxStore) MarkDead(ctx context.Context, id, errorCode string) (bool, error) {
	now := time.Now().UTC()
	return s.outboxMarkTransition(ctx, id, func(u *entclient.NotificationOutboxUpdate) {
		u.SetStatus(OutboxDead).
			SetLastErrorCode(errorCode).
			ClearLockedUntil().
			SetUpdatedAt(now)
	})
}

// CancelPendingByRepository 只取消 pending 状态的 Release 通知。
// 已进入 sending 的记录可能已经调用外部渠道，不能安全回滚；sent/dead 也保留历史状态。
// 匹配依据是写入时冗余的 repository_full_name 列（非 Release 类别留空），
// 单条批量 UPDATE 完成，成本与积压量无关，也不依赖关联事件是否已被级联删除。
func (s *outboxStore) CancelPendingByRepository(ctx context.Context, fullName string) (int, error) {
	n, err := s.client.NotificationOutbox.Update().
		Where(
			notificationoutbox.StatusEQ(OutboxPending),
			notificationoutbox.RepositoryFullNameEQ(fullName),
		).
		SetStatus(OutboxCancelled).
		SetLastErrorCode("repository_unstarred").
		ClearLockedUntil().
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	return n, mapStoreError(err)
}

func (s *outboxStore) List(ctx context.Context, f ListFilter) ([]NotificationOutbox, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.NotificationOutbox.Query()
	if f.Status != "" {
		q = q.Where(notificationoutbox.StatusEQ(f.Status))
	}
	if len(f.ChannelIDs) > 0 {
		q = q.Where(notificationoutbox.ChannelIDIn(f.ChannelIDs...))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	if total == 0 || (f.Page-1)*f.PerPage >= total {
		return []NotificationOutbox{}, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
	}
	rows, err := q.Order(entclient.Desc(notificationoutbox.FieldCreatedAt), entclient.Asc(notificationoutbox.FieldID)).
		Offset((f.Page - 1) * f.PerPage).Limit(f.PerPage).All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	out := make([]NotificationOutbox, 0, len(rows))
	for _, row := range rows {
		out = append(out, outboxFromEntity(row))
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *outboxStore) CountByStatus(ctx context.Context, status string) (int, error) {
	n, err := s.client.NotificationOutbox.Query().Where(notificationoutbox.StatusEQ(status)).Count(ctx)
	return n, mapStoreError(err)
}

func (s *outboxStore) RetryDead(ctx context.Context, id string, next time.Time) error {
	now := time.Now().UTC()
	// 状态守卫：仅 dead 可重试。无守卫时把 worker 正在投递（sending、锁未过期）的行
	// 清锁翻回 pending，下个 tick 会被重复领取造成同一通知投递两次。
	n, err := s.client.NotificationOutbox.Update().
		Where(notificationoutbox.IDEQ(id), notificationoutbox.StatusEQ(OutboxDead)).
		SetStatus(OutboxPending).
		SetNextAttemptAt(next.UTC()).
		SetLastErrorCode("").
		ClearLockedUntil().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return mapStoreError(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RetryAllDead 批量重新排队：与 RetryDead 同一字段语义（pending + 立即到期 + 清错误码与锁）。
func (s *outboxStore) RetryAllDead(ctx context.Context, channelIDs []string, next time.Time) (int, error) {
	now := time.Now().UTC()
	q := s.client.NotificationOutbox.Update().
		Where(notificationoutbox.StatusEQ(OutboxDead))
	if len(channelIDs) > 0 {
		q = q.Where(notificationoutbox.ChannelIDIn(channelIDs...))
	}
	n, err := q.
		SetStatus(OutboxPending).
		SetNextAttemptAt(next.UTC()).
		SetLastErrorCode("").
		ClearLockedUntil().
		SetUpdatedAt(now).
		Save(ctx)
	return n, mapStoreError(err)
}

func (s *outboxStore) DeleteTerminalOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	cutoffUTC := cutoff.UTC()
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		ids, err := s.client.NotificationOutbox.Query().
			Where(
				notificationoutbox.StatusIn(OutboxSent, OutboxDead, OutboxCancelled),
				notificationoutbox.CreatedAtLT(cutoffUTC),
			).
			Limit(retentionBatchSize).
			Select(notificationoutbox.FieldID).
			Strings(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		if len(ids) == 0 {
			break
		}
		n, err := s.client.NotificationOutbox.Delete().
			Where(notificationoutbox.IDIn(ids...)).
			Exec(ctx)
		if err != nil {
			return total, mapStoreError(err)
		}
		total += n
		if len(ids) < retentionBatchSize {
			break
		}
		runtime.Gosched()
	}
	return total, nil
}

func outboxFromEntity(e *entclient.NotificationOutbox) NotificationOutbox {
	out := NotificationOutbox{
		ID: e.ID, ChannelID: e.ChannelID, EventID: e.EventID, AggregateKey: e.AggregateKey,
		IdempotencyKey: e.IdempotencyKey, Status: e.Status, AttemptCount: e.AttemptCount,
		NextAttemptAt: e.NextAttemptAt, LockedUntil: e.LockedUntil, ClaimToken: e.ClaimToken, LastErrorCode: e.LastErrorCode,
		Title: e.Title, BodyText: e.BodyText, BodyJSON: e.BodyJSON, ParseMode: e.ParseMode,
		RepositoryFullName: e.RepositoryFullName,
		SuppressedReason:   e.SuppressedReason,
		CreatedAt:          e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	out.HTMLURL = outboxHTMLURLOf(e.BodyJSON)
	return out
}

// --- cursors ---

type cursorStore struct{ client *entclient.Client }

func (s *cursorStore) Get(ctx context.Context, repoID, resource string) (SyncCursor, error) {
	entity, err := s.client.SyncCursor.Query().
		Where(synccursor.RepositoryIDEQ(repoID), synccursor.ResourceEQ(resource)).
		Only(ctx)
	if err != nil {
		return SyncCursor{}, mapStoreError(err)
	}
	return cursorFromEntity(entity), nil
}

func (s *cursorStore) Upsert(ctx context.Context, in SyncCursor) (SyncCursor, error) {
	now := time.Now().UTC()
	existing, err := s.Get(ctx, in.RepositoryID, in.Resource)
	if err == nil {
		upd := s.client.SyncCursor.UpdateOneID(existing.ID).
			SetCursorValue(in.CursorValue).
			SetEtag(in.ETag).
			SetLastErrorCode(in.LastErrorCode).
			SetUpdatedAt(now)
		if in.LastSuccessAt != nil {
			upd.SetLastSuccessAt(*in.LastSuccessAt)
		}
		entity, err := upd.Save(ctx)
		if err != nil {
			return SyncCursor{}, mapStoreError(err)
		}
		return cursorFromEntity(entity), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return SyncCursor{}, err
	}
	if in.ID == "" {
		in.ID = newID()
	}
	c := s.client.SyncCursor.Create().
		SetID(in.ID).
		SetRepositoryID(in.RepositoryID).
		SetResource(in.Resource).
		SetCursorValue(in.CursorValue).
		SetEtag(in.ETag).
		SetLastErrorCode(in.LastErrorCode).
		SetUpdatedAt(now)
	if in.LastSuccessAt != nil {
		c.SetLastSuccessAt(*in.LastSuccessAt)
	}
	entity, err := c.Save(ctx)
	if err != nil {
		return SyncCursor{}, mapStoreError(err)
	}
	return cursorFromEntity(entity), nil
}

func cursorFromEntity(e *entclient.SyncCursor) SyncCursor {
	return SyncCursor{
		ID: e.ID, RepositoryID: e.RepositoryID, Resource: e.Resource, CursorValue: e.CursorValue,
		ETag: e.Etag, LastSuccessAt: e.LastSuccessAt, LastErrorCode: e.LastErrorCode, UpdatedAt: e.UpdatedAt,
	}
}

// CleanupRetention 按策略删除过期历史数据；days<=0 的类别跳过。
func (s *storeImpl) CleanupRetention(ctx context.Context, policy RetentionPolicy, now time.Time) (CleanupResult, error) {
	var result CleanupResult
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if policy.EventsDays > 0 {
		n, err := s.Events().DeleteOlderThan(ctx, now.AddDate(0, 0, -policy.EventsDays))
		if err != nil {
			return result, err
		}
		result.EventsDeleted = n
	}
	if policy.OutboxDays > 0 {
		n, err := s.Outbox().DeleteTerminalOlderThan(ctx, now.AddDate(0, 0, -policy.OutboxDays))
		if err != nil {
			return result, err
		}
		result.OutboxDeleted = n
	}
	if policy.WebhookDeliveriesDays > 0 {
		n, err := s.WebhookDeliveries().DeleteOlderThan(ctx, now.AddDate(0, 0, -policy.WebhookDeliveriesDays))
		if err != nil {
			return result, err
		}
		result.WebhookDeliveriesDeleted = n
	}
	dehydrateDays := policy.WebhookPayloadDehydrateDays
	if dehydrateDays <= 0 {
		dehydrateDays = 1
	}
	dehydrateCutoff := now.AddDate(0, 0, -dehydrateDays)
	for {
		n, err := s.WebhookDeliveries().DehydrateWebhookPayloads(ctx, dehydrateCutoff, 500)
		if err != nil {
			return result, err
		}
		result.WebhookPayloadsDehydrated += n
		if n < 500 {
			break
		}
	}
	if s.driver != nil && s.driver.Dialect() == "sqlite" && (result.EventsDeleted > 0 || result.OutboxDeleted > 0 || result.WebhookDeliveriesDeleted > 0 || result.WebhookPayloadsDehydrated > 0) {
		_ = s.driver.Exec(ctx, "PRAGMA incremental_vacuum(50)", []any{}, nil)
	}
	return result, nil
}

// cleanupExpiredChatOps 删除过期的 ChatOps 交互令牌与其领取标记，返回删除行数。
// 令牌自身带 expires_at（写在 ValueJSON 内），无法在 SQL 层比较，故按键前缀取回后在
// Go 侧判定；领取标记无独立过期字段，按 updated_at 早于 chatOpsClaimRetention 兜底清理。
// 不清理会让 settings 表随每次按钮交互无界增长。
func (s *storeImpl) cleanupExpiredChatOps(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	deleted := 0

	tokens, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyHasPrefix(chatOpsTokenKeyPrefix)).
		Select(systemsetting.FieldKey, systemsetting.FieldValueJSON).
		All(ctx)
	if err != nil {
		return deleted, mapStoreError(err)
	}
	expiredKeys := make([]string, 0, len(tokens))
	for _, entity := range tokens {
		var data struct {
			ExpiresAt time.Time `json:"expires_at"`
		}
		// 解析失败的令牌无法判定过期时间，按已失效处理并清除，避免残留不可用行。
		if err := json.Unmarshal(entity.ValueJSON, &data); err != nil || data.ExpiresAt.IsZero() ||
			!now.Before(data.ExpiresAt.UTC()) {
			expiredKeys = append(expiredKeys, entity.Key)
		}
	}

	staleClaims, err := s.client.SystemSetting.Delete().
		Where(systemsetting.UpdatedAtLT(now.Add(-chatOpsClaimRetention)),
			systemsetting.KeyHasPrefix(chatOpsClaimKeyPrefix)).
		Exec(ctx)
	if err != nil {
		return deleted, mapStoreError(err)
	}
	deleted += staleClaims
	if len(expiredKeys) > 0 {
		n, err := s.client.SystemSetting.Delete().
			Where(systemsetting.KeyIn(expiredKeys...)).
			Exec(ctx)
		if err != nil {
			return deleted, mapStoreError(err)
		}
		deleted += n
		for _, key := range expiredKeys {
			s.settingsCache.Invalidate(key)
		}
	}
	return deleted, nil
}

// CleanupTransientSettings 清理可再生的一次性临时设置行，返回删除行数。
// 以下三类行只为短期幂等或一次性消费而存在，长期残留会让 settings 表无界增长：
//   - ChatOps 交互令牌（按自身 expires_at 判定过期）；
//   - 令牌领取标记（按 updated_at 早于保留窗口判定）；
//   - 自动打标回执（按 updated_at 早于 autoLabelReceiptRetention 判定）。
func (s *storeImpl) CleanupTransientSettings(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	chatOpsDeleted, err := s.cleanupExpiredChatOps(ctx, now)
	if err != nil {
		return chatOpsDeleted, err
	}

	autoLabelDeleted, err := s.client.SystemSetting.Delete().
		Where(systemsetting.UpdatedAtLT(now.Add(-autoLabelReceiptRetention)),
			systemsetting.Or(
				systemsetting.KeyHasPrefix(autoLabelReceiptKeyPrefix),
				systemsetting.KeyHasPrefix("github_label:"),
				systemsetting.KeyHasPrefix("side_effect:"),
			)).
		Exec(ctx)
	if err != nil {
		return chatOpsDeleted, mapStoreError(err)
	}
	return chatOpsDeleted + autoLabelDeleted, nil
}

// Dashboard 聚合统计。结果按 dashboardCacheTTL 短缓存：统计容忍秒级陈旧
// （前端 30s 轮询，直读时 3s 内命中缓存），不做逐写失效；
// 活跃/归档清单经 repoIDSets 短缓存，与各列表/计数查询共享同一份仓库扫描。
func (s *storeImpl) Dashboard(ctx context.Context) (DashboardStats, error) {
	if cached, ok := s.dashboardCache.Get(); ok {
		return cached, nil
	}
	var stats DashboardStats
	sets, err := repoIDSets(ctx, s.client, s.repoIDsCache)
	if err != nil {
		return stats, err
	}
	activeIDs, archivedIDs := sets.active, sets.archived
	if len(activeIDs) == 0 {
		stats.OpenIssues = 0
		stats.OpenPulls = 0
		stats.FailedActions = 0
		stats.OpenSecurity = 0
	} else {
		// Issues 与 PR 同在 work_items 表：一次按 kind 分组的聚合取回两个计数，
		// 消掉原先两次全表计数各自的往返。
		var workRows []struct {
			Kind  string
			Count int
		}
		if err := s.client.WorkItem.Query().Where(
			workitem.StateEQ("open"),
			workitem.IgnoredEQ(false),
			workitem.RepositoryIDIn(activeIDs...),
		).GroupBy(workitem.FieldKind).
			Aggregate(func(sel *entsql.Selector) string { return entsql.Count("*") }).
			Scan(ctx, &workRows); err != nil {
			return stats, mapStoreError(err)
		}
		for _, row := range workRows {
			switch row.Kind {
			case WorkItemKindIssue:
				stats.OpenIssues = row.Count
			case WorkItemKindPR:
				stats.OpenPulls = row.Count
			}
		}
		if stats.FailedActions, err = s.client.WorkflowRun.Query().Where(
			workflowrun.ConclusionIn(FailedConclusions()...),
			workflowrun.IgnoredEQ(false),
			workflowrun.RepositoryIDIn(activeIDs...),
		).Count(ctx); err != nil {
			return stats, mapStoreError(err)
		}
		if stats.OpenSecurity, err = s.client.SecurityAlert.Query().Where(
			securityalert.StateIn("open", "reopened"),
			securityalert.IgnoredEQ(false),
			securityalert.RepositoryIDIn(activeIDs...),
		).Count(ctx); err != nil {
			return stats, mapStoreError(err)
		}
	}
	if stats.Events24h, err = countEventsSince(ctx, s.client, time.Now().UTC().Add(-24*time.Hour), archivedIDs); err != nil {
		return stats, err
	}
	if stats.OutboxDead, err = s.Outbox().CountByStatus(ctx, OutboxDead); err != nil {
		return stats, err
	}
	// 活跃/基线同时只需 repositories 表一次按 sync_status 分组的聚合。
	var repoRows []struct {
		SyncStatus string `sql:"sync_status"`
		Count      int
	}
	if err := s.client.Repository.Query().
		GroupBy(repository.FieldSyncStatus).
		Aggregate(func(sel *entsql.Selector) string { return entsql.Count("*") }).
		Scan(ctx, &repoRows); err != nil {
		return stats, mapStoreError(err)
	}
	for _, row := range repoRows {
		switch row.SyncStatus {
		case SyncStatusActive:
			stats.ReposActive = row.Count
		case SyncStatusBaseline:
			stats.ReposBaseline = row.Count
		}
	}
	channels, err := s.client.NotificationChannel.Query().Where(notificationchannel.EnabledEQ(true)).Count(ctx)
	if err != nil {
		return stats, mapStoreError(err)
	}
	stats.ChannelsEnabled = channels

	// 活跃仓库新鲜度遥测：过滤 sync_status IN ('active', 'baseline_sync') AND is_archived = false
	activeRepos, err := s.client.Repository.Query().
		Where(
			repository.SyncStatusIn(SyncStatusActive, SyncStatusBaseline),
			repository.IsArchivedEQ(false),
		).
		All(ctx)
	if err != nil {
		return stats, mapStoreError(err)
	}

	now := time.Now().UTC()
	freshness := &SyncFreshnessSummary{
		AsOf: now,
	}
	if len(activeRepos) > 0 {
		var maxLag int64 = -1
		var mostLaggedName string
		var laggingCount int
		var hasSyncError bool

		for _, repo := range activeRepos {
			if repo.LastSyncErrorCode != "" {
				hasSyncError = true
			}
			var lag int64
			if repo.LastSyncedAt != nil {
				lag = int64(now.Sub(*repo.LastSyncedAt).Seconds())
			} else if repo.BaselineStartedAt != nil {
				lag = int64(now.Sub(*repo.BaselineStartedAt).Seconds())
			} else if !repo.CreatedAt.IsZero() {
				lag = int64(now.Sub(repo.CreatedAt).Seconds())
			}
			if lag < 0 {
				lag = 0
			}
			if lag > 300 {
				laggingCount++
			}
			if lag > maxLag {
				maxLag = lag
				mostLaggedName = repo.FullName
			}
		}
		if maxLag < 0 {
			maxLag = 0
		}
		freshness.MaxLagSeconds = maxLag
		freshness.LaggingRepoCount = laggingCount
		freshness.MostLaggedRepoName = mostLaggedName
		freshness.HasSyncError = hasSyncError
	}
	stats.Freshness = freshness

	s.dashboardCache.Set(stats)
	return stats, nil
}

// StarTrend 汇总全部活跃监控仓的 star 快照为按日总趋势。
// days>0 时仅返回最近 days 天（含今天，缺数据日不补 0）；days<=0 返回全部（从最早快照日起）。
// 每仓某日无快照时向前补最近一次快照值；范围内首日之前无快照的仓，从该仓首个快照日起参与求和。
func (s *storeImpl) StarTrend(ctx context.Context, days int) ([]StarTrendPoint, error) {
	repoIDs, err := s.client.Repository.Query().
		Where(
			repository.IsArchivedEQ(false),
			repository.MonitorEnabledEQ(true),
			repository.StarsEnabledEQ(true),
		).
		Select(repository.FieldID).
		Strings(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if len(repoIDs) == 0 {
		return []StarTrendPoint{}, nil
	}
	end := time.Now().UTC().Format("2006-01-02")
	from := ""
	if days > 0 {
		from = time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	}
	// 窗口查询：days>0 只加载窗口内快照，避免把整个快照历史读进内存。
	q := s.client.RepoStatSnapshot.Query().
		Where(repostatsnapshot.RepositoryIDIn(repoIDs...), repostatsnapshot.MetricEQ(MetricStargazers))
	if days > 0 {
		q = q.Where(repostatsnapshot.SampleDateGTE(from))
	}
	rows, err := q.Order(entclient.Asc(repostatsnapshot.FieldSampleDate)).All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	// 窗口起点之前可能已有快照（前向补值的种值）：每仓取起点前最近一条，
	// 使窗口曲线的首日求和与全量载入语义一致。
	seeds := map[string]int64{}
	if days > 0 {
		if seeds, err = s.stargazerSeeds(ctx, repoIDs, from); err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 && len(seeds) == 0 {
		return []StarTrendPoint{}, nil
	}
	// 按仓库分组，日期升序。
	byRepo := map[string][]RepoStatSnapshot{}
	var earliest string
	for _, e := range rows {
		snap := repoStatSnapshotFromEntity(e)
		byRepo[snap.RepositoryID] = append(byRepo[snap.RepositoryID], snap)
		if earliest == "" || snap.SampleDate < earliest {
			earliest = snap.SampleDate
		}
	}
	start := earliest
	if days > 0 {
		// 窗口起点即曲线起点；无种值时起点不得早于首个在窗快照（该日前总数必然为 0，省略）。
		start = from
		if len(seeds) == 0 && earliest > from {
			start = earliest
		}
	}
	if start > end {
		return []StarTrendPoint{}, nil
	}
	// 逐日向前补值求和：每仓序列升序且日期单增，游标只前进不回头，
	// 整体 O(快照数 + 天数×仓数)。
	type repoCursor struct {
		snaps []RepoStatSnapshot
		idx   int
		last  int64
		has   bool
	}
	cursors := make(map[string]*repoCursor, len(byRepo)+len(seeds))
	for id, snaps := range byRepo {
		cursors[id] = &repoCursor{snaps: snaps}
	}
	for id, v := range seeds {
		if c, ok := cursors[id]; ok {
			c.last, c.has = v, true
		} else {
			cursors[id] = &repoCursor{last: v, has: true}
		}
	}
	out := []StarTrendPoint{}
	for d := start; ; {
		var total int64
		for _, c := range cursors {
			for c.idx < len(c.snaps) && c.snaps[c.idx].SampleDate <= d {
				c.last = c.snaps[c.idx].Value
				c.has = true
				c.idx++
			}
			if c.has {
				total += c.last
			}
		}
		out = append(out, StarTrendPoint{Date: d, Total: total})
		if d >= end {
			break
		}
		t, _ := time.Parse("2006-01-02", d)
		d = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return out, nil
}

// stargazerSeeds 返回每仓在窗口起点（不含）之前最近一条 stargazers 快照值，
// 作为逐日前向补值的种值。先 GroupBy 各仓最大日期（1 次查询），再按
// (repository_id, metric, sample_date) 唯一索引一次取回对应行，与窗口行数无关。
func (s *storeImpl) stargazerSeeds(ctx context.Context, repoIDs []string, from string) (map[string]int64, error) {
	var maxRows []struct {
		RepositoryID string `json:"repository_id"`
		MaxDate      string `json:"max_date"`
	}
	if err := s.client.RepoStatSnapshot.Query().
		Where(
			repostatsnapshot.RepositoryIDIn(repoIDs...),
			repostatsnapshot.MetricEQ(MetricStargazers),
			repostatsnapshot.SampleDateLT(from),
		).
		GroupBy(repostatsnapshot.FieldRepositoryID).
		Aggregate(func(sel *entsql.Selector) string {
			return entsql.As(entsql.Max(repostatsnapshot.FieldSampleDate), "max_date")
		}).
		Scan(ctx, &maxRows); err != nil {
		return nil, mapStoreError(err)
	}
	if len(maxRows) == 0 {
		return map[string]int64{}, nil
	}
	preds := make([]predicate.RepoStatSnapshot, 0, len(maxRows))
	for _, m := range maxRows {
		preds = append(preds, repostatsnapshot.And(
			repostatsnapshot.RepositoryIDEQ(m.RepositoryID),
			repostatsnapshot.SampleDateEQ(m.MaxDate),
		))
	}
	seedRows, err := s.client.RepoStatSnapshot.Query().
		Where(repostatsnapshot.MetricEQ(MetricStargazers), repostatsnapshot.Or(preds...)).
		Select(repostatsnapshot.FieldRepositoryID, repostatsnapshot.FieldValue).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	seeds := make(map[string]int64, len(seedRows))
	for _, row := range seedRows {
		seeds[row.RepositoryID] = row.Value
	}
	return seeds, nil
}

// --- repo stat snapshots ---

type repoStatSnapshotStore struct{ client *entclient.Client }

func (s *repoStatSnapshotStore) Upsert(ctx context.Context, in RepoStatSnapshot) (RepoStatSnapshot, error) {
	now := time.Now().UTC()
	existing, err := s.client.RepoStatSnapshot.Query().
		Where(
			repostatsnapshot.RepositoryIDEQ(in.RepositoryID),
			repostatsnapshot.MetricEQ(in.Metric),
			repostatsnapshot.SampleDateEQ(in.SampleDate),
		).
		Only(ctx)
	if err == nil {
		entity, err := s.client.RepoStatSnapshot.UpdateOneID(existing.ID).
			SetValue(in.Value).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			return RepoStatSnapshot{}, mapStoreError(err)
		}
		return repoStatSnapshotFromEntity(entity), nil
	}
	if mapStoreError(err) != ErrNotFound {
		return RepoStatSnapshot{}, mapStoreError(err)
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	entity, err := s.client.RepoStatSnapshot.Create().
		SetID(in.ID).
		SetRepositoryID(in.RepositoryID).
		SetMetric(in.Metric).
		SetValue(in.Value).
		SetSampleDate(in.SampleDate).
		SetCreatedAt(in.CreatedAt.UTC()).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return RepoStatSnapshot{}, mapStoreError(err)
	}
	return repoStatSnapshotFromEntity(entity), nil
}

func (s *repoStatSnapshotStore) ListInRange(ctx context.Context, repoIDs []string, metric, fromDate, toDate string) ([]RepoStatSnapshot, error) {
	q := s.client.RepoStatSnapshot.Query().
		Where(
			repostatsnapshot.MetricEQ(metric),
			repostatsnapshot.SampleDateGTE(fromDate),
			repostatsnapshot.SampleDateLTE(toDate),
		)
	if len(repoIDs) > 0 {
		q = q.Where(repostatsnapshot.RepositoryIDIn(repoIDs...))
	}
	rows, err := q.Order(entclient.Asc(repostatsnapshot.FieldSampleDate)).All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]RepoStatSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, repoStatSnapshotFromEntity(row))
	}
	return out, nil
}

func repoStatSnapshotFromEntity(e *entclient.RepoStatSnapshot) RepoStatSnapshot {
	return RepoStatSnapshot{
		ID: e.ID, RepositoryID: e.RepositoryID, Metric: e.Metric, Value: e.Value,
		SampleDate: e.SampleDate, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- system leases ---

type leaseStore struct {
	client *entclient.Client
	driver dialect.Driver
}

func (s *leaseStore) getDB() *sql.DB {
	if s.driver == nil {
		return nil
	}
	if sqlDriver, ok := s.driver.(*entsql.Driver); ok {
		return sqlDriver.DB()
	}
	return nil
}

func leaseFromEntity(entity *entclient.SystemLease) *SystemLease {
	if entity == nil {
		return nil
	}
	return &SystemLease{
		ID:           entity.ID,
		TaskName:     entity.TaskName,
		HolderID:     entity.HolderID,
		AcquiredAt:   entity.AcquiredAt.UTC(),
		ExpiresAt:    entity.ExpiresAt.UTC(),
		FencingToken: entity.FencingToken,
	}
}

func (s *leaseStore) Get(ctx context.Context, taskName string) (*SystemLease, error) {
	entity, err := s.client.SystemLease.Query().
		Where(systemlease.TaskNameEQ(taskName)).
		Only(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return leaseFromEntity(entity), nil
}

func (s *leaseStore) Acquire(ctx context.Context, taskName, holderID string, ttl time.Duration) (int64, bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	ttlSecs := int(ttl.Seconds())
	if ttlSecs < 1 {
		ttlSecs = 1
	}

	db := s.getDB()
	if db != nil {
		isPG := s.driver != nil && s.driver.Dialect() == dialect.Postgres
		if isPG {
			query := `INSERT INTO system_leases (id, task_name, holder_id, acquired_at, expires_at, fencing_token)
VALUES ($1, $2, $3, now(), now() + make_interval(secs => $4), 1)
ON CONFLICT (task_name) DO UPDATE
SET holder_id = EXCLUDED.holder_id,
    acquired_at = now(),
    expires_at = now() + make_interval(secs => $4),
    fencing_token = CASE 
        WHEN system_leases.holder_id = EXCLUDED.holder_id AND system_leases.expires_at > now() 
        THEN system_leases.fencing_token 
        ELSE system_leases.fencing_token + 1 
    END
WHERE system_leases.expires_at <= now() OR system_leases.holder_id = EXCLUDED.holder_id
RETURNING fencing_token`
			var token int64
			err := db.QueryRowContext(ctx, query, newID(), taskName, holderID, ttlSecs).Scan(&token)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, false, nil
				}
				return 0, false, fmt.Errorf("%w: acquire lease %s: %v", errDatabaseOperation, taskName, err)
			}
			return token, true, nil
		}

		// SQLite: atomic transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return 0, false, fmt.Errorf("%w: begin lease tx: %v", errDatabaseOperation, err)
		}
		defer func() { _ = tx.Rollback() }()

		id := newID()
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO system_leases (id, task_name, holder_id, acquired_at, expires_at, fencing_token)
VALUES (?, ?, ?, CURRENT_TIMESTAMP, datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds'), 1)`, id, taskName, holderID, ttlSecs)
		if err != nil {
			return 0, false, fmt.Errorf("%w: insert lease: %v", errDatabaseOperation, err)
		}
		rowsAffected, err := res.RowsAffected()
		if err == nil && rowsAffected > 0 {
			if err := tx.Commit(); err != nil {
				return 0, false, fmt.Errorf("%w: commit lease insert: %v", errDatabaseOperation, err)
			}
			return 1, true, nil
		}

		res, err = tx.ExecContext(ctx, `UPDATE system_leases
SET holder_id = ?,
    acquired_at = CURRENT_TIMESTAMP,
    expires_at = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds'),
    fencing_token = CASE 
        WHEN holder_id = ? AND expires_at > CURRENT_TIMESTAMP 
        THEN fencing_token 
        ELSE fencing_token + 1 
    END
WHERE task_name = ? AND (expires_at <= CURRENT_TIMESTAMP OR holder_id = ?)`,
			holderID, ttlSecs, holderID, taskName, holderID)
		if err != nil {
			return 0, false, fmt.Errorf("%w: update lease: %v", errDatabaseOperation, err)
		}
		rowsAffected, err = res.RowsAffected()
		if err != nil || rowsAffected == 0 {
			return 0, false, nil
		}

		var token int64
		err = tx.QueryRowContext(ctx, `SELECT fencing_token FROM system_leases WHERE task_name = ?`, taskName).Scan(&token)
		if err != nil {
			return 0, false, fmt.Errorf("%w: read updated lease token: %v", errDatabaseOperation, err)
		}
		if err := tx.Commit(); err != nil {
			return 0, false, fmt.Errorf("%w: commit lease update: %v", errDatabaseOperation, err)
		}
		return token, true, nil
	}

	return 0, false, errors.New("db driver not available")
}

func (s *leaseStore) Renew(ctx context.Context, taskName, holderID string, fencingToken int64, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	ttlSecs := int(ttl.Seconds())
	if ttlSecs < 1 {
		ttlSecs = 1
	}

	db := s.getDB()
	if db != nil {
		isPG := s.driver != nil && s.driver.Dialect() == dialect.Postgres
		var res sql.Result
		var err error
		if isPG {
			res, err = db.ExecContext(ctx, `UPDATE system_leases
SET expires_at = now() + make_interval(secs => $1)
WHERE task_name = $2 AND holder_id = $3 AND fencing_token = $4 AND expires_at > now()`, ttlSecs, taskName, holderID, fencingToken)
		} else {
			res, err = db.ExecContext(ctx, `UPDATE system_leases
SET expires_at = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds')
WHERE task_name = ? AND holder_id = ? AND fencing_token = ? AND expires_at > CURRENT_TIMESTAMP`, ttlSecs, taskName, holderID, fencingToken)
		}
		if err != nil {
			return false, fmt.Errorf("%w: renew lease %s: %v", errDatabaseOperation, taskName, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		return rows > 0, nil
	}
	return false, errors.New("db driver not available")
}

func (s *leaseStore) Release(ctx context.Context, taskName, holderID string, fencingToken int64) (bool, error) {
	db := s.getDB()
	if db != nil {
		isPG := s.driver != nil && s.driver.Dialect() == dialect.Postgres
		var res sql.Result
		var err error
		if isPG {
			res, err = db.ExecContext(ctx, `UPDATE system_leases
SET expires_at = now()
WHERE task_name = $1 AND holder_id = $2 AND fencing_token = $3`, taskName, holderID, fencingToken)
		} else {
			res, err = db.ExecContext(ctx, `UPDATE system_leases
SET expires_at = CURRENT_TIMESTAMP
WHERE task_name = ? AND holder_id = ? AND fencing_token = ?`, taskName, holderID, fencingToken)
		}
		if err != nil {
			return false, fmt.Errorf("%w: release lease %s: %v", errDatabaseOperation, taskName, err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		return rows > 0, nil
	}
	return false, errors.New("db driver not available")
}

func (s *outboxStore) ClaimOne(ctx context.Context, workerID string, timeout time.Duration) (*NotificationOutbox, string, error) {
	if timeout <= 0 {
		timeout = 1 * time.Minute
	}
	ttlSecs := int(timeout.Seconds())
	if ttlSecs < 1 {
		ttlSecs = 1
	}

	db := s.getDB()
	isPG := s.driver != nil && s.driver.Dialect() == dialect.Postgres

	if db != nil && isPG {
		claimToken := newID()
		query := `UPDATE notification_outbox
SET status = 'sending',
    claim_token = $1,
    locked_until = now() + make_interval(secs => $2),
    attempt_count = attempt_count + 1,
    updated_at = now()
WHERE id = (
    SELECT id FROM notification_outbox
    WHERE status IN ('pending', 'sending')
      AND next_attempt_at <= now()
      AND (locked_until IS NULL OR locked_until <= now())
    ORDER BY next_attempt_at ASC, id ASC
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING id`
		var id string
		err := db.QueryRowContext(ctx, query, claimToken, ttlSecs).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, "", nil
			}
			return nil, "", fmt.Errorf("%w: claim outbox: %v", errDatabaseOperation, err)
		}
		entity, err := s.client.NotificationOutbox.Get(ctx, id)
		if err != nil {
			return nil, "", mapStoreError(err)
		}
		item := outboxFromEntity(entity)
		return &item, claimToken, nil
	}

	if db != nil {
		// SQLite: transaction with retry loop
		for retry := 0; retry < 5; retry++ {
			claimToken := newID()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return nil, "", fmt.Errorf("%w: begin outbox claim tx: %v", errDatabaseOperation, err)
			}

			var id string
			err = tx.QueryRowContext(ctx, `SELECT id FROM notification_outbox
WHERE status IN ('pending', 'sending')
  AND next_attempt_at <= CURRENT_TIMESTAMP
  AND (locked_until IS NULL OR locked_until <= CURRENT_TIMESTAMP)
ORDER BY next_attempt_at ASC, id ASC
LIMIT 1`).Scan(&id)
			if err != nil {
				_ = tx.Rollback()
				if errors.Is(err, sql.ErrNoRows) {
					return nil, "", nil
				}
				return nil, "", fmt.Errorf("%w: select claim candidate: %v", errDatabaseOperation, err)
			}

			res, err := tx.ExecContext(ctx, `UPDATE notification_outbox
SET status = 'sending',
    claim_token = ?,
    locked_until = datetime(CURRENT_TIMESTAMP, '+' || ? || ' seconds'),
    attempt_count = attempt_count + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status IN ('pending', 'sending') AND (locked_until IS NULL OR locked_until <= CURRENT_TIMESTAMP)`, claimToken, ttlSecs, id)
			if err != nil {
				_ = tx.Rollback()
				return nil, "", fmt.Errorf("%w: update claim candidate: %v", errDatabaseOperation, err)
			}
			rows, err := res.RowsAffected()
			if err != nil || rows == 0 {
				_ = tx.Rollback()
				continue // retry with another candidate
			}

			if err := tx.Commit(); err != nil {
				return nil, "", fmt.Errorf("%w: commit outbox claim: %v", errDatabaseOperation, err)
			}

			entity, err := s.client.NotificationOutbox.Get(ctx, id)
			if err != nil {
				return nil, "", mapStoreError(err)
			}
			item := outboxFromEntity(entity)
			return &item, claimToken, nil
		}
		return nil, "", nil
	}

	return nil, "", errors.New("db driver not available")
}

func (s *outboxStore) MarkSentWithToken(ctx context.Context, id, claimToken string) (TransitionResult, error) {
	now := time.Now().UTC()
	n, err := s.client.NotificationOutbox.Update().
		Where(
			notificationoutbox.IDEQ(id),
			notificationoutbox.ClaimTokenEQ(claimToken),
			notificationoutbox.StatusIn(OutboxPending, OutboxSending),
		).
		SetStatus(OutboxSent).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 1 {
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	exists, err := s.client.NotificationOutbox.Query().Where(notificationoutbox.IDEQ(id)).Exist(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if !exists {
		return TransitionResult{Applied: false, Stale: false}, ErrNotFound
	}
	return TransitionResult{Applied: false, Stale: true}, nil
}

func (s *outboxStore) MarkFailedWithToken(ctx context.Context, id, claimToken, lastErr string, nextRetryAt *time.Time) (TransitionResult, error) {
	now := time.Now().UTC()
	next := now.Add(30 * time.Second)
	if nextRetryAt != nil && !nextRetryAt.IsZero() {
		next = nextRetryAt.UTC()
	}
	n, err := s.client.NotificationOutbox.Update().
		Where(
			notificationoutbox.IDEQ(id),
			notificationoutbox.ClaimTokenEQ(claimToken),
			notificationoutbox.StatusIn(OutboxPending, OutboxSending),
		).
		SetStatus(OutboxPending).
		SetNextAttemptAt(next).
		SetLastErrorCode(lastErr).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 1 {
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	exists, err := s.client.NotificationOutbox.Query().Where(notificationoutbox.IDEQ(id)).Exist(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if !exists {
		return TransitionResult{Applied: false, Stale: false}, ErrNotFound
	}
	return TransitionResult{Applied: false, Stale: true}, nil
}

func (s *outboxStore) MarkDeadWithToken(ctx context.Context, id, claimToken, finalErr string) (TransitionResult, error) {
	now := time.Now().UTC()
	n, err := s.client.NotificationOutbox.Update().
		Where(
			notificationoutbox.IDEQ(id),
			notificationoutbox.ClaimTokenEQ(claimToken),
			notificationoutbox.StatusIn(OutboxPending, OutboxSending),
		).
		SetStatus(OutboxDead).
		SetLastErrorCode(finalErr).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if n == 1 {
		return TransitionResult{Applied: true, Stale: false}, nil
	}

	exists, err := s.client.NotificationOutbox.Query().Where(notificationoutbox.IDEQ(id)).Exist(ctx)
	if err != nil {
		return TransitionResult{}, mapStoreError(err)
	}
	if !exists {
		return TransitionResult{Applied: false, Stale: false}, ErrNotFound
	}
	return TransitionResult{Applied: false, Stale: true}, nil
}
