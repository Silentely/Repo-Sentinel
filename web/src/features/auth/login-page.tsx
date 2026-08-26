import { KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { GithubIcon } from "../../components/github-icon";
import { ThemeToggle } from "../../components/theme-toggle";
import { apiRequest } from "../../lib/api/client";
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
  version,
}: LoginPageProps) {
  const [requestError, setRequestError] = useState<ApiError>();
  // 页脚版本：优先测试注入值；生产从公开构建信息端点获取，兜底 dev。
  const [buildVersion, setBuildVersion] = useState<string>();
  useEffect(() => {
    let active = true;
    void apiRequest<{ version: string }>("/api/v1/system/build-info")
      .then((res) => {
        if (active && res.version) setBuildVersion(res.version);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  const versionText = version ?? buildVersion ?? "dev";
  // 登录页不在 RootLayout 内，标签页标题需独立设置。
  useEffect(() => {
    document.title = "登录 · RepoSentinel";
  }, []);
  const {
    register,
    handleSubmit,
    clearErrors,
    setError,
    setValue,
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
      // 重试安全：凭据失败后清空密码并聚焦，避免旧输入残留被误提交。
      setValue("password", "");
      document.getElementById("login-password")?.focus();
    }
  });

  const invalidCredentials = requestError?.errorCode === "invalid_credentials";
  const rateLimited = requestError?.errorCode === "rate_limited";

  // 登录限流后提交按钮被禁用：与后端 Retry-After（12s，见 login_limiter.go）对齐，
  // 到点自动解除限流态，避免用户卡死到刷新页面才能重试。
  useEffect(() => {
    if (!rateLimited) {
      return;
    }
    const timer = window.setTimeout(() => setRequestError(undefined), 12000);
    return () => window.clearTimeout(timer);
  }, [rateLimited]);

  return (
    <main className="auth-shell">
      <div className="auth-shell__theme">
        {/* 仓库入口：登录页右上角 GitHub 图标直达源码/Issue。 */}
        <a
          className="auth-shell__github"
          href="https://github.com/Silentely/Repo-Sentinel"
          target="_blank"
          rel="noreferrer"
          aria-label="GitHub 仓库"
          title="GitHub 仓库"
        >
          <GithubIcon size={16} strokeWidth={1.8} />
        </a>
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
            title={invalidCredentials ? "用户名或密码不正确" : rateLimited ? "尝试过于频繁" : "暂时无法登录"}
            message={
              invalidCredentials
                ? "请检查输入后重试；若凭据已遗失，请使用 CLI 重置密码。"
                : rateLimited
                  ? "登录尝试过多，请稍候片刻再试。限流按来源 IP 生效，更换用户名不会重置额度。"
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

          <button className="primary-button" type="submit" disabled={isSubmitting || rateLimited}>
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
          <span>版本 {versionText}</span>
        </footer>
      </section>
    </main>
  );
}
