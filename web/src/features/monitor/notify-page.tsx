import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { ConfirmDialog } from "../../components/confirm-dialog";
import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { toApiError } from "../../lib/api/errors";
import { channelLabel } from "../../lib/format";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import { useCopyFeedback } from "../../lib/use-copy-feedback";
import {
  channelsQueryOptions,
  settingsQueryOptions,
  upsertChannel,
  testChannel,
  deleteChannel,
  toggleChannel,
  type ChannelType,
  type NotificationChannelRow,
  type SystemSettings,
} from "./api";
import { SUBSCRIBABLE_KINDS, subscriptionSummary, uiCheckedKinds } from "./notify-subscription";

function kindGloballyEnabled(settings: SystemSettings | undefined, featureKey: (typeof SUBSCRIBABLE_KINDS)[number]["featureKey"]) {
  return settings?.[featureKey] !== false;
}

// ---- ChannelForm：Telegram 与 HTTP Webhook 共用的渠道配置表单 ----
// 渠道差异（文案、占位符、端点类型、校验提示）通过 props 参数化，
// 表单结构、DOM class 与既有样式保持一致。

interface ChannelFormProps {
  // 渠道标识：决定 upsert/test 端点类型。
  type: ChannelType;
  // 区块标题，同时作为 upsert 时的渠道名称。
  title: string;
  // 区块顶部说明文案。
  hint: ReactNode;
  // 当前服务端渠道记录；未配置时为空。
  channel?: NotificationChannelRow;
  // 全局设置，用于功能模块关闭时灰显订阅勾选。
  settings?: SystemSettings;
  // 目标字段文案与占位符。
  targetLabel: string;
  targetPlaceholder: string;
  // 密钥字段文案与占位符（两渠道均支持密钥；showSecret 预留给无密钥渠道）。
  showSecret?: boolean;
  secretLabel: string;
  secretPlaceholder: string;
  // 状态行中目标值的显示名（如 "Chat ID" / "URL"）。
  statusTargetLabel: string;
  // 新建且目标为空时的校验提示。
  emptyTargetError: string;
  // 保存成功的提示文案。
  successMessage: string;
  // 当前渠道的测试请求状态与触发回调。
  testPending: boolean;
  onTest: () => void;
  // 全局提示：成功写入消息并清空旧错误；失败只写错误。
  onNotice: (message: string) => void;
  onFail: (message: string) => void;
  // 保存成功后刷新渠道与仪表盘查询。
  onSaved: () => Promise<void>;
}

