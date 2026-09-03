import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";

import { ApiErrorAlert, ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import { useUrlState } from "../../lib/use-url-state";
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
import { NumberField } from "./number-field";

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

/** 追踪列表每页条数：查询参数、页数计算与分页条显隐共用单一来源。 */
const TRACKERS_PER_PAGE = 20;

export function StarredReleasesPage() {
  const queryClient = useQueryClient();
  const config = useQuery(starredReleasesConfigQueryOptions);
  const [form, setForm] = useState<FormState>(() => formFromConfig(undefined));
  const [msg, setMsg] = useAutoDismiss();
  const [error, setError] = useState<string>();
  // 周期输入即时校验：与服务端范围一致（star 同步 1m~30d，release 轮询 1m~24h），
  // 失焦反馈格式/范围问题，提交时再强校验一次（服务端仍为最终裁决）。
  const [intervalHint, setIntervalHint] = useState<{ starSync: string; releasePoll: string }>({ starSync: "", releasePoll: "" });
  const validateInterval = (raw: string, maxSeconds: number): string => {
    const seconds = parseGoDurationSeconds(raw);
    if (seconds == null) return "周期格式非法，请使用 Go duration 格式（如 6h、10m、1.5h）。";
    if (seconds < 60 || seconds > maxSeconds) {
      // 上限按天/小时表述（30 天上限不会提示成「720 小时」）。
      const maxHuman = maxSeconds >= 86400 ? `${Math.round(maxSeconds / 86400)} 天` : `${Math.round(maxSeconds / 3600)} 小时`;
      return `周期需在 1 分钟 ~ ${maxHuman} 之间。`;
    }
    return "";
  };

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
    // 提交前强校验周期：非法直接阻断并提示，避免服务端 400 后只剩通用错误条。
    const starSyncHint = validateInterval(form.starSyncInterval, 30 * 24 * 3600);
    const releasePollHint = validateInterval(form.releasePollInterval, 24 * 3600);
    setIntervalHint({ starSync: starSyncHint, releasePoll: releasePollHint });
    if (starSyncHint || releasePollHint) {
      setError("周期配置不符合要求，请修正后保存。");
      return;
    }
    saveMut.mutate(configBody(form, config.data));
  }

  // 立即同步：触发一轮 star 枚举（新 star 仓即时注册）。
  const syncMut = useMutation({
    mutationFn: () => syncStarredReleases(),
    onSuccess: async (res) => {
      await invalidateAll();
      setMsg(res.started ? "已触发一轮同步，追踪列表将自动更新。" : "未配置用户名，无法同步。");
    },
    onError: (err) => setError(toApiError(err).message),
  });

  // 追踪列表：分页 + state 筛选。默认只看「追踪中」，无 Release/停用/不可用需手动切换。
  // 分页与状态筛选同步到 URL（?page=2&state=inactive 等）：刷新/复制链接保留当前视角。
  const [pageRaw, setPageRaw] = useUrlState("page", "1");
  const page = Math.max(1, Number(pageRaw) || 1);
  const setPage = (next: number) => setPageRaw(String(next));
  const [stateFilter, setStateFilter] = useUrlState("state", "tracking");
  const trackers = useQuery({
    queryKey: ["starred-releases-trackers", page, stateFilter] as const,
    queryFn: () => listStarredTrackers({ page, per_page: TRACKERS_PER_PAGE, state: stateFilter || undefined }),
    staleTime: 10_000,
  });
  const total = trackers.data?.total ?? 0;
  const pageCount = Math.max(1, Math.ceil(total / TRACKERS_PER_PAGE));
  // URL 中 page 超界（如筛选切换后共 3 页但 ?page=99）时钳制回末页，避免「第 99 / 3 页」+ 空列表。
  useEffect(() => {
    if (total > 0 && page > pageCount) {
      setPage(pageCount);
    }
  }, [page, pageCount, total, setPage]);
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

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">追踪</p>
          <h1>Star Release</h1>
          <p>追踪你 star 的公开仓库：新版本发布实时通知，可配更新速览。</p>
        </div>
      </section>

      <section className="onboarding-card channel-form" aria-labelledby="starred-releases-title">
        <h2 id="starred-releases-title">Star Release 追踪</h2>
        <p className="field-hint">
          填写 GitHub 用户名后，匿名枚举其公开 star 仓库（自动排除 fork 与已归档），并定期轮询各仓最新
          Release——新版本发布即实时通知（含更新速览，可在「设置」→「智能值守」关闭）。release 轮询用
          ETag 条件请求，304 不计限流。部署在网络受限环境时设置 <code>HTTPS_PROXY</code> 即可（客户端默认走系统代理）。
        </p>
        {msg ? <p className="success-banner" role="status">{msg}</p> : null}
        {error ? <ErrorAlert title="Star Release 追踪操作失败" message={error} /> : null}
        {config.isError ? <ApiErrorAlert error={config.error} title="无法加载配置" /> : null}

        <label className="check-row">
          <input type="checkbox" checked={form.enabled} onChange={(e) => set("enabled", e.target.checked)} />
          <span>启用 Star Release 追踪</span>
        </label>

        <div className="form-grid">
          <label className="field--plain">
            <span>GitHub 用户名</span>
            <input size={24} value={form.username} onChange={(e) => set("username", e.target.value)} placeholder="octocat（可粘贴 github.com/xxx 链接）" />
          </label>
          <label className="field--plain">
            <span>Star 列表同步周期</span>
            <input
              value={form.starSyncInterval}
              size={10}
              onChange={(e) => set("starSyncInterval", e.target.value)}
              onBlur={() => setIntervalHint((prev) => ({ ...prev, starSync: validateInterval(form.starSyncInterval, 30 * 24 * 3600) }))}
              placeholder="6h0m0s"
            />
          </label>
          {intervalHint.starSync ? <p className="field-hint" role="status">{intervalHint.starSync}</p> : null}
          <label className="field--plain">
            <span>Release 轮询周期</span>
            <input
              value={form.releasePollInterval}
              size={10}
              onChange={(e) => set("releasePollInterval", e.target.value)}
              onBlur={() => setIntervalHint((prev) => ({ ...prev, releasePoll: validateInterval(form.releasePollInterval, 24 * 3600) }))}
              placeholder="10m0s"
            />
          </label>
          {intervalHint.releasePoll ? <p className="field-hint" role="status">{intervalHint.releasePoll}</p> : null}
          <label className="field--plain">
            <span>追踪上限</span>
            <NumberField min={1} max={10000} integer value={form.maxTrackers} onChange={(v) => set("maxTrackers", v)} />
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
        {trackers.isError ? <ApiErrorAlert error={trackers.error} title="无法加载追踪列表" /> : null}
        {/* 空态仅在非加载/非错误时展示，避免与错误条矛盾或首载时闪空态文案 */}
        {!trackers.isPending && !trackers.isError && items.length === 0 ? (
          <p className="field-hint">暂无追踪记录。保存用户名并点击「立即同步」后，star 仓库会出现在这里。</p>
        ) : !trackers.isError ? (
          <ul className="plain-list" aria-label="Star Release 追踪列表">
            {items.map((it) => (
              <TrackerRow key={it.id} item={it} busy={setStateMut.isPending && setStateMut.variables?.id === it.id} onToggle={(state) => setStateMut.mutate({ id: it.id, state })} />
            ))}
          </ul>
        ) : null}
        {total > TRACKERS_PER_PAGE ? (
          <div className="pager-row">
            <button className="quiet-button quiet-button--compact" type="button" disabled={page <= 1} onClick={() => setPage(Math.max(1, page - 1))}>
              上一页
            </button>
            <span>第 {page} / {pageCount} 页（共 {total} 条）</span>
            <button className="quiet-button quiet-button--compact" type="button" disabled={page >= pageCount} onClick={() => setPage(Math.min(pageCount, page + 1))}>
              下一页
            </button>
          </div>
        ) : null}
        {saveMut.isError ? <ApiErrorAlert error={saveMut.error} title="保存失败" /> : null}
      </section>
    </>
  );
}

