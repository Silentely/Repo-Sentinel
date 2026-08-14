import type { ReactNode } from "react";
import type { FieldValues, UseFormRegisterReturn, UseFormSetError } from "react-hook-form";
import type { ZodIssue } from "zod";

// ---- 登录/首次安装共用的卡片头部与表单字段 ----
// 两页的 auth-card 头部（产品标识 + eyebrow + 标题）与字段结构（label + 图标 + input + 校验错误）
// 逐字重复，收敛为共享组件避免漂移。

export interface AuthCardHeaderProps {
  titleId: string;
  eyebrow: string;
  title: string;
  subtitle: string;
  productMark: ReactNode;
}

/** 认证卡片头部：产品标识 + 分步说明 + 标题 + 副标题。 */
export function AuthCardHeader({ titleId, eyebrow, title, subtitle, productMark }: AuthCardHeaderProps) {
  return (
    <header className="auth-card__header">
      <span className="product-mark" aria-hidden="true">
        {productMark}
      </span>
      <div>
        <p className="eyebrow">{eyebrow}</p>
        <h1 id={titleId}>{title}</h1>
        <p>{subtitle}</p>
      </div>
    </header>
  );
}

export interface AuthFieldProps {
  id: string;
  label: string;
  icon: ReactNode;
  type?: "text" | "password";
  autoComplete?: string;
  /** 页面载入时自动聚焦（登录页用户名）；表单在可见区域内时才有意义。 */
  autoFocus?: boolean;
  // 校验错误消息；存在时展示红色错误提示。
  error?: string;
  // 无错误时的帮助文本（如密码强度规则）。
  help?: string;
  registration: UseFormRegisterReturn;
}

/** 认证表单字段：label + 图标输入框 + 校验错误/帮助文本。 */
export function AuthField({ id, label, icon, type = "text", autoComplete, autoFocus, error, help, registration }: AuthFieldProps) {
  const errorId = `${id}-error`;
  const helpId = `${id}-help`;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <span className="field__control">
        {icon}
        <input
          id={id}
          type={type}
          autoComplete={autoComplete}
          autoFocus={autoFocus}
          // 登录/初始化表单均必填：表单 noValidate 场景下用 aria-required 告知读屏。
          aria-required="true"
          aria-invalid={Boolean(error)}
          aria-describedby={error ? errorId : help ? helpId : undefined}
          {...registration}
        />
      </span>
      {error ? (
        <small id={errorId} className="field__error">
          {error}
        </small>
      ) : help ? (
        <small id={helpId}>{help}</small>
      ) : null}
    </div>
  );
}

/** 把 zod 校验错误按字段路径分发到表单错误状态（登录/安装共用）。 */
export function applyZodErrors<TFieldValues extends FieldValues>(
  issues: ZodIssue[],
  setError: UseFormSetError<TFieldValues>,
  allowedFields: readonly string[],
) {
  type ErrorName = Parameters<typeof setError>[0];
  for (const issue of issues) {
    const field = issue.path[0];
    if (typeof field === "string" && allowedFields.includes(field)) {
      setError(field as ErrorName, { message: issue.message });
    }
  }
}
