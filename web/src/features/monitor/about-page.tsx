import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";

import { ErrorAlert } from "../../components/error-alert";
import { changePassword } from "../auth/api";
import { toApiError } from "../../lib/api/errors";
import {
  aiConfigQueryOptions,
  checkForUpdates,
  saveAIConfig,
  saveSystemSettings,
  settingsQueryOptions,
  testAIConnectivity,
  versionQueryOptions,
  type AIConfig,
  type AIConfigInput,
  type SystemSettings,
} from "./api";

const DOCS_CONFIG = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md";
const DOCS_FAQ = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/faq.md";
const DOCS_CHANGELOG = "https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md";

// ---- 运行设置表单：单一受控表单对象，替代逐字段 useState + 同步 effect ----

interface SettingsFormState {
  timezone: string;
  digestTime: string;
  digestEmpty: boolean;
  reportWeekly: boolean;
  reportWeeklyDay: string;
  reportMonthly: boolean;
  reportMonthlyDay: number;
  aggregateSec: number;
  burstThreshold: number;
  burstWindowSec: number;
  closedDisplayLimit: number;
  retentionEventsDays: number;
  retentionOutboxDays: number;
  retentionDeliveriesDays: number;
  featureIssues: boolean;
  featurePRs: boolean;
  featureActions: boolean;
  featureAlerts: boolean;
  featureStars: boolean;
  featureWatches: boolean;
}

// 定期报告发送日枚举（与后端 report.weekly_day 一致）。
const WEEKLY_DAYS = [
  { value: "monday", label: "周一" },
  { value: "tuesday", label: "周二" },
  { value: "wednesday", label: "周三" },
  { value: "thursday", label: "周四" },
  { value: "friday", label: "周五" },
  { value: "saturday", label: "周六" },
  { value: "sunday", label: "周日" },
] as const;

// 从服务端设置快照构造表单初值；查询未完成时返回默认值（与后端缺省一致）。
function formFromSettings(data: SystemSettings | undefined): SettingsFormState {
  return {
    timezone: String(data?.["admin.timezone"] ?? "UTC"),
    digestTime: String(data?.["digest.local_time"] ?? "09:00"),
    digestEmpty: Boolean(data?.["digest.send_empty"]),
    reportWeekly: Boolean(data?.["report.weekly_enabled"]),
    reportWeeklyDay: String(data?.["report.weekly_day"] ?? "monday"),
    reportMonthly: Boolean(data?.["report.monthly_enabled"]),
    reportMonthlyDay: Number(data?.["report.monthly_day"] ?? 1),
    aggregateSec: Number(data?.["notify.aggregate_window_sec"] ?? 60),
    burstThreshold: Number(data?.["notify.burst_threshold"] ?? 15),
    burstWindowSec: Number(data?.["notify.burst_window_sec"] ?? 300),
    closedDisplayLimit: Number(data?.["display.closed_limit"] ?? 20),
    retentionEventsDays: Number(data?.["retention.events_days"] ?? 90),
    retentionOutboxDays: Number(data?.["retention.outbox_days"] ?? 30),
    retentionDeliveriesDays: Number(data?.["retention.webhook_deliveries_days"] ?? 30),
    featureIssues: data?.["feature.issues"] !== false,
    featurePRs: data?.["feature.pull_requests"] !== false,
    featureActions: data?.["feature.actions"] !== false,
    featureAlerts: data?.["feature.security_alerts"] !== false,
    featureStars: data?.["feature.stars"] !== false,
    featureWatches: data?.["feature.watches"] !== false,
  };
}

