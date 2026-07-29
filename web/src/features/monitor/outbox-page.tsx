import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiRequest } from "../../lib/api/client";
import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { formatRelativeTime } from "../../lib/format";
import { retryOutbox, type OutboxItem, type Page } from "./api";

export function OutboxPage() {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useState<string>("");

  const outbox = useQuery({
    queryKey: ["outbox", statusFilter],
    queryFn: () =>
      apiRequest<Page<OutboxItem>>(
        `/api/v1/notifications/outbox?per_page=50${statusFilter ? `&status=${statusFilter}` : ""}`,
      ),
    refetchInterval: 15_000,
  });

  const retry = useMutation({
    mutationFn: (id: string) => retryOutbox(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const items = outbox.data?.items ?? [];
  const filters = [
    { label: "全部", value: "" },
    { label: "待发送", value: "pending" },
    { label: "发送中", value: "sending" },
    { label: "已发送", value: "sent" },
    { label: "投递失败", value: "dead" },
  ];

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
        <div className="outbox-filters">
          {filters.map((f) => (
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

        {items.length === 0 ? (
          <EmptyState title="没有投递记录" description={statusFilter ? "当前筛选条件下没有记录。" : "配置通知渠道后，实时通知会进入投递队列。"} />
        ) : (
          <ul className="event-list">
            {items.map((item) => (
              <li key={item.id}>
                <span className={`event-kind status-${item.status}`}>{statusLabel(item.status)}</span>
                <strong>{item.title || item.id}</strong>
                <span className="muted">尝试 {item.attempt_count} 次</span>
                {item.last_error_code && <span className="error-code">{item.last_error_code}</span>}
                {item.created_at && <span className="event-time">{formatRelativeTime(item.created_at)}</span>}
                {item.status === "dead" ? (
                  <button
                    className="quiet-button"
                    type="button"
                    onClick={() => retry.mutate(item.id)}
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
    </>
  );
}

function statusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "待发送";
    case "sending":
      return "发送中";
    case "sent":
      return "已发送";
    case "dead":
      return "投递失败";
    default:
      return status;
  }
}

