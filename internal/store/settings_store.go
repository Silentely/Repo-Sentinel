package store

import (
	"context"

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