// 运行偏好区块提交负载：只含 prefs 键，避免覆盖另一区块字段。
function prefsBody(form: SettingsFormState): SystemSettings {
  return {
    "admin.timezone": form.timezone.trim() || "UTC",
    "digest.local_time": form.digestTime.trim() || "09:00",
    "digest.send_empty": form.digestEmpty,
    "report.weekly_enabled": form.reportWeekly,
    "report.weekly_day": form.reportWeeklyDay,
    "report.monthly_enabled": form.reportMonthly,
    "report.monthly_day": form.reportMonthlyDay,
    "notify.aggregate_window_sec": form.aggregateSec,
    "notify.burst_threshold": form.burstThreshold,
    "notify.burst_window_sec": form.burstWindowSec,
    "display.closed_limit": form.closedDisplayLimit,
    "retention.events_days": form.retentionEventsDays,
    "retention.outbox_days": form.retentionOutboxDays,
    "retention.webhook_deliveries_days": form.retentionDeliveriesDays,
  };
}

// 功能模块区块提交负载：只含 feature 键。
function featuresBody(form: SettingsFormState): SystemSettings {
  return {
    "feature.issues": form.featureIssues,
    "feature.pull_requests": form.featurePRs,
    "feature.actions": form.featureActions,
    "feature.security_alerts": form.featureAlerts,
    "feature.stars": form.featureStars,
    "feature.watches": form.featureWatches,
  };
}

// ---- AI 集成配置表单：API Key 留空表示不更新（不回显明文）----

interface AIFormState {
  enabled: boolean;
  baseURL: string;
  model: string;
  timeoutSec: number;
  maxTokens: number;
  digest: boolean;
  triage: boolean;
  apiKey: string;
}

// 从服务端 AI 配置快照构造表单初值；查询未完成或字段未设置（空串 / 0）时
// 回退到与后端 ai.Client 一致的显示默认值，避免把 0/空提交给后端触发校验失败。
function aiFormFromConfig(data: AIConfig | undefined): AIFormState {
  return {
    enabled: Boolean(data?.enabled),
    baseURL: data?.base_url || "https://api.openai.com/v1",
    model: data?.model || "gpt-4o-mini",
    timeoutSec: Number(data?.timeout_sec || 20),
    maxTokens: Number(data?.max_tokens || 800),
    digest: data?.digest_enabled !== false,
    triage: data?.triage_enabled !== false,
    apiKey: "",
  };
}

// AI 配置提交负载：跳过环境变量锁定的字段；API Key 仅在输入非空时提交。
function aiBody(form: AIFormState, cfg: AIConfig | undefined): AIConfigInput {
  const body: AIConfigInput = {};
  if (!cfg?.enabled_locked) body.enabled = form.enabled;
  if (!cfg?.base_url_locked) body.base_url = form.baseURL.trim() || undefined;
  if (!cfg?.model_locked) body.model = form.model.trim() || undefined;
  if (!cfg?.timeout_locked) body.timeout_sec = form.timeoutSec;
  if (!cfg?.max_tokens_locked) body.max_tokens = form.maxTokens;
  if (!cfg?.digest_enabled_locked) body.digest_enabled = form.digest;
  if (!cfg?.triage_enabled_locked) body.triage_enabled = form.triage;
  if (!cfg?.api_key_locked && form.apiKey.trim()) body.api_key = form.apiKey.trim();
  return body;
}

