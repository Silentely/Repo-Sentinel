import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { EyeOff, RotateCcw } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ListShell } from "../../components/list-shell";
import { ListSkeleton } from "../../components/list-skeleton";
import { repositoriesQueryOptions, settingsQueryOptions, type Repository, type SystemSettings } from "./api";

export type IgnoredMode = "active" | "ignored";

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
  const enabled = settings.data?.[featureKey] !== false;
  if (!enabled) {
    return (
      <ListShell eyebrow="仓库" title={featureName} description={description}>
        <EmptyState
          title={`${featureName} 功能已禁用`}
          description="可在「关于与设置 → 功能模块开关」中重新启用。"
          action={<Link to="/about">打开设置</Link>}
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
  return { active, isLoading: repos.isLoading };
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
            {r.full_name || `${r.owner}/${r.name}`}
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
        onClick={() => onChange("active")}
      >
        关注中
      </button>
      <button
        className={`quiet-button${mode === "ignored" ? " active" : ""}`}
        type="button"
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
