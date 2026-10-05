package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

type dryRunRequest struct {
	EventType  string                      `json:"event_type"`
	Action     string                      `json:"action,omitempty"`
	Repository string                      `json:"repository,omitempty"`
	Branch     string                      `json:"branch,omitempty"`
	PayloadRaw string                      `json:"payload_raw,omitempty"`
	Payload    map[string]any              `json:"payload,omitempty"`
	Channels   []store.NotificationChannel `json:"channels,omitempty"`
}

type dryRunStoreWrapper struct {
	store.Store
	customChannels []store.NotificationChannel
}

type dryRunEventStore struct {
	store.EventStore
}

func (s *dryRunEventStore) Create(ctx context.Context, ev store.Event) (store.Event, error) {
	if ev.ID == "" {
		ev.ID = "event-dryrun-" + ulid.Make().String()
	}
	return ev, nil
}

func (s *dryRunEventStore) GetByFingerprint(ctx context.Context, fp string) (store.Event, error) {
	return store.Event{}, store.ErrNotFound
}

func (w *dryRunStoreWrapper) Events() store.EventStore {
	if w.Store == nil {
		return &dryRunEventStore{}
	}
	return &dryRunEventStore{EventStore: w.Store.Events()}
}

type dryRunOutboxStore struct {
	store.OutboxStore
}

func (s *dryRunOutboxStore) Create(ctx context.Context, ob store.NotificationOutbox) (store.NotificationOutbox, error) {
	return ob, nil
}

func (w *dryRunStoreWrapper) Outbox() store.OutboxStore {
	if w.Store == nil {
		return &dryRunOutboxStore{}
	}
	return &dryRunOutboxStore{OutboxStore: w.Store.Outbox()}
}

type dryRunRepoStore struct {
	store.RepositoryStore
}

func (s *dryRunRepoStore) Upsert(ctx context.Context, repo store.Repository) (store.Repository, error) {
	return repo, nil
}

func (s *dryRunRepoStore) UpdateSyncStatus(context.Context, string, string) error { return nil }
func (s *dryRunRepoStore) UpdateSettings(context.Context, string, store.RepositorySettings) error {
	return nil
}
func (s *dryRunRepoStore) DeleteRepository(context.Context, string) error { return nil }

func (w *dryRunStoreWrapper) Repositories() store.RepositoryStore {
	if w.Store == nil {
		return &dryRunRepoStore{}
	}
	return &dryRunRepoStore{RepositoryStore: w.Store.Repositories()}
}

type dryRunWorkItemStore struct {
	store.WorkItemStore
}

func (s *dryRunWorkItemStore) UpsertIfNewer(ctx context.Context, item store.WorkItem, known *store.WorkItem) (store.WorkItem, bool, error) {
	return item, true, nil
}

func (s *dryRunWorkItemStore) SetIgnored(context.Context, string, bool) error { return nil }
func (s *dryRunWorkItemStore) MarkMerged(context.Context, string, int) error  { return nil }

type dryRunInstallationStore struct {
	store.InstallationStore
}

func (s *dryRunInstallationStore) Upsert(ctx context.Context, in store.GitHubInstallation) (store.GitHubInstallation, error) {
	return in, nil
}

func (w *dryRunStoreWrapper) Installations() store.InstallationStore {
	if w.Store == nil {
		return &dryRunInstallationStore{}
	}
	return &dryRunInstallationStore{InstallationStore: w.Store.Installations()}
}

func (w *dryRunStoreWrapper) WorkItems() store.WorkItemStore {
	if w.Store == nil {
		return &dryRunWorkItemStore{}
	}
	return &dryRunWorkItemStore{WorkItemStore: w.Store.WorkItems()}
}

type dryRunWorkflowRunStore struct {
	store.WorkflowRunStore
}

func (s *dryRunWorkflowRunStore) UpsertIfNewer(ctx context.Context, run store.WorkflowRun) (store.WorkflowRun, bool, error) {
	return run, true, nil
}

func (w *dryRunStoreWrapper) WorkflowRuns() store.WorkflowRunStore {
	if w.Store == nil {
		return &dryRunWorkflowRunStore{}
	}
	return &dryRunWorkflowRunStore{WorkflowRunStore: w.Store.WorkflowRuns()}
}

type dryRunSecurityAlertStore struct {
	store.SecurityAlertStore
}

func (s *dryRunSecurityAlertStore) UpsertIfNewer(ctx context.Context, alert store.SecurityAlert) (store.SecurityAlert, bool, error) {
	return alert, true, nil
}

func (w *dryRunStoreWrapper) SecurityAlerts() store.SecurityAlertStore {
	if w.Store == nil {
		return &dryRunSecurityAlertStore{}
	}
	return &dryRunSecurityAlertStore{SecurityAlertStore: w.Store.SecurityAlerts()}
}