function ChannelForm({
  type,
  title,
  hint,
  channel,
  settings,
  targetLabel,
  targetPlaceholder,
  showSecret = true,
  secretLabel,
  secretPlaceholder,
  statusTargetLabel,
  emptyTargetError,
  successMessage,
  testPending,
  onTest,
  onNotice,
  onFail,
  onSaved,
}: ChannelFormProps) {
  // 订阅配置：默认全订阅 + 收定期汇总；渠道加载后按服务端数据回填一次。
  const [target, setTarget] = useState("");
  const [secret, setSecret] = useState("");
  const [kinds, setKinds] = useState<string[]>(() => SUBSCRIBABLE_KINDS.map((k) => k.value));
  const [digest, setDigest] = useState(true);

  // 仅在渠道记录就绪时回填一次（按实例 ID 记账），避免覆盖用户正在编辑的勾选；
  // 依赖完整（channel 与 setter 均入数组），不依赖禁用 exhaustive-deps。
  // 密钥不回填：服务端不返回明文，保存成功后置空等待重新输入。
  const prefilledIdRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!channel || prefilledIdRef.current === channel.id) {
      return;
    }
    prefilledIdRef.current = channel.id;
    setKinds(uiCheckedKinds(channel.event_kinds));
    setDigest(channel.digest_enabled);
    // 预填目标值，避免「只改订阅」时表单为空误清空。
    if (channel.target) setTarget(channel.target);
  }, [channel, setKinds, setDigest, setTarget]);

  const saveMut = useMutation({
    mutationFn: () => {
      const trimmedTarget = target.trim();
      // 已有渠道时允许留空 target（后端保留原值）；新建必须填写。
      if (!channel && !trimmedTarget) {
        throw new Error(emptyTargetError);
      }
      return upsertChannel(type, {
        name: title,
        enabled: true,
        target: trimmedTarget,
        secret: secret.trim() || undefined,
        event_kinds: kinds,
        digest_enabled: digest,
      });
    },
    onSuccess: async () => {
      onNotice(successMessage);
      setSecret("");
      await onSaved();
    },
    onError: (err) => {
      onFail(toApiError(err).message || (err instanceof Error ? err.message : "保存失败"));
    },
  });

  // 目标字段失焦即时校验：与后端保存校验一致，提前反馈格式问题。
  const [targetHint, setTargetHint] = useState("");
  const validateTarget = (value: string) => {
    const v = value.trim();
    if (v === "") {
      setTargetHint("");
      return;
    }
    if (type === "telegram") {
      if (!/^-?\d+$/.test(v)) {
        setTargetHint("Chat ID 应为数字；群组通常以 -100 开头。");
        return;
      }
    } else {
      try {
        const u = new URL(v);
        if (u.protocol !== "https:") {
          setTargetHint("出站投递仅允许 HTTPS URL。");
          return;
        }
      } catch {
        setTargetHint("URL 格式无法解析，请检查是否完整。");
        return;
      }
    }
    setTargetHint("");
  };

  function renderKindChecks(kinds: string[], setKinds: (next: string[]) => void) {
    return (
      <div className="channel-kinds">
        <div className="channel-kinds__toolbar">
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            onClick={() =>
              // 全选仅勾选全局开关未关闭的类型：关闭类型提交无意义，且后端会忽略。
              setKinds(SUBSCRIBABLE_KINDS.filter((k) => kindGloballyEnabled(settings, k.featureKey)).map((k) => k.value))
            }
          >
            全选
          </button>
          <button className="quiet-button quiet-button--compact" type="button" onClick={() => setKinds([])}>
            清空
          </button>
          <span className="muted channel-kinds__count">
            已选 {kinds.length} / {SUBSCRIBABLE_KINDS.length}
          </span>
        </div>
        {SUBSCRIBABLE_KINDS.map((k) => {
          const globalOn = kindGloballyEnabled(settings, k.featureKey);
          return (
            <label
              key={k.value}
              className={`channel-kinds__item${!globalOn ? " channel-kinds__item--disabled" : ""}`}
              title={!globalOn ? "全局功能模块已关闭，此类型不会采集与通知" : undefined}
            >
              <input
                type="checkbox"
                checked={kinds.includes(k.value)}
                disabled={!globalOn}
                onChange={(e) =>
                  setKinds(e.target.checked ? [...kinds, k.value] : kinds.filter((v) => v !== k.value))
                }
              />
              {k.label}
              {!globalOn ? <span className="muted"> · 全局关闭</span> : null}
            </label>
          );
        })}
      </div>
    );
  }

  return (
    <section className="onboarding-card channel-form">
      <h2>{title}</h2>
      {hint}
      {channel && (
        <p className="field-hint">
          当前状态：<strong>{channel.enabled ? "已启用" : "已禁用"}</strong>
          {channel.target && (
            <>
              {" "}
              · {statusTargetLabel}: <code>{channel.target}</code>
            </>
          )}
        </p>
      )}
      <label className="field--plain">
        <span>{targetLabel}{channel ? "（留空保留原值）" : ""}</span>
        <input
          size={32}
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          onBlur={(e) => validateTarget(e.target.value)}
          placeholder={targetPlaceholder}
        />
      </label>
      {targetHint ? <p className="field-hint" role="status">{targetHint}</p> : null}
      {showSecret ? (
        <label className="field--plain">
          <span>{secretLabel}</span>
          <input
            size={32}
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder={secretPlaceholder}
            autoComplete="off"
          />
        </label>
      ) : null}
      <fieldset className="field--plain">
        <legend>订阅通知类型</legend>
        {renderKindChecks(kinds, setKinds)}
      </fieldset>
      <div className="field--plain">
        <label className="channel-kinds__item">
          <input type="checkbox" checked={digest} onChange={(e) => setDigest(e.target.checked)} />
          接收定期汇总（日/周/月）
        </label>
      </div>
      <div className="channel-form__buttons">
        <button
          className="primary-button primary-button--inline"
          type="button"
          disabled={saveMut.isPending}
          onClick={() => saveMut.mutate()}
        >
          {saveMut.isPending ? "保存中…" : `保存 ${title}`}
        </button>
        {channel?.enabled && (
          <button
            className="secondary-button"
            type="button"
            disabled={testPending}
            onClick={onTest}
          >
            {testPending ? "发送中…" : "🔔 发送测试通知"}
          </button>
        )}
      </div>
    </section>
  );
}

