import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, CircleDashed, ExternalLink } from "lucide-react";

import { CollapsiblePanel } from "../../components/collapsible-panel";
import { EmptyState } from "../../components/empty-state";
import { ApiErrorAlert, ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { changePassword } from "../auth/api";
import { toApiError } from "../../lib/api/errors";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import { syncStatusLabel } from "../../lib/format";
import {
  activateRepository,
  aiConfigQueryOptions,
  reconcileAll,
  reconcileRepository,
  repositoriesQueryOptions,
  saveAIConfig,
  saveSystemSettings,
  settingsQueryOptions,
  testAIConnectivity,
  type AIConfig,
  type AIConfigInput,
  type SystemSettings,
} from "./api";
import { NumberField } from "./number-field";
import { TwoFactorCard } from "./two-factor-card";

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
    "report.weekly_day": form.reportWeeklyDay.trim().toLowerCase() || "monday",
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
  retries: number;
  digest: boolean;
  triage: boolean;
  releaseSummary: boolean;
  apiKey: string;
}

// 从服务端 AI 配置快照构造表单初值；查询未完成或字段未设置（空串 / 0）时
// 回退到与后端 ai.Client 一致的显示默认值，避免把 0/空提交给后端触发校验失败。
// 重试次数默认 1（与后端 DefaultRetries 一致），0 为合法显式值（不重试）。
function aiFormFromConfig(data: AIConfig | undefined): AIFormState {
  return {
    enabled: Boolean(data?.enabled),
    baseURL: data?.base_url || "https://api.openai.com/v1",
    model: data?.model || "gpt-4o-mini",
    timeoutSec: Number(data?.timeout_sec || 20),
    maxTokens: Number(data?.max_tokens || 800),
    retries: data?.retries ?? 1,
    digest: data?.digest_enabled !== false,
    triage: data?.triage_enabled !== false,
    releaseSummary: data?.release_summary_enabled !== false,
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
  if (!cfg?.retries_locked) body.retries = form.retries;
  if (!cfg?.digest_enabled_locked) body.digest_enabled = form.digest;
  if (!cfg?.triage_enabled_locked) body.triage_enabled = form.triage;
  if (!cfg?.release_summary_enabled_locked) body.release_summary_enabled = form.releaseSummary;
  if (!cfg?.api_key_locked && form.apiKey.trim()) body.api_key = form.apiKey.trim();
  return body;
}

// useSettingsForm：初始值来自查询结果、编辑态独立维护、保存时合并为提交负载。
// 仅在查询快照首次到达时整体回填一次：分区块提交场景下，保存某区块后 invalidate
// 回传的新快照不应覆盖用户尚未保存的另一区块编辑（否则未保存改动被静默丢弃）。
function useSettingsForm(data: SystemSettings | undefined) {
  const [form, setForm] = useState<SettingsFormState>(() => formFromSettings(undefined));
  const hydratedRef = useRef(false);

  useEffect(() => {
    if (!data || hydratedRef.current) return;
    hydratedRef.current = true;
    setForm(formFromSettings(data));
  }, [data]);

  const set = <K extends keyof SettingsFormState>(key: K, value: SettingsFormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  return { form, set };
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const settings = useQuery(settingsQueryOptions);
  const repos = useQuery(repositoriesQueryOptions);
  const { form, set } = useSettingsForm(settings.data);
  // 时区输入即时校验提示：保存后后端会再强校验，这里在失焦时提前反馈。
  const [timezoneHint, setTimezoneHint] = useState("");
  const validateTimezone = (value: string) => {
    const v = value.trim();
    if (v === "") {
      setTimezoneHint("时区为空将回退 UTC。");
      return;
    }
    try {
      // Intl 对非法 IANA 时区抛 RangeError：与后端 time.LoadLocation 判定一致。
      new Intl.DateTimeFormat("en-US", { timeZone: v }).format();
      setTimezoneHint("");
    } catch {
      setTimezoneHint("无法识别的时区，请使用 IANA 名称（如 Asia/Shanghai）。");
    }
  };
  const [settingsMsg, setSettingsMsg] = useAutoDismiss();
  const [featuresMsg, setFeaturesMsg] = useAutoDismiss();
  const [aiMsg, setAIMsg] = useAutoDismiss();
  const [passwordMsg, setPasswordMsg] = useAutoDismiss();
  const [passwordError, setPasswordError] = useState<string>();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  // AI 集成配置：表单值由查询快照回填，API Key 留空表示不更新。
  // 与 useSettingsForm 同一守卫：仅首次到达时整体回填，保存后 invalidate 回传的
  // 新快照不应覆盖其它区块尚未保存的编辑。
  const aiConfig = useQuery(aiConfigQueryOptions);
  const [aiForm, setAIForm] = useState<AIFormState>(() => aiFormFromConfig(undefined));
  const aiHydratedRef = useRef(false);
  useEffect(() => {
    if (!aiConfig.data || aiHydratedRef.current) return;
    aiHydratedRef.current = true;
    setAIForm(aiFormFromConfig(aiConfig.data));
  }, [aiConfig.data]);
  const setAI = <K extends keyof AIFormState>(key: K, value: AIFormState[K]) => {
    setAIForm((prev) => ({ ...prev, [key]: value }));
  };
  const saveAIConfigMut = useMutation({
    mutationFn: (body: AIConfigInput) => saveAIConfig(body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["ai-config"] });
      setAIMsg("智能值守配置已保存。");
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

  const saveSettings = useMutation({
    mutationFn: (body: SystemSettings) => saveSystemSettings(body),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["system-settings"] });
    },
  });

  // 分块提交：只 PUT 本区块字段，避免慢网时用另一区未同步的本地 state 覆盖服务端。
  // 错误也按区块独立展示：任一区块保存失败不再同时在两处渲染同一错误。
  const [prefsError, setPrefsError] = useState<string | null>(null);
  const [featuresError, setFeaturesError] = useState<string | null>(null);
  function submitSettings(section: "prefs" | "features") {
    const setMsg = section === "prefs" ? setSettingsMsg : setFeaturesMsg;
    const setErr = section === "prefs" ? setPrefsError : setFeaturesError;
    const successText = section === "prefs" ? "系统偏好已保存。" : "功能模块开关已保存。";
    setMsg("");
    setErr(null);
    const body: SystemSettings = section === "prefs" ? prefsBody(form) : featuresBody(form);
    saveSettings.mutate(body, {
      onSuccess: () => setMsg(successText),
      onError: (err) => setErr(toApiError(err).message || "保存失败"),
    });
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

  function submitPassword() {
    setPasswordMsg("");
    setPasswordError(undefined);
    if (!currentPassword.trim()) { setPasswordError("请输入当前密码。"); return; }
    if (Array.from(newPassword).length < 12) { setPasswordError("新密码至少需要 12 个 Unicode 字符。"); return; }
    if (newPassword !== confirmPassword) { setPasswordError("两次输入的新密码不一致。"); return; }
    savePassword.mutate();
  }

  // ---- 仓库与基线对账（自仪表盘迁入）----
  const repoItems = repos.data?.items ?? [];
  // 排除已归档，避免归档仓继续占位；派生数据缓存避免每渲染重建。
  const visibleRepos = useMemo(
    () => repoItems.filter((r) => !r.is_archived && r.sync_status !== "archived"),
    [repoItems],
  );
  const baselineRepos = useMemo(
    () => visibleRepos.filter((r) => r.sync_status === "baseline_sync"),
    [visibleRepos],
  );
  // 行级忙碌与对账错误：只让当前操作的行转圈，失败时在对应区块内提示。
  const [reconcileBusyId, setReconcileBusyId] = useState<string | null>(null);
  const [reconcileError, setReconcileError] = useState<string | null>(null);
  // 面板折叠状态：默认展开，方便直接看到对账入口与仓库列表。
  const [reposOpen, setReposOpen] = useState(true);

  const invalidateReposAndDashboard = async () => {
    await queryClient.invalidateQueries({ queryKey: ["repositories"] });
    await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  };
  const activate = useMutation({
    mutationFn: activateRepository,
    onSuccess: invalidateReposAndDashboard,
  });
  const reconcileOne = useMutation({
    mutationFn: reconcileRepository,
    onMutate: (id) => {
      setReconcileBusyId(id);
      setReconcileError(null);
    },
    onSettled: () => setReconcileBusyId(null),
    onSuccess: invalidateReposAndDashboard,
    onError: (error) => setReconcileError(toApiError(error).message || "对账请求失败"),
  });
  const reconcileEverything = useMutation({
    mutationFn: reconcileAll,
    onMutate: () => setReconcileError(null),
    onSuccess: invalidateReposAndDashboard,
    onError: (error) => setReconcileError(toApiError(error).message || "对账请求失败"),
  });

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>设置</h1>
          <p>仓库对账、运行偏好、功能模块与账号。</p>
        </div>
      </section>

      <CollapsiblePanel
        id="repos"
        title="仓库与基线对账"
        open={reposOpen}
        onToggle={() => setReposOpen((prev) => !prev)}
        headerExtra={
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            disabled={reconcileEverything.isPending || visibleRepos.length === 0}
            aria-busy={reconcileEverything.isPending}
            title={visibleRepos.length === 0 ? "暂无自有仓可对账" : undefined}
            onClick={() => reconcileEverything.mutate()}
          >
            {reconcileEverything.isPending ? "对账排队中…" : "立即对账全部自有仓"}
          </button>
        }
      >
        <p className="field-hint">基线中的仓库抑制实时通知，对账成功会自动结束基线；也可单仓「立即放行」。仓库状态与开关在「仓库管理」维护。</p>
        {reconcileError ? <ErrorAlert title="对账失败" message={reconcileError} /> : null}
        <QueryGate
          query={repos}
          errorTitle="无法加载仓库与基线"
          isEmpty={visibleRepos.length === 0}
          emptyState={
            <EmptyState
              title="还没有关注中的仓库"
              description="安装 GitHub App 并配置 Webhook 后，仓库会自动出现。已归档仓库请在「仓库管理」查看。"
              action={
                <span className="link-row link-row--centered">
                  <Link to="/github">打开 GitHub App 页</Link>
                  <span className="muted">· 安装后点「从 GitHub 同步仓库」可补拉仓库</span>
                </span>
              }
            />
          }
        >
          <ul className="repo-baseline-list">
            {visibleRepos.map((repo) => {
              const name = repo.full_name || `${repo.owner}/${repo.name}`.replace(/^\/|\/$/g, "") || repo.id;
              const isActive = repo.sync_status === "active";
              const isBaseline = repo.sync_status === "baseline_sync";
              return (
                <li key={repo.id} className="repo-baseline-row" data-state={isActive ? "done" : "next"}>
                  <span className="repo-baseline-row__icon" aria-hidden="true">
                    {isActive ? <CheckCircle2 size={18} /> : <CircleDashed size={18} />}
                  </span>
                  <div className="repo-baseline-row__body">
                    <strong className="repo-baseline-row__name" title={name}>
                      {name}
                    </strong>
                    <span className="repo-baseline-row__meta">
                      {syncStatusLabel(repo.sync_status || "")}
                      <span aria-hidden="true"> · </span>
                      {repo.type === "external_public" ? "外部" : "自有"}
                    </span>
                  </div>
                  <div className="repo-baseline-row__actions">
                    {repo.html_url ? (
                      <a className="quiet-button quiet-button--compact" href={repo.html_url} target="_blank" rel="noopener noreferrer" title="在新窗口打开">
                        <ExternalLink size={14} aria-hidden="true" />
                        <span>GitHub</span>
                      </a>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                    {repo.type !== "external_public" ? (
                      <button
                        className="quiet-button quiet-button--compact"
                        type="button"
                        disabled={reconcileBusyId === repo.id}
                        onClick={() => reconcileOne.mutate(repo.id)}
                      >
                        {reconcileBusyId === repo.id ? "对账中…" : "对账"}
                      </button>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                    {isBaseline ? (
                      <button
                        className="quiet-button quiet-button--compact quiet-button--primary-ghost"
                        type="button"
                        aria-busy={activate.isPending && activate.variables === repo.id}
                        disabled={activate.isPending && activate.variables === repo.id}
                        onClick={() => activate.mutate(repo.id)}
                      >
                        {activate.isPending && activate.variables === repo.id ? "放行中…" : "立即放行"}
                      </button>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </QueryGate>
        {baselineRepos.length > 0 ? (
          <p className="panel-footnote">
            基线中抑制实时通知，避免首次同步洪流。对账成功后会自动结束基线；也可点「立即放行」跳过等待。
          </p>
        ) : null}
      </CollapsiblePanel>

      <section className="onboarding-card channel-form" aria-labelledby="settings-prefs-title">
        <h2 id="settings-prefs-title">运行偏好</h2>
        <p className="field-hint">时区与摘要时间影响本地展示与摘要调度；聚合/超频窗口用于短时合并与突发降级。保存后立即写入数据库。</p>
        {settingsMsg ? <p className="success-banner" role="status">{settingsMsg}</p> : null}
        {settings.isError ? <ApiErrorAlert error={settings.error} title="无法加载设置" /> : null}
        <div className="form-grid">
          <label className="field--plain">
            <span>管理员时区</span>
            <input size={16} value={form.timezone} onChange={(e) => set("timezone", e.target.value)} onBlur={(e) => validateTimezone(e.target.value)} placeholder="UTC 或 Asia/Shanghai" />
          </label>
          {timezoneHint ? <p className="field-hint" role="status">{timezoneHint}</p> : null}
          <label className="field--plain">
            <span>每日摘要本地时间</span>
            <input size={6} value={form.digestTime} onChange={(e) => set("digestTime", e.target.value)} placeholder="09:00" />
          </label>
          <label className="field--plain">
            <span>通知聚合窗口（秒）</span>
            <NumberField min={1} max={86400} integer value={form.aggregateSec} onChange={(v) => set("aggregateSec", v)} />
          </label>
          <label className="field--plain">
            <span>超频阈值</span>
            <NumberField min={1} integer value={form.burstThreshold} onChange={(v) => set("burstThreshold", v)} />
          </label>
          <label className="field--plain">
            <span>超频窗口（秒）</span>
            <NumberField min={1} max={86400} integer value={form.burstWindowSec} onChange={(v) => set("burstWindowSec", v)} />
          </label>
          <label className="field--plain">
            <span>已关闭/已忽略显示数量</span>
            <NumberField min={1} max={200} integer value={form.closedDisplayLimit} onChange={(v) => set("closedDisplayLimit", v)} />
          </label>
          <label className="field--plain">
            <span>事件保留天数</span>
            <NumberField min={0} max={3650} integer value={form.retentionEventsDays} onChange={(v) => set("retentionEventsDays", v)} />
          </label>
          <label className="field--plain">
            <span>投递记录保留天数</span>
            <NumberField min={0} max={3650} integer value={form.retentionOutboxDays} onChange={(v) => set("retentionOutboxDays", v)} />
          </label>
          <label className="field--plain">
            <span>Webhook Delivery 保留天数</span>
            <NumberField min={0} max={3650} integer value={form.retentionDeliveriesDays} onChange={(v) => set("retentionDeliveriesDays", v)} />
          </label>
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
          <label className="check-row"><input type="checkbox" checked={form.reportMonthly} onChange={(e) => set("reportMonthly", e.target.checked)} /><span>启用每月报告</span></label>
          {form.reportMonthly ? (
            <label className="field--plain"><span>每月发送日</span>
              <NumberField min={1} max={28} integer value={form.reportMonthlyDay} onChange={(v) => set("reportMonthlyDay", v)} />
            </label>
          ) : null}
          <p className="field-hint">周报/月报与每日简报共用渠道的「接收定期汇总」开关；发送时刻沿用每日简报本地时间。正文在配置智能值守后由模型生成，否则使用分组模板。</p>
        </div>
        <button className="primary-button primary-button--inline" type="button" disabled={saveSettings.isPending || settings.isLoading} onClick={() => submitSettings("prefs")}>
          {saveSettings.isPending ? "保存中…" : "保存偏好"}
        </button>
        {prefsError ? <ErrorAlert title="保存失败" message={prefsError} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="settings-features-title">
        <h2 id="settings-features-title">功能模块开关</h2>
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
        {featuresError ? <ErrorAlert title="保存失败" message={featuresError} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="settings-ai-title">
        <h2 id="settings-ai-title">智能值守</h2>
        <p className="field-hint">
          可选能力：每日/周/月报告正文由模型总结，新安全告警附带影响分析与处理建议。
          网络波动瞬时失败（超时/5xx 等）按「重试次数」自动重试，0 表示不重试。
          环境变量（<code>REPOSENTINEL_AI_*</code>）已设置的字段在此锁定；API Key 加密存储，不回显明文。
        </p>
        {aiMsg ? <p className="success-banner" role="status">{aiMsg}</p> : null}
        {aiConfig.isError ? <ApiErrorAlert error={aiConfig.error} title="无法加载智能值守配置" /> : null}
        <label className="check-row">
          <input type="checkbox" checked={aiForm.enabled} disabled={aiConfig.data?.enabled_locked} onChange={(e) => setAI("enabled", e.target.checked)} />
          <span>启用智能值守（{aiConfig.data?.api_key_configured ? "API Key 已配置" : "API Key 未配置"}）</span>
        </label>
        <div className="form-grid">
          <label className="field--plain"><span>API Base URL</span><input size={30} value={aiForm.baseURL} disabled={aiConfig.data?.base_url_locked} onChange={(e) => setAI("baseURL", e.target.value)} placeholder="https://api.openai.com/v1" /></label>
          <label className="field--plain">
            <span>模型</span>
            <input size={16} value={aiForm.model} disabled={aiConfig.data?.model_locked} onChange={(e) => setAI("model", e.target.value)} placeholder="gpt-4o-mini" />
          </label>
          <label className="field--plain">
            <span>请求超时（秒）</span>
            <NumberField min={1} max={3600} integer value={aiForm.timeoutSec} disabled={aiConfig.data?.timeout_locked} onChange={(v) => setAI("timeoutSec", v)} />
          </label>
          <label className="field--plain">
            <span>重试次数</span>
            <NumberField min={0} max={5} integer value={aiForm.retries} disabled={aiConfig.data?.retries_locked} onChange={(v) => setAI("retries", v)} />
          </label>
          <label className="field--plain">
            <span>输出 token 上限</span>
            <NumberField min={100} max={8000} integer value={aiForm.maxTokens} disabled={aiConfig.data?.max_tokens_locked} onChange={(v) => setAI("maxTokens", v)} />
          </label>
          <label className="field--plain">
            <span>API Key（留空保持不变）</span>
            <input size={32} type="password" autoComplete="off" value={aiForm.apiKey} disabled={aiConfig.data?.api_key_locked} onChange={(e) => setAI("apiKey", e.target.value)} placeholder={aiConfig.data?.api_key_configured ? "••••••••（已配置）" : "sk-…"} />
          </label>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={aiForm.digest} disabled={aiConfig.data?.digest_enabled_locked} onChange={(e) => setAI("digest", e.target.checked)} />
          <span>智能简报（每日/周/月报告）</span>
        </label>
        <label className="check-row">
          <input type="checkbox" checked={aiForm.triage} disabled={aiConfig.data?.triage_enabled_locked} onChange={(e) => setAI("triage", e.target.checked)} />
          <span>安全告警分诊</span>
        </label>
        <label className="check-row">
          <input type="checkbox" checked={aiForm.releaseSummary} disabled={aiConfig.data?.release_summary_enabled_locked} onChange={(e) => setAI("releaseSummary", e.target.checked)} />
          <span>Release 更新速览（star 仓库新版本通知附翻译要点）</span>
        </label>
        <div className="channel-form__buttons">
          <button className="primary-button primary-button--inline" type="button" disabled={saveAIConfigMut.isPending || aiConfig.isLoading || aiConfig.isError} onClick={submitAIConfig}>
            {saveAIConfigMut.isPending ? "保存中…" : "保存智能值守配置"}
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
        {saveAIConfigMut.isError ? <ApiErrorAlert error={saveAIConfigMut.error} title="保存失败" /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="settings-password-title">
        <h2 id="settings-password-title">修改管理员密码</h2>
        <p className="field-hint">修改成功后其它会话会失效。若已遗失当前密码，请在服务器上运行 <code>reposentinel admin reset-password --password-stdin</code>。</p>
        {passwordMsg ? <p className="success-banner" role="status">{passwordMsg}</p> : null}
        {passwordError ? <ErrorAlert title="无法修改密码" message={passwordError} /> : null}
        <label className="field--plain">
          <span>当前密码</span>
          <input size={24} type="password" autoComplete="current-password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} />
        </label>
        <label className="field--plain">
          <span>新密码（至少 12 个字符）</span>
          <input size={24} type="password" autoComplete="new-password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
        </label>
        <label className="field--plain">
          <span>确认新密码</span>
          <input size={24} type="password" autoComplete="new-password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} />
        </label>
        <button className="primary-button primary-button--inline" type="button" disabled={savePassword.isPending || !currentPassword || !newPassword} onClick={submitPassword}>
          {savePassword.isPending ? "更新中…" : "更新密码"}
        </button>
      </section>

      <TwoFactorCard />
    </>
  );
}
