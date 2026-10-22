package httpapi

import (
	"context"
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

	EmergencyAdvisoryUntil  *string           `json:"emergency_advisory_until,omitempty"`
	EmergencyAdvisoryReason string            `json:"emergency_advisory_reason,omitempty"`
	Status                  jevStatusResponse `json:"status"`
}

type jevStatusResponse struct {
	Mode               string  `json:"mode"`
	ShadowDays         int     `json:"shadow_days"`
	ShadowStartedAt    *string `json:"shadow_started_at,omitempty"`
	TotalRequests      uint64  `json:"total_requests"`
	TotalFailed        uint64  `json:"total_failed"`
	SuccessRate        float64 `json:"success_rate"`
	CacheHits          uint64  `json:"cache_hits"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	CacheEvictions     uint64  `json:"cache_evictions"`
	LatencyP50Ms       float64 `json:"latency_p50_ms"`
	LatencyP95Ms       float64 `json:"latency_p95_ms"`
	CircuitState       string  `json:"circuit_state"`
	CircuitTransitions int64   `json:"circuit_transitions"`
	WouldSuppressCount uint64  `json:"would_suppress_count"`
	WouldSkipCount     uint64  `json:"would_skip_count"`
	SuppressedCount    uint64  `json:"suppressed_count"`
	SkippedCount       uint64  `json:"skipped_count"`
	ActiveReady        bool    `json:"active_ready"`
	ActiveGateDetail   string  `json:"active_gate_detail"`
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

	// 计算运行状态、天数与指标
	var existingStored jev.StoredConfig
	var storedUpdatedAt time.Time
	if existingSetting, err := s.dependencies.Store.Settings().Get(r.Context(), jev.RuntimeSettingKey); err == nil {
		_ = json.Unmarshal([]byte(existingSetting.ValueJSON), &existingStored)
		storedUpdatedAt = existingSetting.UpdatedAt
	}

	var shadowStartedAt *time.Time
	if existingStored.ShadowStartedAt != nil {
		shadowStartedAt = existingStored.ShadowStartedAt
	} else if snap.Enabled && snap.Mode == "shadow" && !storedUpdatedAt.IsZero() {
		shadowStartedAt = &storedUpdatedAt
	}

	shadowDays := 0
	var shadowStartedStr *string
	if shadowStartedAt != nil {
		str := shadowStartedAt.Format(time.RFC3339)
		shadowStartedStr = &str
		days := int(time.Since(*shadowStartedAt).Hours() / 24)
		if days < 1 {
			days = 1
		}
		shadowDays = days
	}

	mSnap := jev.GlobalMetrics().Snapshot()
	var totalCalls, totalFailures, totalHits, totalMisses, totalEvicts uint64
	var maxP50, maxP95 float64
	for _, c := range mSnap.Calls {
		totalCalls += c
	}
	for _, f := range mSnap.Failures {
		totalFailures += f
	}
	for _, h := range mSnap.CacheHits {
		totalHits += h
	}
	for _, m := range mSnap.CacheMisses {
		totalMisses += m
	}
	for _, e := range mSnap.CacheEvicts {
		totalEvicts += e
	}
	for _, p := range mSnap.LatencyP50 {
		if p > maxP50 {
			maxP50 = p
		}
	}
	for _, p := range mSnap.LatencyP95 {
		if p > maxP95 {
			maxP95 = p
		}
	}
	cacheHitRate := 0.0
	if totalHits+totalMisses > 0 {
		cacheHitRate = float64(totalHits) / float64(totalHits+totalMisses) * 100
	}

	circuitState := "closed"
	var totalTransitions int64
	if s.dependencies.JevRuntime != nil {
		bStats := s.dependencies.JevRuntime.BreakerStats()
		for _, st := range bStats {
			totalTransitions += st.TotalTransitions
			if st.State == "open" {
				circuitState = "open"
			} else if st.State == "half_open" && circuitState != "open" {
				circuitState = "half_open"
			}
		}
	}

	successRate := 100.0
	if totalCalls > 0 {
		if totalFailures >= totalCalls {
			successRate = 0
		} else {
			successRate = float64(totalCalls-totalFailures) / float64(totalCalls) * 100
		}
	}

	statusMode := "disabled"
	if snap.Enabled {
		statusMode = snap.Mode
	}

	activeReady := false
	activeGateDetail := ""
	if !snap.Enabled {
		activeGateDetail = "Jev 决策引擎处于禁用状态"
	} else if snap.Mode == "active" {
		activeReady = true
		activeGateDetail = "生产接管模式运行中，具备全量决策权限"
	} else {
		if evalSetting, err := s.dependencies.Store.Settings().Get(r.Context(), jev.EvaluationSettingKey); err == nil {
			var summary jev.EvaluationSummary
			if err := json.Unmarshal([]byte(evalSetting.ValueJSON), &summary); err == nil {
				if summary.Model == snap.Model {
					if err := jev.ValidateActivation(summary); err == nil {
						activeReady = true
						activeGateDetail = "已满足全部离线与影子评测门禁，可安全切换至 Active 模式"
					} else {
						activeGateDetail = "评测门禁未完全达成: " + err.Error()
					}
				} else {
					activeGateDetail = fmt.Sprintf("已评测模型 (%s) 与当前配置模型 (%s) 不一致", summary.Model, snap.Model)
				}
			} else {
				activeGateDetail = "评测记录损坏，需重新执行评测验证"
			}
		} else {
			if shadowDays < 7 {
				activeGateDetail = fmt.Sprintf("影子观察期未满 7 天 (当前已运行 %d 天)，且尚无离线评测证据", shadowDays)
			} else {
				activeGateDetail = "影子期已满，但尚未写入离线与真实评测证据"
			}
		}
	}

	statusResp := jevStatusResponse{
		Mode:               statusMode,
		ShadowDays:         shadowDays,
		ShadowStartedAt:    shadowStartedStr,
		TotalRequests:      totalCalls,
		TotalFailed:        totalFailures,
		SuccessRate:        successRate,
		CacheHits:          totalHits,
		CacheHitRate:       cacheHitRate,
		CacheEvictions:     totalEvicts,
		LatencyP50Ms:       maxP50,
		LatencyP95Ms:       maxP95,
		CircuitState:       circuitState,
		CircuitTransitions: totalTransitions,
		WouldSuppressCount: mSnap.WouldSuppress,
		WouldSkipCount:     mSnap.WouldSkip,
		SuppressedCount:    mSnap.Suppressed,
		SkippedCount:       mSnap.Skipped,
		ActiveReady:        activeReady,
		ActiveGateDetail:   activeGateDetail,
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
		Status:                  statusResp,
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
		if req.Notification.Timeout > 0 {
			candidate.Notification.Timeout = req.Notification.Timeout
		} else if candidate.Notification.Timeout == 0 {
			candidate.Notification.Timeout = 30 * time.Second
		}
		if req.Notification.MaxConcurrency > 0 {
			candidate.Notification.MaxConcurrency = req.Notification.MaxConcurrency
		}
	}
	if req.Review != nil {
		if req.Review.Timeout > 0 {
			candidate.Review.Timeout = req.Review.Timeout
		} else if candidate.Review.Timeout == 0 {
			candidate.Review.Timeout = 30 * time.Second
		}
		if req.Review.MaxConcurrency > 0 {
			candidate.Review.MaxConcurrency = req.Review.MaxConcurrency
		}
	}
	if req.ChatOps != nil {
		if req.ChatOps.Timeout > 0 {
			candidate.ChatOps.Timeout = req.ChatOps.Timeout
		} else if candidate.ChatOps.Timeout == 0 {
			candidate.ChatOps.Timeout = 30 * time.Second
		}
		if req.ChatOps.MaxConcurrency > 0 {
			candidate.ChatOps.MaxConcurrency = req.ChatOps.MaxConcurrency
		}
	}

	// 基础校验
	if candidate.Enabled && candidate.APIKey.Reveal() == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "jev.api_key", "error": "api key is required when enabled"})
		return
	}
	if candidate.BaseURL != "" {
		parsed, err := url.Parse(candidate.BaseURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
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
	var shadowStartedAt *time.Time
	if candidate.Enabled && candidate.Mode == "shadow" {
		if existingStored.ShadowStartedAt != nil {
			shadowStartedAt = existingStored.ShadowStartedAt
		} else {
			shadowStartedAt = &now
		}
	} else if existingStored.ShadowStartedAt != nil {
		shadowStartedAt = existingStored.ShadowStartedAt
	}

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
		ShadowStartedAt:         shadowStartedAt,
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

	// 专门构造带有探针属性 (Plane: "probe", Timeout: 10s) 的客户端，避免受到生产业务面 500ms 严苛延时限制
	client, err := jev.NewClient(jev.ClientOptions{
		BaseURL: snap.BaseURL,
		APIKey:  snap.APIKey,
		Model:   snap.Model,
		Plane:   "probe",
		Timeout: 30 * time.Second,
	})
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.WarnContext(r.Context(), "jev test connectivity: client init failed",
				"error", err,
				"model", snap.Model,
				"base_url", snap.BaseURL,
			)
		}
		s.writeAPIError(w, r, http.StatusBadRequest, "client_init_failed", map[string]any{"error": err.Error()})
		return
	}

	// 发送固定非敏感测试样例
	q := map[string]jev.Question{
		"is_actionable": jev.NoulQuestion{
			Type:         "noul",
			Instructions: "Does this synthetic heartbeat check require human intervention?",
		},
	}
	state := json.RawMessage(`{"kind":"test","synthetic":true}`)

	testCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, err := client.Judge(testCtx, state, q)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.WarnContext(r.Context(), "jev test connectivity failed",
				"error", err,
				"error_class", jev.ClassifyError(err),
				"model", snap.Model,
				"base_url", snap.BaseURL,
				"request_id", requestIDFromContext(r.Context()),
			)
		}
		s.writeAPIError(w, r, http.StatusBadGateway, errorCodeJevTestFailed, map[string]any{
			"error":      err.Error(),
			"request_id": requestIDFromContext(r.Context()),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"model":         res.Model,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
	})
}
