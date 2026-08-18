import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLink } from "lucide-react";

import { ApiErrorAlert, ErrorAlert } from "../../components/error-alert";
import { GithubIcon } from "../../components/github-icon";
import { toApiError } from "../../lib/api/errors";
import { checkForUpdates, versionQueryOptions } from "./api";

const REPO_URL = "https://github.com/Silentely/Repo-Sentinel";
const DOCS_CONFIG = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md";
const DOCS_FAQ = "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/faq.md";
const DOCS_CHANGELOG = "https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md";

export function AboutPage() {
  const queryClient = useQueryClient();
  const version = useQuery(versionQueryOptions);
  const [checking, setChecking] = useState(false);
  const [banner, setBanner] = useState<{
    kind: "update" | "latest" | "error" | "info";
    text: string;
    url?: string | null;
  } | null>(null);

  const v = version.data || {};

  const checkUpdate = async (force = true) => {
    setChecking(true);
    try {
      const res = await checkForUpdates(force);
      const uc = res.update_check;
      // 检查完成（含失败）后刷新版本信息：底部「版本」列表可能因远程响应带出构建元数据变化。
      await queryClient.invalidateQueries({ queryKey: ["version"] });
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

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统</p>
          <h1>关于</h1>
          <p>版本信息、更新检查与运维提示。运行偏好与账号请在「设置」维护。</p>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-product-title">
        <h2 id="about-product-title">你在用什么</h2>
        <p>单管理员实例，数据与密钥都在你控制的环境中。</p>
        <div className="link-row">
          <a className="quiet-button" href={REPO_URL} target="_blank" rel="noreferrer" title="在新窗口打开"><GithubIcon size={14} aria-hidden="true" /> GitHub 仓库</a>
          <a className="quiet-button" href={DOCS_CONFIG} target="_blank" rel="noreferrer" title="在新窗口打开"><ExternalLink size={14} aria-hidden="true" /> 配置参考</a>
          <a className="quiet-button" href={DOCS_FAQ} target="_blank" rel="noreferrer" title="在新窗口打开"><ExternalLink size={14} aria-hidden="true" /> 常见问题</a>
          <a className="quiet-button" href={DOCS_CHANGELOG} target="_blank" rel="noreferrer" title="在新窗口打开"><ExternalLink size={14} aria-hidden="true" /> 变更日志</a>
          <Link className="quiet-button" to="/github">GitHub App 指引</Link>
          <Link className="quiet-button" to="/settings">打开设置</Link>
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="about-build-title">
        <div className="onboarding-card__header">
          <h2 id="about-build-title">构建与运行</h2>
          <button type="button" className="quiet-button" disabled={checking || version.isLoading} onClick={() => void checkUpdate(true)}>
            {checking ? "检查中…" : "检查更新"}
          </button>
        </div>
        {version.isError ? <ApiErrorAlert error={version.error} title="无法加载版本" /> : null}
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
          <div><dt>构建时间</dt><dd>{v.build_time ? new Date(v.build_time).toLocaleString("zh-CN") : "—"}</dd></div>
          <div><dt>Go</dt><dd>{v.go_version || "—"}</dd></div>
          <div><dt>数据库</dt><dd>{v.database_driver || "—"}</dd></div>
          <div><dt>Schema</dt><dd className="mono">{v.schema_version || "—"}</dd></div>
          <div><dt>监听地址</dt><dd className="mono">{v.http_addr || "—"}</dd></div>
          <div><dt>Public Base URL</dt><dd className="mono">{v.public_base_url || "（未设置）"}</dd></div>
          <div><dt>更新检查</dt><dd>{v.update_check_enabled === false ? "已关闭" : "已开启"}</dd></div>
          <div><dt>Webhook 路径</dt><dd className="mono">{v.github?.webhook_path || "/webhooks/github"}</dd></div>
        </dl>
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
