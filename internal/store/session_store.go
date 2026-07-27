package store

import (
	"context"
	"time"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/adminsession"
)

type sessionStore struct {
	client *entclient.Client
}

func (s *sessionStore) Create(ctx context.Context, input AdminSession) (AdminSession, error) {
	entity, err := s.client.AdminSession.Create().
		SetID(input.ID).
		SetAdminID(input.AdminID).
		SetTokenHash(input.TokenHash).
		SetCsrfHash(input.CSRFHash).
		SetCreatedAt(input.CreatedAt.UTC()).
		SetExpiresAt(input.ExpiresAt.UTC()).
		SetLastSeenAt(input.LastSeenAt.UTC()).
		SetIPAddress(input.IPAddress).
		SetUserAgent(input.UserAgent).
		Save(ctx)
	if err != nil {
		return AdminSession{}, mapStoreError(err)
	}
	return sessionFromEntity(entity), nil
}

func (s *sessionStore) GetActiveByTokenHash(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (AdminSession, error) {
	entity, err := s.client.AdminSession.Query().
		Where(
			adminsession.TokenHashEQ(tokenHash),
			adminsession.ExpiresAtGT(now.UTC()),
		).
		Only(ctx)
	if err != nil {
		return AdminSession{}, mapStoreError(err)
	}
	return sessionFromEntity(entity), nil
}

func (s *sessionStore) Revoke(ctx context.Context, id string) error {
	return mapStoreError(s.client.AdminSession.DeleteOneID(id).Exec(ctx))
}

func (s *sessionStore) DeleteOthers(ctx context.Context, adminID, keepSessionID string) (int, error) {
	deleted, err := s.client.AdminSession.Delete().
		Where(
			adminsession.AdminIDEQ(adminID),
			adminsession.IDNEQ(keepSessionID),
		).
		Exec(ctx)
	if err != nil {
		return 0, mapStoreError(err)
	}
	return deleted, nil
}

func (s *sessionStore) Touch(ctx context.Context, id string, lastSeenAt time.Time) (AdminSession, error) {
	entity, err := s.client.AdminSession.UpdateOneID(id).
		SetLastSeenAt(lastSeenAt.UTC()).
		Save(ctx)
	if err != nil {
		return AdminSession{}, mapStoreError(err)
	}
	return sessionFromEntity(entity), nil
}

func (s *sessionStore) CleanupExpired(ctx context.Context, now time.Time) (int, error) {
	deleted, err := s.client.AdminSession.Delete().
		Where(adminsession.ExpiresAtLTE(now.UTC())).
		Exec(ctx)
	if err != nil {
		return 0, mapStoreError(err)
	}
	return deleted, nil
}

func sessionFromEntity(entity *entclient.AdminSession) AdminSession {
	return AdminSession{
		ID:         entity.ID,
		AdminID:    entity.AdminID,
		TokenHash:  entity.TokenHash,
		CSRFHash:   entity.CsrfHash,
		CreatedAt:  entity.CreatedAt.UTC(),
		ExpiresAt:  entity.ExpiresAt.UTC(),
		LastSeenAt: entity.LastSeenAt.UTC(),
		IPAddress:  entity.IPAddress,
		UserAgent:  entity.UserAgent,
	}
}
