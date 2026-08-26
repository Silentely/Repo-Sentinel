import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, X } from "lucide-react";

import { ConfirmDialog } from "../../components/confirm-dialog";
import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { RelativeTime } from "../../components/relative-time";
import { apiRequest } from "../../lib/api/client";
import { toApiError } from "../../lib/api/errors";
import { channelLabel, htmlToPlainText, outboxErrorHint, outboxStatusLabel } from "../../lib/format";
import { useCopyFeedback } from "../../lib/use-copy-feedback";
import { useUrlState } from "../../lib/use-url-state";
import { ClearFiltersButton, StateFilterButtons } from "./list-shared";
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
  // 状态/渠道筛选同步到 URL（?status=dead&channel=telegram 等）：
  // 仪表盘「投递失败」跳转直达对应筛选，刷新/复制链接保留当前视角。
  const [statusFilter, setStatusFilter] = useUrlState("status", "");
  const [channelFilter, setChannelFilter] = useUrlState("channel", "");
  const [selectedItem, setSelectedItem] = useState<OutboxItem | null>(null);
  const closeDetail = useCallback(() => setSelectedItem(null), []);

  const outbox = useQuery(outboxQueryOptions(statusFilter, channelFilter));

  // 行级忙碌：只让当前重试的记录转圈，避免整列禁用。
  const [retryBusyId, setRetryBusyId] = useState<string | null>(null);
  const [batchRetryResult, setBatchRetryResult] = useState<BatchRetryResult | null>(null);
  // 单条重试失败：顶部提示原因（错误码已写入该记录，可结合列表查看）。
  const [retryError, setRetryError] = useState<string | null>(null);
  // 跨页批量重试确认：批量操作覆盖全部失败投递，样式化对话框防误触。
  const [retryAllConfirmOpen, setRetryAllConfirmOpen] = useState(false);

  // 重试（单条/批量）成功后刷新投递列表与仪表盘：两处共享同一失效逻辑。
  const invalidateOutboxAndDashboard = async () => {
    await queryClient.invalidateQueries({ queryKey: ["outbox"] });
    await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  };

  const retry = useMutation({
    mutationFn: (id: string) => retryOutbox(id),
    onMutate: (id) => {
      setRetryBusyId(id);
      setRetryError(null);
    },
    onSettled: () => setRetryBusyId(null),
    onSuccess: invalidateOutboxAndDashboard,
    onError: (err) => setRetryError(toApiError(err).message || "重试失败"),
  });

  // 批量重试共享回调：清空上次结果、回填本次结果并刷新列表/仪表盘。
  const retryBatchOptions = {
    onMutate: () => setBatchRetryResult(null),
    onSuccess: async (result: BatchRetryResult) => {
      setBatchRetryResult(result);
      await invalidateOutboxAndDashboard();
    },
  } as const;

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
    ...retryBatchOptions,
  });

  // 跨页重试全部失败：先分页收集全部 dead 投递 id（不受当前筛选限制），再逐个重新排队。
  // 用于失败记录跨多页时一键恢复，避免用户逐页操作。
  const retryAllDeadAcrossPages = useMutation({
    mutationFn: async (): Promise<BatchRetryResult> => {
      const ids: string[] = [];
      for (let page = 1; ; page++) {
        // 带上当前渠道筛选：与按钮计数（status=dead 时取当前筛选 total）保持一致，
        // 避免「仅重试 Telegram 的 3 条」按钮实际重试了所有渠道的失败投递。
        const params = new URLSearchParams({ per_page: "100", page: String(page), status: "dead" });
        if (channelFilter) params.set("channel_type", channelFilter);
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
    ...retryBatchOptions,
  });

  // 三个重试入口互斥：任一重试进行中禁用其余按钮，
  // 避免同一记录被并发重复排队、批量计数错乱。
  const anyRetryPending = retry.isPending || retryAllDead.isPending || retryAllDeadAcrossPages.isPending;

  // 全局 dead 总数：用于「重试全部失败」按钮的显示条件（无失败时隐藏）。
  // dead 筛选页本身已是 dead 列表，直接复用当前列表 total，避免多余请求。
  const deadTotalQuery = useQuery({
    queryKey: ["outbox", "dead-total"],
    queryFn: async (): Promise<number> => {
      const data = await apiRequest<Page<OutboxItem>>("/api/v1/notifications/outbox?status=dead&per_page=1");
      return data.total;
    },
    staleTime: 30_000,
    enabled: statusFilter !== "dead",
  });
  const totalDead = statusFilter === "dead" ? (outbox.data?.total ?? 0) : (deadTotalQuery.data ?? 0);

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
            <StateFilterButtons
              options={statusFilters}
              value={statusFilter}
              onChange={setStatusFilter}
            />
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
              <ClearFiltersButton onClick={() => { setStatusFilter(""); setChannelFilter(""); }} />
            ) : null}
            {statusFilter === "dead" && deadCount > 0 && (
              <button
                className="quiet-button quiet-button--primary-ghost"
                type="button"
                onClick={() => retryAllDead.mutate()}
                disabled={anyRetryPending}
                aria-busy={retryAllDead.isPending}
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
                onClick={() => setRetryAllConfirmOpen(true)}
                disabled={anyRetryPending}
                aria-busy={retryAllDeadAcrossPages.isPending}
                title={`${channelFilter ? `重试${channelLabel(channelFilter)}渠道全部 ` : "跨页重试全部 "}${totalDead} 条失败投递`}
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
        {retryError ? <ErrorAlert title="重试失败" message={retryError} /> : null}
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
              action={
                // 与 Issues/PR/Actions 页同一约定：筛选清空为操作，不显示导航箭头。
                statusFilter || channelFilter ? (
                  <ClearFiltersButton variant="primary" onClick={() => { setStatusFilter(""); setChannelFilter(""); }} />
                ) : undefined
              }
              actionArrow={false}
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
                <strong title={item.title || item.id}>{item.title || item.id}</strong>
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
                    disabled={anyRetryPending}
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

      <ConfirmDialog
        open={retryAllConfirmOpen}
        title="重新排队全部失败投递"
        message={`确定要重新排队全部 ${totalDead} 条失败投递吗？将不受当前筛选限制，按页收集后逐条重新入队。`}
        confirmLabel={retryAllDeadAcrossPages.isPending ? "收集中…" : "重新排队"}
        busy={retryAllDeadAcrossPages.isPending}
        onConfirm={() => {
          setRetryAllConfirmOpen(false);
          retryAllDeadAcrossPages.mutate();
        }}
        onCancel={() => setRetryAllConfirmOpen(false)}
      />
    </>
  );
}

