package sideeffect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// memSettingsStore 是纯内存的 SettingsStore 实现，用于隔离测试 ExecuteWithReceipt 的生命周期。
type memSettingsStore struct {
	mu       sync.Mutex
	settings map[string]store.SystemSetting
}

func newMemSettingsStore() *memSettingsStore {
	return &memSettingsStore{
		settings: make(map[string]store.SystemSetting),
	}
}

func (m *memSettingsStore) Create(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.settings[s.Key]; exists {
		// 与真实 store 保持一致：唯一键冲突返回 ErrConflict。
		return store.SystemSetting{}, store.ErrConflict
	}
	m.settings[s.Key] = s
	return s, nil
}

func (m *memSettingsStore) Upsert(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[s.Key] = s
	return s, nil
}

func (m *memSettingsStore) Get(ctx context.Context, key string) (store.SystemSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.settings[key]
	if !ok {
		return store.SystemSetting{}, errors.New("not found")
	}
	return s, nil
}

func (m *memSettingsStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.settings, key)
	return nil
}

func (m *memSettingsStore) GetMany(ctx context.Context, keys ...string) ([]store.SystemSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []store.SystemSetting
	for _, k := range keys {
		if s, ok := m.settings[k]; ok {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *memSettingsStore) UpdateAIBudgetUsageAtomic(ctx context.Context, todayKey string, tokens int, costCents int, budgetLimitCents int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return false, nil
}

// TestSideEffects_Concurrency 验证并发 10 次调用 ExecuteWithReceipt 时，底层网络执行函数仅被调用 1 次。
func TestSideEffects_Concurrency(t *testing.T) {
	memStore := newMemSettingsStore()
	ctx := context.Background()

	var callCount int64
	var wg sync.WaitGroup
	const concurrency = 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ExecuteWithReceipt(ctx, memStore, "rerun", "repo-1", "101", "v1", "action-1", func() (string, error) {
				atomic.AddInt64(&callCount, 1)
				time.Sleep(10 * time.Millisecond) // 模拟网络延迟
				return "run-id-999", nil
			})
		}()
	}

	wg.Wait()

	if calls := atomic.LoadInt64(&callCount); calls != 1 {
		t.Fatalf("外部副作用函数期望仅调用 1 次，实际调用 %d 次", calls)
	}

	// 验证最终落库回执状态为 succeeded
	key := SideEffectReceiptKey("rerun", "repo-1", "101", "v1", "action-1")
	setting, err := memStore.Get(ctx, key)
	if err != nil {
		t.Fatalf("回查回执失败: %v", err)
	}
	var rec SideEffectReceipt
	if err := json.Unmarshal(setting.ValueJSON, &rec); err != nil {
		t.Fatalf("反序列化回执失败: %v", err)
	}
	if rec.State != ReceiptSucceeded || rec.ExternalID != "run-id-999" {
		t.Errorf("回执状态不符合预期: state=%s, external_id=%s", rec.State, rec.ExternalID)
	}
}

// TestSideEffects_PreSendError 验证前置参数错误时（未发出网络调用），回执被立即删除并允许后续重试。
func TestSideEffects_PreSendError(t *testing.T) {
	memStore := newMemSettingsStore()
	ctx := context.Background()

	key := SideEffectReceiptKey("comment", "repo-1", "102", "v1", "head-sha-1")

	// 第 1 次执行：返回前置参数错误
	err := ExecuteWithReceipt(ctx, memStore, "comment", "repo-1", "102", "v1", "head-sha-1", func() (string, error) {
		return "", MarkPreSendError(errors.New("invalid parameter: token missing"))
	})
	if err == nil || !IsPreSendError(err) {
		t.Fatalf("期望返回 PreSendError, 实际: %v", err)
	}

	// 回执必须已被删除
	if _, err := memStore.Get(ctx, key); err == nil {
		t.Fatal("前置失败时期望回执被安全删除，但仍能查到")
	}

	// 第 2 次重试：修复后成功执行
	var secondCallExecuted bool
	err = ExecuteWithReceipt(ctx, memStore, "comment", "repo-1", "102", "v1", "head-sha-1", func() (string, error) {
		secondCallExecuted = true
		return "comment-id-123", nil
	})
	if err != nil {
		t.Fatalf("第 2 次重试失败: %v", err)
	}
	if !secondCallExecuted {
		t.Fatal("第 2 次应当正常执行并被调用")
	}
}

