import type { ErrorComponentProps } from "@tanstack/react-router";
import { Link } from "@tanstack/react-router";

import { EmptyState } from "../components/empty-state";
import { ErrorAlert } from "../components/error-alert";
import { ListSkeleton } from "../components/list-skeleton";
import { toApiError } from "../lib/api/errors";

// 路由级错误兜底：lazy chunk 加载失败（如升级后旧 hash 失效）或渲染抛错时，避免整树白屏。
export function RouteErrorFallback({ error, reset }: ErrorComponentProps) {
  // 升级后旧 chunk 失效是最常见的触发场景：ChunkLoadError 被 toApiError 误判为
  // 「无法连接」，文案与真实原因不符，需单独识别并引导刷新。
  const isChunkError = error instanceof Error && /Loading chunk|dynamically imported module|ChunkLoadError/i.test(error.message);
  if (isChunkError) {
    return (
      <section className="route-fallback">
        <ErrorAlert
          title="页面资源已更新"
          message="应用发布了新版本，当前页面引用的旧资源已失效，刷新后即可继续使用。"
        />
        <div className="route-fallback__actions">
          <button type="button" className="primary-button primary-button--inline" onClick={() => window.location.reload()}>
            刷新页面
          </button>
          <Link className="quiet-button" to="/">
            返回仪表盘
          </Link>
        </div>
      </section>
    );
  }
  const apiError = toApiError(error);
  return (
    <section className="route-fallback">
      <ErrorAlert title="页面加载失败" message={apiError.message} errorCode={apiError.errorCode} />
      <div className="route-fallback__actions">
        <button type="button" className="primary-button primary-button--inline" onClick={() => reset()}>
          重试
        </button>
        <button type="button" className="quiet-button" onClick={() => window.location.reload()}>
          刷新页面
        </button>
        <Link className="quiet-button" to="/">
          返回仪表盘
        </Link>
      </div>
    </section>
  );
}

export function RouteNotFoundFallback() {
  return (
    <EmptyState
      eyebrow="404"
      title="页面不存在"
      description="你访问的地址没有对应页面，可能是链接已过期或输入有误。"
      action={<Link to="/">返回仪表盘</Link>}
    />
  );
}

// 路由级加载兜底：lazy chunk 首载期间展示骨架而非空白内容区
//（慢网/直接地址栏进入时 defaultPreload:"intent" 帮不上忙）。
export function RoutePendingFallback() {
  return (
    <section className="route-fallback" aria-busy="true" aria-label="页面加载中">
      <ListSkeleton />
    </section>
  );
}
