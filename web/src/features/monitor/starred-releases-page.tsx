import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";

import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import {
  listStarredTrackers,
  saveStarredReleasesConfig,
  setStarredTrackerState,
  starredReleasesConfigQueryOptions,
  syncStarredReleases,
  type StarredReleasesConfig,
  type StarredReleasesConfigInput,
  type StarredTrackerItem,
} from "./api";

interface FormState {
  enabled: boolean;
  username: string;
  starSyncInterval: string;
  releasePollInterval: string;
  maxTrackers: number;
  notifyPrerelease: boolean;
}

function formFromConfig(data: StarredReleasesConfig | undefined): FormState {
  return {
    enabled: data?.enabled !== false,
    username: data?.username || "",
    starSyncInterval: data?.star_sync_interval || "6h0m0s",
    releasePollInterval: data?.release_poll_interval || "10m0s",
    maxTrackers: data?.max_trackers || 500,
    notifyPrerelease: Boolean(data?.notify_prerelease),
  };
}

const TRACKER_STATE_LABELS: Record<string, string> = {
  tracking: "追踪中",
  inactive: "无 Release",
  disabled: "已停用",
  unavailable: "不可用",
};

// 提交负载：跳过与快照一致的字段，仅提交变更，避免慢网时覆盖并发修改。
function configBody(form: FormState, cfg: StarredReleasesConfig | undefined): StarredReleasesConfigInput {
  const body: StarredReleasesConfigInput = {};
  if (!cfg || form.enabled !== (cfg.enabled !== false)) body.enabled = form.enabled;
  if (!cfg || form.username !== cfg.username) body.username = form.username.trim();
  if (!cfg || form.starSyncInterval !== cfg.star_sync_interval) body.star_sync_interval = form.starSyncInterval.trim();
  if (!cfg || form.releasePollInterval !== cfg.release_poll_interval) body.release_poll_interval = form.releasePollInterval.trim();
  if (!cfg || form.maxTrackers !== cfg.max_trackers) body.max_trackers = form.maxTrackers;
  if (!cfg || form.notifyPrerelease !== cfg.notify_prerelease) body.notify_prerelease = form.notifyPrerelease;
  return body;
}

