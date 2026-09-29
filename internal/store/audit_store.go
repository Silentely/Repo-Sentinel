package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/auditlog"
)

type auditStore struct {
	client *entclient.Client
}

func (s *auditStore) Append(ctx context.Context, input AuditLog) (AuditLog, error) {
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	sanitizedMeta := sanitizeAuditMetadata(input.MetadataJSON)
	entity, err := s.client.AuditLog.Create().
		SetID(input.ID).
		SetAction(input.Action).
		SetActorType(input.ActorType).
		SetActorID(input.ActorID).
		SetTargetType(input.TargetType).
		SetTargetID(input.TargetID).
		SetMetadataJSON(cloneJSON(sanitizedMeta)).
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

func (s *auditStore) ListCursor(ctx context.Context, cursorTime time.Time, cursorID string, limit int) ([]AuditLog, error) {
	if limit <= 0 {
		return []AuditLog{}, nil
	}
	q := s.client.AuditLog.Query()
	if !cursorTime.IsZero() {
		q = q.Where(
			auditlog.Or(
				auditlog.CreatedAtLT(cursorTime.UTC()),
				auditlog.And(
					auditlog.CreatedAtEQ(cursorTime.UTC()),
					auditlog.IDLT(cursorID),
				),
			),
		)
	}
	entities, err := q.
		Order(entclient.Desc(auditlog.FieldCreatedAt), entclient.Desc(auditlog.FieldID)).
		Limit(limit).
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

func sanitizeAuditMetadata(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return raw
	}
	sanitized := sanitizeAny(data)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return raw
	}
	return out
}

func sanitizeAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, child := range val {
			lowerK := strings.ToLower(k)
			if isSensitiveKey(lowerK) {
				res[k] = maskSensitiveValue(child)
			} else {
				res[k] = sanitizeAny(child)
			}
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, child := range val {
			res[i] = sanitizeAny(child)
		}
		return res
	case string:
		if isSensitiveString(val) {
			return maskToken(val)
		}
		return val
	default:
		return v
	}
}

func isSensitiveKey(k string) bool {
	return strings.Contains(k, "password") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "apikey") ||
		k == "key"
}

func maskSensitiveValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return "***"
	}
	return maskToken(s)
}

func maskToken(s string) string {
	if strings.HasPrefix(s, "ghp_") {
		return "ghp_***"
	}
	if strings.HasPrefix(s, "sk-") {
		return "sk-***"
	}
	return "***"
}

func isSensitiveString(s string) bool {
	return strings.HasPrefix(s, "ghp_") || strings.HasPrefix(s, "sk-") || strings.HasPrefix(s, "Bearer ")
}

func (s *auditStore) Get(ctx context.Context, id string) (AuditLog, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AuditLog{}, ErrNotFound
	}
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
