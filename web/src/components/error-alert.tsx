import { AlertCircle } from "lucide-react";
import type { ReactNode } from "react";

import { toApiError } from "../lib/api/errors";

export interface ErrorAlertProps {
  title: string;
  message: string;
  errorCode?: string;
  action?: ReactNode;
}

export function ErrorAlert({ title, message, errorCode, action }: ErrorAlertProps) {
  return (
    <div className="error-alert" role="alert">
      <AlertCircle aria-hidden="true" size={18} strokeWidth={1.8} />
      <div>
        <strong>{title || "发生错误"}</strong>
        {message ? <p>{message}</p> : null}
        {errorCode ? <code>{errorCode}</code> : null}
        {action ? <div className="error-alert__action">{action}</div> : null}
      </div>
    </div>
  );
}

/**
 * 从查询/变更错误对象直接渲染错误条：内部只调用一次 toApiError，
 * 避免各处重复 `toApiError(err).message / .errorCode` 的双重解析。
 */
export function ApiErrorAlert({ error, title, action }: { error: unknown; title: string; action?: ReactNode }) {
  const apiError = toApiError(error);
  return <ErrorAlert title={title} message={apiError.message} errorCode={apiError.errorCode} action={action} />;
}