export function StarredReleasesPage() {
  const queryClient = useQueryClient();
  const config = useQuery(starredReleasesConfigQueryOptions);
  const [form, setForm] = useState<FormState>(() => formFromConfig(undefined));
  const [msg, setMsg] = useAutoDismiss();
  const [error, setError] = useState<string>();

  useEffect(() => {
    if (!config.data) return;
    setForm(formFromConfig(config.data));
  }, [config.data]);

  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const invalidateAll = async () => {
    await queryClient.invalidateQueries({ queryKey: ["starred-releases-config"] });
    await queryClient.invalidateQueries({ queryKey: ["starred-releases-trackers"] });
  };

  const saveMut = useMutation({
    mutationFn: (body: StarredReleasesConfigInput) => saveStarredReleasesConfig(body),
    onSuccess: async () => {
      setError(undefined);
      await invalidateAll();
      setMsg("Star Release 追踪配置已保存。");
    },
    onError: (err) => setError(toApiError(err).message),
  });
  function submitConfig() {
    setMsg("");
    setError(undefined);
    saveMut.mutate(configBody(form, config.data));
  }

  // 立即同步：触发一轮 star 枚举（新 star 仓即时注册）。
  const syncMut = useMutation({
    mutationFn: () => syncStarredReleases(),
    onSuccess: async (res) => {
      await invalidateAll();
      setMsg(res.started ? "已触发一轮同步，请稍后刷新列表。" : "未配置用户名，无法同步。");
    },
    onError: (err) => setError(toApiError(err).message),
  });

  // 追踪列表：分页 + state 筛选。默认只看「追踪中」，无 Release/停用/不可用需手动切换。
  const [page, setPage] = useState(1);
  const [stateFilter, setStateFilter] = useState("tracking");
  const trackers = useQuery({
    queryKey: ["starred-releases-trackers", page, stateFilter] as const,
    queryFn: () => listStarredTrackers({ page, per_page: 20, state: stateFilter || undefined }),
    staleTime: 10_000,
  });
  const setStateMut = useMutation({
    mutationFn: ({ id, state }: { id: string; state: "disabled" | "tracking" }) => setStarredTrackerState(id, state),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["starred-releases-trackers"] });
      await queryClient.invalidateQueries({ queryKey: ["starred-releases-config"] });
    },
    onError: (err) => setError(toApiError(err).message),
  });

  const counts = config.data?.counts;
  const items = trackers.data?.items ?? [];
  const total = trackers.data?.total ?? 0;
  const perPage = 20;
  const pageCount = Math.max(1, Math.ceil(total / perPage));

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">追踪</p>
          <h1>Star Release</h1>
          <p>追踪你 star 的公开仓库：新版本发布实时通知，可配 AI 中文总结。</p>
        </div>
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="starred-releases-title">
        <h2 id="starred-releases-title">Star Release 追踪</h2>
        <p className="field-hint">
          填写 GitHub 用户名后，匿名枚举其公开 star 仓库（自动排除 fork 与已归档），并定期轮询各仓最新
          Release——新版本发布即实时通知（含 AI 中文总结，可在「设置」→「AI 集成」关闭）。release 轮询用
          ETag 条件请求，304 不计限流。部署在网络受限环境时设置 <code>HTTPS_PROXY</code> 即可（客户端默认走系统代理）。
        </p>
        {msg ? <p className="success-banner" role="status">{msg}</p> : null}
        {error ? <ErrorAlert title="Star Release 追踪操作失败" message={error} /> : null}
        {config.isError ? <ErrorAlert title="无法加载配置" message={toApiError(config.error).message} errorCode={toApiError(config.error).errorCode} /> : null}

        <label className="check-row">
          <input type="checkbox" checked={form.enabled} onChange={(e) => set("enabled", e.target.checked)} />
          <span>启用 Star Release 追踪</span>
        </label>

        <div className="form-grid">
          <label className="field--plain">
            <span>GitHub 用户名</span>
            <input value={form.username} onChange={(e) => set("username", e.target.value)} placeholder="octocat（可粘贴 github.com/xxx 链接）" />
          </label>
          <label className="field--plain">
            <span>Star 列表同步周期</span>
            <input value={form.starSyncInterval} onChange={(e) => set("starSyncInterval", e.target.value)} placeholder="6h0m0s" />
          </label>
          <label className="field--plain">
            <span>Release 轮询周期</span>
            <input value={form.releasePollInterval} onChange={(e) => set("releasePollInterval", e.target.value)} placeholder="10m0s" />
          </label>
          <label className="field--plain">
            <span>追踪上限</span>
            <input type="number" min={1} max={10000} value={form.maxTrackers} onChange={(e) => set("maxTrackers", Math.min(10000, Math.max(1, Number(e.target.value) || 1)))} />
          </label>
        </div>
        <p className="field-hint">周期使用 Go duration 格式（如 6h、10m、24h）。Star 行为低频，同步周期可放宽；Release 轮询周期决定通知延迟。</p>
        <label className="check-row">
          <input type="checkbox" checked={form.notifyPrerelease} onChange={(e) => set("notifyPrerelease", e.target.checked)} />
          <span>通知预发布版本（默认不通知，正式版发布时仍会通知）</span>
        </label>
        <div className="channel-form__buttons">
          <button className="primary-button primary-button--inline" type="button" disabled={saveMut.isPending || config.isLoading || config.isError} onClick={submitConfig}>
            {saveMut.isPending ? "保存中…" : "保存配置"}
          </button>
          <button className="secondary-button" type="button" disabled={syncMut.isPending || config.isError} aria-busy={syncMut.isPending} onClick={() => syncMut.mutate()}>
            {syncMut.isPending ? "同步中…" : "立即同步 Star 列表"}
          </button>
        </div>

        {counts ? (
          <p className="field-hint" role="status">
            状态概览：追踪中 {counts.tracking ?? 0} ｜ 无 Release {counts.inactive ?? 0} ｜ 已停用 {counts.disabled ?? 0} ｜ 不可用 {counts.unavailable ?? 0}
          </p>
        ) : null}

        <div className="channel-form__header">
          <h3>追踪列表</h3>
          <select value={stateFilter} onChange={(e) => { setStateFilter(e.target.value); setPage(1); }} aria-label="按状态筛选">
            <option value="">全部状态</option>
            <option value="tracking">追踪中</option>
            <option value="inactive">无 Release</option>
            <option value="disabled">已停用</option>
            <option value="unavailable">不可用</option>
          </select>
        </div>
        {trackers.isError ? <ErrorAlert title="无法加载追踪列表" message={toApiError(trackers.error).message} errorCode={toApiError(trackers.error).errorCode} /> : null}
        {items.length === 0 ? (
          <p className="field-hint">暂无追踪记录。保存用户名并点击「立即同步」后，star 仓库会出现在这里。</p>
        ) : (
          <ul className="plain-list" aria-label="Star Release 追踪列表">
            {items.map((it) => (
              <TrackerRow key={it.id} item={it} busy={setStateMut.isPending} onToggle={(state) => setStateMut.mutate({ id: it.id, state })} />
            ))}
          </ul>
        )}
        {total > perPage ? (
          <div className="pager-row">
            <button className="quiet-button quiet-button--compact" type="button" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>
              上一页
            </button>
            <span>第 {page} / {pageCount} 页（共 {total} 条）</span>
            <button className="quiet-button quiet-button--compact" type="button" disabled={page >= pageCount} onClick={() => setPage((p) => Math.min(pageCount, p + 1))}>
              下一页
            </button>
          </div>
        ) : null}
        {saveMut.isError ? <ErrorAlert title="保存失败" message={toApiError(saveMut.error).message} errorCode={toApiError(saveMut.error).errorCode} /> : null}
      </section>
    </>
  );
}

