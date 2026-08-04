package store

import (
	"context"
	"encoding/json"
	"math"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/systemsetting"
)

type settingsStore struct {
	client *entclient.Client
}

func (s *settingsStore) Get(ctx context.Context, key string) (SystemSetting, error) {
	entity, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(key)).
		Only(ctx)
	if err != nil {
		return SystemSetting{}, mapStoreError(err)
	}
	return settingFromEntity(entity), nil
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
