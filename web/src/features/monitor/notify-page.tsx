import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "../../lib/api/client";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { upsertChannel, testChannel, deleteChannel, toggleChannel } from "./api";
import { SUBSCRIBABLE_KINDS, subscriptionSummary, uiCheckedKinds } from "./notify-subscription";

interface ChannelRow {
  id: string;
  channel_type: string;
  name: string;
  enabled: boolean;
  target: string;
  secret_configured: boolean;
  // 订阅的实时通知类型；null 表示全部订阅。
  event_kinds: string[] | null;
  digest_enabled: boolean;
  updated_at?: string;
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
  const [error, setError] = useState("");

  // 订阅配置：默认全订阅 + 收每日汇总；渠道加载后按服务端数据回填一次。
  const allKinds = () => SUBSCRIBABLE_KINDS.map((k) => k.value);
  const [telegramKinds, setTelegramKinds] = useState<string[]>(allKinds);
  const [telegramDigest, setTelegramDigest] = useState(true);
  const [httpKinds, setHttpKinds] = useState<string[]>(allKinds);
  const [httpDigest, setHttpDigest] = useState(true);

  const telegramCh = channels.data?.items.find((ch) => ch.channel_type === "telegram");
  const httpCh = channels.data?.items.find((ch) => ch.channel_type === "http_webhook");
  const telegramChId = telegramCh?.id;
  const httpChId = httpCh?.id;
  useEffect(() => {
    if (telegramCh) {
      setTelegramKinds(uiCheckedKinds(telegramCh.event_kinds));
      setTelegramDigest(telegramCh.digest_enabled);
    }
    // 仅在渠道记录就绪时回填一次，避免覆盖用户正在编辑的勾选。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [telegramChId]);
  useEffect(() => {
    if (httpCh) {
      setHttpKinds(uiCheckedKinds(httpCh.event_kinds));
      setHttpDigest(httpCh.digest_enabled);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [httpChId]);

  const invalidateAll = async () => {
    await queryClient.invalidateQueries({ queryKey: ["channels"] });
    await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  };

  const saveTelegram = useMutation({
    mutationFn: () =>
      upsertChannel("telegram", {
        name: "Telegram",
        enabled: true,
        target: telegramTarget,
        secret: telegramSecret || undefined,
        event_kinds: telegramKinds,
        digest_enabled: telegramDigest,
      }),
    onSuccess: async () => {
      setMessage("Telegram 渠道已保存。");
      setError("");
      setTelegramSecret("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const saveHTTP = useMutation({
    mutationFn: () =>
      upsertChannel("http_webhook", {
        name: "HTTP Webhook",
        enabled: true,
        target: httpTarget,
        secret: httpSecret || undefined,
        event_kinds: httpKinds,
        digest_enabled: httpDigest,
      }),
    onSuccess: async () => {
      setMessage("HTTP Webhook 渠道已保存。");
      setError("");
      setHttpSecret("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const testMut = useMutation({
    mutationFn: (type: "telegram" | "http_webhook") => testChannel(type),
    onSuccess: () => {
      setMessage("测试通知已发送，请检查您的通知渠道。");
      setError("");
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const deleteMut = useMutation({
    mutationFn: (type: "telegram" | "http_webhook") => deleteChannel(type),
    onSuccess: async () => {
      setMessage("渠道已删除。");
      setError("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const toggleMut = useMutation({
    mutationFn: ({ type, enabled }: { type: "telegram" | "http_webhook"; enabled: boolean }) =>
      toggleChannel(type, enabled),
    onSuccess: async () => {
      setMessage("渠道状态已更新。");
      setError("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const handleDelete = (type: "telegram" | "http_webhook") => {
    if (window.confirm(`确定要删除 ${type === "telegram" ? "Telegram" : "HTTP Webhook"} 渠道吗？`)) {
      deleteMut.mutate(type);
    }
  };

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">通知</p>
          <h1>配置投递渠道</h1>
          <p>每种渠道最多启用 1 个实例，通知按各渠道订阅的类型投递，可单独开关每日汇总。</p>
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
      {error ? <ErrorAlert title="操作失败" message={error} /> : null}

      {/* 渠道状态概览 */}
      <section className="onboarding-card">
        <h2>当前渠道</h2>
        <ul className="event-list">
          {(channels.data?.items ?? []).map((ch) => (
            <li key={ch.id} className="channel-row">
              <span className={`event-kind ${ch.enabled ? "status-sent" : "status-dead"}`}>
                {ch.channel_type === "telegram" ? "📱 Telegram" : "🌐 HTTP Webhook"}
              </span>
              <strong>{ch.enabled ? "已启用" : "已禁用"}</strong>
              <span className="channel-target">{ch.target || "（无目标）"}</span>
              <span className="muted">{ch.secret_configured ? "密钥已配置" : "无密钥"}</span>
              <span className="muted">{subscriptionSummary(ch.event_kinds, ch.digest_enabled)}</span>
              <div className="channel-actions">
                <button
                  className="quiet-button"
                  type="button"
                  onClick={() =>
                    toggleMut.mutate({ type: ch.channel_type as "telegram" | "http_webhook", enabled: !ch.enabled })
                  }
                  disabled={toggleMut.isPending}
                >
                  {ch.enabled ? "禁用" : "启用"}
                </button>
                <button
                  className="quiet-button"
                  type="button"
                  onClick={() => testMut.mutate(ch.channel_type as "telegram" | "http_webhook")}
                  disabled={testMut.isPending || !ch.enabled}
                >
                  {testMut.isPending ? "发送中…" : "测试"}
                </button>
                <button
                  className="quiet-button quiet-button--danger"
                  type="button"
                  onClick={() => handleDelete(ch.channel_type as "telegram" | "http_webhook")}
                  disabled={deleteMut.isPending}
                >
                  删除
                </button>
              </div>
            </li>
          ))}
          {(channels.data?.items ?? []).length === 0 && (
            <li className="muted">尚未配置任何渠道，请在下方添加。</li>
          )}
        </ul>
      </section>

      {/* Telegram 配置 */}
      <section className="onboarding-card channel-form">
        <h2>Telegram</h2>
        <p className="field-hint">
          向 Bot 发消息的目标。Token 也可通过环境变量 <code>REPOSENTINEL_TELEGRAM_TOKEN</code> 在启动时初始化；页面保存会写入数据库并加密存储。
        </p>
        {telegramCh && (
          <p className="field-hint">
            当前状态：<strong>{telegramCh.enabled ? "已启用" : "已禁用"}</strong>
            {telegramCh.target && <> · Chat ID: <code>{telegramCh.target}</code></>}
          </p>
        )}
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
        <fieldset className="field--plain">
          <span>订阅通知类型</span>
          <div className="channel-kinds">
            {SUBSCRIBABLE_KINDS.map((k) => (
              <label key={k.value} className="channel-kinds__item">
                <input
                  type="checkbox"
                  checked={telegramKinds.includes(k.value)}
                  onChange={(e) =>
                    setTelegramKinds(
                      e.target.checked ? [...telegramKinds, k.value] : telegramKinds.filter((v) => v !== k.value),
                    )
                  }
                />
                {k.label}
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field--plain">
          <label className="channel-kinds__item">
            <input type="checkbox" checked={telegramDigest} onChange={(e) => setTelegramDigest(e.target.checked)} />
            接收每日汇总
          </label>
        </div>
        <div className="channel-form__buttons">
          <button className="primary-button primary-button--inline" type="button" disabled={saveTelegram.isPending} onClick={() => saveTelegram.mutate()}>
            {saveTelegram.isPending ? "保存中…" : "保存 Telegram"}
          </button>
          {telegramCh?.enabled && (
            <button className="secondary-button" type="button" disabled={testMut.isPending} onClick={() => testMut.mutate("telegram")}>
              {testMut.isPending ? "发送中…" : "🔔 发送测试通知"}
            </button>
          )}
        </div>
      </section>

      {/* HTTP Webhook 配置 */}
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
        {httpCh && (
          <p className="field-hint">
            当前状态：<strong>{httpCh.enabled ? "已启用" : "已禁用"}</strong>
            {httpCh.target && <> · URL: <code>{httpCh.target}</code></>}
          </p>
        )}
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
        <fieldset className="field--plain">
          <span>订阅通知类型</span>
          <div className="channel-kinds">
            {SUBSCRIBABLE_KINDS.map((k) => (
              <label key={k.value} className="channel-kinds__item">
                <input
                  type="checkbox"
                  checked={httpKinds.includes(k.value)}
                  onChange={(e) =>
                    setHttpKinds(e.target.checked ? [...httpKinds, k.value] : httpKinds.filter((v) => v !== k.value))
                  }
                />
                {k.label}
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field--plain">
          <label className="channel-kinds__item">
            <input type="checkbox" checked={httpDigest} onChange={(e) => setHttpDigest(e.target.checked)} />
            接收每日汇总
          </label>
        </div>
        <div className="channel-form__buttons">
          <button className="primary-button primary-button--inline" type="button" disabled={saveHTTP.isPending} onClick={() => saveHTTP.mutate()}>
            {saveHTTP.isPending ? "保存中…" : "保存 HTTP Webhook"}
          </button>
          {httpCh?.enabled && (
            <button className="secondary-button" type="button" disabled={testMut.isPending} onClick={() => testMut.mutate("http_webhook")}>
              {testMut.isPending ? "发送中…" : "🔔 发送测试通知"}
            </button>
          )}
        </div>
      </section>
    </>
  );
}
