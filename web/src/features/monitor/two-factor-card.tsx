import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Shield, ShieldAlert, ShieldCheck } from "lucide-react";

import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import {
  disable2FA,
  enable2FA,
  get2FAStatus,
  setup2FA,
  type TwoFactorSetup,
} from "../auth/api";

export function TwoFactorCard() {
  const queryClient = useQueryClient();
  const [setupData, setSetupData] = useState<TwoFactorSetup | null>(null);
  const [verifyCode, setVerifyCode] = useState("");
  const [disablePassword, setDisablePassword] = useState("");
  const [isDisabling, setIsDisabling] = useState(false);
  const [actionMsg, setActionMsg] = useState("");
  const [actionError, setActionError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const statusQuery = useQuery({
    queryKey: ["admin", "2fa-status"],
    queryFn: () => get2FAStatus(),
  });

  const setupMutation = useMutation({
    mutationFn: () => setup2FA(),
    onSuccess: (data) => {
      setSetupData(data);
      setVerifyCode("");
      setActionError(null);
      setActionMsg("");
    },
    onError: (err) => {
      setActionError(toApiError(err).message || "获取两步验证密钥失败");
    },
  });

  const enableMutation = useMutation({
    mutationFn: () =>
      enable2FA({
        secret: setupData!.secret,
        passcode: verifyCode.trim(),
      }),
    onSuccess: () => {
      setActionMsg("二步验证已成功开启！下次登录时将需要动态验证码。");
      setActionError(null);
      setSetupData(null);
      setVerifyCode("");
      void queryClient.invalidateQueries({ queryKey: ["admin", "2fa-status"] });
    },
    onError: (err) => {
      setActionError(toApiError(err).message || "验证码校验失败，请检查后重试");
    },
  });

  const disableMutation = useMutation({
    mutationFn: () => disable2FA({ current_password: disablePassword }),
    onSuccess: () => {
      setActionMsg("二步验证已关闭。");
      setActionError(null);
      setIsDisabling(false);
      setDisablePassword("");
      void queryClient.invalidateQueries({ queryKey: ["admin", "2fa-status"] });
    },
    onError: (err) => {
      setActionError(toApiError(err).message || "关闭二步验证失败，请检查密码");
    },
  });

  const isEnabled = Boolean(statusQuery.data?.enabled);

  function copySecret() {
    if (!setupData?.secret) return;
    if (navigator.clipboard?.writeText) {
      navigator.clipboard
        .writeText(setupData.secret)
        .then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        })
        .catch(() => {});
    }
  }

  return (
    <section className="onboarding-card channel-form" aria-labelledby="settings-2fa-title">
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <h2 id="settings-2fa-title" style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <Shield size={20} />
          <span>两步验证 (2FA / TOTP)</span>
        </h2>
        {isEnabled ? (
          <span className="badge badge--success" style={{ display: "inline-flex", alignItems: "center", gap: "0.3rem" }}>
            <ShieldCheck size={14} />
            已开启
          </span>
        ) : (
          <span className="badge badge--neutral" style={{ display: "inline-flex", alignItems: "center", gap: "0.3rem" }}>
            <ShieldAlert size={14} />
            未开启
          </span>
        )}
      </div>

      <p className="field-hint">
        基于 RFC 6238 标准的 TOTP 动态口令保护。开启后，登录时不仅需要密码，还需提供验证器 App（如 Google Authenticator、1Password、Bitwarden）生成的 6 位实时验证码。
      </p>

      {actionMsg ? <p className="success-banner" role="status">{actionMsg}</p> : null}
      {actionError ? <ErrorAlert title="操作失败" message={actionError} /> : null}

      {!isEnabled && !setupData && (
        <div>
          <button
            className="primary-button primary-button--inline"
            type="button"
            disabled={setupMutation.isPending}
            onClick={() => setupMutation.mutate()}
          >
            {setupMutation.isPending ? "正在生成密钥…" : "配置并开启两步验证"}
          </button>
        </div>
      )}

      {!isEnabled && setupData && (
        <div style={{ display: "flex", flexDirection: "column", gap: "1rem", marginTop: "0.5rem", padding: "1rem", background: "var(--surface-sunken, rgba(0,0,0,0.03))", borderRadius: "8px" }}>
          <h3 style={{ fontSize: "1rem", fontWeight: 600 }}>第 1 步：在验证器中绑定密钥</h3>
          <p className="field-hint" style={{ margin: 0 }}>
            在身份验证器应用中选择「手动添加账号」，并输入以下密钥；或者在支持协议的应用中点击快捷导入：
          </p>

          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <code style={{ fontSize: "1.1rem", letterSpacing: "2px", fontWeight: "bold", padding: "0.3rem 0.6rem", background: "var(--surface-raised, #fff)", borderRadius: "4px", border: "1px solid var(--border-subtle, #ddd)" }}>
              {setupData.secret}
            </code>
            <button
              type="button"
              className="ghost-button"
              onClick={copySecret}
              title="复制密钥"
              style={{ display: "inline-flex", alignItems: "center", gap: "0.3rem" }}
            >
              {copied ? <Check size={16} /> : <Copy size={16} />}
              {copied ? "已复制" : "复制"}
            </button>
            <a
              href={setupData.otpauth_url}
              className="ghost-button"
              style={{ display: "inline-flex", alignItems: "center" }}
            >
              应用快速唤起绑定
            </a>
          </div>

          <h3 style={{ fontSize: "1rem", fontWeight: 600, marginTop: "0.5rem" }}>第 2 步：输入 6 位动态验证码完成激活</h3>
          <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
            <input
              size={12}
              type="text"
              inputMode="numeric"
              maxLength={6}
              placeholder="6 位数字"
              value={verifyCode}
              onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
            />
            <button
              className="primary-button primary-button--inline"
              type="button"
              disabled={enableMutation.isPending || verifyCode.trim().length !== 6}
              onClick={() => enableMutation.mutate()}
            >
              {enableMutation.isPending ? "正在校验…" : "确认并开启"}
            </button>
            <button
              type="button"
              className="ghost-button"
              onClick={() => {
                setSetupData(null);
                setVerifyCode("");
                setActionError(null);
              }}
            >
              取消
            </button>
          </div>
        </div>
      )}

      {isEnabled && !isDisabling && (
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <p className="field-hint" style={{ margin: 0 }}>
            若丢失验证器无法登录，可在服务器上运行 <code>reposentinel admin reset-2fa</code> 应急重置。
          </p>
          <button
            className="ghost-button danger-button--ghost"
            type="button"
            onClick={() => {
              setIsDisabling(true);
              setActionError(null);
            }}
          >
            关闭两步验证
          </button>
        </div>
      )}

      {isEnabled && isDisabling && (
        <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem", padding: "1rem", background: "var(--surface-sunken, rgba(0,0,0,0.03))", borderRadius: "8px" }}>
          <p style={{ margin: 0, fontWeight: 500 }}>
            关闭两步验证需核验当前管理员密码：
          </p>
          <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
            <input
              size={24}
              type="password"
              placeholder="请输入当前管理员密码"
              value={disablePassword}
              onChange={(e) => setDisablePassword(e.target.value)}
            />
            <button
              className="primary-button primary-button--inline"
              type="button"
              disabled={disableMutation.isPending || !disablePassword.trim()}
              onClick={() => disableMutation.mutate()}
            >
              {disableMutation.isPending ? "正在关闭…" : "确认关闭"}
            </button>
            <button
              type="button"
              className="ghost-button"
              onClick={() => {
                setIsDisabling(false);
                setDisablePassword("");
                setActionError(null);
              }}
            >
              取消
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
