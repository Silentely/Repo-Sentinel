package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"entgo.io/ent/dialect"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/systemsetting"
	"github.com/oklog/ulid/v2"
)

type settingsStore struct {
	client *entclient.Client
	driver dialect.Driver
	// cache 进程内短 TTL 缓存（由 storeImpl 共享同一实例）；测试直构时为 nil。
	cache *settingsCache
}

func (s *settingsStore) getDB() *sql.DB {
	type hasDB interface{ DB() *sql.DB }
	if d, ok := s.driver.(hasDB); ok {
		return d.DB()
	}
	return nil
}

func (s *settingsStore) Get(ctx context.Context, key string) (SystemSetting, error) {
	if row, ok := s.cache.Get(key); ok {
		return row, nil
	}
	entity, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(key)).
		Only(ctx)
	if err != nil {
		return SystemSetting{}, mapStoreError(err)
	}
	row := settingFromEntity(entity)
	s.cache.Set(key, row)
	return row, nil
}

func (s *settingsStore) Create(ctx context.Context, input SystemSetting) (SystemSetting, error) {
	entity, err := s.client.SystemSetting.Create().
		SetID(input.ID).
		SetKey(input.Key).
		SetValueJSON(cloneJSON(input.ValueJSON)).
		SetUpdatedAt(input.UpdatedAt.UTC()).
		SetUpdatedBy(input.UpdatedBy).
		Save(ctx)
	if err != nil {
		if entclient.IsConstraintError(err) {
			return SystemSetting{}, ErrConflict
		}
		return SystemSetting{}, mapStoreError(err)
	}
	s.cache.Invalidate(input.Key)
	return settingFromEntity(entity), nil
}

func (s *settingsStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.SystemSetting.Delete().Where(systemsetting.KeyEQ(key)).Exec(ctx)
	if err != nil {
		return mapStoreError(err)
	}
	// 与 Create/Upsert 一致失效缓存：否则删除后同键 Get 仍命中旧值。
	s.cache.Invalidate(key)
	return nil
}

// GetMany 批量读取设置：先取缓存命中键，缺失键单次查库并回填缓存。
// 相比逐个 Get 可减少设置页渲染（handleGetSettings）的多次往返；回填后
// webhook 热路径对同键的 Get 直接命中缓存，不再重复落库。
func (s *settingsStore) GetMany(ctx context.Context, keys ...string) ([]SystemSetting, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]SystemSetting, 0, len(keys))
	missing := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			continue // 调用方传重复键时去重，避免重复查询
		}
		seen[key] = true
		if row, ok := s.cache.Get(key); ok {
			out = append(out, row)
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		entities, err := s.client.SystemSetting.Query().
			Where(systemsetting.KeyIn(missing...)).
			All(ctx)
		if err != nil {
			return nil, mapStoreError(err)
		}
		for _, entity := range entities {
			row := settingFromEntity(entity)
			out = append(out, row)
			s.cache.Set(row.Key, row)
		}
	}
	return out, nil
}

func (s *settingsStore) Upsert(ctx context.Context, input SystemSetting) (SystemSetting, error) {
	entity, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(input.Key)).
		Only(ctx)
	switch {
	case err == nil:
		return s.update(ctx, entity, input)
	case !entclient.IsNotFound(err):
		return SystemSetting{}, mapStoreError(err)
	}

	entity, err = s.client.SystemSetting.Create().
		SetID(input.ID).
		SetKey(input.Key).
		SetValueJSON(cloneJSON(input.ValueJSON)).
		SetUpdatedAt(input.UpdatedAt.UTC()).
		SetUpdatedBy(input.UpdatedBy).
		Save(ctx)
	if err == nil {
		s.cache.Invalidate(input.Key)
		return settingFromEntity(entity), nil
	}
	if !entclient.IsConstraintError(err) {
		return SystemSetting{}, mapStoreError(err)
	}
	entity, err = s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(input.Key)).
		Only(ctx)
	if err != nil {
		return SystemSetting{}, mapStoreError(err)
	}
	return s.update(ctx, entity, input)
}

func (s *settingsStore) update(
	ctx context.Context,
	entity *entclient.SystemSetting,
	input SystemSetting,
) (SystemSetting, error) {
	updated, err := entity.Update().
		SetValueJSON(cloneJSON(input.ValueJSON)).
		SetUpdatedAt(input.UpdatedAt.UTC()).
		SetUpdatedBy(input.UpdatedBy).
		Save(ctx)
	if err != nil {
		return SystemSetting{}, mapStoreError(err)
	}
	s.cache.Invalidate(input.Key)
	return settingFromEntity(updated), nil
}

func settingFromEntity(entity *entclient.SystemSetting) SystemSetting {
	return SystemSetting{
		ID:        entity.ID,
		Key:       entity.Key,
		ValueJSON: cloneJSON(entity.ValueJSON),
		UpdatedAt: entity.UpdatedAt.UTC(),
		UpdatedBy: entity.UpdatedBy,
	}
}

