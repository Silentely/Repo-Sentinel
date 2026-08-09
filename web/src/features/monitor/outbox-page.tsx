import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, X } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { RelativeTime } from "../../components/relative-time";
import { apiRequest } from "../../lib/api/client";
import { channelLabel, htmlToPlainText, outboxErrorHint, outboxStatusLabel } from "../../lib/format";
import { useUrlState } from "../../lib/use-url-state";
import { outboxQueryOptions, retryOutbox, type OutboxItem, type Page } from "./api";

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

interface BatchRetryResult {
  succeeded: number;
  failed: number;
}

export function OutboxPage() {
  const queryClient = useQueryClient();
  // 状态筛选同步到 URL（?status=dead 等）：仪表盘「投递失败」跳转时直接进入对应筛选。
  const [statusFilter, setStatusFilter] = useUrlState("status", "");
  const [channelFilter, setChannelFilter] = useState<string>("");
  const [selectedItem, setSelectedItem] = useState<OutboxItem | null>(null);
  const closeDetail = useCallback(() => setSelectedItem(null), []);

  const outbox = useQuery(outboxQueryOptions(statusFilter, channelFilter));

  // 行级忙碌：只让当前重试的记录转圈，避免整列禁用。
  const [retryBusyId, setRetryBusyId] = useState<string | null>(null);
  const [batchRetryResult, setBatchRetryResult] = useState<BatchRetryResult | null>(null);

  const retry = useMutation({
    mutationFn: (id: string) => retryOutbox(id),
    onMutate: (id) => setRetryBusyId(id),
    onSettled: () => setRetryBusyId(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const retryAllDead = useMutation({
    mutationFn: async (): Promise<BatchRetryResult> => {
      const deadItems = (outbox.data?.items ?? []).filter((it) => it.status === "dead");
      let succeeded = 0;
      let failed = 0;
      for (const item of deadItems) {
        try {
          await retryOutbox(item.id);
          succeeded += 1;
        } catch {
          // 单条失败不阻断后续项目，页面只反馈数量，详细错误保留在对应记录中。
          failed += 1;
        }
      }
      return { succeeded, failed };
    },
    onMutate: () => setBatchRetryResult(null),
    onSuccess: async (result) => {
      setBatchRetryResult(result);
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  // 跨页重试全部失败：先分页收集全部 dead 投递 id（不受当前筛选限制），再逐个重新排队。
  // 用于失败记录跨多页时一键恢复，避免用户逐页操作。
  const retryAllDeadAcrossPages = useMutation({
    mutationFn: async (): Promise<BatchRetryResult> => {
      const ids: string[] = [];
      for (let page = 1; ; page++) {
        const params = new URLSearchParams({ per_page: "100", page: String(page), status: "dead" });
        const data = await apiRequest<Page<OutboxItem>>(`/api/v1/notifications/outbox?${params.toString()}`);
        ids.push(...data.items.map((it) => it.id));
        if (data.items.length === 0 || page * data.per_page >= data.total) break;
      }
      let succeeded = 0;
      let failed = 0;
      for (const id of ids) {
        try {
          await retryOutbox(id);
          succeeded += 1;
        } catch {
          failed += 1;
        }
      }
      return { succeeded, failed };
    },
    onMutate: () => setBatchRetryResult(null),
    onSuccess: async (result) => {
      setBatchRetryResult(result);
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  // 全局 dead 总数：用于「重试全部失败」按钮的显示条件（无失败时隐藏）。
  const deadTotalQuery = useQuery({
    queryKey: ["outbox", "dead-total"],
    queryFn: async (): Promise<number> => {
      const data = await apiRequest<Page<OutboxItem>>("/api/v1/notifications/outbox?status=dead&per_page=1");
      return data.total;
    },
    staleTime: 30_000,
  });
  const totalDead = deadTotalQuery.data ?? 0;

  const items = outbox.data?.items ?? [];
  const deadCount = items.filter((it) => it.status === "dead").length;

  function toggleDetail(item: OutboxItem) {
    setSelectedItem(selectedItem?.id === item.id ? null : item);
  }

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">通知</p>
          <h1>投递记录</h1>
          <p>查看通知投递状态，重试失败的投递消息。</p>
        </div>
      </section>

      <section className="onboarding-card">
        <div className="outbox-toolbar">
          <div className="outbox-filters">
            {statusFilters.map((f) => (
              <button
                key={f.value}
                className={`quiet-button ${statusFilter === f.value ? "quiet-button--active" : ""}`}
                type="button"
                aria-pressed={statusFilter === f.value}
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
            {statusFilter || channelFilter ? (
              <button
                className="quiet-button"
                type="button"
                onClick={() => { setStatusFilter(""); setChannelFilter(""); }}
              >
                清除筛选
              </button>
            ) : null}
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
                  `重试本页失败 (${deadCount})`
                )}
              </button>
            )}
            {totalDead > 0 ? (
              <button
                className="quiet-button quiet-button--primary-ghost"
                type="button"
                onClick={() => {
                  // 批量操作覆盖全部失败投递：确认防误触。
                  if (window.confirm(`确定要重新排队全部 ${totalDead} 条失败投递吗？`)) {
                    retryAllDeadAcrossPages.mutate();
                  }
                }}
                disabled={retryAllDeadAcrossPages.isPending}
                title={`跨页重试全部 ${totalDead} 条失败投递`}
              >
                {retryAllDeadAcrossPages.isPending ? (
                  <><Loader2 size={14} className="spin" aria-hidden="true" /> 收集中…</>
                ) : (
                  `重试全部失败 (${totalDead})`
                )}
              </button>
            ) : null}
          </div>
        </div>
        {batchRetryResult && batchRetryResult.failed > 0 ? (
          <ErrorAlert
            title="批量重试未全部成功"
            message={`已重新排队 ${batchRetryResult.succeeded} 条，另有 ${batchRetryResult.failed} 条失败；请查看列表中的错误码后重试。`}
          />
        ) : batchRetryResult ? (
          <p className="success-banner" role="status">
            已重新排队 {batchRetryResult.succeeded} 条失败投递。
          </p>
        ) : null}

        <QueryGate
          query={outbox}
          errorTitle="无法加载投递记录"
          isEmpty={items.length === 0}
          emptyState={
            <EmptyState
              title="没有投递记录"
              description={
                statusFilter || channelFilter
                  ? "当前筛选条件下没有记录。"
                  : "配置通知渠道后，实时通知会进入投递队列。"
              }
            />
          }
        >
          <ul className="event-list">
            {items.map((item) => (
              <li
                key={item.id}
                className={selectedItem?.id === item.id ? "event-list__item--selected" : ""}
                aria-busy={retryBusyId === item.id ? "true" : undefined}
              >
                <span className={`event-kind status-${item.status}`}>{outboxStatusLabel(item.status)}</span>
                {item.channel_type && (
                  <span className="muted channel-tag">{channelLabel(item.channel_type)}</span>
                )}
                <strong>{item.title || item.id}</strong>
                <span className="muted">尝试 {item.attempt_count} 次</span>
                {item.last_error_code && <span className="error-code" title={outboxErrorHint(item.last_error_code) || undefined}>{item.last_error_code}</span>}
                {item.created_at ? <RelativeTime date={item.created_at} className="event-time" /> : null}
                <button
                  className="quiet-button quiet-button--compact"
                  type="button"
                  aria-expanded={selectedItem?.id === item.id}
                  aria-controls={`outbox-detail-${item.id}`}
                  aria-label={`${selectedItem?.id === item.id ? "收起" : "查看"}投递详情：${item.title || item.id}`}
                  onClick={() => toggleDetail(item)}
                >
                  {selectedItem?.id === item.id ? "收起详情" : "查看详情"}
                </button>
                {item.status === "dead" ? (
                  <button
                    className="quiet-button quiet-button--compact"
                    type="button"
                    aria-label={`重试投递：${item.title || item.id}`}
                    onClick={() => retry.mutate(item.id)}
                    disabled={retryBusyId === item.id}
                  >
                    {retryBusyId === item.id ? "重试中…" : "重试"}
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        </QueryGate>
      </section>

      {selectedItem && (
        <OutboxDetailDrawer item={selectedItem} onClose={closeDetail} />
      )}
    </>
  );
}

function OutboxDetailDrawer({ item, onClose }: { item: OutboxItem; onClose: () => void }) {
  const panelRef = useRef<HTMLElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  // 复制投递 ID：排查时便于粘贴到日志/工单；剪贴板不可用静默降级。
  const [copiedId, setCopiedId] = useState(false);
  const copyId = async () => {
    try {
      await navigator.clipboard.writeText(item.id);
      setCopiedId(true);
      window.setTimeout(() => setCopiedId(false), 1500);
    } catch {
      // 非安全上下文或权限受限：不打断使用。
    }
  };

  // 抽屉生命周期：记录触发焦点、锁定背景滚动、支持 Escape 关闭，
  // 关闭后归还焦点给触发元素（与移动端导航抽屉行为一致）。
  useEffect(() => {
    triggerRef.current = document.activeElement as HTMLElement | null;
    panelRef.current?.focus();

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", onKeyDown);
      triggerRef.current?.focus();
    };
  }, [onClose]);

  return (
    <div className="drawer-overlay" onClick={onClose} role="presentation">
      <aside
        ref={panelRef}
        className="drawer-panel"
        id={`outbox-detail-${item.id}`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="投递详情"
        tabIndex={-1}
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
          {item.body_text ? (
            <div className="drawer-field">
              <dt>通知正文</dt>
              {/* 纯文本化展示：外部输入 HTML 不直接渲染，避免注入。 */}
              <dd className="drawer-field__body">{htmlToPlainText(item.body_text)}</dd>
            </div>
          ) : null}
          <div className="drawer-field">
            <dt>状态</dt>
            <dd><span className={`event-kind status-${item.status}`}>{outboxStatusLabel(item.status)}</span></dd>
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
              <dd>
                <code>{item.last_error_code}</code>
                {outboxErrorHint(item.last_error_code) ? (
                  <p className="field-hint">{outboxErrorHint(item.last_error_code)}</p>
                ) : null}
              </dd>
            </div>
          )}
          {item.html_url && (
            <div className="drawer-field">
              <dt>关联链接</dt>
              <dd><a href={item.html_url} target="_blank" rel="noreferrer" title="在新窗口打开">{item.html_url}</a></dd>
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
            <dd>
              <code>{item.id}</code>{" "}
              <button type="button" className="quiet-button quiet-button--compact" onClick={() => void copyId()} aria-label="复制投递 ID">
                {copiedId ? "已复制" : "复制"}
              </button>
            </dd>
          </div>
        </dl>
      </aside>
    </div>
  );
}
