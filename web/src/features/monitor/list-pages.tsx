import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, Circle, Copy, ExternalLink } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { changePassword } from "../auth/api";
import { apiRequest } from "../../lib/api/client";
import { toApiError } from "../../lib/api/errors";
import {
  addExternalRepository,
  checkForUpdates,
  installationsQueryOptions,
  saveSystemSettings,
  settingsQueryOptions,
  versionQueryOptions,
  type SystemSettings,
  type VersionInfo,
} from "./api";

interface Page<T> {
  items: T[];
  total: number;
}

interface WorkItem {
  id: string;
  kind: string;
  number: number;
  title: string;
  state: string;
  html_url: string;
  author: string;
}

interface WorkflowRun {
  id: string;
  workflow_name: string;
  run_number: number;
  head_branch: string;
  conclusion?: string | null;
  status: string;
  html_url: string;
}

interface SecurityAlert {
  id: string;
  alert_kind: string;
  alert_number: number;
  state: string;
  severity: string;
  rule_or_dependency: string;
  html_url: string;
}

const DOCS_GITHUB_APP =
  "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/deploy/docker.md#4-github-app-webhook";
const DOCS_CONFIG =
  "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md";
const DOCS_FAQ = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/faq.md";
const DOCS_CHANGELOG = "https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md";
const GITHUB_NEW_APP = "https://github.com/settings/apps/new";

