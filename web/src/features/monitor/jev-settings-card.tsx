import React, { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiErrorAlert, ErrorAlert } from "../../components/error-alert";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import { toApiError } from "../../lib/api/errors";
import {
  jevConfigQueryOptions,
  testJevConfig,
  updateJevConfig,
  type JevPlaneConfig,
  type JevTestResult,
  type UpdateJevConfigPayload,
  type JevStatusDTO,
} from "./api";

const DEFAULT_PLANE: JevPlaneConfig = {
  p50_budget_ms: 150,
  p95_budget_ms: 500,
  max_concurrency: 20,
  cache_ttl_sec: 3600,
  circuit_fail_count: 5,
  circuit_reset_sec: 30,
};

function JevStatusDashboard({ status }: { status?: JevStatusDTO }) {
  if (!status) return null;

  const isShadow = status.mode === "shadow";
  const isActive = status.mode === "active";
  const isDisabled = status.mode === "disabled" || !status.mode;

  const shadowPercent = Math.min(100, Math.round((status.shadow_days / 7) * 100));

  return (
    <div className="status-card mb-6">
      <div className="two-factor-header mb-3">
        <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
          <span className="eyebrow" style={{ margin: 0 }}>引擎实时运行状态</span>
          {isDisabled && <span className="system-badge system-badge--neutral">未启用</span>}
          {isShadow && <span className="system-badge system-badge--warning">影子观察中 (Shadow)</span>}
          {isActive && <span className="system-badge system-badge--success">生产接管中 (Active)</span>}
        </div>
        {isShadow && (
          <span style={{ fontSize: "0.85rem", fontWeight: 700, color: "var(--text-secondary)" }}>
            已持续观察：<strong style={{ color: "var(--text-primary)" }}>{status.shadow_days}</strong> 天 / 目标 7 天
          </span>
        )}
      </div>

      {isShadow && (
        <div style={{ marginBottom: "16px" }}>
          <div className="neo-progress-bar">
            <div
              className={`neo-progress-bar__fill${status.shadow_days >= 7 ? " neo-progress-bar__fill--success" : ""}`}
              style={{ width: `${shadowPercent}%` }}
            />
          </div>
        </div>
      )}

      {/* 复古新野兽派 6 宫格指标看板 */}
      <div className="status-grid mb-3">
        <div className="status-item">
          <span className="status-item__label">累计决策研判</span>
          <strong>{status.total_requests.toLocaleString()}</strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: status.success_rate >= 99 ? "var(--success)" : "var(--warning)", marginTop: "4px", fontWeight: 600 }}>
            成功率 {status.success_rate.toFixed(1)}%
          </span>
        </div>

        <div className="status-item">
          <span className="status-item__label">
            {isShadow ? "建议静音通知" : "实际降噪静音"}
          </span>
          <strong style={{ color: "var(--accent)" }}>
            {(isShadow ? status.would_suppress_count : status.suppressed_count).toLocaleString()}
          </strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: "var(--text-muted)", marginTop: "4px" }}>
            {isShadow ? "降噪潜力" : "已拦截骚扰"}
          </span>
        </div>

        <div className="status-item">
          <span className="status-item__label">
            {isShadow ? "建议放行 PR" : "实际放行 PR"}
          </span>
          <strong style={{ color: "var(--success)" }}>
            {(isShadow ? status.would_skip_count : status.skipped_count).toLocaleString()}
          </strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: "var(--text-muted)", marginTop: "4px" }}>
            {isShadow ? "快速放行潜力" : "加速跳过审查"}
          </span>
        </div>

        <div className="status-item">
          <span className="status-item__label">决策缓存 (LRU+TTL)</span>
          <strong>{status.cache_hits.toLocaleString()}</strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: "var(--text-muted)", marginTop: "4px" }}>
            命中率 {(status.cache_hit_rate ?? 0).toFixed(1)}% · 淘汰 {status.cache_evictions ?? 0}
          </span>
        </div>

        <div className="status-item">
          <span className="status-item__label">熔断状态 (Circuit)</span>
          <strong style={{
            fontSize: "1.1rem",
            color: status.circuit_state === "open" ? "var(--danger)" : status.circuit_state === "half_open" ? "var(--warning)" : "var(--success)"
          }}>
            {status.circuit_state === "open" ? "熔断开启" : status.circuit_state === "half_open" ? "半开探测" : "正常闭合"}
          </strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: "var(--text-muted)", marginTop: "4px" }}>
            流转 {status.circuit_transitions ?? 0} 次 · {status.circuit_state || "closed"}
          </span>
        </div>

        <div className="status-item">
          <span className="status-item__label">研判延时 (分位)</span>
          <strong>{(status.latency_p50_ms ?? 0).toFixed(1)} <span style={{ fontSize: "0.85rem", fontWeight: 500 }}>ms</span></strong>
          <span style={{ display: "block", fontSize: "0.72rem", color: "var(--text-muted)", marginTop: "4px" }}>
            P50 · P95: {(status.latency_p95_ms ?? 0).toFixed(1)} ms
          </span>
        </div>
      </div>

      {/* 门禁就绪提示 */}
      <div className={`callout ${status.active_ready ? "callout--ok" : "callout--warn"}`} style={{ margin: 0 }}>
        <strong>{status.active_ready ? "● 门禁状态：已就绪" : "● 门禁状态：观察准备中"}</strong> — {status.active_gate_detail}
      </div>
    </div>
  );
}

