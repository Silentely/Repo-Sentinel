import { AlertCircle } from "lucide-react";
import type { ReactNode } from "react";

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
        <strong>{title}</strong>
        <p>{message}</p>
        {errorCode ? <code>{errorCode}</code> : null}
        {action ? <div className="error-alert__action">{action}</div> : null}
      </div>
    </div>
  );
}
