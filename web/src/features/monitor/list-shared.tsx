import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { EyeOff, RotateCcw } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { ListShell } from "../../components/list-shell";
import { ListSkeleton } from "../../components/list-skeleton";
import { toApiError } from "../../lib/api/errors";
import { repoDisplayName } from "../../lib/format";
import { repositoriesQueryOptions, settingsQueryOptions, type Repository, type SystemSettings } from "./api";

export type IgnoredMode = "active" | "ignored";

/** 状态/结论筛选按钮组：当前选中项高亮（列表页/投递记录页共用，避免各页维护同一结构）。 */
export function StateFilterButtons({
  options,
  value,
  onChange,
}: {
  options: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <>
      {options.map((opt) => (
        <button
          key={opt.value}
          className={`quiet-button${value === opt.value ? " active" : ""}`}
          type="button"
          aria-pressed={value === opt.value}
          onClick={() => onChange(opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </>
  );
}

/** 清除筛选按钮：筛选栏紧凑版与空态主按钮版共用，避免各列表页重复维护同一块。 */
export function ClearFiltersButton({
  onClick,
  variant = "compact",
}: {
  onClick: () => void;
  variant?: "compact" | "primary";
}) {
  if (variant === "primary") {
    return (
      <button type="button" className="primary-button primary-button--inline" onClick={onClick} aria-label="清除当前所有筛选条件">
        清除筛选
      </button>
    );
  }
  return (
    <button className="quiet-button quiet-button--compact" type="button" onClick={onClick} aria-label="清除当前所有筛选条件">
      清除筛选
    </button>
  );
}

/** 功能开关守卫：加载中展示骨架，关闭时展示空状态。 */
export function FeatureGuard({
  featureKey,
  featureName,
  description = "",
  children,
}: {
  featureKey: keyof SystemSettings;
  featureName: string;
  description?: string;
  children: ReactNode;
}) {
  const settings = useQuery(settingsQueryOptions);
  // isPending 覆盖「无缓存且加载中」，与 QueryGate 判定一致。
  if (settings.isPending) {
    return (
      <ListShell eyebrow="仓库" title={featureName} description={description}>
        <ListSkeleton />
      </ListShell>
    );
  }
  // 开关查询失败不能静默按「启用」放行（可能让未订阅的页面误展示空数据），
  // 显式报错并把错误码透出，便于按 request_id 排查。
  if (settings.isError) {
    const apiError = toApiError(settings.error);
    return (
      <ListShell eyebrow="仓库" title={featureName} description={description}>
        <ErrorAlert title="无法加载功能开关" message={apiError.message} errorCode={apiError.errorCode} />
      </ListShell>
    );
  }
  const enabled = settings.data?.[featureKey] !== false;
  if (!enabled) {
    return (
      <ListShell eyebrow="仓库" title={featureName} description={description}>
        <EmptyState
          title={`${featureName} 功能已禁用`}
          description="可在「设置 → 功能模块开关」中重新启用。"
          action={<Link to="/settings">打开设置</Link>}
        />
      </ListShell>
    );
  }
  return <>{children}</>;
}

/** 活跃（未归档、未不可用）仓库列表，用于四页共用筛选。 */
export function useActiveRepos() {
  const repos = useQuery(repositoriesQueryOptions);
  const active = useMemo(
    // 排除 archived 与 unavailable：不可用仓库的工作项列表本就不展示，进下拉只会选中即空态。
    () =>
      (repos.data?.items ?? []).filter(
        (r) => !r.is_archived && r.sync_status !== "archived" && r.sync_status !== "unavailable",
      ),
    [repos.data?.items],
  );
  return { active };
}

export function RepoFilterSelect({
  value,
  onChange,
  repos,
}: {
  value: string;
  onChange: (id: string) => void;
  repos: Repository[];
}) {
  return (
    // label 关联 + select aria-label 双份命名重复：保留 aria-label（读屏只取其一），
    // sr-only 文本删去避免标注冗余。
    <label className="repo-filter">
      <select value={value} onChange={(e) => onChange(e.target.value)} aria-label="按仓库筛选">
        <option value="">全部仓库</option>
        {repos.map((r) => (
          <option key={r.id} value={r.id}>
            {repoDisplayName(r)}
          </option>
        ))}
      </select>
    </label>
  );
}

export function IgnoredToggle({
  mode,
  onChange,
}: {
  mode: IgnoredMode;
  onChange: (mode: IgnoredMode) => void;
}) {
  return (
    <>
      <span className="filter-bar__sep" />
      <button
        className={`quiet-button${mode === "active" ? " active" : ""}`}
        type="button"
        aria-pressed={mode === "active"}
        onClick={() => onChange("active")}
      >
        关注中
      </button>
      <button
        className={`quiet-button${mode === "ignored" ? " active" : ""}`}
        type="button"
        aria-pressed={mode === "ignored"}
        onClick={() => onChange("ignored")}
      >
        已忽略
      </button>
    </>
  );
}

export function IgnoreButton({
  ignored,
  busy,
  onToggle,
}: {
  ignored?: boolean;
  busy?: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      className="quiet-button quiet-button--compact"
      type="button"
      disabled={busy}
      onClick={onToggle}
      title={ignored ? "取消忽略" : "忽略此项"}
    >
      {ignored ? <RotateCcw size={14} aria-hidden="true" /> : <EyeOff size={14} aria-hidden="true" />}
      <span>{ignored ? "取消忽略" : "忽略"}</span>
    </button>
  );
}

/** 忽略开关 mutation：统一 busy 状态、查询失效与失败反馈。 */
export function useIgnoreMutation(
  mutateFn: (id: string, ignored: boolean) => Promise<void>,
  invalidateKeys: string[],
) {
  const queryClient = useQueryClient();
  const [busyId, setBusyId] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: ({ id, ignored }: { id: string; ignored: boolean }) => mutateFn(id, ignored),
    onMutate: ({ id }) => {
      setBusyId(id);
      setErrorMessage(null);
    },
    onSettled: async () => {
      setBusyId(null);
      for (const key of invalidateKeys) {
        await queryClient.invalidateQueries({ queryKey: [key] });
      }
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
    onError: (error) => setErrorMessage(toApiError(error).message || "操作失败"),
  });
  return { mutation, busyId, errorMessage };
}

export { ListShell, ListSkeleton };
