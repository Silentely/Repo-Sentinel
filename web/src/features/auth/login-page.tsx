import { KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { ThemeToggle } from "../../components/theme-toggle";
import { ApiError, toApiError } from "../../lib/api/errors";
import { login } from "./api";
import { loginSchema, type LoginCredentials } from "./schemas";

export interface LoginPageProps {
  loginAction?: (credentials: LoginCredentials) => void | Promise<unknown>;
  onAuthenticated?: (path: "/") => void;
  version?: string;
}

export function LoginPage({
  loginAction = async (credentials) => {
    await login(credentials);
  },
  onAuthenticated,
  version = "dev",
}: LoginPageProps) {
  const [requestError, setRequestError] = useState<ApiError>();
  const {
    register,
    handleSubmit,
    clearErrors,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<LoginCredentials>({ defaultValues: { username: "", password: "" } });

  const submit = handleSubmit(async (values) => {
    setRequestError(undefined);
    clearErrors();
    const parsed = loginSchema.safeParse(values);
    if (!parsed.success) {
      for (const issue of parsed.error.issues) {
        if (issue.path[0] === "username") {
          setError("username", { message: issue.message });
        }
        if (issue.path[0] === "password") {
          setError("password", { message: issue.message });
        }
      }
      return;
    }
    try {
      await loginAction(parsed.data);
      onAuthenticated?.("/");
    } catch (error) {
      setRequestError(toApiError(error));
    }
  });

  const invalidCredentials = requestError?.errorCode === "invalid_credentials";

  return (
    <main className="auth-shell">
      <div className="auth-shell__theme">
        <ThemeToggle />
      </div>
      <section className="auth-card" aria-labelledby="login-title">
        <header className="auth-card__header">
          <span className="product-mark" aria-hidden="true">
            <ShieldCheck size={22} strokeWidth={1.8} />
          </span>
          <div>
            <p className="eyebrow">仓库值守台</p>
            <h1 id="login-title">RepoSentinel</h1>
            <p>自托管的 GitHub 仓库监控</p>
          </div>
        </header>

        {requestError ? (
          <ErrorAlert
            title={invalidCredentials ? "用户名或密码不正确" : "暂时无法登录"}
            message={
              invalidCredentials
                ? "请检查输入后重试；若凭据已遗失，请使用 CLI 重置密码。"
                : requestError.message
            }
            errorCode={requestError.errorCode}
          />
        ) : null}

        <form className="auth-form" onSubmit={submit} noValidate>
          <label className="field">
            <span>用户名</span>
            <span className="field__control">
              <UserRound aria-hidden="true" size={17} />
              <input
                autoComplete="username"
                aria-invalid={Boolean(errors.username)}
                aria-describedby={errors.username ? "login-username-error" : undefined}
                {...register("username")}
              />
            </span>
            {errors.username ? (
              <small id="login-username-error" className="field__error">
                {errors.username.message}
              </small>
            ) : null}
          </label>

          <label className="field">
            <span>密码</span>
            <span className="field__control">
              <KeyRound aria-hidden="true" size={17} />
              <input
                type="password"
                autoComplete="current-password"
                aria-invalid={Boolean(errors.password)}
                aria-describedby={errors.password ? "login-password-error" : undefined}
                {...register("password")}
              />
            </span>
            {errors.password ? (
              <small id="login-password-error" className="field__error">
                {errors.password.message}
              </small>
            ) : null}
          </label>

          <button className="primary-button" type="submit" disabled={isSubmitting}>
            {isSubmitting ? "正在登录…" : "登录"}
          </button>
        </form>

        <footer className="auth-card__footer">
          <a
            href="https://github.com/Silentely/Repo-Sentinel/blob/main/docs/operations/administrator-access.md#使用-cli-重置密码"
            target="_blank"
            rel="noreferrer"
          >
            使用 CLI 重置密码
          </a>
          <span>版本 {version}</span>
        </footer>
      </section>
    </main>
  );
}
