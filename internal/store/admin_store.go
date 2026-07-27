package store

import (
	"context"
	"strings"
	"time"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/adminaccount"
)

type adminStore struct {
	client *entclient.Client
}

func (s *adminStore) Create(ctx context.Context, input AdminAccount) (AdminAccount, error) {
	entity, err := s.client.AdminAccount.Create().
		SetID(input.ID).
		SetUsername(input.Username).
		SetUsernameNormalized(normalizeUsername(input.Username)).
		SetPasswordHash(input.PasswordHash).
		SetCreatedAt(input.CreatedAt.UTC()).
		SetUpdatedAt(input.UpdatedAt.UTC()).
		SetPasswordChangedAt(input.PasswordChangedAt.UTC()).
		Save(ctx)
	if err != nil {
		return AdminAccount{}, mapStoreError(err)
	}
	return adminFromEntity(entity), nil
}

func (s *adminStore) Get(ctx context.Context, id string) (AdminAccount, error) {
	entity, err := s.client.AdminAccount.Get(ctx, id)
	if err != nil {
		return AdminAccount{}, mapStoreError(err)
	}
	return adminFromEntity(entity), nil
}

func (s *adminStore) GetOnly(ctx context.Context) (AdminAccount, error) {
	entity, err := s.client.AdminAccount.Query().Only(ctx)
	if err != nil {
		return AdminAccount{}, mapStoreError(err)
	}
	return adminFromEntity(entity), nil
}

func (s *adminStore) FindByUsername(ctx context.Context, username string) (AdminAccount, error) {
	entity, err := s.client.AdminAccount.Query().
		Where(adminaccount.UsernameNormalizedEQ(normalizeUsername(username))).
		Only(ctx)
	if err != nil {
		return AdminAccount{}, mapStoreError(err)
	}
	return adminFromEntity(entity), nil
}

func (s *adminStore) UpdatePassword(
	ctx context.Context,
	id string,
	passwordHash string,
	changedAt time.Time,
) (AdminAccount, error) {
	changedAt = changedAt.UTC()
	entity, err := s.client.AdminAccount.UpdateOneID(id).
		SetPasswordHash(passwordHash).
		SetPasswordChangedAt(changedAt).
		SetUpdatedAt(changedAt).
		Save(ctx)
	if err != nil {
		return AdminAccount{}, mapStoreError(err)
	}
	return adminFromEntity(entity), nil
}

func (s *adminStore) UpdatePasswordIfCurrent(
	ctx context.Context,
	id string,
	expectedHash string,
	newHash string,
	changedAt time.Time,
) (bool, error) {
	changedAt = changedAt.UTC()
	updated, err := s.client.AdminAccount.Update().
		Where(
			adminaccount.IDEQ(id),
			adminaccount.PasswordHashEQ(expectedHash),
		).
		SetPasswordHash(newHash).
		SetPasswordChangedAt(changedAt).
		SetUpdatedAt(changedAt).
		Save(ctx)
	if err != nil {
		return false, mapStoreError(err)
	}
	return updated == 1, nil
}

func (s *adminStore) DeleteForTest(ctx context.Context, id string) error {
	return mapStoreError(s.client.AdminAccount.DeleteOneID(id).Exec(ctx))
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func adminFromEntity(entity *entclient.AdminAccount) AdminAccount {
	return AdminAccount{
		ID:                entity.ID,
		Username:          entity.Username,
		PasswordHash:      entity.PasswordHash,
		CreatedAt:         entity.CreatedAt.UTC(),
		UpdatedAt:         entity.UpdatedAt.UTC(),
		PasswordChangedAt: entity.PasswordChangedAt.UTC(),
	}
}