function TrackerRow({ item, busy, onToggle }: { item: StarredTrackerItem; busy: boolean; onToggle: (state: "disabled" | "tracking") => void }) {
  const [busyId, setBusyId] = useState<string | null>(null);
  const isBusy = busyId === item.id;
  const published = item.last_release_published_at ? new Date(item.last_release_published_at).toLocaleString() : "—";
  const url = releaseURL(item.full_name, item.last_release_tag);
  return (
    <li className="tracker-row">
      <span className="tracker-row__main">
        <a className="tracker-row__name" href={url} target="_blank" rel="noreferrer" title="查看该仓库的 Release">
          <code>{item.full_name}</code>
        </a>
        <span className="tracker-row__meta">
          {TRACKER_STATE_LABELS[item.state] ?? item.state}
          {item.last_release_tag ? ` ｜ 最新 ${item.last_release_tag}` : ""} ｜ {published}
        </span>
      </span>
      <span className="tracker-row__actions">
        <a
          className="quiet-button quiet-button--compact tracker-row__link"
          href={url}
          target="_blank"
          rel="noreferrer"
          aria-label="查看最新 Release"
          title={item.last_release_tag ? `查看 ${item.last_release_tag}` : "查看 Releases"}
        >
          <ExternalLink aria-hidden="true" size={13} />
        </a>
        {item.state === "tracking" || item.state === "inactive" || item.state === "unavailable" ? (
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            disabled={busy}
            aria-busy={isBusy}
            onClick={() => { setBusyId(item.id); onToggle("disabled"); }}
          >
            停用
          </button>
        ) : (
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            disabled={busy}
            aria-busy={isBusy}
            onClick={() => { setBusyId(item.id); onToggle("tracking"); }}
          >
            恢复
          </button>
        )}
      </span>
    </li>
  );
}

// releaseURL 由 full_name 与最新 tag 拼 GitHub Release 跳转链接；无 tag（从未发布）时指向仓库 Releases 页。
export function releaseURL(fullName: string, tag: string | undefined): string {
  const parts = fullName.split("/");
  const base = parts.length === 2 ? `https://github.com/${parts[0]}/${parts[1]}/releases` : `https://github.com/${fullName}/releases`;
  return tag ? `${base}/tag/${encodeURIComponent(tag)}` : base;
}