// TestSideEffects_NetworkUnknown 验证网络不确定态（超时、断开）时，回执绝不被删除，严格保留为 unknown 杜绝重复调用。
func TestSideEffects_NetworkUnknown(t *testing.T) {
	memStore := newMemSettingsStore()
	ctx := context.Background()

	key := SideEffectReceiptKey("rerun", "repo-1", "103", "v1", "action-run-1")

	// 第 1 次执行：发生网络超时错误
	timeoutErr := context.DeadlineExceeded
	err := ExecuteWithReceipt(ctx, memStore, "rerun", "repo-1", "103", "v1", "action-run-1", func() (string, error) {
		return "", timeoutErr
	})
	if !errors.Is(err, timeoutErr) {
		t.Fatalf("期望返回超时错误, 实际: %v", err)
	}

	// 回执必须绝不被删除，且状态为 unknown
	setting, err := memStore.Get(ctx, key)
	if err != nil {
		t.Fatalf("网络超时后回执必须留存，但查询失败: %v", err)
	}
	var rec SideEffectReceipt
	if err := json.Unmarshal(setting.ValueJSON, &rec); err != nil {
		t.Fatalf("反序列化回执失败: %v", err)
	}
	if rec.State != ReceiptUnknown {
		t.Fatalf("网络超时后回执状态必须为 unknown, 实际: %s", rec.State)
	}

	// 第 2 次执行：由于回执处于 unknown 态（远端可能已生效），必须幂等跳过，禁止重复调用外部 API
	var retryCallExecuted bool
	err = ExecuteWithReceipt(ctx, memStore, "rerun", "repo-1", "103", "v1", "action-run-1", func() (string, error) {
		retryCallExecuted = true
		return "run-id-should-not-reach", nil
	})
	if err != nil {
		t.Fatalf("重试时遇到未知态回执应幂等静默跳过，实际返回错误: %v", err)
	}
	if retryCallExecuted {
		t.Fatal("在 unknown 态下重试绝不应再次调用外部 API（防止重复评论/重复执行）")
	}
}

// TestSideEffects_GitHub422_LabelHandling 验证 GitHub AddIssueLabels 精细化 422 响应体解析：
// 1. missing label 返回包装 ErrLabelNotFound
// 2. already_exists 视为幂等成功返回 nil
// 3. 其他 422 拒绝（如 Issue 锁定）正常返回 statusError 而非 ErrLabelNotFound
func TestSideEffects_GitHub422_LabelHandling(t *testing.T) {
	tests := []struct {
		name          string
		responseCode  int
		responseJSON  string
		wantErrIsNF   bool
		wantErrNil    bool
		wantErrStatus bool
	}{
		{
			name:         "missing label in errors array",
			responseCode: http.StatusUnprocessableEntity,
			responseJSON: `{"message":"Validation Failed","errors":[{"resource":"Issue","field":"labels","code":"missing_field","message":"Label does not exist"}]}`,
			wantErrIsNF:  true,
		},
		{
			name:         "label already exists on issue",
			responseCode: http.StatusUnprocessableEntity,
			responseJSON: `{"message":"Validation Failed","errors":[{"resource":"Issue","field":"labels","code":"already_exists"}]}`,
			wantErrNil:   true,
		},
		{
			name:          "issue locked validation failed",
			responseCode:  http.StatusUnprocessableEntity,
			responseJSON:  `{"message":"Validation Failed","errors":[{"resource":"Issue","field":"base","code":"custom","message":"Issue is locked"}]}`,
			wantErrStatus: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.responseCode)
				_, _ = w.Write([]byte(tc.responseJSON))
			}))
			defer server.Close()

			client := githubx.NewAppClient(1, "")
			client.BaseURL = server.URL
			client.HTTP = server.Client()

			err := client.AddIssueLabels(context.Background(), "token", "owner", "repo", 42, []string{"sentinel:bug"})
			if tc.wantErrNil {
				if err != nil {
					t.Fatalf("期望返回 nil, 实际: %v", err)
				}
				return
			}
			if tc.wantErrIsNF {
				if err == nil || !githubx.IsLabelNotFoundError(err) {
					t.Fatalf("期望错误匹配 ErrLabelNotFound, 实际: %v", err)
				}
				return
			}
			if tc.wantErrStatus {
				if err == nil {
					t.Fatal("期望返回错误，实际为 nil")
				}
				if githubx.IsLabelNotFoundError(err) {
					t.Fatalf("非标签丢失错误不应误报为 ErrLabelNotFound: %v", err)
				}
			}
		})
	}
}

// createErrorStore 包装 memSettingsStore，模拟 Create 返回非冲突错误（DB 抖动）。
type createErrorStore struct {
	*memSettingsStore
}

func (c *createErrorStore) Create(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	return store.SystemSetting{}, errors.New("database temporary unavailable")
}

// TestSideEffects_CreateErrorSurfaces 验证回执写入遇非冲突错误时返回错误，绝不把「未执行」伪装成成功。
func TestSideEffects_CreateErrorSurfaces(t *testing.T) {
	store := &createErrorStore{memSettingsStore: newMemSettingsStore()}
	ctx := context.Background()

	executed := false
	err := ExecuteWithReceipt(ctx, store, "rerun", "repo-1", "104", "v1", "action-1", func() (string, error) {
		executed = true
		return "run-id", nil
	})
	if err == nil {
		t.Fatal("非冲突写入错误应返回错误，而不是静默成功")
	}
	if executed {
		t.Fatal("回执写入失败时绝不应执行外部副作用")
	}
}

