import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Circle, Copy, ExternalLink } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ApiErrorAlert, ErrorAlert } from "../../components/error-alert";
import { apiRequest } from "../../lib/api/client";
import { toApiError } from "../../lib/api/errors";
import { useCopyFeedback } from "../../lib/use-copy-feedback";
import {
  addExternalRepository,
  githubConfigQueryOptions,
  installationsQueryOptions,
  saveGitHubConfig,
  syncInstallationRepositories,
  versionQueryOptions,
} from "./api";

const DOCS_GITHUB_APP =
  "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/deploy/docker.md#4-github-app创建表单逐项";
const GITHUB_NEW_APP = "https://github.com/settings/apps/new";
const GITHUB_APPS_SETTINGS = "https://github.com/settings/apps";
const GITHUB_INSTALLATIONS = "https://github.com/settings/installations";

export function GitHubPage() {
  const queryClient = useQueryClient();
  const version = useQuery(versionQueryOptions);
  const githubConfig = useQuery(githubConfigQueryOptions);
  const installations = useQuery(installationsQueryOptions);
  const [externalName, setExternalName] = useState("");
  // 复制 Webhook URL：ok/fail 短暂反馈（失败提示「复制失败」），定时器卸载时清理。
  const { isCopied: copiedWebhook, isFailed: copyWebhookFailed, copy: copyText } = useCopyFeedback();
  async function copyWebhook() {
    await copyText("webhook", webhookURL);
  }
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
        `已从 GitHub 同步：${res.imported_or_updated} 个仓库（${res.installations} 个 Installation）。请到仪表盘查看基线；对账成功会自动放行，也可点「立即放行」。`,
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
    { ok: Boolean(cfg?.webhook_secret_configured), label: "Webhook Secret", hint: sourceLabel(cfg?.webhook_secret_source) },
    { ok: Boolean(cfg?.app_id_configured), label: "App ID", hint: sourceLabel(cfg?.app_id_source) },
    { ok: Boolean(cfg?.client_id_configured), label: "Client ID", hint: sourceLabel(cfg?.client_id_source) },
    { ok: Boolean(cfg?.private_key_configured), label: "私钥", hint: sourceLabel(cfg?.private_key_source) },
    { ok: Boolean(cfg?.external_pat_configured), label: "外部仓 PAT（可选）", hint: "仅环境变量 REPOSENTINEL_EXTERNAL_PAT" },
  ];
  const readyCount = checklist.filter((c) => c.ok).length;
  const inboundReady = Boolean(cfg?.webhook_secret_configured);
  const outboundReady = Boolean(cfg?.app_id_configured && cfg?.private_key_configured);

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>GitHub App</h1>
          <p>填写 App ID、私钥等凭据，敏感字段加密入库并立即生效。环境变量优先（管理台锁定）。</p>
        </div>
      </section>

      {githubConfig.isError ? (
        <ApiErrorAlert error={githubConfig.error} title="无法加载 GitHub 配置" />
      ) : null}

      <section className="onboarding-card channel-form" aria-labelledby="gh-config-title">
        <div className="onboarding-card__header">
          <h2 id="gh-config-title">运行时配置</h2>
          <span className="muted">{cfg ? `${readyCount} / ${checklist.length} 项就绪` : "加载中…"}</span>
        </div>
        <p className="field-hint">{cfg?.note || "环境变量优先；未用环境变量设置的字段可在此保存。"}</p>
        {configMessage ? <p className="success-banner" role="status">{configMessage}</p> : null}
        <div className="form-grid">
          <label className="field--plain">
            <span>App ID {cfg?.app_id_locked ? <em className="field-lock">环境变量锁定</em> : null}</span>
            <input inputMode="numeric" value={appID} disabled={cfg?.app_id_locked} onChange={(e) => setAppID(e.target.value.replace(/[^\d]/g, ""))} placeholder="123456" autoComplete="off" />
          </label>
          <label className="field--plain">
            <span>Client ID {cfg?.client_id_locked ? <em className="field-lock">环境变量锁定</em> : null}</span>
            <input value={clientID} disabled={cfg?.client_id_locked} onChange={(e) => setClientID(e.target.value)} placeholder="Iv1.xxxxxxxx" autoComplete="off" />
          </label>
          <label className="field--plain">
            <span>Public Base URL {cfg?.public_base_url_locked ? <em className="field-lock">环境变量锁定</em> : null}</span>
            <input value={publicBaseURL} disabled={cfg?.public_base_url_locked} onChange={(e) => setPublicBaseURL(e.target.value)} placeholder="https://monitor.example.com" autoComplete="off" />
          </label>
          <label className="field--plain">
            <span>Webhook Secret {cfg?.webhook_secret_locked ? <em className="field-lock">环境变量锁定</em> : null}</span>
            <input type="password" value={webhookSecret} disabled={cfg?.webhook_secret_locked} onChange={(e) => setWebhookSecret(e.target.value)} placeholder={cfg?.webhook_secret_configured ? "已配置 · 留空保留" : "与 GitHub App Secret 相同"} autoComplete="off" />
          </label>
        </div>
        <label className="field--plain">
          <span>私钥 PEM 粘贴 {cfg?.private_key_locked ? <em className="field-lock">环境变量锁定</em> : null}</span>
          <textarea className="field-textarea" rows={5} value={privateKeyPEM} disabled={cfg?.private_key_locked} onChange={(e) => setPrivateKeyPEM(e.target.value)} placeholder={cfg?.private_key_configured ? "已配置 · 留空保留" : "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"} autoComplete="off" spellCheck={false} />
        </label>
        <label className="field--plain">
          <span>或：服务器上的私钥路径（二选一）</span>
          <input value={privateKeyPath} disabled={cfg?.private_key_locked || Boolean(privateKeyPEM.trim())} onChange={(e) => setPrivateKeyPath(e.target.value)} placeholder="/secrets/github-app.pem" autoComplete="off" />
        </label>
        <p className="field-hint">
          推荐直接粘贴 PEM（加密入库，容器无需挂载文件）。External PAT 仅支持环境变量。
          {privateKeyPEM.trim() && !cfg?.private_key_locked ? " 已粘贴 PEM，路径输入暂不可用，清空 PEM 后可改回路径方式。" : null}
        </p>
        <button className="primary-button primary-button--inline" type="button" disabled={saveConfig.isPending || cfg?.can_edit_in_ui === false} onClick={() => { setConfigMessage(""); saveConfig.mutate(); }}>
          {saveConfig.isPending ? "保存中…" : "保存 GitHub 配置"}
        </button>
        {saveConfig.isError ? <ApiErrorAlert error={saveConfig.error} title="保存失败" /> : null}
        <ul className="status-checklist mt-md">
          {checklist.map((item) => (
            <li key={item.label} data-ok={item.ok ? "true" : "false"}>
              {item.ok ? <CheckCircle2 size={18} aria-hidden="true" /> : <Circle size={18} aria-hidden="true" />}
              <div><strong>{item.label}</strong><span className="muted">{item.ok ? "已配置" : "未配置"} · {item.hint}</span></div>
            </li>
          ))}
        </ul>
        <div className="capability-lights" role="status">
          <div className={`capability-light${inboundReady ? " is-ok" : " is-warn"}`}>
            <strong>入站 Webhook</strong>
            <span className="muted">
              {inboundReady ? "验签就绪，可接收 GitHub 推送" : "需配置 Webhook Secret，否则推送会被拒绝"}
            </span>
          </div>
          <div className={`capability-light${outboundReady ? " is-ok" : " is-warn"}`}>
            <strong>出站 GitHub API</strong>
            <span className="muted">
              {outboundReady ? "App ID + 私钥就绪，可对账/同步仓库" : "需 App ID 与私钥，才能同步仓库与对账"}
            </span>
          </div>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-webhook-title">
        <h2 id="gh-webhook-title">Webhook URL</h2>
        <p className="field-hint">在 GitHub App 设置中填入此 URL，Content type 选 <code>application/json</code>。</p>
        <div className="copy-row">
          <code className="copy-row__value">{webhookURL}</code>
          <button
            className="quiet-button"
            type="button"
            onClick={() => void copyWebhook()}
            aria-live="polite"
            aria-label={copiedWebhook("webhook") ? "Webhook URL 已复制" : copyWebhookFailed("webhook") ? "复制失败，请手动复制" : "复制 Webhook URL"}
          >
            <Copy size={15} aria-hidden="true" />
            {copiedWebhook("webhook") ? "已复制" : copyWebhookFailed("webhook") ? "复制失败" : "复制"}
          </button>
        </div>
        <div className="link-row">
          <a className="quiet-button" href={GITHUB_NEW_APP} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 创建 GitHub App</a>
          <a className="quiet-button" href={DOCS_GITHUB_APP} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 部署文档</a>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-form-title">
        <h2 id="gh-form-title">创建 GitHub App 要点</h2>
        <details className="collapse-section">
          <summary>展开查看详细填写指南</summary>
          <div className="collapse-body">
            <p className="field-hint">本产品<strong>不使用</strong> OAuth 登录，只用 App 私钥换 Installation Token。</p>
            <h3 className="section-subtitle">Webhook（必填）</h3>
            <p className="field-hint">Active <strong>必须勾选</strong>。URL 填上方复制框。Secret 用 <code>openssl rand -hex 32</code> 生成。</p>
            <h3 className="section-subtitle">Repository permissions → 全部 Read-only</h3>
            <p className="field-hint">Metadata、Contents、Issues、Pull requests、Actions、Dependabot/Code/Secret scanning alerts</p>
            <h3 className="section-subtitle">Subscribe to events</h3>
            <p className="field-hint">勾选：Issues / Pull request / Workflow run / Dependabot alert / Code scanning alert / Secret scanning alert / Installation / Installation repositories / Repository</p>
          </div>
        </details>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-install-action-title">
        <h2 id="gh-install-action-title">安装到仓库</h2>
        <p className="field-hint">保存凭据后，须到 GitHub 点 Install。仪表盘仓库为空？点「从 GitHub 同步仓库」补拉。</p>
        <div className="link-row">
          <a className="primary-button primary-button--inline" href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer"><ExternalLink size={15} aria-hidden="true" /> 去 GitHub 安装 / 管理 App</a>
          <a className="quiet-button" href={GITHUB_APPS_SETTINGS} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 我的 GitHub Apps</a>
          <button className="quiet-button" type="button" disabled={syncRepos.isPending || !cfg?.app_id_configured || !cfg?.private_key_configured} onClick={() => { setSyncMessage(""); syncRepos.mutate(); }}>
            {syncRepos.isPending ? "同步中…" : "从 GitHub 同步仓库"}
          </button>
        </div>
        {syncRepos.isError ? <ApiErrorAlert error={syncRepos.error} title="同步失败" /> : null}
        {syncMessage ? <p className="success-banner mt-md" role="status">{syncMessage}</p> : null}
        <details className="collapse-section mt-md">
          <summary>安装步骤（4 步）</summary>
          <div className="collapse-body">
            <ol className="guide-steps">
              <li><strong>打开安装页</strong><span>点「去 GitHub 安装」→ 找到 App → Install</span></li>
              <li><strong>选择仓库</strong><span>All 或 Only select，保存后 GitHub 自动推送</span></li>
              <li><strong>回本页刷新</strong><span>Installation 列表出现账号，仪表盘出现仓库</span></li>
              <li><strong>结束基线</strong><span>对账成功会自动放行；也可在仪表盘点「立即放行」</span></li>
            </ol>
          </div>
        </details>
      </section>

      <section className="onboarding-card" aria-labelledby="gh-install-title">
        <div className="onboarding-card__header">
          <h2 id="gh-install-title">已记录的 Installation</h2>
          <div className="link-row">
            <button className="quiet-button" type="button" disabled={installations.isFetching} onClick={() => void installations.refetch()}>
              {installations.isFetching ? "刷新中…" : "刷新"}
            </button>
            <a className="quiet-button" href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer"><ExternalLink size={14} aria-hidden="true" /> 去 GitHub 安装</a>
          </div>
        </div>
        {installations.isError ? <ApiErrorAlert error={installations.error} title="无法加载 Installation" /> : null}
        {(() => {
          // 查询成功才渲染列表：避免非空断言，条件渲染与数据解耦。
          const items = installations.data?.items;
          if (!items || items.length === 0) {
            return (
              <EmptyState eyebrow="等待事件" title="尚未收到 Installation" description="请先在 GitHub 安装 App。" action={<a href={GITHUB_INSTALLATIONS} target="_blank" rel="noreferrer">去 GitHub 安装 App</a>} />
            );
          }
          return (
            <ul className="event-list">
              {items.map((inst) => (
                <li key={inst.id}>
                  <span className="event-kind">{inst.account_type || "account"}</span>
                  <strong>{inst.account_login || "（未知账号）"}</strong>
                  <span className="muted">installation {inst.installation_id > 0 ? inst.installation_id : "—"}</span>
                  <span className="muted">{inst.suspended === "true" ? "已挂起" : "正常"}</span>
                  {inst.installation_id > 0 ? (
                    <a className="quiet-button" href={`https://github.com/settings/installations/${inst.installation_id}`} target="_blank" rel="noreferrer">配置</a>
                  ) : null}
                </li>
              ))}
            </ul>
          );
        })()}
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="gh-external-title">
        <h2 id="gh-external-title">登记外部公开仓库</h2>
        <p className="field-hint">关注非本 App 安装范围内的<strong>公开</strong>仓（最多 20 个）。</p>
        {formMessage ? <p className="success-banner" role="status">{formMessage}</p> : null}
        <label className="field--plain">
          <span>仓库全名</span>
          <input value={externalName} onChange={(e) => setExternalName(e.target.value)} placeholder="owner/repo" autoComplete="off" />
        </label>
        <button className="primary-button primary-button--inline" type="button" disabled={addExternal.isPending || !externalName.trim()} onClick={() => { setFormMessage(""); addExternal.mutate(); }}>
          {addExternal.isPending ? "登记中…" : "添加外部仓"}
        </button>
        {addExternal.isError ? <ApiErrorAlert error={addExternal.error} title="无法添加" /> : null}
      </section>
    </>
  );
}

function buildWebhookURL(version?: { public_base_url?: string; github?: { webhook_path?: string } }): string {
  const path = version?.github?.webhook_path || "/webhooks/github";
  const base = (version?.public_base_url || (typeof window !== "undefined" ? window.location.origin : "")).replace(/\/$/, "");
  if (!base) return path;
  return `${base}${path.startsWith("/") ? path : `/${path}`}`;
}