// SettingInt 读取整数型设置：仅接受正整数（0/负值视为未配置），
// 键不存在或 JSON 非法时返回 defaultVal。语义与 httpapi 旧 getIntSetting 一致，
// 供各包复用，避免出现多套"读整数设置"实现。
func SettingInt(ctx context.Context, settings SettingsStore, key string, defaultVal int) int {
	if settings == nil {
		return defaultVal
	}
	row, err := settings.Get(ctx, key)
	if err != nil {
		return defaultVal
	}
	var v float64
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil || v <= 0 || math.Trunc(v) != v {
		return defaultVal
	}
	return int(v)
}

// SettingBool 读取布尔型设置；键不存在或 JSON 非法时返回 defaultVal。
func SettingBool(ctx context.Context, settings SettingsStore, key string, defaultVal bool) bool {
	if settings == nil {
		return defaultVal
	}
	row, err := settings.Get(ctx, key)
	if err != nil {
		return defaultVal
	}
	var v bool
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return defaultVal
	}
	return v
}

// SettingString 读取字符串型设置；键不存在、JSON 非法或值为空串时返回 defaultVal。
func SettingString(ctx context.Context, settings SettingsStore, key, defaultVal string) string {
	if settings == nil {
		return defaultVal
	}
	row, err := settings.Get(ctx, key)
	if err != nil {
		return defaultVal
	}
	var v string
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil || v == "" {
		return defaultVal
	}
	return v
}

// UpdateAIBudgetUsageAtomic 原子累加当日 AI 预算用量，并返回是否触发软限流熔断。
// 双轨方言安全实现，显式补齐 updated_by = 'ai_budget' 哨兵值。
func (s *settingsStore) UpdateAIBudgetUsageAtomic(ctx context.Context, todayKey string, tokens int, costCents int, budgetLimitCents int) (bool, error) {
	db := s.getDB()
	id := ulid.Make().String()
	var isThrottled bool

	if db != nil {
		if s.driver != nil && s.driver.Dialect() == dialect.Postgres {
			query := "INSERT INTO system_settings (id, key, value_json, updated_by, updated_at) " +
				"VALUES ($1, $2, jsonb_build_object('calls', 1, 'tokens_est', $3::int, 'cost_est_cents', $4::int, 'is_throttled', ($4 >= $5)), 'ai_budget', now()) " +
				"ON CONFLICT (key) DO UPDATE " +
				"SET value_json = jsonb_set(" +
				"      jsonb_set(" +
				"        jsonb_set(" +
				"          jsonb_set(system_settings.value_json, '{calls}', " +
				"            ((COALESCE(system_settings.value_json->>'calls', '0')::int + 1)::text)::jsonb), " +
				"          '{tokens_est}', " +
				"            ((COALESCE(system_settings.value_json->>'tokens_est', '0')::int + $3)::text)::jsonb), " +
				"        '{cost_est_cents}', " +
				"          ((COALESCE(system_settings.value_json->>'cost_est_cents', '0')::int + $4)::text)::jsonb), " +
				"      '{is_throttled}', " +
				"        (((COALESCE(system_settings.value_json->>'cost_est_cents', '0')::int + $4) >= $5)::text)::jsonb), " +
				"    updated_by = 'ai_budget', " +
				"    updated_at = now() " +
				"RETURNING (value_json->>'is_throttled')::boolean;"
			row := db.QueryRowContext(ctx, query, id, todayKey, tokens, costCents, budgetLimitCents)
			var rawVal any
			if err := row.Scan(&rawVal); err != nil {
				return false, mapStoreError(err)
			}
			isThrottled = parseBoolScan(rawVal)
		} else {
			query := "INSERT INTO system_settings (id, key, value_json, updated_by, updated_at) " +
				"VALUES (?, ?, json_object('calls', 1, 'tokens_est', ?, 'cost_est_cents', ?, 'is_throttled', ? >= ?), 'ai_budget', CURRENT_TIMESTAMP) " +
				"ON CONFLICT (key) DO UPDATE " +
				"SET value_json = json_set(" +
				"      value_json, " +
				"      '$.calls', COALESCE(json_extract(value_json, '$.calls'), 0) + 1, " +
				"      '$.tokens_est', COALESCE(json_extract(value_json, '$.tokens_est'), 0) + ?, " +
				"      '$.cost_est_cents', COALESCE(json_extract(value_json, '$.cost_est_cents'), 0) + ?, " +
				"      '$.is_throttled', (COALESCE(json_extract(value_json, '$.cost_est_cents'), 0) + ?) >= ? " +
				"    ), " +
				"    updated_by = 'ai_budget', " +
				"    updated_at = CURRENT_TIMESTAMP " +
				"RETURNING json_extract(value_json, '$.is_throttled');"
			row := db.QueryRowContext(ctx, query, id, todayKey, tokens, costCents, costCents, budgetLimitCents, tokens, costCents, costCents, budgetLimitCents)
			var rawVal any
			if err := row.Scan(&rawVal); err != nil {
				return false, mapStoreError(err)
			}
			isThrottled = parseBoolScan(rawVal)
		}
	} else {
		return false, fmt.Errorf("underlying sql.DB unavailable")
	}

	if s.cache != nil {
		s.cache.Invalidate(todayKey)
	}
	return isThrottled, nil
}

func parseBoolScan(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int64:
		return val != 0
	case int:
		return val != 0
	case string:
		return val == "true" || val == "1" || val == "t"
	case []byte:
		s := string(val)
		return s == "true" || s == "1" || s == "t"
	default:
		return false
	}
}
