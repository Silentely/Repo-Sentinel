import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, X } from "lucide-react";

import { apiRequest } from "../../lib/api/client";
import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { formatRelativeTime } from "../../lib/format";
import { retryOutbox, type OutboxItem, type Page } from "./api";

const statusFilters = [
  { label: "全部", value: "" },
  { label: "待发送", value: "pending" },
  { label: "发送中", value: "sending" },
  { label: "已发送", value: "sent" },
  { label: "投递失败", value: "dead" },
];

const channelFilters = [
  { label: "全部渠道", value: "" },
  { label: "Telegram", value: "telegram" },
  { label: "HTTP Webhook", value: "http_webhook" },
];

export function OutboxPage() {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [channelFilter, setChannelFilter] = useState<string>("");
  const [selectedItem, setSelectedItem] = useState<OutboxItem | null>(null);

  const outbox = useQuery({
    queryKey: ["outbox", statusFilter, channelFilter],
    queryFn: () => {
      const params = new URLSearchParams({ per_page: "50" });
      if (statusFilter) params.set("status", statusFilter);
      if (channelFilter) params.set("channel_type", channelFilter);
      return apiRequest<Page<OutboxItem>>(`/api/v1/notifications/outbox?${params.toString()}`);
    },
    refetchInterval: 15_000,
  });

  const retry = useMutation({
    mutationFn: (id: string) => retryOutbox(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const retryAllDead = useMutation({
    mutationFn: async () => {
      const deadItems = (outbox.data?.items ?? []).filter((it) => it.status === "dead");
      for (const item of deadItems) {
        await retryOutbox(item.id);
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const items = outbox.data?.items ?? [];
  const deadCount = items.filter((it) => it.status === "dead").length;

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">通知</p>
          <h1>投递记录</h1>
          <p>查看通知投递状态，重试失败的投递消息。</p>
        </div>
      </section>

      {outbox.isError ? (
        <ErrorAlert
          title="无法加载投递记录"
          message={toApiError(outbox.error).message}
          errorCode={toApiError(outbox.error).errorCode}
        />
      ) : null}

      <section className="onboarding-card">
        <div className="outbox-toolbar">
          <div className="outbox-filters">
            {statusFilters.map((f) => (
              <button
                key={f.value}
                className={`quiet-button ${statusFilter === f.value ? "quiet-button--active" : ""}`}
                type="button"
                onClick={() => setStatusFilter(f.value)}
              >
                {f.label}
              </button>
            ))}
          </div>
          <div className="outbox-toolbar__right">
            <label className="repo-filter">
              <span className="sr-only">按渠道筛选</span>
              <select
                value={channelFilter}
                onChange={(e) => setChannelFilter(e.target.value)}
                aria-label="按渠道筛选"
              >
                {channelFilters.map((f) => (
                  <option key={f.value} value={f.value}>{f.label}</option>
                ))}
              </select>
            </label>
            {statusFilter === "dead" && deadCount > 0 && (
              <button
                className="quiet-button quiet-button--primary-ghost"
                type="button"
                onClick={() => retryAllDead.mutate()}
                disabled={retryAllDead.isPending}
              >
                {retryAllDead.isPending ? (
                  <><Loader2 size={14} className="spin" aria-hidden="true" /> 重试中…</>
                ) : (
                  `全部重试 (${deadCount})`
                )}
              </button>
            )}
          </div>
        </div>

        {items.length === 0 ? (
          <EmptyState
            title="没有投递记录"
            description={
              statusFilter || channelFilter
                ? "当前筛选条件下没有记录。"
                : "配置通知渠道后，实时通知会进入投递队列。"
            }
          />
        ) : (
          <ul className="event-list">
            {items.map((item) => (
              <li
                key={item.id}
                className={selectedItem?.id === item.id ? "event-list__item--selected" : ""}
                onClick={() => setSelectedItem(selectedItem?.id === item.id ? null : item)}
              >
                <span className={`event-kind status-${item.status}`}>{statusLabel(item.status)}</span>
                {item.channel_type && (
                  <span className="muted channel-tag">{channelLabel(item.channel_type)}</span>
                )}
                <strong>{item.title || item.id}</strong>
                <span className="muted">尝试 {item.attempt_count} 次</span>
                {item.last_error_code && <span className="error-code">{item.last_error_code}</span>}
                {item.created_at && <span className="event-time">{formatRelativeTime(item.created_at)}</span>}
                {item.status === "dead" ? (
                  <button
                    className="quiet-button"
                    type="button"
                    onClick={(e) => { e.stopPropagation(); retry.mutate(item.id); }}
                    disabled={retry.isPending}
                  >
                    {retry.isPending ? "重试中…" : "重试"}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>

      {selectedItem && (
        <OutboxDetailDrawer item={selectedItem} onClose={() => setSelectedItem(null)} />
      )}
    </>
  );
}

function OutboxDetailDrawer({ item, onClose }: { item: OutboxItem; onClose: () => void }) {
  return (
    <div className="drawer-overlay" onClick={onClose} role="presentation">
      <aside
        className="drawer-panel"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label="投递详情"
      >
        <div className="drawer-panel__header">
          <h2>投递详情</h2>
          <button className="quiet-button" type="button" onClick={onClose} aria-label="关闭">
            <X size={18} aria-hidden="true" />
          </button>
        </div>
        <dl className="drawer-panel__body">
          <div className="drawer-field">
            <dt>标题</dt>
            <dd>{item.title || "（无标题）"}</dd>
          </div>
          <div className="drawer-field">
            <dt>状态</dt>
            <dd><span className={`event-kind status-${item.status}`}>{statusLabel(item.status)}</span></dd>
          </div>
          <div className="drawer-field">
            <dt>渠道类型</dt>
            <dd>{channelLabel(item.channel_type) || "未知"}</dd>
          </div>
          <div className="drawer-field">
            <dt>尝试次数</dt>
            <dd>{item.attempt_count}</dd>
          </div>
          {item.last_error_code && (
            <div className="drawer-field">
              <dt>错误码</dt>
              <dd><code>{item.last_error_code}</code></dd>
            </div>
          )}
          {item.html_url && (
            <div className="drawer-field">
              <dt>关联链接</dt>
              <dd><a href={item.html_url} target="_blank" rel="noreferrer">{item.html_url}</a></dd>
            </div>
          )}
          <div className="drawer-field">
            <dt>创建时间</dt>
            <dd>{item.created_at ? new Date(item.created_at).toLocaleString("zh-CN") : "—"}</dd>
          </div>
          <div className="drawer-field">
            <dt>最后更新</dt>
            <dd>{item.updated_at ? new Date(item.updated_at).toLocaleString("zh-CN") : "—"}</dd>
          </div>
          <div className="drawer-field">
            <dt>投递 ID</dt>
            <dd><code>{item.id}</code></dd>
          </div>
        </dl>
      </aside>
    </div>
  );
}

function statusLabel(status: string): string {
  switch (status) {
    case "pending": return "待发送";
    case "sending": return "发送中";
    case "sent": return "已发送";
    case "dead": return "投递失败";
    default: return status;
  }
}

function channelLabel(channelType: string): string {
  switch (channelType) {
    case "telegram": return "Telegram";
    case "http_webhook": return "HTTP Webhook";
    default: return channelType;
  }
}
