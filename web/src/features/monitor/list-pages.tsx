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
  githubConfigQueryOptions,
  installationsQueryOptions,
  saveGitHubConfig,
  saveSystemSettings,
  settingsQueryOptions,
  syncInstallationRepositories,
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
  "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/deploy/docker.md#4-github-app创建表单逐项";
const DOCS_CONFIG =
  "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md";
const DOCS_FAQ = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/faq.md";
const DOCS_CHANGELOG = "https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md";
const GITHUB_NEW_APP = "https://github.com/settings/apps/new";
/** 用户级 GitHub Apps 列表；可点进具体 App 再 Install。 */
const GITHUB_APPS_SETTINGS = "https://github.com/settings/apps";
/** 当前账号已安装的 Apps（含 Install / Configure）。 */
const GITHUB_INSTALLATIONS = "https://github.com/settings/installations";

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
  const githubConfig = useQuery(githubConfigQueryOptions);
  const installations = useQuery(installationsQueryOptions);
  const [externalName, setExternalName] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "ok" | "fail">("idle");
  const [formMessage, setFormMessage] = useState("");
  const [syncMessage, setSyncMessage] = useState("");
  const [configMessage, setConfigMessage] = useState("");
  const [appID, setAppID] = useState("");
  const [clientID, setClientID] = useState("");
  const [publicBaseURL, setPublicBaseURL] = useState("");
  const [privateKeyPath, setPrivateKeyPath] = useState("");
  const [privateKeyPEM, setPrivateKeyPEM] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");

  const cfg = githubConfig.data;
  const webhookURL = useMemo(() => {
    if (cfg?.webhook_url) return cfg.webhook_url;
    return buildWebhookURL(version.data);
  }, [cfg?.webhook_url, version.data]);

  useEffect(() => {
    if (!cfg) return;
    setAppID(cfg.app_id > 0 ? String(cfg.app_id) : "");
    setClientID(cfg.client_id || "");
    setPublicBaseURL(cfg.public_base_url || "");
    setPrivateKeyPath(cfg.private_key_path || "");
  }, [cfg]);

  const addExternal = useMutation({
    mutationFn: () => addExternalRepository(externalName.trim()),
    onSuccess: async () => {
      setExternalName("");
      setFormMessage("外部公开仓库已登记，将进入基线同步。");
      await queryClient.invalidateQueries({ queryKey: ["repositories"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const syncRepos = useMutation({
    mutationFn: () => syncInstallationRepositories(),
    onSuccess: async (res) => {
      setSyncMessage(
        `已从 GitHub 同步：${res.imported_or_updated} 个仓库（${res.installations} 个 Installation）。请到仪表盘查看基线状态并「完成基线」。`,
      );
      await queryClient.invalidateQueries({ queryKey: ["repositories"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
      await queryClient.invalidateQueries({ queryKey: ["installations"] });
    },
  });

  const saveConfig = useMutation({
    mutationFn: () => {
      const body: Parameters<typeof saveGitHubConfig>[0] = {};
      if (!cfg?.app_id_locked) {
        body.app_id = appID.trim() === "" ? 0 : Number(appID);
      }
      if (!cfg?.client_id_locked) {
        body.client_id = clientID;
      }
      if (!cfg?.public_base_url_locked) {
        body.public_base_url = publicBaseURL;
      }
      if (!cfg?.private_key_locked) {
        if (privateKeyPEM.trim()) {
          body.private_key_pem = privateKeyPEM.trim();
        } else if (privateKeyPath.trim()) {
          body.private_key_path = privateKeyPath.trim();
        }
      }
      if (!cfg?.webhook_secret_locked && webhookSecret.trim()) {
        body.webhook_secret = webhookSecret.trim();
      }
      return saveGitHubConfig(body);
    },
    onSuccess: async () => {
      setConfigMessage("GitHub 配置已保存，立即生效（无需重启）。");
      setPrivateKeyPEM("");
      setWebhookSecret("");
      await queryClient.invalidateQueries({ queryKey: ["github-config"] });
      await queryClient.invalidateQueries({ queryKey: ["version"] });
    },
  });

  const sourceLabel = (source?: string) => {
    if (source === "env") return "环境变量（锁定）";
    if (source === "database") return "管理台 / 数据库";
    return "未设置";
  };

  const checklist = [
    {
      ok: Boolean(cfg?.webhook_secret_configured),
      label: "Webhook Secret",
      hint: sourceLabel(cfg?.webhook_secret_source),
    },
    {
      ok: Boolean(cfg?.app_id_configured),
      label: "App ID",
      hint: sourceLabel(cfg?.app_id_source),
    },
    {
      ok: Boolean(cfg?.client_id_configured),
      label: "Client ID",
      hint: sourceLabel(cfg?.client_id_source),
    },
    {
      ok: Boolean(cfg?.private_key_configured),
      label: "私钥",
      hint: sourceLabel(cfg?.private_key_source),
    },
    {
      ok: Boolean(cfg?.external_pat_configured),
      label: "外部仓 PAT（可选）",
      hint: "仅环境变量 REPOSENTINEL_EXTERNAL_PAT",
    },
  ];
  const readyCount = checklist.filter((c) => c.ok).length;
  const coreReady = Boolean(cfg?.webhook_secret_configured);

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
            可在本页直接填写 App ID、Client ID、私钥、Webhook Secret 与 Public Base URL；敏感字段加密入库并立即生效。若已用环境变量设置同一字段，则以环境变量为准（管理台锁定，避免漂移）。
          </p>
        </div>
      </section>

      {githubConfig.isError ? (
        <ErrorAlert
          title="无法加载 GitHub 配置"
          message={toApiError(githubConfig.error).message}
          errorCode={toApiError(githubConfig.error).errorCode}
        />
      ) : null}

      <section className="onboarding-card channel-form" aria-labelledby="gh-config-title">
        <div className="onboarding-card__header">
          <h2 id="gh-config-title">在管理台填写运行时配置</h2>
          <span className="muted">{cfg ? `${readyCount} / ${checklist.length} 项就绪` : "加载中…"}</span>
        </div>
        <p className="field-hint">
          {cfg?.note ||
            "环境变量优先；未用环境变量设置的字段可在此保存。私钥与 Webhook Secret 使用主密钥加密，页面不会回显明文。"}
        </p>
        {configMessage ? (
          <p className="success-banner" role="status">
            {configMessage}
          </p>
        ) : null}
        <div className="form-grid">
          <label className="field--plain">
            <span>
              App ID {cfg?.app_id_locked ? <em className="field-lock">环境变量锁定</em> : null}
            </span>
            <input
              inputMode="numeric"
              value={appID}
              disabled={cfg?.app_id_locked}
              onChange={(e) => setAppID(e.target.value.replace(/[^\d]/g, ""))}
              placeholder="123456"
              autoComplete="off"
            />
          </label>
          <label className="field--plain">
            <span>
              Client ID {cfg?.client_id_locked ? <em className="field-lock">环境变量锁定</em> : null}
            </span>
            <input
              value={clientID}
              disabled={cfg?.client_id_locked}
              onChange={(e) => setClientID(e.target.value)}
              placeholder="Iv1.xxxxxxxx"
              autoComplete="off"
            />
          </label>
          <label className="field--plain">
            <span>
              Public Base URL {cfg?.public_base_url_locked ? <em className="field-lock">环境变量锁定</em> : null}
            </span>
            <input
              value={publicBaseURL}
              disabled={cfg?.public_base_url_locked}
              onChange={(e) => setPublicBaseURL(e.target.value)}
              placeholder="https://monitor.example.com"
              autoComplete="off"
            />
          </label>
          <label className="field--plain">
            <span>
              Webhook Secret {cfg?.webhook_secret_locked ? <em className="field-lock">环境变量锁定</em> : null}
            </span>
            <input
              type="password"
              value={webhookSecret}
              disabled={cfg?.webhook_secret_locked}
              onChange={(e) => setWebhookSecret(e.target.value)}
              placeholder={cfg?.webhook_secret_configured ? "已配置 · 留空保留" : "与 GitHub App Secret 相同"}
              autoComplete="off"
            />
          </label>
        </div>
        <label className="field--plain">
          <span>
            私钥 PEM 粘贴 {cfg?.private_key_locked ? <em className="field-lock">环境变量锁定</em> : null}
          </span>
          <textarea
            className="field-textarea"
            rows={5}
            value={privateKeyPEM}
            disabled={cfg?.private_key_locked}
            onChange={(e) => setPrivateKeyPEM(e.target.value)}
            placeholder={
              cfg?.private_key_configured
                ? "已配置 · 留空保留；若粘贴新 PEM 将覆盖路径配置"
                : "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
            }
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <label className="field--plain">
          <span>或：服务器上的私钥路径（二选一）</span>
          <input
            value={privateKeyPath}
            disabled={cfg?.private_key_locked || Boolean(privateKeyPEM.trim())}
            onChange={(e) => setPrivateKeyPath(e.target.value)}
            placeholder="/secrets/github-app.pem"
            autoComplete="off"
          />
        </label>
        <p className="field-hint">
          推荐直接粘贴 PEM（加密入库，容器无需挂载文件）。若走路径方式，需保证进程能读取该文件。External PAT
          仍仅支持环境变量。
        </p>
        <button
          className="primary-button primary-button--inline"
          type="button"
          disabled={saveConfig.isPending || cfg?.can_edit_in_ui === false}
          onClick={() => {
            setConfigMessage("");
            saveConfig.mutate();
          }}
        >
          {saveConfig.isPending ? "保存中…" : "保存 GitHub 配置"}
        </button>
        {saveConfig.isError ? (
          <ErrorAlert
            title="保存失败"
            message={toApiError(saveConfig.error).message}
            errorCode={toApiError(saveConfig.error).errorCode}
          />
        ) : null}
        <ul className="status-checklist" style={{ marginTop: "1rem" }}>
          {checklist.map((item) => (
            <li key={item.label} data-ok={item.ok ? "true" : "false"}>
              {item.ok ? <CheckCircle2 size={18} aria-hidden="true" /> : <Circle size={18} aria-hidden="true" />}
              <div>
                <strong>{item.label}</strong>
                <span className="muted">
                  {item.ok ? "已配置" : "未配置"} · {item.hint}
                </span>
              </div>
            </li>
          ))}
        </ul>
        {!coreReady ? (
          <p className="callout callout--warn" role="status">
            尚未配置入站 Webhook Secret 时，GitHub 推送会被拒绝。在上方填写并保存，或设置环境变量{" "}
            <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code>。
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
          在 GitHub App 设置里填写下方 URL，Content type 选 <code>application/json</code>。Secret 与上方「Webhook
          Secret」保持一致。
        </p>
        <div className="copy-row">
          <code className="copy-row__value">{webhookURL}</code>
          <button className="quiet-button" type="button" onClick={() => void copyWebhook()}>
            <Copy size={15} aria-hidden="true" />
            {copyState === "ok" ? "已复制" : copyState === "fail" ? "复制失败" : "复制"}
          </button>
        </div>
        {!cfg?.public_base_url && !version.data?.public_base_url ? (
          <p className="field-hint">
            当前未设置 Public Base URL，上式使用浏览器地址推导。生产环境请在上方表单或{" "}
            <code>REPOSENTINEL_PUBLIC_BASE_URL</code> 配置公网 HTTPS 基址。
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

      <section className="onboarding-card" aria-labelledby="gh-form-title">
        <h2 id="gh-form-title">Create GitHub App 表单怎么填</h2>
        <p className="field-hint">
          本产品<strong>不使用</strong>「用户授权 OAuth」登录 GitHub，只用 App 私钥换 Installation Token。创建页里与
          Callback / Device Flow 相关的项都可以空着或关掉。
        </p>

        <h3 className="section-subtitle">基本信息</h3>
        <dl className="field-map">
          <div>
            <dt>GitHub App name</dt>
            <dd>
              任意全局唯一名，例如 <code>RepoSentinel</code>。与本机产品名无关，可加后缀避免撞名。
            </dd>
          </div>
          <div>
            <dt>Homepage URL</dt>
            <dd>
              填你的管理台公网地址（与 <code>REPOSENTINEL_PUBLIC_BASE_URL</code> 一致）。本地可先填{" "}
              <code>http://127.0.0.1:8080</code>，Webhook 仍需 GitHub 能访问的 HTTPS。
            </dd>
          </div>
        </dl>

        <h3 className="section-subtitle">Identifying and authorizing users</h3>
        <dl className="field-map">
          <div>
            <dt>Callback URL</dt>
            <dd>
              <strong>留空</strong>。RepoSentinel 没有 OAuth 回调路由。
            </dd>
          </div>
          <div>
            <dt>Expire user authorization tokens</dt>
            <dd>不勾。</dd>
          </div>
          <div>
            <dt>Request user authorization (OAuth) during installation</dt>
            <dd>
              <strong>不勾</strong>。安装时不需要用户 OAuth。
            </dd>
          </div>
          <div>
            <dt>Enable Device Flow</dt>
            <dd>
              <strong>不勾</strong>。
            </dd>
          </div>
        </dl>

        <h3 className="section-subtitle">Post installation</h3>
        <dl className="field-map">
          <div>
            <dt>Setup URL</dt>
            <dd>可选；可填管理台地址，安装后跳回控制台。不填也不影响收 Webhook。</dd>
          </div>
          <div>
            <dt>Redirect on update</dt>
            <dd>可选；一般不勾。</dd>
          </div>
        </dl>

        <h3 className="section-subtitle">Webhook（必填）</h3>
        <dl className="field-map">
          <div>
            <dt>Active</dt>
            <dd>
              <strong>必须勾选</strong>。
            </dd>
          </div>
          <div>
            <dt>Webhook URL</dt>
            <dd>
              填上方复制框中的地址，形如 <code>https://你的域名/webhooks/github</code>。
            </dd>
          </div>
          <div>
            <dt>Secret</dt>
            <dd>
              自己生成一串随机值（如 <code>openssl rand -hex 32</code>），填到 GitHub，并<strong>原样</strong>写入{" "}
              <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code>。两边必须一致。
            </dd>
          </div>
        </dl>

        <h3 className="section-subtitle">Repository permissions（建议全部 Read-only）</h3>
        <p className="field-hint">权限决定下方能勾哪些事件。Organization / Account 权限保持 No access 即可。</p>
        <ul className="bullet-list">
          <li>
            <strong>Metadata</strong> → Read-only（必选）
          </li>
          <li>
            <strong>Contents</strong> → Read-only
          </li>
          <li>
            <strong>Issues</strong> → Read-only
          </li>
          <li>
            <strong>Pull requests</strong> → Read-only
          </li>
          <li>
            <strong>Actions</strong> → Read-only（workflow_run / 对账）
          </li>
          <li>
            <strong>Dependabot alerts</strong> → Read-only
          </li>
          <li>
            <strong>Code scanning alerts</strong> → Read-only
          </li>
          <li>
            <strong>Secret scanning alerts</strong> → Read-only
          </li>
        </ul>

        <h3 className="section-subtitle">Subscribe to events（勾选）</h3>
        <ul className="bullet-list">
          <li>
            <code>Issues</code>、<code>Pull request</code>、<code>Workflow run</code>
          </li>
          <li>
            <code>Dependabot alert</code>、<code>Code scanning alert</code>、<code>Secret scanning alert</code>
          </li>
          <li>
            <code>Installation</code>、<code>Installation repositories</code>、<code>Repository</code>
          </li>
          <li>
            创建页里的 <code>Installation target</code> / <code>Meta</code> / <code>Security advisory</code>{" "}
            <strong>可不勾</strong>（本服务不依赖）。
          </li>
        </ul>
        <p className="field-hint">若列表里暂时看不到某事件，先提高对应 Repository permission 再回来勾选。</p>

        <h3 className="section-subtitle">Where can this GitHub App be installed?</h3>
        <dl className="field-map">
          <div>
            <dt>Only on this account</dt>
            <dd>个人自用推荐：只能装到当前账号（如 @Silentely）。</dd>
          </div>
          <div>
            <dt>Any account</dt>
            <dd>需要装到多个组织 / 账号时再选。</dd>
          </div>
        </dl>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-install-action-title">
        <h2 id="gh-install-action-title">安装到仓库（在 GitHub 完成）</h2>
        <p className="field-hint">
          本管理台<strong>不能代替</strong> GitHub 授权安装。保存凭据后，须到 GitHub 点 Install，GitHub 会推送{" "}
          <code>installation</code> 事件；仓库会出现在仪表盘（基线中）。若你已安装但仪表盘仍空，多半是旧版本未解析{" "}
          <code>repositories</code> 字段——点下方「从 GitHub 同步仓库」补拉即可。
        </p>
        <div className="link-row">
          <a className="primary-button primary-button--inline" href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer">
            <ExternalLink size={15} aria-hidden="true" /> 去 GitHub 安装 / 管理 App
          </a>
          <a className="quiet-button" href={GITHUB_APPS_SETTINGS} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" /> 我的 GitHub Apps
          </a>
          <button
            className="quiet-button"
            type="button"
            disabled={syncRepos.isPending || !cfg?.app_id_configured || !cfg?.private_key_configured}
            onClick={() => {
              setSyncMessage("");
              syncRepos.mutate();
            }}
            title={
              !cfg?.app_id_configured || !cfg?.private_key_configured
                ? "需先配置 App ID 与私钥"
                : "用 Installation Token 拉取已授权仓库"
            }
          >
            {syncRepos.isPending ? "同步中…" : "从 GitHub 同步仓库"}
          </button>
        </div>
        {syncRepos.isError ? (
          <ErrorAlert
            title="同步失败"
            message={toApiError(syncRepos.error).message}
            errorCode={toApiError(syncRepos.error).errorCode}
          />
        ) : null}
        {syncMessage ? (
          <p className="success-banner" role="status" style={{ marginTop: "0.75rem" }}>
            {syncMessage}
          </p>
        ) : null}
        <ol className="guide-steps" style={{ marginTop: "1rem" }}>
          <li>
            <strong>打开安装页</strong>
            <span>
              点击「去 GitHub 安装 / 管理 App」→ 找到 <code>repo-sentinel-bot</code>（或你的 App 名）→ Install / Configure。
            </span>
          </li>
          <li>
            <strong>选择仓库</strong>
            <span>可选 All repositories 或 Only select；保存后 GitHub 会 POST <code>/webhooks/github</code>。</span>
          </li>
          <li>
            <strong>回到本页刷新</strong>
            <span>
              Installation 列表应出现账号；仪表盘应出现仓库（状态「基线中」）。没有仓库就点「从 GitHub 同步仓库」。
            </span>
          </li>
          <li>
            <strong>完成基线</strong>
            <span>仪表盘对需要监控的仓点「完成基线」后，才会发实时通知（避免首次洪流）。</span>
          </li>
        </ol>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-install-title">
        <div className="onboarding-card__header">
          <h2 id="gh-install-title">已记录的 Installation</h2>
          <div className="link-row">
            <button
              className="quiet-button"
              type="button"
              disabled={installations.isFetching}
              onClick={() => void installations.refetch()}
            >
              {installations.isFetching ? "刷新中…" : "刷新"}
            </button>
            <a className="quiet-button" href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer">
              <ExternalLink size={14} aria-hidden="true" /> 去 GitHub 安装
            </a>
          </div>
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
                {inst.installation_id ? (
                  <a
                    className="quiet-button"
                    href={`https://github.com/settings/installations/${inst.installation_id}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    配置
                  </a>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState
            eyebrow="等待事件"
            title="尚未收到 Installation"
            description="请先在 GitHub 安装 App。安装后本列表会出现账号；若只有 Installation 没有仓库，点上方「从 GitHub 同步仓库」。"
            action={
              <a href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer">
                去 GitHub 安装 App
              </a>
            }
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
