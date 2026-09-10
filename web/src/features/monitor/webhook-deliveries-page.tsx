import { useState, useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, RefreshCw, Eye } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { RelativeTime } from "../../components/relative-time";
import { toApiError } from "../../lib/api/errors";
import { useAutoDismiss } from "../../lib/use-auto-dismiss";
import { useUrlState } from "../../lib/use-url-state";
import { webhookStatusLabel } from "../../lib/format";
import {
  replayWebhookDelivery,
  webhookDeliveriesQueryOptions,
  webhookDeliveryDetailQueryOptions,
  type WebhookDeliveryItem,
} from "./api";
import { ClearFiltersButton, RepoFilterSelect, StateFilterButtons, useActiveRepos } from "./list-shared";

const statusFilters = [
  { label: "全部", value: "" },
  { label: "已接收", value: "accepted" },
  { label: "已处理", value: "processed" },
  { label: "处理失败", value: "failed" },
];

export function WebhookDeliveriesPage() {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useUrlState("status", "");
  const [eventTypeFilter, setEventTypeFilter] = useUrlState("event_type", "");
  const [repoFilter, setRepoFilter] = useUrlState("repo", "");
  const [page, setPage] = useState(1);

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [notice, setNotice] = useAutoDismiss();
  const [error, setError] = useState("");
  const { active: activeRepos } = useActiveRepos();

  const listQuery = useQuery(
    webhookDeliveriesQueryOptions({
      page,
      per_page: 20,
      status: statusFilter,
      event_type: eventTypeFilter,
      repository: repoFilter,
    })
  );

  const detailQuery = useQuery(
    webhookDeliveryDetailQueryOptions(selectedId ?? "")
  );

  const replayMut = useMutation({
    mutationFn: (id: string) => replayWebhookDelivery(id),
    onSuccess: (data) => {
      setNotice(`Webhook 已重放并重新排队处理（ID: ${data.delivery_id}）`);
      setError("");
      void queryClient.invalidateQueries({ queryKey: ["webhook-deliveries"] });
    },
    onError: (err) => {
      setError(toApiError(err).message || "重放失败");
    },
  });

  const items = listQuery.data?.items ?? [];
  const total = listQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / 20));

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && selectedId) {
        setSelectedId(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [selectedId]);

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">系统 / 排障</p>
          <h1>Webhook 投递检查与回放</h1>
          <p>
            查看 GitHub 投递的历史 Webhook 记录、接收状态、排障错误码与原始载荷，支持一键重放触发规则评估。
          </p>
        </div>
      </section>

      {notice ? <p className="success-banner" role="status">{notice}</p> : null}
      {error ? <ErrorAlert title="操作失败" message={error} /> : null}

      <section className="onboarding-card">
        <div className="outbox-toolbar">
          <div className="outbox-filters">
            <StateFilterButtons
              options={statusFilters}
              value={statusFilter}
              onChange={(v) => {
                setStatusFilter(v);
                setPage(1);
              }}
            />
          </div>
          <div className="outbox-toolbar__right">
            <input
              type="search"
              placeholder="按事件筛选 (如 push, issues)"
              value={eventTypeFilter}
              onChange={(e) => {
                setEventTypeFilter(e.target.value);
                setPage(1);
              }}
              style={{ width: "200px" }}
            />
            <RepoFilterSelect
              value={repoFilter}
              onChange={(v) => {
                setRepoFilter(v);
                setPage(1);
              }}
              repos={activeRepos}
            />
            {statusFilter || eventTypeFilter || repoFilter ? (
              <ClearFiltersButton
                onClick={() => {
                  setStatusFilter("");
                  setEventTypeFilter("");
                  setRepoFilter("");
                  setPage(1);
                }}
              />
            ) : null}
            <button
              type="button"
              className="quiet-button"
              onClick={() => void listQuery.refetch()}
              title="刷新列表"
            >
              <RefreshCw size={14} /> 刷新
            </button>
          </div>
        </div>

        <QueryGate
          query={listQuery}
          errorTitle="无法加载 Webhook 投递记录"
          isEmpty={items.length === 0}
          emptyState={
            <EmptyState
              title="暂无 Webhook 投递记录"
              description="GitHub 触发的 Webhook 事件将自动记录在此，供对账与排障使用。"
            />
          }
        >
          <ul className="event-list">
            {items.map((d) => {
              const isDead = d.status === "failed";
              const isAccepted = d.status === "accepted";
              const statusClass = isDead ? "status-dead" : isAccepted ? "status-sending" : "status-sent";

              return (
                <li key={d.id} className="channel-row">
                  <span className={`event-kind ${statusClass}`}>
                    {webhookStatusLabel(d.status)}
                  </span>
                  <div>
                    <strong>{d.event_type}</strong>
                    {d.action ? <span className="muted" style={{ marginLeft: "6px" }}>({d.action})</span> : null}
                    <span className="muted channel-tag" style={{ marginLeft: "8px" }}>
                      {d.repository_full_name || "全局"}
                    </span>
                  </div>
                  <span className="muted" style={{ fontFamily: "monospace", fontSize: "12px" }}>
                    {d.delivery_id}
                  </span>
                  {d.error_code ? (
                    <span className="error-badge" title={d.error_code}>
                      {d.error_code}
                    </span>
                  ) : null}
                  <span className="muted">
                    <RelativeTime date={d.received_at} />
                  </span>

                  <div className="channel-actions">
                    <button
                      type="button"
                      className="quiet-button"
                      onClick={() => setSelectedId(d.id)}
                    >
                      <Eye size={14} /> 检查
                    </button>
                    <button
                      type="button"
                      className="quiet-button quiet-button--primary-ghost"
                      onClick={() => replayMut.mutate(d.id)}
                      disabled={replayMut.isPending && replayMut.variables === d.id}
                      title="重放此 Webhook 触发规则评估"
                    >
                      <Play size={14} />
                      {replayMut.isPending && replayMut.variables === d.id ? "重放中…" : "重放"}
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>

          {totalPages > 1 ? (
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: "16px" }}>
              <span className="muted">共 {total} 条记录，第 {page} / {totalPages} 页</span>
              <div style={{ display: "flex", gap: "8px" }}>
                <button
                  type="button"
                  className="quiet-button"
                  disabled={page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  上一页
                </button>
                <button
                  type="button"
                  className="quiet-button"
                  disabled={page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  下一页
                </button>
              </div>
            </div>
          ) : null}
        </QueryGate>
      </section>

      {/* 检查详情弹窗 / Inspector */}
      {selectedId ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="inspector-title"
          style={{
            position: "fixed",
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: "rgba(0, 0, 0, 0.5)",
            zIndex: 1000,
            display: "flex",
            justifyContent: "center",
            alignItems: "center",
            padding: "20px",
          }}
          onClick={() => setSelectedId(null)}
        >
          <div
            style={{
              backgroundColor: "var(--color-surface, #fff)",
              borderRadius: "8px",
              maxWidth: "800px",
              width: "100%",
              maxHeight: "90vh",
              display: "flex",
              flexDirection: "column",
              boxShadow: "0 10px 25px rgba(0,0,0,0.2)",
              padding: "24px",
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "16px" }}>
              <h2 id="inspector-title" style={{ margin: 0, fontSize: "1.25rem" }}>
                Webhook 载荷检查 (Inspector)
              </h2>
              <button
                type="button"
                className="quiet-button"
                onClick={() => setSelectedId(null)}
              >
                ✕ 关闭
              </button>
            </div>

            {detailQuery.isLoading ? (
              <p className="muted">正在拉取载荷…</p>
            ) : detailQuery.data ? (
              <div style={{ overflowY: "auto", flex: 1, display: "flex", flexDirection: "column", gap: "12px" }}>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "8px", fontSize: "13px" }}>
                  <div><strong>Delivery ID:</strong> <code>{detailQuery.data.delivery.delivery_id}</code></div>
                  <div><strong>事件类型:</strong> <code>{detailQuery.data.delivery.event_type}</code></div>
                  <div><strong>操作 (Action):</strong> {detailQuery.data.delivery.action || "—"}</div>
                  <div><strong>仓库:</strong> {detailQuery.data.delivery.repository_full_name || "—"}</div>
                  <div><strong>状态:</strong> {webhookStatusLabel(detailQuery.data.delivery.status)}</div>
                  <div><strong>接收时间:</strong> {detailQuery.data.delivery.received_at}</div>
                  {detailQuery.data.delivery.error_code ? (
                    <div style={{ gridColumn: "span 2" }}>
                      <strong>错误码:</strong> <span className="error-badge">{detailQuery.data.delivery.error_code}</span>
                    </div>
                  ) : null}
                </div>

                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: "8px" }}>
                  <strong>原始 Payload:</strong>
                  <button
                    type="button"
                    className="primary-button primary-button--inline"
                    onClick={() => replayMut.mutate(detailQuery.data.delivery.id)}
                    disabled={replayMut.isPending}
                  >
                    <Play size={14} /> {replayMut.isPending ? "重放中…" : "一键重放此 Webhook"}
                  </button>
                </div>

                <pre
                  style={{
                    backgroundColor: "var(--color-surface-sunken, #f6f8fa)",
                    padding: "12px",
                    borderRadius: "6px",
                    fontSize: "12px",
                    fontFamily: "monospace",
                    overflowX: "auto",
                    maxHeight: "400px",
                    border: "1px solid var(--color-border, #d0d7de)",
                  }}
                >
                  {detailQuery.data.payload_json
                    ? JSON.stringify(detailQuery.data.payload_json, null, 2)
                    : detailQuery.data.payload_raw || "（空载荷）"}
                </pre>
              </div>
            ) : (
              <p className="muted">未找到记录详情</p>
            )}
          </div>
        </div>
      ) : null}
    </>
  );
}
