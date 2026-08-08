import { KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { ThemeToggle } from "../../components/theme-toggle";
import { ApiError, toApiError } from "../../lib/api/errors";
import { AuthCardHeader, AuthField, applyZodErrors } from "./auth-card";
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
  // 登录页不在 RootLayout 内，标签页标题需独立设置。
  useEffect(() => {
    document.title = "登录 · RepoSentinel";
  }, []);
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
      applyZodErrors(parsed.error.issues, setError, ["username", "password"]);
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
        <AuthCardHeader
          titleId="login-title"
          eyebrow="仓库值守台"
          title="RepoSentinel"
          subtitle="自托管的 GitHub 仓库监控"
          productMark={<ShieldCheck size={22} strokeWidth={1.8} />}
        />

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
          <AuthField
            id="login-username"
            label="用户名"
            icon={<UserRound aria-hidden="true" size={17} />}
            autoComplete="username"
            autoFocus
            error={errors.username?.message}
            registration={register("username")}
          />

          <AuthField
            id="login-password"
            label="密码"
            icon={<KeyRound aria-hidden="true" size={17} />}
            type="password"
            autoComplete="current-password"
            error={errors.password?.message}
            registration={register("password")}
          />

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