function OutboxDetailDrawer({ item, onClose }: { item: OutboxItem; onClose: () => void }) {
  const panelRef = useRef<HTMLElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  // 复制投递 ID：排查时便于粘贴到日志/工单；失败给出短暂反馈，不打断使用。
  const { isCopied: copiedId, copy: copyText } = useCopyFeedback();
  const copyId = () => void copyText("id", item.id);

  // 抽屉生命周期：记录触发焦点、锁定背景滚动、支持 Escape 关闭与 Tab 焦点循环，
  // 关闭后归还焦点给触发元素（与移动端导航抽屉行为一致）。
  useEffect(() => {
    triggerRef.current = document.activeElement as HTMLElement | null;
    panelRef.current?.focus();

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
        return;
      }
      // 焦点陷阱：Tab 在对话框内循环，避免焦点逃逸到抽屉后的页面内容。
      if (event.key !== "Tab" || !panelRef.current) {
        return;
      }
      const focusables = panelRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      if (focusables.length === 0) {
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (!first || !last) {
        return;
      }
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !panelRef.current.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && active === last) {
        event.preventDefault();
        first.focus();
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
          {item.next_attempt_at ? (
            <div className="drawer-field">
              <dt>下次重试</dt>
              <dd>
                <RelativeTime date={item.next_attempt_at} />
              </dd>
            </div>
          ) : null}
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
                {copiedId("id") ? "已复制" : "复制"}
              </button>
            </dd>
          </div>
        </dl>
      </aside>
    </div>
  );
}
