package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

type jevConfigResponse struct {
	Enabled                 bool               `json:"enabled"`
	BaseURL                 string             `json:"base_url"`
	HasAPIKey               bool               `json:"has_api_key"`
	APIKeySource            string             `json:"api_key_source"` // "env" | "db" | "none"
	Model                   string             `json:"model"`
	Mode                    string             `json:"mode"`
	Retries                 int                `json:"retries"`
	NoiseFilterEnabled      bool               `json:"noise_filter_enabled"`
	RoutingEnabled          bool               `json:"routing_enabled"`
	CascadeGateEnabled      bool               `json:"cascade_gate_enabled"`
	ChatOpsGuardrailEnabled bool               `json:"chatops_guardrail_enabled"`
	AllowPrivateState       bool               `json:"allow_private_state"`
	Notification            config.PlaneConfig `json:"notification"`
	Review                  config.PlaneConfig `json:"review"`
	ChatOps                 config.PlaneConfig `json:"chatops"`

	EmergencyAdvisoryUntil  *string `json:"emergency_advisory_until,omitempty"`
	EmergencyAdvisoryReason string  `json:"emergency_advisory_reason,omitempty"`
}

type updateJevConfigRequest struct {
	Enabled                 *bool               `json:"enabled"`
	BaseURL                 *string             `json:"base_url"`
	APIKey                  *string             `json:"api_key"`
	ClearAPIKey             bool                `json:"clear_api_key"`
	Model                   *string             `json:"model"`
	Mode                    *string             `json:"mode"`
	Retries                 *int                `json:"retries"`
	NoiseFilterEnabled      *bool               `json:"noise_filter_enabled"`
	RoutingEnabled          *bool               `json:"routing_enabled"`
	CascadeGateEnabled      *bool               `json:"cascade_gate_enabled"`
	ChatOpsGuardrailEnabled *bool               `json:"chatops_guardrail_enabled"`
	AllowPrivateState       *bool               `json:"allow_private_state"`
	Notification            *config.PlaneConfig `json:"notification"`
	Review                  *config.PlaneConfig `json:"review"`
	ChatOps                 *config.PlaneConfig `json:"chatops"`

	EmergencyAdvisoryUntil  *time.Time `json:"emergency_advisory_until"`
	EmergencyAdvisoryReason string     `json:"emergency_advisory_reason"`
}

