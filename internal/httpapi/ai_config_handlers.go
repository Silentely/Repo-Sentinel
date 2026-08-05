package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
)

// aiConfigResponse 管理台 AI 配置视图：不返回密钥明文，仅返回已配置状态与来源。
type aiConfigResponse struct {
	Enabled          bool   `json:"enabled"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	TimeoutSec       int64  `json:"timeout_sec"`
	MaxTokens        int    `json:"max_tokens"`
	DigestEnabled    bool   `json:"digest_enabled"`
	TriageEnabled    bool   `json:"triage_enabled"`
	APIKeyConfigured bool   `json:"api_key_configured"`

	EnabledSource       string `json:"enabled_source"`
	BaseURLSource       string `json:"base_url_source"`
	ModelSource         string `json:"model_source"`
	TimeoutSource       string `json:"timeout_source"`
	MaxTokensSource     string `json:"max_tokens_source"`
	APIKeySource        string `json:"api_key_source"`
	DigestEnabledSource string `json:"digest_enabled_source"`
	TriageEnabledSource string `json:"triage_enabled_source"`

	EnabledLocked       bool `json:"enabled_locked"`
	BaseURLLocked       bool `json:"base_url_locked"`
	ModelLocked         bool `json:"model_locked"`
	TimeoutLocked       bool `json:"timeout_locked"`
	MaxTokensLocked     bool `json:"max_tokens_locked"`
	APIKeyLocked        bool `json:"api_key_locked"`
	DigestEnabledLocked bool `json:"digest_enabled_locked"`
	TriageEnabledLocked bool `json:"triage_enabled_locked"`

	CanEditInUI bool   `json:"can_edit_in_ui"`
	Note        string `json:"note"`
}

type aiConfigPutRequest struct {
	Enabled       *bool   `json:"enabled"`
	BaseURL       *string `json:"base_url"`
	Model         *string `json:"model"`
	TimeoutSec    *int64  `json:"timeout_sec"`
	MaxTokens     *int    `json:"max_tokens"`
	DigestEnabled *bool   `json:"digest_enabled"`
	TriageEnabled *bool   `json:"triage_enabled"`
	APIKey        *string `json:"api_key"`
	ClearAPIKey   bool    `json:"clear_api_key"`
}

// aiConfigPutRequest 可写标量字段的取值范围（PUT 保存与连通性测试共用）。
const (
	minAITimeoutSec int64 = 1
	maxAITimeoutSec int64 = 3600
	minAIMaxTokens  int   = 100
	maxAIMaxTokens  int   = 8000
)

// aiTestResponse AI 连通性测试结果：一律 200，ok 区分可达性，message 为人话描述。
type aiTestResponse struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	LatencyMS int64  `json:"latency_ms"`
}

func (s *server) aiRuntime() *ai.RuntimeConfig {
	if s.dependencies.AIRuntime == nil {
		return nil
	}
	return s.dependencies.AIRuntime
}

// rejectAILockedField 字段被环境变量锁定时拒绝写入并返回 true。
func (s *server) rejectAILockedField(w http.ResponseWriter, r *http.Request, source, field string) bool {
	if source == "env" {
		s.writeAPIError(w, r, http.StatusConflict, "ai_field_locked", map[string]any{"field": field})
		return true
	}
	return false
}

func (s *server) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	rt := s.aiRuntime()
	if rt == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	snap := rt.Snapshot()
	writeJSON(w, http.StatusOK, aiConfigResponse{
		Enabled:             snap.Enabled,
		BaseURL:             snap.BaseURL,
		Model:               snap.Model,
		TimeoutSec:          int64(snap.Timeout / time.Second),
		MaxTokens:           snap.MaxTokens,
		DigestEnabled:       snap.DigestEnabled,
		TriageEnabled:       snap.TriageEnabled,
		APIKeyConfigured:    snap.APIKey != "",
		EnabledSource:       snap.EnabledSource,
		BaseURLSource:       snap.BaseURLSource,
		ModelSource:         snap.ModelSource,
		TimeoutSource:       snap.TimeoutSource,
		MaxTokensSource:     snap.MaxTokensSource,
		APIKeySource:        snap.APIKeySource,
		DigestEnabledSource: snap.DigestEnabledSource,
		TriageEnabledSource: snap.TriageEnabledSource,
		EnabledLocked:       snap.EnabledSource == "env",
		BaseURLLocked:       snap.BaseURLSource == "env",
		ModelLocked:         snap.ModelSource == "env",
		TimeoutLocked:       snap.TimeoutSource == "env",
		MaxTokensLocked:     snap.MaxTokensSource == "env",
		APIKeyLocked:        snap.APIKeySource == "env",
		DigestEnabledLocked: snap.DigestEnabledSource == "env",
		TriageEnabledLocked: snap.TriageEnabledSource == "env",
		CanEditInUI:         true,
		Note:                "环境变量设置的字段在管理台锁定；API Key 加密存储，不回显明文。",
	})
}

func (s *server) handlePutAIConfig(w http.ResponseWriter, r *http.Request) {
	rt := s.aiRuntime()
	if rt == nil || s.dependencies.Store == nil || s.dependencies.KeyRing == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	var body aiConfigPutRequest
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}

	snap := rt.Snapshot()
	stored, err := ai.LoadStoredConfig(r.Context(), s.dependencies.Store)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	// 仅更新「未被环境变量锁定」的字段；先全量校验再写库。
	if body.Enabled != nil {
		if s.rejectAILockedField(w, r, snap.EnabledSource, "enabled") {
			return
		}
		stored.Enabled = body.Enabled
	}
	if body.BaseURL != nil {
		if s.rejectAILockedField(w, r, snap.BaseURLSource, "base_url") {
			return
		}
		base := strings.TrimSpace(*body.BaseURL)
		if base != "" && !validAIBaseURL(base) {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "base_url"})
			return
		}
		stored.BaseURL = base
	}
	if body.Model != nil {
		if s.rejectAILockedField(w, r, snap.ModelSource, "model") {
			return
		}
		stored.Model = strings.TrimSpace(*body.Model)
	}
	if body.TimeoutSec != nil {
		if s.rejectAILockedField(w, r, snap.TimeoutSource, "timeout_sec") {
			return
		}
		if *body.TimeoutSec < minAITimeoutSec || *body.TimeoutSec > maxAITimeoutSec {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "timeout_sec"})
			return
		}
		stored.TimeoutSec = body.TimeoutSec
	}
	if body.MaxTokens != nil {
		if s.rejectAILockedField(w, r, snap.MaxTokensSource, "max_tokens") {
			return
		}
		if *body.MaxTokens < minAIMaxTokens || *body.MaxTokens > maxAIMaxTokens {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "max_tokens"})
			return
		}
		stored.MaxTokens = body.MaxTokens
	}
	if body.DigestEnabled != nil {
		if s.rejectAILockedField(w, r, snap.DigestEnabledSource, "digest_enabled") {
			return
		}
		stored.DigestEnabled = body.DigestEnabled
	}
	if body.TriageEnabled != nil {
		if s.rejectAILockedField(w, r, snap.TriageEnabledSource, "triage_enabled") {
			return
		}
		stored.TriageEnabled = body.TriageEnabled
	}
	if body.APIKey != nil {
		if s.rejectAILockedField(w, r, snap.APIKeySource, "api_key") {
			return
		}
		key := strings.TrimSpace(*body.APIKey)
		if key == "" {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "api_key"})
			return
		}
		env, err := ai.EncryptAPIKey(r.Context(), s.dependencies.KeyRing, key)
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		stored.APIKeyEnvelope = env
	}
	if body.ClearAPIKey {
		if s.rejectAILockedField(w, r, snap.APIKeySource, "api_key") {
			return
		}
		stored.APIKeyEnvelope = ""
	}

	if err := ai.SaveStoredConfig(r.Context(), s.dependencies.Store, stored); err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	// 热更新：以 env 基线 + 最新 DB 值重建运行时（Clear 等操作需要覆盖而非仅补缺），
	// 再物化客户端广播给 digest / rules。
	fresh := ai.RuntimeFromEnv(s.dependencies.Config.AI)
	if err := ai.MergeFromStore(r.Context(), s.dependencies.Store, s.dependencies.KeyRing, fresh); err != nil {
		s.dependencies.Logger.Warn("ai runtime reload failed", "error", err.Error())
	}
	rt.Replace(fresh)
	next := fresh.Client()
	next.Logger = s.dependencies.Logger
	if s.dependencies.AI != nil {
		s.dependencies.AI.Replace(next)
	}

	s.handleGetAIConfig(w, r)
}

// validAIBaseURL 校验 AI 端点：http(s) 且无 userinfo。
func validAIBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

// handleTestAIConfig 用当前生效配置执行一次最小对话，验证端点 / 模型 / API Key 连通性。
// 请求体可携带未锁定字段的临时覆盖值（便于保存前先验证），env 锁定字段不可覆盖；
// 测试只读不改：不写库、不改变运行时。结果一律 200，ok 区分可达性。
func (s *server) handleTestAIConfig(w http.ResponseWriter, r *http.Request) {
	rt := s.aiRuntime()
	if rt == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeInternal, nil)
		return
	}
	var body aiConfigPutRequest
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	snap := rt.Snapshot()

	baseURL := snap.BaseURL
	if body.BaseURL != nil && snap.BaseURLSource != "env" {
		base := strings.TrimSpace(*body.BaseURL)
		if base != "" && !validAIBaseURL(base) {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "base_url"})
			return
		}
		baseURL = base
	}
	model := snap.Model
	if body.Model != nil && snap.ModelSource != "env" {
		model = strings.TrimSpace(*body.Model)
	}
	timeout := snap.Timeout
	if body.TimeoutSec != nil && snap.TimeoutSource != "env" {
		if *body.TimeoutSec < minAITimeoutSec || *body.TimeoutSec > maxAITimeoutSec {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "timeout_sec"})
			return
		}
		timeout = time.Duration(*body.TimeoutSec) * time.Second
	}
	maxTokens := snap.MaxTokens
	if body.MaxTokens != nil && snap.MaxTokensSource != "env" {
		if *body.MaxTokens < minAIMaxTokens || *body.MaxTokens > maxAIMaxTokens {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "max_tokens"})
			return
		}
		maxTokens = *body.MaxTokens
	}
	apiKey := snap.APIKey
	if body.APIKey != nil && snap.APIKeySource != "env" {
		if key := strings.TrimSpace(*body.APIKey); key != "" {
			apiKey = key
		}
	}

	if apiKey == "" {
		writeJSON(w, http.StatusOK, aiTestResponse{
			OK:      false,
			Message: "未配置 API Key，无法测试；请先保存 API Key 或在环境变量中设置。",
			Model:   effectiveAIModel(model),
			BaseURL: effectiveAIBaseURL(baseURL),
		})
		return
	}

	// probe 使用按配置超时的专用 HTTP 客户端：包级默认客户端 30s 硬顶会截断
	// 高于 30s 的超时配置，导致测试与真实运行时行为不一致。
	probeTimeout := timeout
	if probeTimeout <= 0 {
		probeTimeout = ai.DefaultTimeout
	}
	probe := &ai.Client{
		Enabled: true, BaseURL: baseURL, APIKey: apiKey, Model: model,
		Timeout: timeout, MaxTokens: maxTokens,
		HTTP: &http.Client{Timeout: probeTimeout},
	}
	latency, err := probe.Ping(r.Context())
	effectiveModel, effectiveBase := effectiveAIModel(model), effectiveAIBaseURL(baseURL)
	if err != nil {
		writeJSON(w, http.StatusOK, aiTestResponse{
			OK:      false,
			Message: fmt.Sprintf("连通性测试失败：%v", err),
			Model:   effectiveModel,
			BaseURL: effectiveBase,
		})
		return
	}
	writeJSON(w, http.StatusOK, aiTestResponse{
		OK:        true,
		Message:   fmt.Sprintf("连通性测试成功：模型 %s 正常回复（%d ms）", effectiveModel, latency.Milliseconds()),
		Model:     effectiveModel,
		BaseURL:   effectiveBase,
		LatencyMS: latency.Milliseconds(),
	})
}

// effectiveAIModel / effectiveAIBaseURL 回显实际生效值（空值表示使用客户端回退默认）。
func effectiveAIModel(model string) string {
	if model == "" {
		return ai.DefaultModel
	}
	return model
}

func effectiveAIBaseURL(base string) string {
	if base == "" {
		return ai.DefaultBaseURL
	}
	return base
}
