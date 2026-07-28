import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "../../lib/api/client";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { upsertChannel } from "./api";

interface ChannelRow {
  id: string;
  channel_type: string;
  name: string;
  enabled: boolean;
  target: string;
  secret_configured: boolean;
}

export function NotifyPage() {
  const queryClient = useQueryClient();
  const channels = useQuery({
    queryKey: ["channels"],
    queryFn: () => apiRequest<{ items: ChannelRow[] }>("/api/v1/notifications/channels"),
  });

  const [telegramTarget, setTelegramTarget] = useState("");
  const [telegramSecret, setTelegramSecret] = useState("");
  const [httpTarget, setHttpTarget] = useState("");
  const [httpSecret, setHttpSecret] = useState("");
  const [message, setMessage] = useState("");

  const saveTelegram = useMutation({
    mutationFn: () =>
      upsertChannel("telegram", {
        name: "Telegram",
        enabled: true,
        target: telegramTarget,
        secret: telegramSecret || undefined,
      }),
    onSuccess: async () => {
      setMessage("Telegram 渠道已保存。");
      setTelegramSecret("");
      await queryClient.invalidateQueries({ queryKey: ["channels"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const saveHTTP = useMutation({
    mutationFn: () =>
      upsertChannel("http_webhook", {
        name: "HTTP Webhook",
        enabled: true,
        target: httpTarget,
        secret: httpSecret || undefined,
      }),
    onSuccess: async () => {
      setMessage("HTTP Webhook 渠道已保存。");
      setHttpSecret("");
      await queryClient.invalidateQueries({ queryKey: ["channels"] });
    },
  });

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">通知</p>
          <h1>配置投递渠道</h1>
          <p>每种渠道最多启用 1 个实例，全量事件发往已启用渠道。</p>
        </div>
      </section>

      {channels.isError ? (
        <ErrorAlert
          title="无法加载渠道"
          message={toApiError(channels.error).message}
          errorCode={toApiError(channels.error).errorCode}
        />
      ) : null}
      {message ? <p className="success-banner" role="status">{message}</p> : null}

      <section className="onboarding-card">
        <h2>当前渠道</h2>
        <ul className="event-list">
          {(channels.data?.items ?? []).map((ch) => (
            <li key={ch.id}>
              <span className="event-kind">{ch.channel_type}</span>
              <strong>{ch.enabled ? "已启用" : "已禁用"}</strong>
              <span>{ch.target || "（无目标）"}</span>
              <span className="muted">{ch.secret_configured ? "密钥已配置" : "无密钥"}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="onboarding-card channel-form">
        <h2>Telegram</h2>
        <p className="field-hint">
          向 Bot 发消息的目标。Token 也可通过环境变量 <code>REPOSENTINEL_TELEGRAM_TOKEN</code> 在启动时初始化；页面保存会写入数据库并加密存储。
        </p>
        <label className="field--plain">
          <span>Chat ID</span>
          <input value={telegramTarget} onChange={(e) => setTelegramTarget(e.target.value)} placeholder="-100..." />
        </label>
        <label className="field--plain">
          <span>Bot Token（留空则保留原密钥）</span>
          <input
            type="password"
            value={telegramSecret}
            onChange={(e) => setTelegramSecret(e.target.value)}
            placeholder="123456:ABC..."
            autoComplete="off"
          />
        </label>
        <button className="primary-button primary-button--inline" type="button" disabled={saveTelegram.isPending} onClick={() => saveTelegram.mutate()}>
          {saveTelegram.isPending ? "保存中…" : "保存 Telegram"}
        </button>
        {saveTelegram.isError ? (
          <ErrorAlert
            title="保存失败"
            message={toApiError(saveTelegram.error).message}
            errorCode={toApiError(saveTelegram.error).errorCode}
          />
        ) : null}
      </section>

      <section className="onboarding-card channel-form">
        <h2>HTTP Webhook</h2>
        <p className="field-hint">
          这是<strong>出站通知</strong>：RepoSentinel 把告警 POST 到你的 HTTPS 地址。签名 Secret 可选，配置后会在请求头附带{" "}
          <code>X-GitHub-Monitor-Signature-256</code>，供接收端校验。
        </p>
        <p className="field-hint">
          与 <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code> <strong>不是同一个</strong>：后者是 GitHub → 本服务的入站 Webhook 校验；本页 Secret 对应{" "}
          <code>REPOSENTINEL_HTTP_WEBHOOK_SECRET</code>（启动时种子）或此处手填，二选一即可，不必重复配置。
        </p>
        <label className="field--plain">
          <span>HTTPS URL</span>
          <input value={httpTarget} onChange={(e) => setHttpTarget(e.target.value)} placeholder="https://hooks.example.com/notify" />
        </label>
        <label className="field--plain">
          <span>签名 Secret（可选）</span>
          <input
            type="password"
            value={httpSecret}
            onChange={(e) => setHttpSecret(e.target.value)}
            placeholder="留空则不签名；已配置时留空保留原值"
            autoComplete="off"
          />
        </label>
        <button className="primary-button primary-button--inline" type="button" disabled={saveHTTP.isPending} onClick={() => saveHTTP.mutate()}>
          {saveHTTP.isPending ? "保存中…" : "保存 HTTP Webhook"}
        </button>
        {saveHTTP.isError ? (
          <ErrorAlert
            title="保存失败"
            message={toApiError(saveHTTP.error).message}
            errorCode={toApiError(saveHTTP.error).errorCode}
          />
        ) : null}
      </section>
    </>
  );
}
