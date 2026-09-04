import { KeyRound, ShieldCheck, Smartphone, UserRound } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { GithubIcon } from "../../components/github-icon";
import { ThemeToggle } from "../../components/theme-toggle";
import { apiRequest } from "../../lib/api/client";
import { ApiError, toApiError } from "../../lib/api/errors";
import { AuthCardHeader, AuthField, applyZodErrors } from "./auth-card";
import { login, login2FA, type LoginResult } from "./api";
import { loginSchema, type LoginCredentials } from "./schemas";

export interface LoginPageProps {
  loginAction?: (credentials: LoginCredentials) => void | Promise<unknown>;
  login2FAAction?: (input: { ticket: string; passcode: string }) => void | Promise<unknown>;
  onAuthenticated?: (path: "/") => void;
  version?: string;
}

export function LoginPage({
  loginAction = async (credentials) => {
    return login(credentials);
  },
  login2FAAction = async (input) => {
    await login2FA(input);
  },
  onAuthenticated,
  version,
}: LoginPageProps) {
  const [twoFactorTicket, setTwoFactorTicket] = useState<string>();
  const [passcode, setPasscode] = useState("");
  const [isSubmitting2FA, setIsSubmitting2FA] = useState(false);
  const [requestError, setRequestError] = useState<ApiError>();
  // 页脚版本：优先测试注入值；生产从公开构建信息端点获取，兜底 dev。
  const [buildVersion, setBuildVersion] = useState<string>();
  useEffect(() => {
    // 调用方已注入版本（测试/内部场景）时不再请求。
    if (version) return;
    let active = true;
    void apiRequest<{ version: string }>("/api/v1/system/build-info")
      .then((res) => {
        if (active && res.version) setBuildVersion(res.version);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [version]);
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


  async function submit2FA(e: React.FormEvent) {
    e.preventDefault();
    setRequestError(undefined);
    const cleanPasscode = passcode.trim();
    if (cleanPasscode.length !== 6) {
      setRequestError(
        new ApiError({
          status: 400,
          errorCode: "validation_failed",
          message: "请输入 6 位动态验证码。",
        }),
      );
      return;
    }
    setIsSubmitting2FA(true);
    try {
      await login2FAAction({ ticket: twoFactorTicket!, passcode: cleanPasscode });
      onAuthenticated?.("/");
    } catch (error) {
      const apiErr = toApiError(error);
      setRequestError(apiErr);
      setPasscode("");
      document.getElementById("login-passcode")?.focus();
      if (
        apiErr.message.includes("ticket") ||
        (apiErr.details && typeof apiErr.details === "object" && "reason" in apiErr.details)
      ) {
        setTwoFactorTicket(undefined);
      }
    } finally {
      setIsSubmitting2FA(false);
    }
  }

  const submit = handleSubmit(async (values) => {
    setRequestError(undefined);
    clearErrors();
    const parsed = loginSchema.safeParse(values);
    if (!parsed.success) {
      applyZodErrors(parsed.error.issues, setError, ["username", "password"]);
      return;
    }
    try {
      const res = (await loginAction(parsed.data)) as LoginResult | undefined;
      if (res && "requires_2fa" in res && res.requires_2fa && res.ticket) {
        setTwoFactorTicket(res.ticket);
        setPasscode("");
        setRequestError(undefined);
        return;
      }
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
          rel="noopener noreferrer"
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
            title={
              twoFactorTicket
                ? "动态验证码校验失败"
                : invalidCredentials
                  ? "用户名或密码不正确"
                  : rateLimited
                    ? "尝试过于频繁"
                    : "暂时无法登录"
            }
            message={
              twoFactorTicket
                ? requestError.message || "请检查身份验证器当前展示的 6 位数字（注意时钟是否同步）；若连续输入错误票据将自动作废。"
                : invalidCredentials
                  ? "请检查输入后重试；若凭据已遗失，请使用 CLI 重置密码。"
                  : rateLimited
                    ? "登录尝试过多，请稍候片刻再试。限流按来源 IP 生效，更换用户名不会重置额度。"
                    : requestError.message
            }
            errorCode={requestError.errorCode}
          />
        ) : null}

        {twoFactorTicket ? (
          <form className="auth-form" onSubmit={submit2FA} noValidate>
            <div className="field">
              <label htmlFor="login-passcode" className="field__label">
                <span className="field__icon">
                  <Smartphone aria-hidden="true" size={17} />
                </span>
                <span>动态验证码</span>
              </label>
              <input
                id="login-passcode"
                className="field__input"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]*"
                maxLength={6}
                placeholder="000000"
                autoFocus
                style={{ letterSpacing: "0.25em", textAlign: "center", fontSize: "1.2rem", fontWeight: 600 }}
                value={passcode}
                onChange={(e) => {
                  const val = e.target.value.replace(/\D/g, "").slice(0, 6);
                  setPasscode(val);
                }}
              />
              <p className="field__hint">请输入身份验证器（如 Google Authenticator）中的 6 位动态验证码。</p>
            </div>

            <button className="primary-button" type="submit" disabled={isSubmitting2FA || passcode.trim().length !== 6}>
              {isSubmitting2FA ? "正在验证…" : "验证并登录"}
            </button>

            <button
              type="button"
              className="ghost-button ghost-button--inline"
              onClick={() => {
                setTwoFactorTicket(undefined);
                setRequestError(undefined);
              }}
            >
              返回重新输入密码
            </button>
          </form>
        ) : (
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
        )}

        <footer className="auth-card__footer">
          <a
            href="https://github.com/Silentely/Repo-Sentinel/blob/main/docs/guide/administrator.md#cli-重置密码"
            target="_blank"
            rel="noopener noreferrer"
          >
            使用 CLI 重置密码
          </a>
          <span>版本 {versionText}</span>
        </footer>
      </section>
    </main>
  );
}