export function TrackerRow({ item, busy, onToggle }: { item: StarredTrackerItem; busy: boolean; onToggle: (state: "disabled" | "tracking") => void }) {
  // busy 由父级 mutation 的 variables.id 判定：请求结束自动恢复，不会残留行级忙碌态。
  let published = "—";
  if (item.last_release_published_at) {
    const d = new Date(item.last_release_published_at);
    published = Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("zh-CN");
  }
  const url = releaseURL(item.full_name, item.last_release_tag);
  return (
    <li className="tracker-row">
      <span className="tracker-row__main">
        <a className="tracker-row__name" href={url} target="_blank" rel="noopener noreferrer" title="查看该仓库的 Release">
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
          rel="noopener noreferrer"
          aria-label="查看最新 Release"
          title={item.last_release_tag ? `查看 ${item.last_release_tag}` : "查看 Releases"}
        >
          <ExternalLink aria-hidden="true" size={13} />
        </a>
        {item.state === "disabled" ? (
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            disabled={busy}
            aria-busy={busy}
            onClick={() => onToggle("tracking")}
          >
            恢复
          </button>
        ) : (
          <>
            {item.state === "inactive" && item.last_release_tag ? (
              // 无 Release 但带已记录 release：多为条件请求 304 误判，提供恢复入口。
              <button
                className="quiet-button quiet-button--compact"
                type="button"
                disabled={busy}
                aria-busy={busy}
                onClick={() => onToggle("tracking")}
              >
                恢复
              </button>
            ) : null}
            <button
              className="quiet-button quiet-button--compact"
              type="button"
              disabled={busy}
              aria-busy={busy}
              onClick={() => onToggle("disabled")}
            >
              停用
            </button>
          </>
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

/**
 * 简化解析 Go duration 字符串（支持 d/h/m/s/ms 组合与小数，如 6h0m0s / 1.5h）。
 * 返回总秒数；格式非法返回 null。仅用于提交前客户端校验，服务端仍做最终强校验。
 */
export function parseGoDurationSeconds(raw: string): number | null {
  const s = raw.trim();
  if (!s || !/^(\d+(\.\d+)?(ms|s|m|h|d))+$/.test(s)) {
    return null;
  }
  let total = 0;
  const re = /(\d+(?:\.\d+)?)(ms|s|m|h|d)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(s)) !== null) {
    const value = parseFloat(m[1] ?? "");
    switch (m[2]) {
      case "ms":
        total += value / 1000;
        break;
      case "s":
        total += value;
        break;
      case "m":
        total += value * 60;
        break;
      case "h":
        total += value * 3600;
        break;
      case "d":
        total += value * 86400;
        break;
      default:
        return null; // 前置正则已限定单位，理论不可达
    }
  }
  return total;
}
