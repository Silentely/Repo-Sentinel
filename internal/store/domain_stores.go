package store

import (
	"context"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/event"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/githubinstallation"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/notificationchannel"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/notificationoutbox"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/repository"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/repostatsnapshot"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/securityalert"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/synccursor"
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

type repositoryStore struct{ client *entclient.Client }

func (s *repositoryStore) Upsert(ctx context.Context, in Repository) (Repository, error) {
	now := time.Now().UTC()
	existing, err := s.client.Repository.Query().Where(repository.FullNameEQ(in.FullName)).Only(ctx)
	if err == nil {
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
	return mapStoreError(upd.Exec(ctx))
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

type webhookDeliveryStore struct{ client *entclient.Client }

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
	entity, err := s.client.WebhookDelivery.Create().
		SetID(in.ID).
		SetDeliveryID(in.DeliveryID).
		SetEventType(in.EventType).
		SetAction(in.Action).
		SetRepositoryFullName(in.RepositoryFullName).
		SetStatus(in.Status).
		SetErrorCode(in.ErrorCode).
		SetPayload(in.Payload).
		SetReceivedAt(in.ReceivedAt.UTC()).
		Save(ctx)
	if err != nil {
		return WebhookDelivery{}, mapStoreError(err)
	}
	return webhookDeliveryFromEntity(entity), nil
}

func (s *webhookDeliveryStore) GetByDeliveryID(ctx context.Context, deliveryID string) (WebhookDelivery, error) {
	entity, err := s.client.WebhookDelivery.Query().Where(webhookdelivery.DeliveryIDEQ(deliveryID)).Only(ctx)
	if err != nil {
		return WebhookDelivery{}, mapStoreError(err)
	}
	return webhookDeliveryFromEntity(entity), nil
}

func (s *webhookDeliveryStore) MarkProcessed(ctx context.Context, id, status, errorCode string) error {
	now := time.Now().UTC()
	err := s.client.WebhookDelivery.UpdateOneID(id).
		SetStatus(status).
		SetErrorCode(errorCode).
		SetProcessedAt(now).
		Exec(ctx)
	return mapStoreError(err)
}

func (s *webhookDeliveryStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	n, err := s.client.WebhookDelivery.Delete().
		Where(webhookdelivery.ReceivedAtLT(cutoff.UTC())).
		Exec(ctx)
	return n, mapStoreError(err)
}

func webhookDeliveryFromEntity(e *entclient.WebhookDelivery) WebhookDelivery {
	return WebhookDelivery{
		ID: e.ID, DeliveryID: e.DeliveryID, EventType: e.EventType, Action: e.Action,
		RepositoryFullName: e.RepositoryFullName, Status: e.Status, ErrorCode: e.ErrorCode,
		Payload: e.Payload, ReceivedAt: e.ReceivedAt, ProcessedAt: e.ProcessedAt,
	}
}

// --- work items ---

type workItemStore struct{ client *entclient.Client }

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
func (s *workItemStore) UpsertIfNewer(ctx context.Context, in WorkItem) (WorkItem, bool, error) {
	now := time.Now().UTC()
	existing, err := s.GetByRepoNumber(ctx, in.RepositoryID, in.Number)
	if err == nil {
		if in.SourceUpdatedAt.Before(existing.SourceUpdatedAt) {
			return existing, false, nil
		}
		if in.SourceUpdatedAt.Equal(existing.SourceUpdatedAt) && in.StateHash == existing.StateHash {
			return existing, false, nil
		}
		entity, err := s.client.WorkItem.UpdateOneID(existing.ID).
			SetKind(in.Kind).
			SetState(in.State).
			SetTitle(in.Title).
			SetAuthor(in.Author).
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
func repoFullNameByID(ctx context.Context, client *entclient.Client, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
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
func activeRepositoryIDs(ctx context.Context, client *entclient.Client) ([]string, error) {
	rows, err := client.Repository.Query().
		Where(repository.IsArchivedEQ(false)).
		Select(repository.FieldID).
		Strings(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return rows, nil
}

// archivedRepositoryIDs 返回已归档仓库的 ID 列表。
// 事件类查询采用「排除归档」而非「仅含活跃」：repository_id 为空的孤儿事件保持可见，
// 避免历史数据因仓库行缺失而凭空消失。
func archivedRepositoryIDs(ctx context.Context, client *entclient.Client) ([]string, error) {
	rows, err := client.Repository.Query().
		Where(repository.IsArchivedEQ(true)).
		Select(repository.FieldID).
		Strings(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return rows, nil
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
		ids, err := activeRepositoryIDs(ctx, s.client)
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
	if f.OnlyIgnored {
		q = q.Where(workitem.IgnoredEQ(true))
	} else if !f.IncludeIgnored {
		q = q.Where(workitem.IgnoredEQ(false))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
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
	names := repoFullNameByID(ctx, s.client, repoIDs)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *workItemStore) CountOpen(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client)
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
		Title: e.Title, Author: e.Author, LabelsJSON: e.LabelsJSON, AssigneesJSON: e.AssigneesJSON,
		Milestone: e.Milestone, Draft: e.Draft, Merged: e.Merged, HTMLURL: e.HTMLURL,
		SourceUpdatedAt: e.SourceUpdatedAt, StateHash: e.StateHash,
		ReviewState: e.ReviewState, ReviewDecision: e.ReviewDecision, Reviewers: e.Reviewers,
		CheckStatus: e.CheckStatus, CheckConclusion: e.CheckConclusion,
		ChecksTotal: e.ChecksTotal, ChecksPassed: e.ChecksPassed,
		Ignored: e.Ignored, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// --- workflow runs ---

type workflowRunStore struct{ client *entclient.Client }

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
		ids, err := activeRepositoryIDs(ctx, s.client)
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
	names := repoFullNameByID(ctx, s.client, repoIDs)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *workflowRunStore) CountFailed(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client)
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

type securityAlertStore struct{ client *entclient.Client }

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
		ids, err := activeRepositoryIDs(ctx, s.client)
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
	names := repoFullNameByID(ctx, s.client, repoIDs)
	for i := range out {
		if n, ok := names[out[i].RepositoryID]; ok {
			out[i].RepositoryFullName = n
		}
	}
	return out, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func (s *securityAlertStore) CountOpen(ctx context.Context) (int, error) {
	ids, err := activeRepositoryIDs(ctx, s.client)
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

type eventStore struct{ client *entclient.Client }

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
		ids, err := archivedRepositoryIDs(ctx, s.client)
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
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
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

func (s *eventStore) CountSince(ctx context.Context, since time.Time) (int, error) {
	n, err := s.client.Event.Query().Where(event.OccurredAtGTE(since.UTC())).Count(ctx)
	return n, mapStoreError(err)
}

func (s *eventStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	n, err := s.client.Event.Delete().
		Where(event.CreatedAtLT(cutoff.UTC())).
		Exec(ctx)
	return n, mapStoreError(err)
}

// ListSince 每日摘要专用：只取未被抑制（非基线/归档期间产生）且非已归档仓库的事件。
func (s *eventStore) ListSince(ctx context.Context, since time.Time, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	q := s.client.Event.Query().
		Where(event.OccurredAtGTE(since.UTC())).
		Where(event.SuppressNotificationEQ(false))
	ids, err := archivedRepositoryIDs(ctx, s.client)
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
		WorkflowRunID: e.WorkflowRunID, WorkflowConclusion: e.WorkflowConclusion, OccurredAt: e.OccurredAt,
		SourceUpdatedAt: e.SourceUpdatedAt, HTMLURL: e.HTMLURL, PayloadSummary: e.PayloadSummary,
		SuppressNotification: e.SuppressNotification, DedupeFingerprint: e.DedupeFingerprint,
		StateHash: e.StateHash, CreatedAt: e.CreatedAt,
	}
}

// --- channels ---

type channelStore struct{ client *entclient.Client }

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
				SetUpdatedAt(now).
				Save(ctx)
			if err != nil {
				return NotificationChannel{}, mapStoreError(err)
			}
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
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return NotificationChannel{}, mapStoreError(err)
	}
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

func (s *channelStore) List(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.client.NotificationChannel.Query().All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]NotificationChannel, 0, len(rows))
	for _, row := range rows {
		out = append(out, channelFromEntity(row))
	}
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
	return mapStoreError(err)
}

func (s *channelStore) Delete(ctx context.Context, id string) error {
	err := s.client.NotificationChannel.DeleteOneID(id).Exec(ctx)
	return mapStoreError(err)
}

func (s *channelStore) ToggleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.client.NotificationChannel.UpdateOneID(id).
		SetEnabled(enabled).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)
	return mapStoreError(err)
}

func channelFromEntity(e *entclient.NotificationChannel) NotificationChannel {
	return NotificationChannel{
		ID: e.ID, ChannelType: e.ChannelType, Name: e.Name, Enabled: e.Enabled,
		Target: e.Target, SecretEnvelope: e.SecretEnvelope, AllowPrivate: e.AllowPrivate,
		EventKinds: e.EventKinds, DigestEnabled: e.DigestEnabled,
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

type outboxStore struct{ client *entclient.Client }

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
		SetParseMode(in.ParseMode).
		SetCreatedAt(now).
		SetUpdatedAt(now)
	if in.EventID != nil {
		c.SetEventID(*in.EventID)
	}
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
		).
		Order(entclient.Asc(notificationoutbox.FieldNextAttemptAt), entclient.Asc(notificationoutbox.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	out := make([]NotificationOutbox, 0, len(rows))
	for _, row := range rows {
		if row.LockedUntil != nil && row.LockedUntil.After(now) {
			continue
		}
		entity, err := s.client.NotificationOutbox.UpdateOneID(row.ID).
			SetStatus(OutboxSending).
			SetLockedUntil(lockUntil).
			SetAttemptCount(row.AttemptCount + 1).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			continue
		}
		out = append(out, outboxFromEntity(entity))
	}
	return out, nil
}

func (s *outboxStore) MarkSent(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return mapStoreError(s.client.NotificationOutbox.UpdateOneID(id).
		SetStatus(OutboxSent).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Exec(ctx))
}

func (s *outboxStore) MarkRetry(ctx context.Context, id string, next time.Time, errorCode string) error {
	now := time.Now().UTC()
	return mapStoreError(s.client.NotificationOutbox.UpdateOneID(id).
		SetStatus(OutboxPending).
		SetNextAttemptAt(next.UTC()).
		SetLastErrorCode(errorCode).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Exec(ctx))
}

func (s *outboxStore) MarkDead(ctx context.Context, id, errorCode string) error {
	now := time.Now().UTC()
	return mapStoreError(s.client.NotificationOutbox.UpdateOneID(id).
		SetStatus(OutboxDead).
		SetLastErrorCode(errorCode).
		ClearLockedUntil().
		SetUpdatedAt(now).
		Exec(ctx))
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
	return mapStoreError(s.client.NotificationOutbox.UpdateOneID(id).
		SetStatus(OutboxPending).
		SetNextAttemptAt(next.UTC()).
		SetLastErrorCode("").
		ClearLockedUntil().
		SetUpdatedAt(now).
		Exec(ctx))
}

func (s *outboxStore) DeleteTerminalOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	n, err := s.client.NotificationOutbox.Delete().
		Where(
			notificationoutbox.StatusIn(OutboxSent, OutboxDead),
			notificationoutbox.CreatedAtLT(cutoff.UTC()),
		).
		Exec(ctx)
	return n, mapStoreError(err)
}

func outboxFromEntity(e *entclient.NotificationOutbox) NotificationOutbox {
	out := NotificationOutbox{
		ID: e.ID, ChannelID: e.ChannelID, EventID: e.EventID, AggregateKey: e.AggregateKey,
		IdempotencyKey: e.IdempotencyKey, Status: e.Status, AttemptCount: e.AttemptCount,
		NextAttemptAt: e.NextAttemptAt, LockedUntil: e.LockedUntil, LastErrorCode: e.LastErrorCode,
		Title: e.Title, BodyText: e.BodyText, BodyJSON: e.BodyJSON, ParseMode: e.ParseMode,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
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
	return result, nil
}

// Dashboard 聚合统计。
func (s *storeImpl) Dashboard(ctx context.Context) (DashboardStats, error) {
	var stats DashboardStats
	var err error
	// 一次取出活跃仓 ID，避免 CountOpen/PR/Actions/Security 各自重复查询。
	activeIDs, err := activeRepositoryIDs(ctx, s.client)
	if err != nil {
		return stats, err
	}
	if len(activeIDs) == 0 {
		stats.OpenIssues = 0
		stats.OpenPulls = 0
		stats.FailedActions = 0
		stats.OpenSecurity = 0
	} else {
		if stats.OpenIssues, err = s.client.WorkItem.Query().Where(
			workitem.KindEQ(WorkItemKindIssue),
			workitem.StateEQ("open"),
			workitem.IgnoredEQ(false),
			workitem.RepositoryIDIn(activeIDs...),
		).Count(ctx); err != nil {
			return stats, mapStoreError(err)
		}
		if stats.OpenPulls, err = s.client.WorkItem.Query().Where(
			workitem.KindEQ(WorkItemKindPR),
			workitem.StateEQ("open"),
			workitem.IgnoredEQ(false),
			workitem.RepositoryIDIn(activeIDs...),
		).Count(ctx); err != nil {
			return stats, mapStoreError(err)
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
	if stats.Events24h, err = s.Events().CountSince(ctx, time.Now().UTC().Add(-24*time.Hour)); err != nil {
		return stats, err
	}
	if stats.OutboxDead, err = s.Outbox().CountByStatus(ctx, OutboxDead); err != nil {
		return stats, err
	}
	active, err := s.client.Repository.Query().Where(repository.SyncStatusEQ(SyncStatusActive)).Count(ctx)
	if err != nil {
		return stats, mapStoreError(err)
	}
	baseline, err := s.client.Repository.Query().Where(repository.SyncStatusEQ(SyncStatusBaseline)).Count(ctx)
	if err != nil {
		return stats, mapStoreError(err)
	}
	channels, err := s.client.NotificationChannel.Query().Where(notificationchannel.EnabledEQ(true)).Count(ctx)
	if err != nil {
		return stats, mapStoreError(err)
	}
	stats.ReposActive = active
	stats.ReposBaseline = baseline
	stats.ChannelsEnabled = channels
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
	rows, err := s.client.RepoStatSnapshot.Query().
		Where(repostatsnapshot.RepositoryIDIn(repoIDs...), repostatsnapshot.MetricEQ(MetricStargazers)).
		Order(entclient.Asc(repostatsnapshot.FieldSampleDate)).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if len(rows) == 0 {
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
		from := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
		if from > start {
			start = from
		}
	}
	end := time.Now().UTC().Format("2006-01-02")
	// 逐日向前补值求和。
	totals := map[string]int64{}
	lastByRepo := map[string]int64{}
	for d := start; d <= end; {
		var total int64
		for id, snaps := range byRepo {
			// 取该仓 <= d 的最近快照值（每仓序列有序，用线性推进即可，仓库量小）。
			for _, s := range snaps {
				if s.SampleDate > d {
					break
				}
				lastByRepo[id] = s.Value
			}
			if v, ok := lastByRepo[id]; ok {
				total += v
			}
		}
		totals[d] = total
		// 推进日期。
		t, _ := time.Parse("2006-01-02", d)
		d = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	out := make([]StarTrendPoint, 0, len(totals))
	for d := start; d <= end; {
		out = append(out, StarTrendPoint{Date: d, Total: totals[d]})
		t, _ := time.Parse("2006-01-02", d)
		d = t.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return out, nil
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