type dryRunRepoStatSnapshotStore struct {
	store.RepoStatSnapshotStore
}

func (s *dryRunRepoStatSnapshotStore) Upsert(ctx context.Context, snapshot store.RepoStatSnapshot) (store.RepoStatSnapshot, error) {
	return snapshot, nil
}

func (w *dryRunStoreWrapper) RepoStatSnapshots() store.RepoStatSnapshotStore {
	if w.Store == nil {
		return &dryRunRepoStatSnapshotStore{}
	}
	return &dryRunRepoStatSnapshotStore{RepoStatSnapshotStore: w.Store.RepoStatSnapshots()}
}

type dryRunChannelStore struct {
	store.ChannelStore
	channels []store.NotificationChannel
}

func (s *dryRunChannelStore) List(ctx context.Context) ([]store.NotificationChannel, error) {
	return s.channels, nil
}

type dryRunDiagnosticStore struct{}

func (d *dryRunDiagnosticStore) GetStorageDiagnostics(ctx context.Context) (store.StorageStats, error) {
	return store.StorageStats{Driver: "sqlite", FileSizeBytes: 0}, nil
}
func (d *dryRunDiagnosticStore) GetOutboxDiagnostics(ctx context.Context) (store.OutboxStats, error) {
	return store.OutboxStats{}, nil
}
func (d *dryRunDiagnosticStore) GetAIBudgetDiagnostics(ctx context.Context) (store.AIBudgetStats, error) {
	return store.AIBudgetStats{}, nil
}

type dryRunAuditStore struct {
	store.AuditStore
}

func (s *dryRunAuditStore) Append(ctx context.Context, log store.AuditLog) (store.AuditLog, error) {
	return log, nil
}

func (w *dryRunStoreWrapper) Audits() store.AuditStore {
	if w.Store == nil {
		return &dryRunAuditStore{}
	}
	return &dryRunAuditStore{AuditStore: w.Store.Audits()}
}

func (w *dryRunStoreWrapper) WithTx(ctx context.Context, fn func(store.Store) error) error {
	return fn(w)
}

func (w *dryRunStoreWrapper) Diagnostics() store.DiagnosticStore {
	if w.Store == nil {
		return &dryRunDiagnosticStore{}
	}
	return w.Store.Diagnostics()
}

func (w *dryRunStoreWrapper) Channels() store.ChannelStore {
	if len(w.customChannels) > 0 {
		return &dryRunChannelStore{channels: w.customChannels}
	}
	if w.Store == nil {
		return &dryRunChannelStore{}
	}
	return w.Store.Channels()
}

func (s *server) handleRulesDryRun(w http.ResponseWriter, r *http.Request) {
	var req dryRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}

	repoFullName := strings.TrimSpace(req.Repository)
	if repoFullName == "" {
		repoFullName = "org/repo"
	}

	var payloadBytes []byte
	if strings.TrimSpace(req.PayloadRaw) != "" {
		payloadBytes = []byte(req.PayloadRaw)
	} else if req.Payload != nil {
		payloadBytes, _ = json.Marshal(req.Payload)
	} else {
		payload := map[string]any{
			"action":     req.Action,
			"repository": map[string]any{"full_name": repoFullName},
		}
		if req.Branch != "" {
			payload["pull_request"] = map[string]any{"head": map[string]any{"ref": req.Branch}}
			if eventType == "workflow_run" {
				delete(payload, "pull_request")
				payload["workflow_run"] = map[string]any{"head_branch": req.Branch}
			}
		}
		payloadBytes, _ = json.Marshal(payload)
	}

	dryStore := &dryRunStoreWrapper{
		Store:          s.dependencies.Store,
		customChannels: req.Channels,
	}

	proc := &normalizer.Processor{Store: dryStore, Logger: s.dependencies.Logger}
	res, err := proc.Process(r.Context(), eventType, "dryrun-"+ulid.Make().String(), payloadBytes)
	if err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{
			"details": err.Error(),
		})
		return
	}

	if res.Event == nil {
		now := time.Now().UTC()
		res.Event = &store.Event{
			ID:         "event-dryrun-" + ulid.Make().String(),
			Kind:       eventType,
			Action:     req.Action,
			Title:      "Dry Run: " + eventType,
			OccurredAt: now,
		}
	}

	rulesEngine := &rules.Engine{
		Store:  dryStore,
		AI:     s.dependencies.AI,
		Logger: s.dependencies.Logger,
		GitHub: func() *githubx.AppClient {
			if s.dependencies.GitHubRuntime != nil {
				return s.dependencies.GitHubRuntime.Client
			}
			return nil
		}(),
	}

	result, err := rulesEngine.DryRun(r.Context(), res, repoFullName, req.Channels)
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