export function JevSettingsCard() {
  const queryClient = useQueryClient();
  const jevQuery = useQuery(jevConfigQueryOptions);

  const [message, setMessage] = useAutoDismiss();
  const [testResult, setTestResult] = useState<JevTestResult | null>(null);
  const [testError, setTestError] = useState<string | null>(null);

  // 表单状态
  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState<"shadow" | "active">("shadow");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [clearApiKey, setClearApiKey] = useState(false);
  const [model, setModel] = useState("jev-1.14.0");
  const [retries, setRetries] = useState(1);
  const [allowPrivateState, setAllowPrivateState] = useState(false);

  const [noiseFilterEnabled, setNoiseFilterEnabled] = useState(false);
  const [routingEnabled, setRoutingEnabled] = useState(false);
  const [cascadeGateEnabled, setCascadeGateEnabled] = useState(false);
  const [chatopsGuardrailEnabled, setChatopsGuardrailEnabled] = useState(false);

  const [notificationPlane, setNotificationPlane] = useState<JevPlaneConfig>(DEFAULT_PLANE);
  const [reviewPlane, setReviewPlane] = useState<JevPlaneConfig>(DEFAULT_PLANE);
  const [chatopsPlane, setChatopsPlane] = useState<JevPlaneConfig>(DEFAULT_PLANE);

  const [emergencyReason, setEmergencyReason] = useState("");
  const [emergencyMinutes, setEmergencyMinutes] = useState(0);
  const [renderNow] = useState(() => Date.now());

  // 服务端数据加载完成后回填
  useEffect(() => {
    if (jevQuery.data) {
      const d = jevQuery.data;
      setEnabled(d.enabled);
      setMode((d.mode === "active" ? "active" : "shadow") as "shadow" | "active");
      setBaseURL(d.base_url || "");
      setModel(d.model || "jev-1.14.0");
      setRetries(d.retries ?? 1);
      setAllowPrivateState(d.allow_private_state ?? false);
      setNoiseFilterEnabled(d.noise_filter_enabled ?? false);
      setRoutingEnabled(d.routing_enabled ?? false);
      setCascadeGateEnabled(d.cascade_gate_enabled ?? false);
      setChatopsGuardrailEnabled(d.chatops_guardrail_enabled ?? false);

      if (d.notification) setNotificationPlane(d.notification);
      if (d.review) setReviewPlane(d.review);
      if (d.chatops) setChatopsPlane(d.chatops);
    }
  }, [jevQuery.data]);

  const updateMutation = useMutation({
    mutationFn: (payload: UpdateJevConfigPayload) => updateJevConfig(payload),
    onSuccess: () => {
      setMessage("Jev 配置已保存");
      setApiKey("");
      setClearApiKey(false);
      queryClient.invalidateQueries({ queryKey: jevConfigQueryOptions.queryKey });
    },
  });

  const testMutation = useMutation({
    mutationFn: () => testJevConfig(),
    onSuccess: (data) => {
      setTestResult(data);
      setTestError(null);
    },
    onError: (err) => {
      setTestResult(null);
      const apiErr = toApiError(err);
      const detailErr = typeof apiErr.details?.error === "string" ? apiErr.details.error : "";
      const msg = detailErr ? `${apiErr.message}（原因：${detailErr}）` : apiErr.message;
      setTestError(msg || "测试连通性失败");
    },
  });

  const handleSave = (e: React.FormEvent) => {
    e.preventDefault();
    setTestResult(null);
    setTestError(null);

    const payload: UpdateJevConfigPayload = {
      enabled,
      mode,
      base_url: baseURL,
      model,
      retries,
      allow_private_state: allowPrivateState,
      noise_filter_enabled: noiseFilterEnabled,
      routing_enabled: routingEnabled,
      cascade_gate_enabled: cascadeGateEnabled,
      chatops_guardrail_enabled: chatopsGuardrailEnabled,
      notification: notificationPlane,
      review: reviewPlane,
      chatops: chatopsPlane,
    };

    if (apiKey) {
      payload.api_key = apiKey;
    } else if (clearApiKey) {
      payload.clear_api_key = true;
    }

    if (emergencyMinutes > 0 && emergencyReason.trim()) {
      const until = new Date(Date.now() + emergencyMinutes * 60 * 1000).toISOString();
      payload.emergency_advisory_until = until;
      payload.emergency_advisory_reason = emergencyReason.trim();
    }

    updateMutation.mutate(payload);
  };

  const handleTest = () => {
    setTestResult(null);
    setTestError(null);
    testMutation.mutate();
  };

  if (jevQuery.isLoading) {
    return (
      <section className="onboarding-card card channel-form" aria-labelledby="settings-jev-title">
        <h2 id="settings-jev-title">Jev 决策引擎配置</h2>
        <p className="field-hint">正在加载 Jev 配置...</p>
      </section>
    );
  }

  const currentConfig = jevQuery.data;
  const isEmergencyActive = Boolean(
    currentConfig?.emergency_advisory_until &&
      new Date(currentConfig.emergency_advisory_until).getTime() > renderNow,
  );

  return (
    <section className="onboarding-card card channel-form" aria-labelledby="settings-jev-title">
      <h2 id="settings-jev-title">判别式决策引擎 (TypeSafe AI)</h2>
      <p className="field-hint">
        为通知降噪、PR 级联审查门控与 ChatOps 提供轻量判别式决策（YES/NO/CHOICE/SCORE），不生成自由文本。
      </p>

      {jevQuery.isError && (
        <div className="mb-4">
          <ApiErrorAlert error={jevQuery.error} title="无法加载 Jev 配置" />
        </div>
      )}

      <JevStatusDashboard status={currentConfig?.status} />

      {isEmergencyActive && (
        <div className="callout callout--warn mb-4">
          <strong>⚠️ 应急临时降级生效中：</strong>
          当前系统正处于应急旁路状态（至{" "}
          {new Date(currentConfig!.emergency_advisory_until!).toLocaleTimeString()}），原因：
          {currentConfig!.emergency_advisory_reason || "未注明"}。
        </div>
      )}

      <form onSubmit={handleSave} className="grid grid--2col gap-4">
        {/* 基础配置 */}
        <div className="grid__span-2 form-section-title">基本与运行模式</div>

        <label className="field--checkbox">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          <span>启用 Jev 决策引擎</span>
        </label>

        <label className="field--plain">
          <span>运行模式</span>
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as "shadow" | "active")}
          >
            <option value="shadow">影子观察 (Shadow - 默认安全模式)</option>
            <option value="active">生产接管 (Active - 需通过全量门禁)</option>
          </select>
        </label>

        <label className="field--plain grid__span-2">
          <span>Endpoint (Base URL)</span>
          <input
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
            placeholder="https://api.typesafe.ai"
          />
        </label>

        <label className="field--plain">
          <span>API Key</span>
          <input
            type="password"
            value={apiKey}
            disabled={clearApiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={currentConfig?.has_api_key ? "已配置（留空保持原值）" : "请输入 API Key"}
          />
        </label>

        {currentConfig?.has_api_key && (
          <label className="field--checkbox mt-6">
            <input
              type="checkbox"
              checked={clearApiKey}
              onChange={(e) => {
                setClearApiKey(e.target.checked);
                if (e.target.checked) setApiKey("");
              }}
            />
            <span>清除现有 API Key</span>
          </label>
        )}

        <label className="field--plain">
          <span>模型</span>
          <input
            size={16}
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="jev-1.14.0"
          />
        </label>

        <label className="field--plain">
          <span>瞬时重试次数</span>
          <input
            type="number"
            min={0}
            max={1}
            value={retries}
            onChange={(e) => setRetries(parseInt(e.target.value, 10) || 0)}
          />
        </label>

        <label className="field--checkbox grid__span-2">
          <input
            type="checkbox"
            checked={allowPrivateState}
            onChange={(e) => setAllowPrivateState(e.target.checked)}
          />
          <span>
            允许私有仓库脱敏元数据外发
            <small className="block text-muted">
              需管理员核实第三方服务数据协议与保留条款（系统不宣称具备 ZDR/DPA 资质）。
            </small>
          </span>
        </label>

        {/* 能力开关 */}
        <div className="grid__span-2 form-section-title">能力开关</div>

        <label className="field--checkbox">
          <input
            type="checkbox"
            checked={noiseFilterEnabled}
            onChange={(e) => setNoiseFilterEnabled(e.target.checked)}
          />
          <span>通知智能降噪 (Noise Filter)</span>
        </label>

        <label className="field--checkbox">
          <input
            type="checkbox"
            checked={routingEnabled}
            onChange={(e) => setRoutingEnabled(e.target.checked)}
          />
          <span>通知 Incident 标签与分诊路由</span>
        </label>

        <label className="field--checkbox">
          <input
            type="checkbox"
            checked={cascadeGateEnabled}
            onChange={(e) => setCascadeGateEnabled(e.target.checked)}
          />
          <span>PR 文档变更快速放行门控 (Cascade Gate)</span>
        </label>

        <label className="field--checkbox">
          <input
            type="checkbox"
            checked={chatopsGuardrailEnabled}
            onChange={(e) => setChatopsGuardrailEnabled(e.target.checked)}
          />
          <span>ChatOps 指令风险护栏 (Guardrail)</span>
        </label>

        {/* 应急临时降级 */}
        <div className="grid__span-2 form-section-title">应急临时旁路降级 (Emergency Advisory)</div>

        <label className="field--plain">
          <span>降级持续时间（分钟，最大 15 分钟）</span>
          <input
            type="number"
            min={0}
            max={15}
            value={emergencyMinutes}
            onChange={(e) => setEmergencyMinutes(parseInt(e.target.value, 10) || 0)}
          />
        </label>

        <label className="field--plain">
          <span>降级原因（必填）</span>
          <input
            value={emergencyReason}
            onChange={(e) => setEmergencyReason(e.target.value)}
            placeholder="如：上游服务抖动，临时旁路"
          />
        </label>

        {/* 提交动作 */}
        <div className="channel-form__buttons grid__span-2">
          <button type="submit" className="primary-button primary-button--inline" disabled={updateMutation.isPending}>
            {updateMutation.isPending ? "正在保存..." : "保存 Jev 设置"}
          </button>
          <button
            type="button"
            className="secondary-button"
            onClick={handleTest}
            disabled={testMutation.isPending || (!apiKey && !currentConfig?.has_api_key)}
          >
            {testMutation.isPending ? "正在测试..." : "测试连通性"}
          </button>
        </div>

        {/* 保存与测试反馈区：紧随按钮下方 */}
        <div className="grid__span-2">
          {message && <p className="success-banner mt-2" role="status">{message}</p>}
          {updateMutation.isError && (
            <div className="mt-2">
              <ApiErrorAlert error={updateMutation.error} title="保存配置失败" />
            </div>
          )}
          {testResult && (
            <p className="success-banner mt-2" role="status">
              连通性测试通过（模型: {testResult.model}，输入: {testResult.input_tokens} tokens，输出:{" "}
              {testResult.output_tokens} tokens）
            </p>
          )}
          {testError && (
            <div className="mt-2">
              <ErrorAlert title="测试失败" message={testError} />
            </div>
          )}
        </div>
      </form>
    </section>
  );
}
