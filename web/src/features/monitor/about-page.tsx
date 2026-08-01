import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";

import { ErrorAlert } from "../../components/error-alert";
import { changePassword } from "../auth/api";
import { toApiError } from "../../lib/api/errors";
import {
  checkForUpdates,
  saveSystemSettings,
  settingsQueryOptions,
  versionQueryOptions,
  type SystemSettings,
} from "./api";

const DOCS_CONFIG = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md";
const DOCS_FAQ = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/faq.md";
const DOCS_CHANGELOG = "https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md";

export function AboutPage() {
  const queryClient = useQueryClient();
  const version = useQuery(versionQueryOptions);
  const settings = useQuery(settingsQueryOptions);
  const [checking, setChecking] = useState(false);
  const [banner, setBanner] = useState<{
    kind: "update" | "latest" | "error" | "info";
    text: string;
    url?: string | null;
  } | null>(null);
  const [settingsMsg, setSettingsMsg] = useState("");
  const [featuresMsg, setFeaturesMsg] = useState("");
  const [passwordMsg, setPasswordMsg] = useState("");
  const [passwordError, setPasswordError] = useState<string>();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [timezone, setTimezone] = useState("UTC");
  const [digestTime, setDigestTime] = useState("09:00");
  const [digestEmpty, setDigestEmpty] = useState(false);
  const [aggregateSec, setAggregateSec] = useState(60);
  const [burstThreshold, setBurstThreshold] = useState(15);
  const [burstWindowSec, setBurstWindowSec] = useState(300);
  const [closedDisplayLimit, setClosedDisplayLimit] = useState(20);
  const [retentionEventsDays, setRetentionEventsDays] = useState(90);
  const [retentionOutboxDays, setRetentionOutboxDays] = useState(30);
  const [retentionDeliveriesDays, setRetentionDeliveriesDays] = useState(30);
  const [featureIssues, setFeatureIssues] = useState(true);
  const [featurePRs, setFeaturePRs] = useState(true);
  const [featureActions, setFeatureActions] = useState(true);
  const [featureAlerts, setFeatureAlerts] = useState(true);

  useEffect(() => {
    if (!settings.data) return;
    setTimezone(String(settings.data["admin.timezone"] ?? "UTC"));
    setDigestTime(String(settings.data["digest.local_time"] ?? "09:00"));
    setDigestEmpty(Boolean(settings.data["digest.send_empty"]));
    setAggregateSec(Number(settings.data["notify.aggregate_window_sec"] ?? 60));
    setBurstThreshold(Number(settings.data["notify.burst_threshold"] ?? 15));
    setBurstWindowSec(Number(settings.data["notify.burst_window_sec"] ?? 300));
    setClosedDisplayLimit(Number(settings.data["display.closed_limit"] ?? 20));
    setRetentionEventsDays(Number(settings.data["retention.events_days"] ?? 90));
    setRetentionOutboxDays(Number(settings.data["retention.outbox_days"] ?? 30));
    setRetentionDeliveriesDays(Number(settings.data["retention.webhook_deliveries_days"] ?? 30));
    setFeatureIssues(settings.data["feature.issues"] !== false);
    setFeaturePRs(settings.data["feature.pull_requests"] !== false);
    setFeatureActions(settings.data["feature.actions"] !== false);
    setFeatureAlerts(settings.data["feature.security_alerts"] !== false);
  }, [settings.data]);

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
    const body: SystemSettings =
      section === "prefs"
        ? {
            "admin.timezone": timezone.trim() || "UTC",
            "digest.local_time": digestTime.trim() || "09:00",
            "digest.send_empty": digestEmpty,
            "notify.aggregate_window_sec": aggregateSec,
            "notify.burst_threshold": burstThreshold,
            "notify.burst_window_sec": burstWindowSec,
            "display.closed_limit": closedDisplayLimit,
            "retention.events_days": retentionEventsDays,
            "retention.outbox_days": retentionOutboxDays,
            "retention.webhook_deliveries_days": retentionDeliveriesDays,
          }
        : {
            "feature.issues": featureIssues,
            "feature.pull_requests": featurePRs,
            "feature.actions": featureActions,
            "feature.security_alerts": featureAlerts,
          };
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
          <label className="field--plain"><span>管理员时区</span><input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="UTC 或 Asia/Shanghai" /></label>
          <label className="field--plain"><span>每日摘要本地时间</span><input value={digestTime} onChange={(e) => setDigestTime(e.target.value)} placeholder="09:00" /></label>
          <label className="field--plain"><span>通知聚合窗口（秒）</span><input type="number" min={1} max={86400} value={aggregateSec} onChange={(e) => setAggregateSec(Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>超频阈值</span><input type="number" min={1} value={burstThreshold} onChange={(e) => setBurstThreshold(Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>超频窗口（秒）</span><input type="number" min={1} max={86400} value={burstWindowSec} onChange={(e) => setBurstWindowSec(Number(e.target.value) || 1)} /></label>
          <label className="field--plain"><span>已关闭/已忽略显示数量</span><input type="number" min={1} max={200} value={closedDisplayLimit} onChange={(e) => setClosedDisplayLimit(Math.min(200, Math.max(1, Number(e.target.value) || 1)))} /></label>
          <label className="field--plain"><span>事件保留天数</span><input type="number" min={0} max={3650} value={retentionEventsDays} onChange={(e) => setRetentionEventsDays(Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
          <label className="field--plain"><span>投递记录保留天数</span><input type="number" min={0} max={3650} value={retentionOutboxDays} onChange={(e) => setRetentionOutboxDays(Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
          <label className="field--plain"><span>Webhook Delivery 保留天数</span><input type="number" min={0} max={3650} value={retentionDeliveriesDays} onChange={(e) => setRetentionDeliveriesDays(Math.min(3650, Math.max(0, Number(e.target.value) || 0)))} /></label>
        </div>
        <p className="field-hint">超频：在超频窗口内通知条数达到阈值时合并为摘要，避免刷屏。Closed/Dismissed 列表默认只显示最近指定数量条目。保留天数 0 表示禁用该类清理。</p>
        <label className="check-row"><input type="checkbox" checked={digestEmpty} onChange={(e) => setDigestEmpty(e.target.checked)} /><span>无事件时仍发送空摘要</span></label>
        <button className="primary-button primary-button--inline" type="button" disabled={saveSettings.isPending || settings.isLoading} onClick={() => submitSettings("prefs")}>
          {saveSettings.isPending ? "保存中…" : "保存偏好"}
        </button>
        {saveSettings.isError ? <ErrorAlert title="保存失败" message={toApiError(saveSettings.error).message} errorCode={toApiError(saveSettings.error).errorCode} /> : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-features-title">
        <h2 id="about-features-title">功能模块开关</h2>
        <p className="field-hint">关闭后：侧边栏隐藏对应入口、列表页显示已禁用；并停止该类型 Webhook 采集、对账与实时/摘要通知。仓库页对应开关会显示为关且不可改（仓级配置保留，重新开启全局后恢复）。渠道订阅勾选在全局关闭时也会灰显。</p>
        {featuresMsg ? <p className="success-banner" role="status">{featuresMsg}</p> : null}
        <label className="check-row"><input type="checkbox" checked={featureIssues} onChange={(e) => setFeatureIssues(e.target.checked)} /><span>Issues</span></label>
        <label className="check-row"><input type="checkbox" checked={featurePRs} onChange={(e) => setFeaturePRs(e.target.checked)} /><span>Pull Requests</span></label>
        <label className="check-row"><input type="checkbox" checked={featureActions} onChange={(e) => setFeatureActions(e.target.checked)} /><span>Actions</span></label>
        <label className="check-row"><input type="checkbox" checked={featureAlerts} onChange={(e) => setFeatureAlerts(e.target.checked)} /><span>安全告警</span></label>
        <button className="primary-button primary-button--inline" type="button" disabled={saveSettings.isPending || settings.isLoading} onClick={() => submitSettings("features")}>
          {saveSettings.isPending ? "保存中…" : "保存开关"}
        </button>
        {saveSettings.isError ? <ErrorAlert title="保存失败" message={toApiError(saveSettings.error).message} errorCode={toApiError(saveSettings.error).errorCode} /> : null}
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
