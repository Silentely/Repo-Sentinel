import { AlertCircle } from "lucide-react";

export interface ErrorAlertProps {
  title: string;
  message: string;
  errorCode?: string;
}

export function ErrorAlert({ title, message, errorCode }: ErrorAlertProps) {
  return (
    <div className="error-alert" role="alert">
      <AlertCircle aria-hidden="true" size={18} strokeWidth={1.8} />
      <div>
        <strong>{title}</strong>
        <p>{message}</p>
        {errorCode ? <code>{errorCode}</code> : null}
      </div>
    </div>
  );
}