export function WorkItemsPage() {
  const kind = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("kind") || "" : "";
  const q = useQuery({
    queryKey: ["work-items", kind],
    queryFn: () =>
      apiRequest<Page<WorkItem>>(`/api/v1/work-items?per_page=50${kind ? `&kind=${encodeURIComponent(kind)}` : ""}`),
  });
  return (
    <ListShell
      eyebrow="仓库"
      title="Issues / Pull Requests"
      description="自有仓与外部公开仓的工作项。Webhook 或对账同步后会出现在这里。"
    >
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((it) => (
            <li key={it.id}>
              <span className="event-kind">{it.kind}</span>
              <strong>
                #{it.number} {it.title}
              </strong>
              <span className="muted">{it.state}</span>
              {it.html_url ? (
                <a className="quiet-button" href={it.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="暂无工作项"
          description="先完成 GitHub App 安装与 Webhook，再在仪表盘确认仓库已出现。"
          action={<Link to="/github">去配置 GitHub App</Link>}
        />
      )}
    </ListShell>
  );
}

export function ActionsPage() {
  const q = useQuery({
    queryKey: ["workflow-runs"],
    queryFn: () => apiRequest<Page<WorkflowRun>>("/api/v1/workflow-runs?per_page=50"),
  });
  return (
    <ListShell eyebrow="仓库" title="Actions" description="Workflow Run 结论与恢复状态，来自 workflow_run 事件或对账。">
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((run) => (
            <li key={run.id}>
              <span className="event-kind">{run.conclusion || run.status}</span>
              <strong>
                {run.workflow_name} #{run.run_number}
              </strong>
              <span className="muted">{run.head_branch}</span>
              {run.html_url ? (
                <a className="quiet-button" href={run.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="暂无 Actions 运行"
          description="App 需具备 Actions 读取权限，并订阅 workflow_run 事件。"
          action={<Link to="/github">查看配置步骤</Link>}
        />
      )}
    </ListShell>
  );
}

export function SecurityPage() {
  const q = useQuery({
    queryKey: ["security-alerts"],
    queryFn: () => apiRequest<Page<SecurityAlert>>("/api/v1/security-alerts?per_page=50"),
  });
  return (
    <ListShell
      eyebrow="仓库"
      title="安全告警"
      description="Dependabot / Code Scanning / Secret Scanning。外部公开仓不读取安全告警。"
    >
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((a) => (
            <li key={a.id}>
              <span className="event-kind">{a.alert_kind}</span>
              <strong>
                #{a.alert_number} {a.rule_or_dependency || a.severity}
              </strong>
              <span className="muted">
                {a.state} · {a.severity}
              </span>
              {a.html_url ? (
                <a className="quiet-button" href={a.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="暂无安全告警"
          description="在 GitHub 仓库开启安全功能，并为 App 授予相应只读权限后，告警会出现在这里。"
          action={<Link to="/github">查看权限清单</Link>}
        />
      )}
    </ListShell>
  );
}

export function GitHubPage() {
  const queryClient = useQueryClient();
  const version = useQuery(versionQueryOptions);
  const installations = useQuery(installationsQueryOptions);
  const [externalName, setExternalName] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "ok" | "fail">("idle");
  const [formMessage, setFormMessage] = useState("");

  const gh = version.data?.github;
  const webhookURL = useMemo(() => buildWebhookURL(version.data), [version.data]);

  const addExternal = useMutation({
    mutationFn: () => addExternalRepository(externalName.trim()),
    onSuccess: async () => {
      setExternalName("");
      setFormMessage("外部公开仓库已登记，将进入基线同步。");
      await queryClient.invalidateQueries({ queryKey: ["repositories"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const checklist = [
    { ok: Boolean(gh?.webhook_secret_configured), label: "Webhook Secret", hint: "REPOSENTINEL_GITHUB_WEBHOOK_SECRET" },
    { ok: Boolean(gh?.app_id_configured), label: "App ID", hint: "REPOSENTINEL_GITHUB_APP_ID" },
    { ok: Boolean(gh?.client_id_configured), label: "Client ID", hint: "REPOSENTINEL_GITHUB_CLIENT_ID" },
    { ok: Boolean(gh?.private_key_configured), label: "私钥文件可读", hint: "REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH" },
    { ok: Boolean(gh?.external_pat_configured), label: "外部仓 PAT（可选）", hint: "REPOSENTINEL_EXTERNAL_PAT" },
  ];
  const readyCount = checklist.filter((c) => c.ok).length;
  const coreReady = Boolean(gh?.webhook_secret_configured);

  async function copyWebhook() {
    try {
      await navigator.clipboard.writeText(webhookURL);
      setCopyState("ok");
      window.setTimeout(() => setCopyState("idle"), 1800);
    } catch {
      setCopyState("fail");
      window.setTimeout(() => setCopyState("idle"), 2200);
    }
  }

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>GitHub App</h1>
          <p>
            凭据只能通过环境变量 / 配置文件注入，管理台不收集 App Secret。本页给出状态核对、Webhook 地址与安装步骤；Installation
            会在 GitHub 推送事件后自动出现。
          </p>
        </div>
      </section>

      {version.isError ? (
        <ErrorAlert
          title="无法加载配置状态"
          message={toApiError(version.error).message}
          errorCode={toApiError(version.error).errorCode}
        />
      ) : null}

      <section className="onboarding-card" aria-labelledby="gh-status-title">
        <div className="onboarding-card__header">
          <h2 id="gh-status-title">运行时配置状态</h2>
          <span className="muted">
            {version.isPending ? "检查中…" : `${readyCount} / ${checklist.length} 项已就绪`}
          </span>
        </div>
        <p className="field-hint">
          以下仅显示「是否已配置」，不会回显 Secret。修改环境变量后需<strong>重启服务</strong>才会更新。
        </p>
        <ul className="status-checklist">
          {checklist.map((item) => (
            <li key={item.label} data-ok={item.ok ? "true" : "false"}>
              {item.ok ? <CheckCircle2 size={18} aria-hidden="true" /> : <Circle size={18} aria-hidden="true" />}
              <div>
                <strong>{item.label}</strong>
                <span className="muted">
                  {item.ok ? "已配置" : "未配置"} · <code>{item.hint}</code>
                </span>
              </div>
            </li>
          ))}
        </ul>
        {!coreReady ? (
          <p className="callout callout--warn" role="status">
            尚未配置入站 Webhook Secret 时，GitHub 推送会被拒绝。请先设置{" "}
            <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code> 并与 GitHub App 中填写的 Secret 保持一致。
          </p>
        ) : (
          <p className="callout callout--ok" role="status">
            入站验签已就绪。若仍无 Installation，请确认 Webhook URL 可被 GitHub 访问，并已安装 App 到目标仓库。
          </p>
        )}
      </section>

      <section className="onboarding-card" aria-labelledby="gh-webhook-title">
        <h2 id="gh-webhook-title">Webhook 接入</h2>
        <p className="field-hint">
          在 GitHub App 设置里填写下方 URL，Content type 选 <code>application/json</code>。Secret 使用与环境变量相同的值。
        </p>
        <div className="copy-row">
          <code className="copy-row__value">{webhookURL}</code>
          <button className="quiet-button" type="button" onClick={() => void copyWebhook()}>
            <Copy size={15} aria-hidden="true" />
            {copyState === "ok" ? "已复制" : copyState === "fail" ? "复制失败" : "复制"}
          </button>
        </div>
        {!version.data?.public_base_url ? (
          <p className="field-hint">
            当前未设置 <code>REPOSENTINEL_PUBLIC_BASE_URL</code>，上式使用浏览器地址推导。生产环境请配置公网 HTTPS 基址，避免 Cookie 与 Webhook 地址不一致。
          </p>
        ) : null}
        <div className="link-row">
          <a className="quiet-button" href={GITHUB_NEW_APP} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 创建 GitHub App
          </a>
          <a className="quiet-button" href={DOCS_GITHUB_APP} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 部署文档
          </a>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-steps-title">
        <h2 id="gh-steps-title">推荐配置步骤</h2>
        <ol className="guide-steps">
          <li>
            <strong>创建 App</strong>
            <span>在 GitHub 开发者设置中新建 GitHub App，Webhook 勾选 Active，URL 填上表地址。</span>
          </li>
          <li>
            <strong>权限（只读为主）</strong>
            <span>
              Repository：Contents、Issues、Pull requests、Actions、Metadata、Dependabot alerts、Code scanning
              alerts、Secret scanning alerts（按需）。
            </span>
          </li>
          <li>
            <strong>订阅事件</strong>
            <span>
              <code>issues</code>、<code>pull_request</code>、<code>workflow_run</code>、三类{" "}
              <code>*_alert</code>、<code>installation</code>、<code>installation_repositories</code>、
              <code>repository</code>。
            </span>
          </li>
          <li>
            <strong>注入运行时</strong>
            <span>
              写入 App ID、Client ID、私钥路径与 <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code>，然后重启
              RepoSentinel。
            </span>
          </li>
          <li>
            <strong>安装到仓库</strong>
            <span>Install App → 选择账号与仓库。新仓会先进入「基线中」，在仪表盘点「完成基线」后才发实时通知。</span>
          </li>
        </ol>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-install-title">
        <div className="onboarding-card__header">
          <h2 id="gh-install-title">已记录的 Installation</h2>
          <button
            className="quiet-button"
            type="button"
            disabled={installations.isFetching}
            onClick={() => void installations.refetch()}
          >
            {installations.isFetching ? "刷新中…" : "刷新"}
          </button>
        </div>
        {installations.isError ? (
          <ErrorAlert
            title="无法加载 Installation"
            message={toApiError(installations.error).message}
            errorCode={toApiError(installations.error).errorCode}
          />
        ) : null}
        {(installations.data?.items ?? []).length ? (
          <ul className="event-list">
            {installations.data!.items.map((inst) => (
              <li key={inst.id}>
                <span className="event-kind">{inst.account_type || "account"}</span>
                <strong>{inst.account_login || "（未知账号）"}</strong>
                <span className="muted">installation {inst.installation_id}</span>
                <span className="muted">{inst.suspended === "true" ? "已挂起" : "正常"}</span>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState
            eyebrow="等待事件"
            title="尚未收到 Installation"
            description="安装 App 并确保 Webhook 可达后，installation / installation_repositories 事件会写入此处。仅配置环境变量不会自动出现记录。"
          />
        )}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="gh-external-title">
        <h2 id="gh-external-title">登记外部公开仓库</h2>
        <p className="field-hint">
          用于关注非本 App 安装范围内的<strong>公开</strong>仓（最多 20 个）。不读取安全告警；可选配置{" "}
          <code>REPOSENTINEL_EXTERNAL_PAT</code> 以提高 API 配额。
        </p>
        {formMessage ? (
          <p className="success-banner" role="status">
            {formMessage}
          </p>
        ) : null}
        <label className="field--plain">
          <span>仓库全名</span>
          <input
            value={externalName}
            onChange={(e) => setExternalName(e.target.value)}
            placeholder="owner/repo"
            autoComplete="off"
          />
        </label>
        <button
          className="primary-button primary-button--inline"
          type="button"
          disabled={addExternal.isPending || !externalName.trim()}
          onClick={() => {
            setFormMessage("");
            addExternal.mutate();
          }}
        >
          {addExternal.isPending ? "登记中…" : "添加外部仓"}
        </button>
        {addExternal.isError ? (
          <ErrorAlert
            title="无法添加"
            message={toApiError(addExternal.error).message}
            errorCode={toApiError(addExternal.error).errorCode}
          />
        ) : null}
      </section>
    </>
  );
}

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

  useEffect(() => {
    if (!settings.data) return;
    setTimezone(String(settings.data["admin.timezone"] ?? "UTC"));
    setDigestTime(String(settings.data["digest.local_time"] ?? "09:00"));
    setDigestEmpty(Boolean(settings.data["digest.send_empty"]));
    setAggregateSec(Number(settings.data["notify.aggregate_window_sec"] ?? 60));
    setBurstThreshold(Number(settings.data["notify.burst_threshold"] ?? 15));
  }, [settings.data]);

  const v = version.data || {};
  const saveSettings = useMutation({
    mutationFn: (body: SystemSettings) => saveSystemSettings(body),
    onSuccess: async () => {
      setSettingsMsg("系统偏好已保存。");
      await queryClient.invalidateQueries({ queryKey: ["system-settings"] });
    },
  });
  const savePassword = useMutation({
    mutationFn: () =>
      changePassword({
        current_password: currentPassword,
        new_password: newPassword,
      }),
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
      if (!uc.enabled) {
        setBanner({ kind: "info", text: "远程更新检查已关闭（REPOSENTINEL_UPDATE_CHECK）。" });
        return;
      }
      if (uc.error && !uc.latest_version) {
        setBanner({ kind: "error", text: `检查失败：${uc.error}` });
        return;
      }
      const url = safeHttpUrl(uc.latest_url);
      if (uc.update_available && uc.latest_version) {
        setBanner({
          kind: "update",
          text: `发现新版本 v${uc.latest_version}（当前 v${res.version.version || v.version || "—"}）`,
          url,
        });
        return;
      }
      setBanner({
        kind: "latest",
        text: uc.latest_version
          ? `已是最新（远程 v${uc.latest_version}${uc.cached ? " · 缓存" : ""}）`
          : "未获取到远程版本信息",
        url,
      });
    } catch (e) {
      setBanner({
        kind: "error",
        text: e instanceof Error ? e.message : "检查更新失败",
      });
    } finally {
      setChecking(false);
    }
  };

  function submitPassword() {
    setPasswordMsg("");
    setPasswordError(undefined);
    if (Array.from(newPassword).length < 12) {
      setPasswordError("新密码至少需要 12 个 Unicode 字符。");
      return;
    }
    if (newPassword !== confirmPassword) {
      setPasswordError("两次输入的新密码不一致。");
      return;
    }
    savePassword.mutate();
  }

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>关于与设置</h1>
          <p>
            RepoSentinel 是自托管的 GitHub 仓库值守台：接收 Webhook、汇总 Issue / PR / Actions /
            安全告警，并按渠道投递通知。本页集中版本信息、更新检查、账号与运行偏好。
          </p>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-product-title">
        <h2 id="about-product-title">你在用什么</h2>
        <p>
          单管理员实例，数据与密钥都在你控制的环境中。GitHub 凭据与入站 Secret 只走环境变量；通知 Token /
          出站 Secret 可在「渠道配置」加密保存。
        </p>
        <div className="link-row">
          <a className="quiet-button" href={DOCS_CONFIG} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 配置参考
          </a>
          <a className="quiet-button" href={DOCS_FAQ} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 常见问题
          </a>
          <a className="quiet-button" href={DOCS_CHANGELOG} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 变更日志
          </a>
          <Link className="quiet-button" to="/github">
            GitHub App 指引
          </Link>
          <Link className="quiet-button" to="/notifications">
            通知渠道
          </Link>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-build-title">
        <div className="onboarding-card__header">
          <h2 id="about-build-title">构建与运行</h2>
          <button
            type="button"
            className="quiet-button"
            disabled={checking || version.isLoading}
            onClick={() => void checkUpdate(true)}
          >
            {checking ? "检查中…" : "检查更新"}
          </button>
        </div>
        {version.isError ? (
          <ErrorAlert
            title="无法加载版本"
            message={toApiError(version.error).message}
            errorCode={toApiError(version.error).errorCode}
          />
        ) : null}
        {banner ? (
          <div
            className={
              banner.kind === "update"
                ? "about-banner about-banner--update"
                : banner.kind === "latest"
                  ? "about-banner about-banner--latest"
                  : banner.kind === "error"
                    ? "about-banner about-banner--error"
                    : "about-banner about-banner--info"
            }
          >
            <div>{banner.text}</div>
            {banner.kind === "update" ? (
              <p className="muted">升级请拉取新镜像或替换二进制，并阅读 CHANGELOG。</p>
            ) : null}
            {banner.url ? (
              <a href={banner.url} target="_blank" rel="noopener noreferrer">
                打开 Release 页面
              </a>
            ) : null}
          </div>
        ) : null}
        <dl className="meta-grid">
          <div>
            <dt>版本</dt>
            <dd>{v.version || "—"}</dd>
          </div>
          <div>
            <dt>构建渠道</dt>
            <dd>{v.build_channel || "—"}</dd>
          </div>
          <div>
            <dt>Git SHA</dt>
            <dd className="mono">{v.git_sha || "—"}</dd>
          </div>
          <div>
            <dt>分支</dt>
            <dd>{v.git_branch || "—"}</dd>
          </div>
          <div>
            <dt>构建时间</dt>
            <dd>{v.build_time || "—"}</dd>
          </div>
          <div>
            <dt>Go</dt>
            <dd>{v.go_version || "—"}</dd>
          </div>
          <div>
            <dt>数据库</dt>
            <dd>{v.database_driver || "—"}</dd>
          </div>
          <div>
            <dt>Schema</dt>
            <dd className="mono">{v.schema_version || "—"}</dd>
          </div>
          <div>
            <dt>监听地址</dt>
            <dd className="mono">{v.http_addr || "—"}</dd>
          </div>
          <div>
            <dt>Public Base URL</dt>
            <dd className="mono">{v.public_base_url || "（未设置）"}</dd>
          </div>
          <div>
            <dt>更新检查</dt>
            <dd>{v.update_check_enabled === false ? "已关闭" : "已开启"}</dd>
          </div>
          <div>
            <dt>Webhook 路径</dt>
            <dd className="mono">{v.github?.webhook_path || "/webhooks/github"}</dd>
          </div>
        </dl>
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-settings-title">
        <h2 id="about-settings-title">运行偏好</h2>
        <p className="field-hint">
          时区与摘要时间影响本地展示与摘要调度语义；聚合窗口用于短时合并同类通知，降低刷屏。保存后立即写入数据库。
        </p>
        {settingsMsg ? (
          <p className="success-banner" role="status">
            {settingsMsg}
          </p>
        ) : null}
        {settings.isError ? (
          <ErrorAlert
            title="无法加载设置"
            message={toApiError(settings.error).message}
            errorCode={toApiError(settings.error).errorCode}
          />
        ) : null}
        <div className="form-grid">
          <label className="field--plain">
            <span>管理员时区</span>
            <input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="UTC 或 Asia/Shanghai" />
          </label>
          <label className="field--plain">
            <span>每日摘要本地时间</span>
            <input value={digestTime} onChange={(e) => setDigestTime(e.target.value)} placeholder="09:00" />
          </label>
          <label className="field--plain">
            <span>通知聚合窗口（秒）</span>
            <input
              type="number"
              min={0}
              value={aggregateSec}
              onChange={(e) => setAggregateSec(Number(e.target.value) || 0)}
            />
          </label>
          <label className="field--plain">
            <span>超频阈值</span>
            <input
              type="number"
              min={1}
              value={burstThreshold}
              onChange={(e) => setBurstThreshold(Number(e.target.value) || 1)}
            />
          </label>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={digestEmpty} onChange={(e) => setDigestEmpty(e.target.checked)} />
          <span>无事件时仍发送空摘要</span>
        </label>
        <button
          className="primary-button primary-button--inline"
          type="button"
          disabled={saveSettings.isPending}
          onClick={() => {
            setSettingsMsg("");
            saveSettings.mutate({
              "admin.timezone": timezone.trim() || "UTC",
              "digest.local_time": digestTime.trim() || "09:00",
              "digest.send_empty": digestEmpty,
              "notify.aggregate_window_sec": aggregateSec,
              "notify.burst_threshold": burstThreshold,
            });
          }}
        >
          {saveSettings.isPending ? "保存中…" : "保存偏好"}
        </button>
        {saveSettings.isError ? (
          <ErrorAlert
            title="保存失败"
            message={toApiError(saveSettings.error).message}
            errorCode={toApiError(saveSettings.error).errorCode}
          />
        ) : null}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="about-password-title">
        <h2 id="about-password-title">修改管理员密码</h2>
        <p className="field-hint">
          修改成功后其它会话会失效。若已遗失当前密码，请在服务器上使用 CLI：
          <code> admin reset-password --password-stdin</code>。
        </p>
        {passwordMsg ? (
          <p className="success-banner" role="status">
            {passwordMsg}
          </p>
        ) : null}
        {passwordError ? <ErrorAlert title="无法修改密码" message={passwordError} /> : null}
        <label className="field--plain">
          <span>当前密码</span>
          <input
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
          />
        </label>
        <label className="field--plain">
          <span>新密码（至少 12 个字符）</span>
          <input
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
          />
        </label>
        <label className="field--plain">
          <span>确认新密码</span>
          <input
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
          />
        </label>
        <button
          className="primary-button primary-button--inline"
          type="button"
          disabled={savePassword.isPending || !currentPassword || !newPassword}
          onClick={submitPassword}
        >
          {savePassword.isPending ? "更新中…" : "更新密码"}
        </button>
      </section>

      <section className="onboarding-card" aria-labelledby="about-ops-title">
        <h2 id="about-ops-title">运维提示</h2>
        <ul className="bullet-list">
          <li>
            健康检查：<code>GET /health/live</code> 与 <code>GET /health/ready</code>。
          </li>
          <li>
            配置自检：服务器上运行 <code>reposentinel doctor</code>。
          </li>
          <li>
            备份：维护窗口执行 <code>reposentinel backup</code>，并同时保管{" "}
            <code>REPOSENTINEL_ENCRYPTION_KEY</code>。
          </li>
          <li>日志默认 info 只保留重点事件；访问明细请设 <code>REPOSENTINEL_LOG_LEVEL=debug</code>。</li>
        </ul>
      </section>
    </>
  );
}

function ListShell({
  eyebrow = "仓库值守",
  title,
  description,
  children,
}: {
  eyebrow?: string;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
      </section>
      <section className="onboarding-card">{children}</section>
    </>
  );
}

function buildWebhookURL(version?: VersionInfo): string {
  const path = version?.github?.webhook_path || "/webhooks/github";
  const base = (version?.public_base_url || (typeof window !== "undefined" ? window.location.origin : "")).replace(
    /\/$/,
    "",
  );
  if (!base) {
    return path;
  }
  return `${base}${path.startsWith("/") ? path : `/${path}`}`;
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
