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
      <button type="button" className="primary-button primary-button--inline" onClick={onClick}>
        清除筛选
      </button>
    );
  }
  return (
    <button className="quiet-button quiet-button--compact" type="button" onClick={onClick}>
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
  if (settings.isLoading) {
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

/** 活跃（未归档）仓库列表，用于四页共用筛选。 */
export function useActiveRepos() {
  const repos = useQuery(repositoriesQueryOptions);
  const active = useMemo(
    () => (repos.data?.items ?? []).filter((r) => !r.is_archived && r.sync_status !== "archived"),
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
    <label className="repo-filter">
      <span className="sr-only">按仓库筛选</span>
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

/** 忽略开关 mutation：统一 busy 状态与查询失效。 */
export function useIgnoreMutation(
  mutateFn: (id: string, ignored: boolean) => Promise<void>,
  invalidateKeys: string[],
) {
  const queryClient = useQueryClient();
  const [busyId, setBusyId] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: ({ id, ignored }: { id: string; ignored: boolean }) => mutateFn(id, ignored),
    onMutate: ({ id }) => setBusyId(id),
    onSettled: async () => {
      setBusyId(null);
      for (const key of invalidateKeys) {
        await queryClient.invalidateQueries({ queryKey: [key] });
      }
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
  return { mutation, busyId };
}

export { ListShell, ListSkeleton };
