import type { ReactNode } from "react";

import { toApiError } from "../lib/api/errors";

import { ErrorAlert } from "./error-alert";
import { ListSkeleton } from "./list-skeleton";

/** QueryGate 只消费查询结果的三态切片，useQuery / useInfiniteQuery 的返回值均可直接整体传入。 */
export interface QueryGateQuery {
  isPending?: boolean;
  isLoading?: boolean;
  isError: boolean;
  error?: unknown;
  /** TanStack Query 提供的重新获取动作；未提供时保持仅展示错误的兼容行为。 */
  refetch?: () => unknown;
}

export interface QueryGateProps {
  query: QueryGateQuery;
  /** 错误提示标题，默认「加载失败」。 */
  errorTitle?: string;
  /** 加载占位节点，默认复用 ListSkeleton。 */
  skeleton?: ReactNode;
  /** 查询成功后的空态判定。 */
  isEmpty: boolean;
  emptyState: ReactNode;
  children: ReactNode;
}

/** 查询三态守卫：统一处理失败 / 加载中 / 空数据，避免查询失败时静默露出空态。 */
export function QueryGate({ query, errorTitle = "加载失败", skeleton, isEmpty, emptyState, children }: QueryGateProps) {
  // TanStack Query v5 以 isPending 为准；isLoading 仅为兼容旧命名兜底。
  const pending = query.isPending ?? query.isLoading ?? false;
  if (query.isError) {
    const apiError = toApiError(query.error);
    return (
      <ErrorAlert
        title={errorTitle}
        message={apiError.message}
        errorCode={apiError.errorCode}
        action={
          query.refetch ? (
            <button type="button" className="primary-button primary-button--inline" onClick={() => void query.refetch?.()}>
              重试
            </button>
          ) : undefined
        }
      />
    );
  }
  if (pending) {
    return <>{skeleton ?? <ListSkeleton />}</>;
  }
  if (isEmpty) {
    return <>{emptyState}</>;
  }
  return <>{children}</>;
}
