import type { ErrorComponentProps } from "@tanstack/react-router";
import { Link } from "@tanstack/react-router";

import { EmptyState } from "../components/empty-state";
import { ErrorAlert } from "../components/error-alert";
import { toApiError } from "../lib/api/errors";

// 路由级错误兜底：lazy chunk 加载失败（如升级后旧 hash 失效）或渲染抛错时，避免整树白屏。
export function RouteErrorFallback({ error, reset }: ErrorComponentProps) {
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
