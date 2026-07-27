import { KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { ThemeToggle } from "../../components/theme-toggle";
import { ApiError, toApiError } from "../../lib/api/errors";
import { createAdmin } from "./api";
import { setupSchema, type LoginCredentials, type SetupFormValues } from "./schemas";

export interface SetupPageProps {
  setupAction?: (credentials: LoginCredentials) => void | Promise<unknown>;
  onCreated?: (path: "/") => void;
}

export function SetupPage({
  setupAction = async (credentials) => {
    await createAdmin(credentials);
  },
  onCreated,
}: SetupPageProps) {
  const [requestError, setRequestError] = useState<ApiError>();
  const {
    register,
    handleSubmit,
    clearErrors,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<SetupFormValues>({
    defaultValues: { username: "", password: "", confirmPassword: "" },
  });

  const submit = handleSubmit(async (values) => {
    setRequestError(undefined);
    clearErrors();
    const parsed = setupSchema.safeParse(values);
    if (!parsed.success) {
      for (const issue of parsed.error.issues) {
        const field = issue.path[0];
        if (field === "username" || field === "password" || field === "confirmPassword") {
          setError(field, { message: issue.message });
        }
      }
      return;
    }
    try {
      await setupAction({ username: parsed.data.username, password: parsed.data.password });
      onCreated?.("/");
    } catch (error) {
      setRequestError(toApiError(error));
    }
  });

  return (
    <main className="auth-shell auth-shell--setup">
      <div className="auth-shell__theme">
        <ThemeToggle />
      </div>
      <section className="auth-card" aria-labelledby="setup-title">
        <header className="auth-card__header">
          <span className="product-mark" aria-hidden="true">
            <ShieldCheck size={22} strokeWidth={1.8} />
          </span>
          <div>
            <p className="eyebrow">首次设置 · 1 / 1</p>
            <h1 id="setup-title">创建唯一管理员</h1>
            <p>凭据仅保存在你的 RepoSentinel 实例中。</p>
          </div>
        </header>

        {requestError ? (
          <ErrorAlert
            title="无法创建管理员"
            message={requestError.message}
            errorCode={requestError.errorCode}
          />
        ) : null}

        <form className="auth-form" onSubmit={submit} noValidate>
          <div className="field">
            <label htmlFor="setup-username">用户名</label>
            <span className="field__control">
              <UserRound aria-hidden="true" size={17} />
              <input
                id="setup-username"
                autoComplete="username"
                aria-invalid={Boolean(errors.username)}
                aria-describedby={errors.username ? "setup-username-error" : undefined}
                {...register("username")}
              />
            </span>
            {errors.username ? (
              <small id="setup-username-error" className="field__error">
                {errors.username.message}
              </small>
            ) : null}
          </div>

          <div className="field">
            <label htmlFor="setup-password">密码</label>
            <span className="field__control">
              <KeyRound aria-hidden="true" size={17} />
              <input
                id="setup-password"
                type="password"
                autoComplete="new-password"
                aria-invalid={Boolean(errors.password)}
                aria-describedby={errors.password ? "setup-password-error" : "setup-password-help"}
                {...register("password")}
              />
            </span>
            {errors.password ? (
              <small id="setup-password-error" className="field__error">
                {errors.password.message}
              </small>
            ) : (
              <small id="setup-password-help">至少 12 个 Unicode 字符。</small>
            )}
          </div>

          <div className="field">
            <label htmlFor="setup-confirm-password">确认密码</label>
            <span className="field__control">
              <KeyRound aria-hidden="true" size={17} />
              <input
                id="setup-confirm-password"
                type="password"
                autoComplete="new-password"
                aria-invalid={Boolean(errors.confirmPassword)}
                aria-describedby={errors.confirmPassword ? "setup-confirm-error" : undefined}
                {...register("confirmPassword")}
              />
            </span>
            {errors.confirmPassword ? (
              <small id="setup-confirm-error" className="field__error">
                {errors.confirmPassword.message}
              </small>
            ) : null}
          </div>

          <button className="primary-button" type="submit" disabled={isSubmitting}>
            {isSubmitting ? "正在创建…" : "创建管理员"}
          </button>
        </form>

        <footer className="auth-card__footer auth-card__footer--single">
          <span>完成后将立即建立安全 Session。</span>
        </footer>
      </section>
    </main>
  );
}
