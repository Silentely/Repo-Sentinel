package store

import (
	"context"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/auditlog"
)

type auditStore struct {
	client *entclient.Client
}

func (s *auditStore) Append(ctx context.Context, input AuditLog) (AuditLog, error) {
	entity, err := s.client.AuditLog.Create().
		SetID(input.ID).
		SetAction(input.Action).
		SetActorType(input.ActorType).
		SetActorID(input.ActorID).
		SetTargetType(input.TargetType).
		SetTargetID(input.TargetID).
		SetMetadataJSON(cloneJSON(input.MetadataJSON)).
		SetIPAddress(input.IPAddress).
		SetCreatedAt(input.CreatedAt.UTC()).
		Save(ctx)
	if err != nil {
		return AuditLog{}, mapStoreError(err)
	}
	return auditFromEntity(entity), nil
}

func (s *auditStore) List(ctx context.Context, limit, offset int) ([]AuditLog, error) {
	if limit <= 0 {
		return []AuditLog{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	entities, err := s.client.AuditLog.Query().
		Order(entclient.Desc(auditlog.FieldCreatedAt)).
		Limit(limit).
		Offset(offset).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	logs := make([]AuditLog, 0, len(entities))
	for _, entity := range entities {
		logs = append(logs, auditFromEntity(entity))
	}
	return logs, nil
}

func (s *auditStore) Get(ctx context.Context, id string) (AuditLog, error) {
	entity, err := s.client.AuditLog.Get(ctx, id)
	if err != nil {
		return AuditLog{}, mapStoreError(err)
	}
	return auditFromEntity(entity), nil
}

func auditFromEntity(entity *entclient.AuditLog) AuditLog {
	return AuditLog{
		ID:           entity.ID,
		Action:       entity.Action,
		ActorType:    entity.ActorType,
		ActorID:      entity.ActorID,
		TargetType:   entity.TargetType,
		TargetID:     entity.TargetID,
		MetadataJSON: cloneJSON(entity.MetadataJSON),
		IPAddress:    entity.IPAddress,
		CreatedAt:    entity.CreatedAt.UTC(),
	}
}