// TestSideEffects_StalePending_BlockedWithoutReplay 验证已存在 pending 回执时，严格视作在途或结果待对账，
// 绝不能仅凭超时无条件删除重放，杜绝导致重复外部副作用。
func TestSideEffects_StalePending_BlockedWithoutReplay(t *testing.T) {
	memStore := newMemSettingsStore()
	ctx := context.Background()

	key := SideEffectReceiptKey("rerun", "repo-1", "105", "v1", "action-1")
	stale := SideEffectReceipt{State: ReceiptPending, CreatedAt: time.Now().UTC().Add(-3 * time.Minute), UpdatedAt: time.Now().UTC().Add(-3 * time.Minute)}
	raw, _ := json.Marshal(stale)
	if _, err := memStore.Upsert(ctx, store.SystemSetting{ID: "seed", Key: key, ValueJSON: raw, UpdatedAt: time.Now().UTC().Add(-3 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	executed := false
	detail, err := ExecuteWithReceiptDetail(ctx, memStore, "rerun", "repo-1", "105", "v1", "action-1", func() (string, error) {
		executed = true
		return "run-id-new", nil
	})
	if err != nil {
		t.Fatalf("遇到 pending 记录不应报错，got err: %v", err)
	}
	if executed {
		t.Fatal("遇到 pending 回执绝不应再次调用外部 API（防止重复评论/重复执行）")
	}
	if detail.State != ReceiptPending {
		t.Fatalf("回执详情状态期望 pending，实际为: %s", detail.State)
	}
}

// upsertErrorStore 包装 memSettingsStore，可按需模拟 Upsert 持久化回执失败。
type upsertErrorStore struct {
	*memSettingsStore
	failUpsert bool
}

func (u *upsertErrorStore) Upsert(ctx context.Context, s store.SystemSetting) (store.SystemSetting, error) {
	if u.failUpsert {
		return store.SystemSetting{}, errors.New("simulated disk full error on upsert")
	}
	return u.memSettingsStore.Upsert(ctx, s)
}

// TestSideEffects_ReceiptPersistFailure_BlocksAndExposesError 验证：
// 当外部副作用已成功、但本地成功回执落库失败时，绝对不能静默忽略返回 nil（避免重试破坏幂等），
// 必须向调用方显式暴露持久化错误。
func TestSideEffects_ReceiptPersistFailure_BlocksAndExposesError(t *testing.T) {
	base := newMemSettingsStore()
	storeWithErr := &upsertErrorStore{
		memSettingsStore: base,
		failUpsert:       true,
	}
	ctx := context.Background()

	executed := false
	err := ExecuteWithReceipt(ctx, storeWithErr, "issue_close", "repo-1", "201", "v1", "action-201", func() (string, error) {
		executed = true
		return "issue-201-closed", nil
	})

	if !executed {
		t.Fatal("外部副作用函数应当被执行")
	}
	if err == nil {
		t.Fatal("成功回执落库失败时必须显式返回错误，绝不可静默返回 nil")
	}
	if !strings.Contains(err.Error(), "receipt persist failed") {
		t.Fatalf("期望错误包含 receipt persist failed，实际为: %v", err)
	}

	// 并发重试场景：在回执落库失败后，验证并发重试时绝不会执行二次外部副作用（fn 绝不可被再次调用）
	var wg sync.WaitGroup
	var concurrentCalls int64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ExecuteWithReceipt(ctx, storeWithErr, "issue_close", "repo-1", "201", "v1", "action-201", func() (string, error) {
				atomic.AddInt64(&concurrentCalls, 1)
				return "issue-201-retry", nil
			})
		}()
	}
	wg.Wait()

	if calls := atomic.LoadInt64(&concurrentCalls); calls != 0 {
		t.Fatalf("并发重试时外部副作用绝不可再次调用，期望 0 次，实际被调用 %d 次", calls)
	}
}

// TestSideEffects_UnknownReceiptPersistFailure 验证：
// 当网络不确定且 unknown 回执落库失败时，必须返回复合错误明确警示远端状态不确定。
func TestSideEffects_UnknownReceiptPersistFailure(t *testing.T) {
	base := newMemSettingsStore()
	storeWithErr := &upsertErrorStore{
		memSettingsStore: base,
		failUpsert:       true,
	}
	ctx := context.Background()

	timeoutErr := context.DeadlineExceeded
	err := ExecuteWithReceipt(ctx, storeWithErr, "rerun", "repo-1", "202", "v1", "action-202", func() (string, error) {
		return "", timeoutErr
	})

	if err == nil {
		t.Fatal("unknown 回执持久化失败时必须返回错误")
	}
	if !strings.Contains(err.Error(), "unknown-receipt persist failed") {
		t.Fatalf("期望错误包含 unknown-receipt persist failed，实际为: %v", err)
	}
}