export function NotifyPage() {
  const queryClient = useQueryClient();
  const channels = useQuery(channelsQueryOptions);
  const settings = useQuery(settingsQueryOptions);

  const [message, setMessage] = useAutoDismiss();
  const [error, setError] = useState("");

  const telegramCh = channels.data?.items.find((ch) => ch.channel_type === "telegram");
  const httpCh = channels.data?.items.find((ch) => ch.channel_type === "http_webhook");
  const digestTime = String(settings.data?.["digest.local_time"] ?? "09:00");
  const digestTz = String(settings.data?.["admin.timezone"] ?? "UTC");

  const invalidateAll = async () => {
    await queryClient.invalidateQueries({ queryKey: ["channels"] });
    await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  };

  const testMut = useMutation({
    mutationFn: (type: ChannelType) => testChannel(type),
    onSuccess: (_data, type) => {
      setMessage(`${channelLabel(type)} 测试通知已发送，请检查您的通知渠道。`);
      setError("");
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const deleteMut = useMutation({
    mutationFn: (type: ChannelType) => deleteChannel(type),
    onSuccess: async (_data, type) => {
      setMessage(`${channelLabel(type)} 渠道已删除。`);
      setError("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  const toggleMut = useMutation({
    mutationFn: ({ type, enabled }: { type: ChannelType; enabled: boolean }) =>
      toggleChannel(type, enabled),
    onSuccess: async (_data, { type, enabled }) => {
      setMessage(`${channelLabel(type)} 渠道已${enabled ? "启用" : "禁用"}。`);
      setError("");
      await invalidateAll();
    },
    onError: (err) => {
      setError(toApiError(err).message);
    },
  });

  // 删除渠道确认：样式化对话框（原生 confirm 与整体 UI 割裂）。
  const [deleteTarget, setDeleteTarget] = useState<ChannelType | null>(null);
  const handleDelete = (type: ChannelType) => {
    setDeleteTarget(type);
  };

  const testingType = testMut.isPending ? testMut.variables : undefined;
  // 复制渠道目标（Chat ID / URL）：配置排查时便于粘贴；失败给出短暂反馈，不打断使用。
  const { isCopied: copiedTarget, copy: copyTarget } = useCopyFeedback();

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">通知</p>
          <h1>配置投递渠道</h1>
          <p>每种渠道最多启用 1 个实例。订阅类型决定实时推送；定期汇总时刻在「设置」配置。</p>
        </div>
      </section>

      {message ? <p className="success-banner" role="status">{message}</p> : null}
      {error ? <ErrorAlert title="操作失败" message={error} /> : null}

      <section className="onboarding-card" aria-labelledby="notify-digest-title">
        <h2 id="notify-digest-title">定期汇总调度</h2>
        <p className="field-hint">
          当前汇总时刻：<strong>{digestTime}</strong>（时区 <code>{digestTz}</code>）。
          渠道勾选「接收定期汇总」后，每日到点合并过去 24 小时事件发送，周报/月报在「设置 → 运行偏好」启用。
          修改时刻请到{" "}
          <Link to="/settings">设置 → 运行偏好</Link>。
        </p>
      </section>

      {/* 渠道状态概览：查询失败仅在本区块提示，下方表单仍可用。 */}
      <section className="onboarding-card">
        <h2>当前渠道</h2>
        <QueryGate
          query={channels}
          errorTitle="无法加载渠道"
          isEmpty={(channels.data?.items ?? []).length === 0}
          emptyState={<EmptyState title="尚未配置渠道" description="请在下方添加 Telegram 或 HTTP Webhook 渠道。" />}
        >
          <ul className="event-list">
            {(channels.data?.items ?? []).map((ch) => (
              <li key={ch.id} className="channel-row">
                <span className={`event-kind ${ch.enabled ? "status-sent" : "status-dead"}`}>
                  {ch.channel_type === "telegram" ? "📱 Telegram" : "🌐 HTTP Webhook"}
                </span>
                <strong>{ch.enabled ? "已启用" : "已禁用"}</strong>
                <span className="channel-target">{ch.target || "（无目标）"}</span>
                {ch.target ? (
                  <button
                    type="button"
                    className="quiet-button quiet-button--compact"
                    aria-label={`复制目标：${ch.channel_type === "telegram" ? "Chat ID" : "URL"}`}
                    onClick={() => void copyTarget(ch.channel_type, ch.target)}
                  >
                    {copiedTarget(ch.channel_type) ? "已复制" : "复制"}
                  </button>
                ) : null}
                <span className="muted">{ch.secret_configured ? "密钥已配置" : "无密钥"}</span>
                <span className="muted">{subscriptionSummary(ch.event_kinds, ch.digest_enabled)}</span>
                <div className="channel-actions">
                  <button
                    className="quiet-button"
                    type="button"
                    onClick={() =>
                      toggleMut.mutate({ type: ch.channel_type, enabled: !ch.enabled })
                    }
                    disabled={toggleMut.isPending && toggleMut.variables?.type === ch.channel_type}
                  >
                    {ch.enabled ? "禁用" : "启用"}
                  </button>
                  <button
                    className="quiet-button"
                    type="button"
                    onClick={() => testMut.mutate(ch.channel_type)}
                    disabled={testingType === ch.channel_type || !ch.enabled}
                  >
                    {testingType === ch.channel_type ? "发送中…" : "测试"}
                  </button>
                  <button
                    className="quiet-button quiet-button--danger"
                    type="button"
                    onClick={() => handleDelete(ch.channel_type)}
                    disabled={deleteMut.isPending && deleteMut.variables === ch.channel_type}
                  >
                    删除
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </QueryGate>
      </section>

      {/* Telegram 配置 */}
      <ChannelForm
        type="telegram"
        title="Telegram"
        hint={
          <p className="field-hint">
            1）与 Bot 私聊或把 Bot 拉进群；2）Chat ID 可用 <code>@userinfobot</code> 或群组 API 获取（群 ID 常为负数）；
            3）Token 也可通过环境变量 <code>REPOSENTINEL_TELEGRAM_TOKEN</code> 初始化。页面保存会加密入库。
          </p>
        }
        channel={telegramCh}
        settings={settings.data}
        targetLabel="Chat ID"
        targetPlaceholder="-100..."
        secretLabel="Bot Token（留空则保留原密钥）"
        secretPlaceholder="123456:ABC..."
        statusTargetLabel="Chat ID"
        emptyTargetError="请填写 Telegram Chat ID。"
        successMessage="Telegram 渠道已保存。"
        testPending={testingType === "telegram"}
        onTest={() => testMut.mutate("telegram")}
        onNotice={(msg) => { setMessage(msg); setError(""); }}
        onFail={setError}
        onSaved={invalidateAll}
      />

      {/* HTTP Webhook 配置 */}
      <ChannelForm
        type="http_webhook"
        title="HTTP Webhook"
        hint={
          <>
            <p className="field-hint">
              这是<strong>出站通知</strong>：RepoSentinel 把告警 POST 到你的 HTTPS 地址。签名 Secret 可选，配置后会在请求头附带{" "}
              <code>X-GitHub-Monitor-Signature-256</code>，供接收端校验。
            </p>
            <p className="field-hint">
              与 <code>REPOSENTINEL_GITHUB_WEBHOOK_SECRET</code> <strong>不是同一个</strong>：后者是 GitHub → 本服务的入站 Webhook
              校验；本页 Secret 对应 <code>REPOSENTINEL_HTTP_WEBHOOK_SECRET</code>（启动时种子）或此处手填。
            </p>
          </>
        }
        channel={httpCh}
        settings={settings.data}
        targetLabel="HTTPS URL"
        targetPlaceholder="https://hooks.example.com/notify"
        secretLabel="签名 Secret（可选）"
        secretPlaceholder="留空则不签名；已配置时留空保留原值"
        statusTargetLabel="URL"
        emptyTargetError="请填写 HTTPS URL。"
        successMessage="HTTP Webhook 渠道已保存。"
        testPending={testingType === "http_webhook"}
        onTest={() => testMut.mutate("http_webhook")}
        onNotice={(msg) => { setMessage(msg); setError(""); }}
        onFail={setError}
        onSaved={invalidateAll}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        title="删除渠道"
        message={
          deleteTarget
            ? `确定要删除 ${channelLabel(deleteTarget)} 渠道吗？删除后该渠道的待投递记录将无法继续发送。`
            : ""
        }
        confirmLabel="删除"
        danger
        busy={deleteMut.isPending}
        busyLabel="删除中…"
        onConfirm={() => {
          const target = deleteTarget;
          setDeleteTarget(null);
          if (target) deleteMut.mutate(target);
        }}
        onCancel={() => setDeleteTarget(null)}
      />
    </>
  );
}
