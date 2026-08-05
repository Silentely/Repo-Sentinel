import { KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";

import { ErrorAlert } from "../../components/error-alert";
import { ThemeToggle } from "../../components/theme-toggle";
import { ApiError, toApiError } from "../../lib/api/errors";
import { AuthCardHeader, AuthField, applyZodErrors } from "./auth-card";
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
      applyZodErrors(parsed.error.issues, setError, ["username", "password", "confirmPassword"]);
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
        <AuthCardHeader
          titleId="setup-title"
          eyebrow="首次设置 · 1 / 1"
          title="创建唯一管理员"
          subtitle="凭据仅保存在你的 RepoSentinel 实例中。"
          productMark={<ShieldCheck size={22} strokeWidth={1.8} />}
        />

        {requestError ? (
          <ErrorAlert
            title="无法创建管理员"
            message={requestError.message}
            errorCode={requestError.errorCode}
          />
        ) : null}

        <form className="auth-form" onSubmit={submit} noValidate>
          <AuthField
            id="setup-username"
            label="用户名"
            icon={<UserRound aria-hidden="true" size={17} />}
            autoComplete="username"
            error={errors.username?.message}
            registration={register("username")}
          />

          <AuthField
            id="setup-password"
            label="密码"
            icon={<KeyRound aria-hidden="true" size={17} />}
            type="password"
            autoComplete="new-password"
            error={errors.password?.message}
            help="至少 12 个 Unicode 字符。"
            registration={register("password")}
          />

          <AuthField
            id="setup-confirm-password"
            label="确认密码"
            icon={<KeyRound aria-hidden="true" size={17} />}
            type="password"
            autoComplete="new-password"
            error={errors.confirmPassword?.message}
            registration={register("confirmPassword")}
          />

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