func (s *server) handleGetJevConfig(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.JevRuntime == nil {
		s.writeAPIError(w, r, http.StatusNotFound, "jev_not_configured", nil)
		return
	}

	snap := s.dependencies.JevRuntime.Snapshot()
	hasKey := snap.APIKey.Reveal() != ""
	keySource := "none"
	if hasKey {
		if s.dependencies.Config.Jev.APIKey.Reveal() != "" {
			keySource = "env"
		} else {
			keySource = "db"
		}
	}

	until, reason := s.dependencies.JevRuntime.EmergencyAdvisory()
	var untilStr *string
	if !until.IsZero() && time.Now().UTC().Before(until) {
		formatted := until.Format(time.RFC3339)
		untilStr = &formatted
	}

	resp := jevConfigResponse{
		Enabled:                 snap.Enabled,
		BaseURL:                 snap.BaseURL,
		HasAPIKey:               hasKey,
		APIKeySource:            keySource,
		Model:                   snap.Model,
		Mode:                    snap.Mode,
		Retries:                 snap.Retries,
		NoiseFilterEnabled:      snap.NoiseFilterEnabled,
		RoutingEnabled:          snap.RoutingEnabled,
		CascadeGateEnabled:      snap.CascadeGateEnabled,
		ChatOpsGuardrailEnabled: snap.ChatOpsGuardrailEnabled,
		AllowPrivateState:       snap.AllowPrivateState,
		Notification:            snap.Notification,
		Review:                  snap.Review,
		ChatOps:                 snap.ChatOps,
		EmergencyAdvisoryUntil:  untilStr,
		EmergencyAdvisoryReason: reason,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handlePutJevConfig(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.JevRuntime == nil || s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusNotFound, "jev_not_configured", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req updateJevConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"error": "invalid json"})
		return
	}

	var existingStored jev.StoredConfig
	if existingSetting, err := s.dependencies.Store.Settings().Get(r.Context(), jev.RuntimeSettingKey); err == nil {
		_ = json.Unmarshal([]byte(existingSetting.ValueJSON), &existingStored)
	}

	current := s.dependencies.JevRuntime.Snapshot()
	candidate := current

	// 处理清除与更新 Key
	newKeyPlain := ""
	if req.ClearAPIKey {
		if (req.Enabled != nil && *req.Enabled) || (req.Enabled == nil && candidate.Enabled) {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"error": "clear_api_key requires jev to be disabled"})
			return
		}
		candidate.APIKey = config.NewSecret("")
	} else if req.APIKey != nil {
		trimmedKey := strings.TrimSpace(*req.APIKey)
		if trimmedKey != "" {
			candidate.APIKey = config.NewSecret(trimmedKey)
			newKeyPlain = trimmedKey
		}
	}

	if req.Enabled != nil {
		candidate.Enabled = *req.Enabled
	}
	if req.BaseURL != nil && strings.TrimSpace(*req.BaseURL) != "" {
		candidate.BaseURL = strings.TrimSpace(*req.BaseURL)
	}
	if req.Model != nil && strings.TrimSpace(*req.Model) != "" {
		candidate.Model = strings.TrimSpace(*req.Model)
	}
	if req.Mode != nil && strings.TrimSpace(*req.Mode) != "" {
		newMode := strings.TrimSpace(*req.Mode)
		if newMode != "shadow" && newMode != "active" {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "mode", "error": "mode must be 'shadow' or 'active'"})
			return
		}
		if newMode == "active" {
			setting, err := s.dependencies.Store.Settings().Get(r.Context(), jev.EvaluationSettingKey)
			if err != nil {
				s.writeAPIError(w, r, http.StatusBadRequest, "activation_evidence_missing", map[string]any{"error": "cannot switch to active without offline and shadow evaluation evidence: no summary record"})
				return
			}
			var summary jev.EvaluationSummary
			if err := json.Unmarshal([]byte(setting.ValueJSON), &summary); err != nil {
				s.writeAPIError(w, r, http.StatusBadRequest, "activation_evidence_invalid", map[string]any{"error": "cannot switch to active: evaluation summary corrupt"})
				return
			}
			if summary.Model != candidate.Model {
				s.writeAPIError(w, r, http.StatusBadRequest, "activation_evidence_mismatch", map[string]any{"error": fmt.Sprintf("model mismatch: evaluated %s, candidate %s", summary.Model, candidate.Model)})
				return
			}
			if err := jev.ValidateActivation(summary); err != nil {
				s.writeAPIError(w, r, http.StatusBadRequest, "activation_gate_failed", map[string]any{"error": err.Error()})
				return
			}
		}
		candidate.Mode = newMode
	}
	if req.Retries != nil {
		candidate.Retries = *req.Retries
	}
	if req.NoiseFilterEnabled != nil {
		candidate.NoiseFilterEnabled = *req.NoiseFilterEnabled
	}
	if req.RoutingEnabled != nil {
		candidate.RoutingEnabled = *req.RoutingEnabled
	}
	if req.CascadeGateEnabled != nil {
		candidate.CascadeGateEnabled = *req.CascadeGateEnabled
	}
	if req.ChatOpsGuardrailEnabled != nil {
		candidate.ChatOpsGuardrailEnabled = *req.ChatOpsGuardrailEnabled
	}
	if req.AllowPrivateState != nil {
		candidate.AllowPrivateState = *req.AllowPrivateState
	}
	if req.Notification != nil {
		candidate.Notification = *req.Notification
	}
	if req.Review != nil {
		candidate.Review = *req.Review
	}
	if req.ChatOps != nil {
		candidate.ChatOps = *req.ChatOps
	}

	// 基础校验
	if candidate.Enabled && candidate.APIKey.Reveal() == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "jev.api_key", "error": "api key is required when enabled"})
		return
	}
	if candidate.BaseURL != "" {
		parsed, err := url.Parse(candidate.BaseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasPrefix(parsed.Path, "/v1") || (parsed.Path != "" && parsed.Path != "/") {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "jev.base_url", "error": "invalid base url"})
			return
		}
	}
	if candidate.Retries < 0 || candidate.Retries > 1 {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "jev.retries", "error": "must be 0 or 1"})
		return
	}

	// 应急 Advisory 校验
	now := time.Now().UTC()
	var emergencyUntil *time.Time
	var emergencyReason string
	if req.EmergencyAdvisoryUntil != nil {
		until := req.EmergencyAdvisoryUntil.UTC()
		if !until.After(now) || until.After(now.Add(15*time.Minute)) {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "emergency_advisory_until", "error": "must be in future and within 15 minutes"})
			return
		}
		reason := strings.TrimSpace(req.EmergencyAdvisoryReason)
		if reason == "" {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "emergency_advisory_reason", "error": "reason is required"})
			return
		}
		emergencyUntil = &until
		emergencyReason = reason
	} else if existingStored.EmergencyAdvisoryUntil != nil && existingStored.EmergencyAdvisoryUntil.After(now) {
		emergencyUntil = existingStored.EmergencyAdvisoryUntil
		emergencyReason = existingStored.EmergencyAdvisoryReason
	}

	// 准备持久化 StoredConfig
	stored := jev.StoredConfig{
		Enabled:                 &candidate.Enabled,
		BaseURL:                 candidate.BaseURL,
		Model:                   candidate.Model,
		Mode:                    candidate.Mode,
		Retries:                 &candidate.Retries,
		NoiseFilterEnabled:      &candidate.NoiseFilterEnabled,
		RoutingEnabled:          &candidate.RoutingEnabled,
		CascadeGateEnabled:      &candidate.CascadeGateEnabled,
		ChatOpsGuardrailEnabled: &candidate.ChatOpsGuardrailEnabled,
		AllowPrivateState:       &candidate.AllowPrivateState,
		Notification:            &candidate.Notification,
		Review:                  &candidate.Review,
		ChatOps:                 &candidate.ChatOps,
		EmergencyAdvisoryUntil:  emergencyUntil,
		EmergencyAdvisoryReason: emergencyReason,
	}

	if newKeyPlain != "" {
		if s.dependencies.KeyRing == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, "keyring_unavailable", map[string]any{"error": "keyring is required to store api key"})
			return
		}
		env, err := jev.EncryptAPIKey(r.Context(), s.dependencies.KeyRing, newKeyPlain)
		if err != nil {
			s.writeAPIError(w, r, http.StatusInternalServerError, "encryption_failed", map[string]any{"error": err.Error()})
			return
		}
		stored.APIKeyEnvelope = env
	} else if req.ClearAPIKey {
		stored.APIKeyEnvelope = ""
	} else {
		stored.APIKeyEnvelope = existingStored.APIKeyEnvelope
	}

	rawStored, err := json.Marshal(stored)
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, "marshal_failed", nil)
		return
	}

	// 使用事务原子持久化系统设置与审计日志
	err = s.dependencies.Store.WithTx(r.Context(), func(tx store.Store) error {
		setting := store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       jev.RuntimeSettingKey,
			ValueJSON: rawStored,
			UpdatedAt: now,
			UpdatedBy: "admin",
		}
		if _, err := tx.Settings().Upsert(r.Context(), setting); err != nil {
			return err
		}

		auditMeta, _ := json.Marshal(map[string]any{
			"enabled":            candidate.Enabled,
			"model":              candidate.Model,
			"mode":               candidate.Mode,
			"emergency_advisory": emergencyUntil != nil,
		})
		actorID := "admin"
		if session, ok := sessionFromContext(r.Context()); ok {
			actorID = session.AdminID
		}
		audit := store.AuditLog{
			ID:           ulid.Make().String(),
			Action:       "jev.config.updated",
			ActorType:    "admin",
			ActorID:      actorID,
			TargetType:   "jev_config",
			TargetID:     "jev.runtime_config",
			MetadataJSON: auditMeta,
			IPAddress:    remoteIPFromContext(r.Context()),
			CreatedAt:    now,
		}
		_, err := tx.Audits().Append(r.Context(), audit)
		return err
	})
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, "save_failed", map[string]any{"error": err.Error()})
		return
	}

	// 事务提交成功后构建候选 Runtime 并原子替换
	nextRuntime := jev.RuntimeFromEnv(candidate)
	if emergencyUntil != nil {
		nextRuntime.SetEmergencyAdvisory(*emergencyUntil, emergencyReason)
	}
	s.dependencies.JevRuntime.Replace(nextRuntime)

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *server) handlePostJevTest(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.JevRuntime == nil {
		s.writeAPIError(w, r, http.StatusNotFound, "jev_not_configured", nil)
		return
	}

	snap := s.dependencies.JevRuntime.Snapshot()
	if snap.APIKey.Reveal() == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"error": "no api key configured"})
		return
	}

	client := s.dependencies.JevRuntime.Client("notification")
	if client == nil {
		// 尝试即时构造临时 client 用于连通性测试
		var err error
		client, err = jev.NewClient(jev.ClientOptions{
			BaseURL: snap.BaseURL,
			APIKey:  snap.APIKey,
			Model:   snap.Model,
			Plane:   "notification",
			Timeout: 1000 * time.Millisecond,
		})
		if err != nil {
			s.writeAPIError(w, r, http.StatusBadRequest, "client_init_failed", map[string]any{"error": err.Error()})
			return
		}
	}

	// 发送固定非敏感测试样例
	q := map[string]jev.Question{
		"is_actionable": jev.NoulQuestion{
			Type:         "noul",
			Instructions: "Does this synthetic heartbeat check require human intervention?",
		},
	}
	state := json.RawMessage(`{"kind":"test","synthetic":true}`)

	res, err := client.Judge(r.Context(), state, q)
	if err != nil {
		s.writeAPIError(w, r, http.StatusBadGateway, "jev_test_failed", map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"model":         res.Model,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
	})
}