// useSettingsForm：初始值来自查询结果、编辑态独立维护、保存时合并为提交负载。
// 查询快照首次到达 / 保存后回填时整体重置一次（单 effect），不做逐字段同步。
function useSettingsForm(data: SystemSettings | undefined) {
  const [form, setForm] = useState<SettingsFormState>(() => formFromSettings(undefined));

  useEffect(() => {
    if (!data) return;
    setForm(formFromSettings(data));
  }, [data]);

  const set = <K extends keyof SettingsFormState>(key: K, value: SettingsFormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  return { form, set };
}

export function AboutPage() {
  const queryClient = useQueryClient();
  const version = useQuery(versionQueryOptions);
  const settings = useQuery(settingsQueryOptions);
  const { form, set } = useSettingsForm(settings.data);
  const [checking, setChecking] = useState(false);
  const [banner, setBanner] = useState<{
    kind: "update" | "latest" | "error" | "info";
    text: string;
    url?: string | null;
  } | null>(null);
  const [settingsMsg, setSettingsMsg] = useState("");
  const [featuresMsg, setFeaturesMsg] = useState("");
  const [aiMsg, setAIMsg] = useState("");
  const [passwordMsg, setPasswordMsg] = useState("");
  const [passwordError, setPasswordError] = useState<string>();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  // AI 集成配置：表单值由查询快照回填，API Key 留空表示不更新。
  const aiConfig = useQuery(aiConfigQueryOptions);
  const [aiForm, setAIForm] = useState<AIFormState>(() => aiFormFromConfig(undefined));
  useEffect(() => {
    if (!aiConfig.data) return;
    setAIForm(aiFormFromConfig(aiConfig.data));
  }, [aiConfig.data]);
  const setAI = <K extends keyof AIFormState>(key: K, value: AIFormState[K]) => {
    setAIForm((prev) => ({ ...prev, [key]: value }));
  };
  const saveAIConfigMut = useMutation({
    mutationFn: (body: AIConfigInput) => saveAIConfig(body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["ai-config"] });
      setAIMsg("AI 配置已保存。");
    },
  });
  function submitAIConfig() {
    setAIMsg("");
    saveAIConfigMut.mutate(aiBody(aiForm, aiConfig.data), {
      onError: () => setAIMsg(""),
    });
  }
  function clearAPIKey() {
    setAIMsg("");
    saveAIConfigMut.mutate({ clear_api_key: true }, { onError: () => setAIMsg("") });
  }

  // AI 连通性测试：复用 aiBody（跳过 env 锁定字段）把当前表单值作为临时覆盖提交，
  // 不写库、不改变运行时；成功/失败结果以 ok 区分展示。
  const [aiTest, setAITest] = useState<{ ok: boolean; message: string } | null>(null);
  const testAIMut = useMutation({
    mutationFn: (body: AIConfigInput) => testAIConnectivity(body),
    onSuccess: (res) => setAITest({ ok: res.ok, message: res.message }),
    onError: (err) => setAITest({ ok: false, message: toApiError(err).message }),
  });
  function runAITest() {
    setAITest(null);
    testAIMut.mutate(aiBody(aiForm, aiConfig.data));
  }

  const v = version.data || {};
  const saveSettings = useMutation({
    mutationFn: (body: SystemSettings) => saveSystemSettings(body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["system-settings"] });
    },
  });

  // 分块提交：只 PUT 本区块字段，避免慢网时用另一区未同步的本地 state 覆盖服务端。
  function submitSettings(section: "prefs" | "features") {
    const setMsg = section === "prefs" ? setSettingsMsg : setFeaturesMsg;
    const successText = section === "prefs" ? "系统偏好已保存。" : "功能模块开关已保存。";
    setMsg("");
    const body: SystemSettings = section === "prefs" ? prefsBody(form) : featuresBody(form);
    saveSettings.mutate(body, { onSuccess: () => setMsg(successText) });
  }
  const savePassword = useMutation({
    mutationFn: () => changePassword({ current_password: currentPassword, new_password: newPassword }),
    onSuccess: () => {
      setPasswordMsg("密码已更新，其它会话将失效，请使用新密码登录。");
      setPasswordError(undefined);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    },
    onError: (error) => {
      setPasswordMsg("");
      setPasswordError(toApiError(error).message);
    },
  });

  const checkUpdate = async (force = true) => {
    setChecking(true);
    try {
      const res = await checkForUpdates(force);
      const uc = res.update_check;
      if (!uc.enabled) { setBanner({ kind: "info", text: "远程更新检查已关闭。" }); return; }
      if (uc.error && !uc.latest_version) { setBanner({ kind: "error", text: `检查失败：${uc.error}` }); return; }
      const url = safeHttpUrl(uc.latest_url);
      if (uc.update_available && uc.latest_version) {
        setBanner({ kind: "update", text: `发现新版本 v${uc.latest_version}（当前 v${res.version.version || v.version || "—"}）`, url });
        return;
      }
      setBanner({ kind: "latest", text: uc.latest_version ? `已是最新（远程 v${uc.latest_version}${uc.cached ? " · 缓存" : ""}）` : "未获取到远程版本信息", url });
    } catch (e) {
      setBanner({ kind: "error", text: e instanceof Error ? e.message : "检查更新失败" });
    } finally {
      setChecking(false);
    }
  };

  function submitPassword() {
    setPasswordMsg("");
    setPasswordError(undefined);
    if (Array.from(newPassword).length < 12) { setPasswordError("新密码至少需要 12 个 Unicode 字符。"); return; }
    if (newPassword !== confirmPassword) { setPasswordError("两次输入的新密码不一致。"); return; }
    savePassword.mutate();
  }

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>关于与设置</h1>
          <p>版本信息、更新检查、账号与运行偏好。</p>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-product-title">
        <h2 id="about-product-title">你在用什么</h2>
        <p>单管理员实例，数据与密钥都在你控制的环境中。</p>
        <div className="link-row">
          <a className="quiet-button" href={DOCS_CONFIG} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 配置参考</a>
          <a className="quiet-button" href={DOCS_FAQ} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 常见问题</a>
          <a className="quiet-button" href={DOCS_CHANGELOG} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 变更日志</a>
          <Link className="quiet-button" to="/github">GitHub App 指引</Link>
          <Link className="quiet-button" to="/notifications">通知渠道</Link>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-build-title">
        <div className="onboarding-card__header">
          <h2 id="about-build-title">构建与运行</h2>
          <button type="button" className="quiet-button" disabled={checking || version.isLoading} onClick={() => void checkUpdate(true)}>
            {checking ? "检查中…" : "检查更新"}
          </button>
        </div>
        {version.isError ? <ErrorAlert title="无法加载版本" message={toApiError(version.error).message} errorCode={toApiError(version.error).errorCode} /> : null}
        {banner ? (
          <div className={`about-banner about-banner--${banner.kind}`}>
            <div>{banner.text}</div>
            {banner.kind === "update" ? <p className="muted">升级请拉取新镜像或替换二进制，并阅读 CHANGELOG。</p> : null}
            {banner.url ? <a href={banner.url} target="_blank" rel="noopener noreferrer">打开 Release 页面</a> : null}
          </div>
        ) : null}
        <dl className="meta-grid">
          <div><dt>版本</dt><dd>{v.version || "—"}</dd></div>
          <div><dt>构建渠道</dt><dd>{v.build_channel || "—"}</dd></div>
          <div><dt>Git SHA</dt><dd className="mono">{v.git_sha || "—"}</dd></div>
          <div><dt>分支</dt><dd>{v.git_branch || "—"}</dd></div>
          <div><dt>构建时间</dt><dd>{v.build_time || "—"}</dd></div>
          <div><dt>Go</dt><dd>{v.go_version || "—"}</dd></div>
          <div><dt>数据库</dt><dd>{v.database_driver || "—"}</dd></div>
          <div><dt>Schema</dt><dd className="mono">{v.schema_version || "—"}</dd></div>
          <div><dt>监听地址</dt><dd className="mono">{v.http_addr || "—"}</dd></div>
          <div><dt>Public Base URL</dt><dd className="mono">{v.public_base_url || "（未设置）"}</dd></div>
          <div><dt>更新检查</dt><dd>{v.update_check_enabled === false ? "已关闭" : "已开启"}</dd></div>
          <div><dt>Webhook 路径</dt><dd className="mono">{v.github?.webhook_path || "/webhooks/github"}</dd></div>
        </dl>
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-settings-title">
        <h2 id="about-settings-title">运行偏好</h2>
        <p className="field-hint">时区与摘要时间影响本地展示与摘要调度；聚合/超频窗口用于短时合并与突发降级。保存后立即写入数据库。</p>
        {settingsMsg ? <p className="success-banner" role="status">{settingsMsg}</p> : null}
        {settings.isError ? <ErrorAlert title="无法加载设置" message={toApiError(settings.error).message} errorCode={toApiError(settings.error).errorCode} /> : null}
        <div className="form-grid">
          <label className="field--plain"><span>管理员时区</span><input value={form.timezone} onChange={(e) => set("timezone", e.target.value)} placeholder="UTC 或 Asia/Shanghai" /></label>
          <label className="field--plain"><span>每日摘要本地时间</span><input value={form.digestTime} onChange={(e) => set("digestTime", e.target.value)} placeholder="09:00" /></label>
          <label className="field--plain"><span>通知聚合窗口（秒）</span><input type="number" min={1} max={86400} value={form.aggregateSec} onChange={(e) => set("aggregateSec", Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>超频阈值</span><input type="number" min={1} value={form.burstThreshold} onChange={(e) => set("burstThreshold", Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>超频窗口（秒）</span><input type="number" min={1} max={86400} value={form.burstWindowSec} onChange={(e) => set("burstWindowSec", Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>已关闭/已忽略显示数量</span><input type="number" min={1} max={200} value={form.closedDisplayLimit} onChange={(e) => set("closedDisplayLimit", Math.min(200, Math.max(1, Number(e.target.value) || 1)))} /></label>
          <label className="field--plain"><span>事件保留天数</span><input type="number" min={0} max={3650} value={form.retentionEventsDays} onChange={(e) => set("retentionEventsDays", Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
          <label className="field--plain"><span>投递记录保留天数</span><input type="number" min={0} max={3650} value={form.retentionOutboxDays} onChange={(e) => set("retentionOutboxDays", Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
          <label className="field--plain"><span>Webhook Delivery 保留天数</span><input type="number" min={0} max={3650} value={form.retentionDeliveriesDays} onChange={(e) => set("retentionDeliveriesDays", Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
        </div>
        <p className="field-hint">超频：在超频窗口内通知条数达到阈值时合并为摘要，避免刷屏。Closed/Dismissed 列表默认只显示最近指定数量条目。保留天数 0 表示禁用该类清理。</p>
        <label className="check-row"><input type="checkbox" checked={form.digestEmpty} onChange={(e) => set("digestEmpty", e.target.checked)} /><span>无事件时仍发送空摘要</span></label>
        <div className="report-schedule">
          <label className="check-row"><input type="checkbox" checked={form.reportWeekly} onChange={(e) => set("reportWeekly", e.target.checked)} /><span>启用每周报告</span></label>
          {form.reportWeekly ? (
            <label className="field--plain"><span>每周发送日</span>
              <select value={form.reportWeeklyDay} onChange={(e) => set("reportWeeklyDay", e.target.value)}>
                {WEEKLY_DAYS.map((d) => <option key={d.value} value={d.value}>{d.label}</option>)}
              </select>
            </label>
          ) : null}
          <label className="check-row"><input type="checkbox" checked={form.reportMonthly} onChange={(e) => set("reportMonthly", e.target.checked)} /><span>启用月度报告</span></label>
          {form.reportMonthly ? (
            <label className="field--plain"><span>每月发送日</span>
              <input type="number" min={1} max={28} value={form.reportMonthlyDay} onChange={(e) => set("reportMonthlyDay", Math.min(28, Math.max(1, Number(e.target.value) || 1)))} />
            </label>
          ) : null}
          <p className="field-hint">周报/月报与每日摘要共用渠道的「接收定期汇总」开关；发送时刻沿用每日摘要本地时间。正文在配置 AI 后由模型总结，否则使用分组模板。</p>
        </div>
        <button className="primary-button primary-button--inline" type="button" disabled={saveSettings.isPending || settings.isLoading} onClick={() => submitSettings("prefs")}>
          {saveSettings.isPending ? "保存中…" : "保存偏好"}
        </button>
        {saveSettings.isError ? <ErrorAlert title="保存失败" message={toApiError(saveSettings.error).message} errorCode={toApiError(saveSettings.error).errorCode} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-features-title">
        <h2 id="about-features-title">功能模块开关</h2>
        <p className="field-hint">关闭后：侧边栏隐藏对应入口、列表页显示已禁用；并停止该类型 Webhook 采集、对账与实时/摘要通知。仓库页对应开关会显示为关且不可改（仓级配置保留，重新开启全局后恢复）。渠道订阅勾选在全局关闭时也会灰显。</p>
        {featuresMsg ? <p className="success-banner" role="status">{featuresMsg}</p> : null}
        <label className="check-row"><input type="checkbox" checked={form.featureIssues} onChange={(e) => set("featureIssues", e.target.checked)} /><span>Issues</span></label>
        <label className="check-row"><input type="checkbox" checked={form.featurePRs} onChange={(e) => set("featurePRs", e.target.checked)} /><span>Pull Requests</span></label>
        <label className="check-row"><input type="checkbox" checked={form.featureActions} onChange={(e) => set("featureActions", e.target.checked)} /><span>Actions</span></label>
        <label className="check-row"><input type="checkbox" checked={form.featureAlerts} onChange={(e) => set("featureAlerts", e.target.checked)} /><span>安全告警</span></label>
        <label className="check-row"><input type="checkbox" checked={form.featureStars} onChange={(e) => set("featureStars", e.target.checked)} /><span>Star 事件</span></label>
        <label className="check-row"><input type="checkbox" checked={form.featureWatches} onChange={(e) => set("featureWatches", e.target.checked)} /><span>Watch 事件</span></label>
        <button className="primary-button primary-button--inline" type="button" disabled={saveSettings.isPending || settings.isLoading} onClick={() => submitSettings("features")}>
          {saveSettings.isPending ? "保存中…" : "保存开关"}
        </button>
        {saveSettings.isError ? <ErrorAlert title="保存失败" message={toApiError(saveSettings.error).message} errorCode={toApiError(saveSettings.error).errorCode} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-ai-title">
        <h2 id="about-ai-title">AI 集成</h2>
        <p className="field-hint">
          可选能力：每日/周/月报告正文由模型总结，新安全告警附带影响分析与处理建议。
          环境变量（<code>REPOSENTINEL_AI_*</code>）已设置的字段在此锁定；API Key 加密存储，不回显明文。
        </p>
        {aiMsg ? <p className="success-banner" role="status">{aiMsg}</p> : null}
        {aiConfig.isError ? <ErrorAlert title="无法加载 AI 配置" message={toApiError(aiConfig.error).message} errorCode={toApiError(aiConfig.error).errorCode} /> : null}
        <label className="check-row">
          <input type="checkbox" checked={aiForm.enabled} disabled={aiConfig.data?.enabled_locked} onChange={(e) => setAI("enabled", e.target.checked)} />
          <span>启用 AI（{aiConfig.data?.api_key_configured ? "API Key 已配置" : "API Key 未配置"}）</span>
        </label>
        <div className="form-grid">
          <label className="field--plain"><span>API Base URL</span><input value={aiForm.baseURL} disabled={aiConfig.data?.base_url_locked} onChange={(e) => setAI("baseURL", e.target.value)} placeholder="https://api.openai.com/v1" /></label>
          <label className="field--plain"><span>模型</span><input value={aiForm.model} disabled={aiConfig.data?.model_locked} onChange={(e) => setAI("model", e.target.value)} placeholder="gpt-4o-mini" /></label>
          <label className="field--plain"><span>请求超时（秒）</span><input type="number" min={1} max={3600} value={aiForm.timeoutSec} disabled={aiConfig.data?.timeout_locked} onChange={(e) => setAI("timeoutSec", Math.min(3600, Math.max(1, Number(e.target.value) || 1)))} /></label>
          <label className="field--plain"><span>输出 token 上限</span><input type="number" min={100} max={8000} value={aiForm.maxTokens} disabled={aiConfig.data?.max_tokens_locked} onChange={(e) => setAI("maxTokens", Math.min(8000, Math.max(100, Number(e.target.value) || 100)))} /></label>
          <label className="field--plain"><span>API Key（留空保持不变）</span><input type="password" autoComplete="off" value={aiForm.apiKey} disabled={aiConfig.data?.api_key_locked} onChange={(e) => setAI("apiKey", e.target.value)} placeholder={aiConfig.data?.api_key_configured ? "••••••••（已配置）" : "sk-…"} /></label>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={aiForm.digest} disabled={aiConfig.data?.digest_enabled_locked} onChange={(e) => setAI("digest", e.target.checked)} />
          <span>AI 摘要（每日/周/月报告）</span>
        </label>
        <label className="check-row">
          <input type="checkbox" checked={aiForm.triage} disabled={aiConfig.data?.triage_enabled_locked} onChange={(e) => setAI("triage", e.target.checked)} />
          <span>安全告警分诊</span>
        </label>
        <div className="channel-form__buttons">
          <button className="primary-button primary-button--inline" type="button" disabled={saveAIConfigMut.isPending || aiConfig.isLoading || aiConfig.isError} onClick={submitAIConfig}>
            {saveAIConfigMut.isPending ? "保存中…" : "保存 AI 配置"}
          </button>
          <button className="secondary-button" type="button" disabled={testAIMut.isPending || aiConfig.isLoading || aiConfig.isError} onClick={runAITest}>
            {testAIMut.isPending ? "测试中…" : "🔌 测试连通性"}
          </button>
          {!aiConfig.data?.api_key_locked && aiConfig.data?.api_key_configured ? (
            <button className="quiet-button" type="button" disabled={saveAIConfigMut.isPending || aiConfig.isError} onClick={clearAPIKey}>
              清除 API Key
            </button>
          ) : null}
        </div>
        {aiTest ? (
          aiTest.ok ? (
            <p className="success-banner" role="status">{aiTest.message}</p>
          ) : (
            <ErrorAlert title="连通性测试失败" message={aiTest.message} />
          )
        ) : null}
        {saveAIConfigMut.isError ? <ErrorAlert title="保存失败" message={toApiError(saveAIConfigMut.error).message} errorCode={toApiError(saveAIConfigMut.error).errorCode} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-password-title">
        <h2 id="about-password-title">修改管理员密码</h2>
        <p className="field-hint">修改成功后其它会话会失效。若已遗失当前密码，请在服务器上运行 <code>reposentinel admin reset-password --password-stdin</code>。</p>
        {passwordMsg ? <p className="success-banner" role="status">{passwordMsg}</p> : null}
        {passwordError ? <ErrorAlert title="无法修改密码" message={passwordError} /> : null}
        <label className="field--plain"><span>当前密码</span><input type="password" autoComplete="current-password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} /></label>
        <label className="field--plain"><span>新密码（至少 12 个字符）</span><input type="password" autoComplete="new-password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} /></label>
        <label className="field--plain"><span>确认新密码</span><input type="password" autoComplete="new-password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} /></label>
        <button className="primary-button primary-button--inline" type="button" disabled={savePassword.isPending || !currentPassword || !newPassword} onClick={submitPassword}>
          {savePassword.isPending ? "更新中…" : "更新密码"}
        </button>
      </section>

      <section className="onboarding-card" aria-labelledby="about-ops-title">
        <h2 id="about-ops-title">运维提示</h2>
        <ul className="bullet-list">
          <li>健康检查：<code>GET /health/live</code> 与 <code>GET /health/ready</code></li>
          <li>配置自检：<code>reposentinel doctor</code></li>
          <li>备份：<code>reposentinel backup</code>，同时保管 <code>REPOSENTINEL_ENCRYPTION_KEY</code></li>
          <li>日志：<code>REPOSENTINEL_LOG_LEVEL=debug</code></li>
        </ul>
      </section>
    </>
  );
}

function safeHttpUrl(raw?: string | null): string | null {
  if (!raw) return null;
  try {
    const u = new URL(String(raw).trim());
    if (u.protocol !== "https:" && u.protocol !== "http:") return null;
    return u.toString();
  } catch {
    return null;
  }
}
