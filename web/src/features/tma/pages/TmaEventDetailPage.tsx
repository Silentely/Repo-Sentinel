import { useEffect, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import {
  executeTmaAction,
  fetchTmaEvent,
  fetchTmaEventDiff,
  fetchTmaEventLogs,
  isTmaConflictError,
  type TmaEventDetailData,
} from "../lib/tma-api";
import { getTmaStartParam, tmaHaptics } from "../lib/tma-sdk";

type TabType = "logs" | "diff" | "checks";

export function TmaEventDetailPage() {
  const routeParams = useParams({ strict: false }) as Record<string, string | undefined>;
  const id = routeParams.id || getTmaStartParam() || "";

  const [detail, setDetail] = useState<TmaEventDetailData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [activeTab, setActiveTab] = useState<TabType>("logs");
  const [logsData, setLogsData] = useState<{
    logs: string;
    truncated: boolean;
    job_name?: string;
  } | null>(null);
  const [diffData, setDiffData] = useState<{ diff: string } | null>(null);
  const [tabLoading, setTabLoading] = useState(false);

  // Action states
  const [lockedActions, setLockedActions] = useState<Record<string, boolean>>({});
  const [runningAction, setRunningAction] = useState<string | null>(null);
  const [commentText, setCommentText] = useState("");
  const [feedback, setFeedback] = useState<{
    type: "success" | "warning" | "error";
    message: string;
  } | null>(null);

  // Fetch Event Details
  useEffect(() => {
    if (!id) {
      setError("未指定事件 ID");
      setLoading(false);
      return;
    }

    const abortCtrl = new AbortController();
    setLoading(true);
    setError(null);

    fetchTmaEvent(id, abortCtrl.signal)
      .then((data) => {
        setDetail(data);
        setLoading(false);
        // Smart default tab
        if (data.event.kind === "pull_request" && !data.workflow_run) {
          setActiveTab("diff");
        } else {
          setActiveTab("logs");
        }
      })
      .catch((err) => {
        if (abortCtrl.signal.aborted) return;
        setError(err instanceof Error ? err.message : "获取事件详情失败");
        setLoading(false);
      });

    return () => {
      abortCtrl.abort();
    };
  }, [id]);

  // Fetch Tab Content (Logs / Diff)
  useEffect(() => {
    if (!id || !detail) return;

    const abortCtrl = new AbortController();

    if (activeTab === "logs" && !logsData) {
      setTabLoading(true);
      fetchTmaEventLogs(id, abortCtrl.signal)
        .then((res) => {
          setLogsData(res);
          setTabLoading(false);
        })
        .catch(() => {
          if (!abortCtrl.signal.aborted) {
            setLogsData({ logs: "", truncated: false });
            setTabLoading(false);
          }
        });
    } else if (activeTab === "diff" && !diffData) {
      setTabLoading(true);
      fetchTmaEventDiff(id, abortCtrl.signal)
        .then((res) => {
          setDiffData(res);
          setTabLoading(false);
        })
        .catch(() => {
          if (!abortCtrl.signal.aborted) {
            setDiffData({ diff: "" });
            setTabLoading(false);
          }
        });
    }

    return () => {
      abortCtrl.abort();
    };
  }, [id, activeTab, detail, logsData, diffData]);

  // Handle Action Trigger
  const handleAction = async (action: string, payload?: { comment_body?: string }) => {
    if (!id || lockedActions[action] || runningAction) return;

    setRunningAction(action);
    setFeedback(null);

    const abortCtrl = new AbortController();
    try {
      const outcome = await executeTmaAction(
        {
          event_id: id,
          action,
          ...payload,
        },
        abortCtrl.signal,
      );

      tmaHaptics.notification("success");
      setFeedback({
        type: "success",
        message: outcome.message || "操作已成功处理",
      });
      if (action === "pr_comment") {
        setCommentText("");
      }
    } catch (err: unknown) {
      if (
        isTmaConflictError(err) ||
        (typeof err === "object" && err !== null && (err as { status?: number }).status === 409)
      ) {
        tmaHaptics.notification("warning");
        // Lock action button to prevent duplicate clicks (Assertion 7 & D1)
        setLockedActions((prev) => ({ ...prev, [action]: true }));
        setFeedback({
          type: "warning",
          message: "该动作已在后台处理中，请勿重复操作",
        });
      } else {
        tmaHaptics.notification("error");
        setFeedback({
          type: "error",
          message: err instanceof Error ? err.message : "操作执行失败，请稍后重试",
        });
      }
    } finally {
      setRunningAction(null);
    }
  };

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center text-sm text-[var(--tg-theme-hint-color,#94a3b8)]">
        <div className="w-8 h-8 mb-3 border-2 border-current border-t-transparent rounded-full animate-spin opacity-60" />
        <p>加载事件详情中…</p>
      </div>
    );
  }

  if (error || !detail) {
    return (
      <div className="rounded-xl p-6 text-center bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-rose-500/30">
        <p className="text-sm text-rose-400 mb-4">{error || "事件未找到"}</p>
        <Link
          to="/tma/dashboard"
          className="inline-block px-4 py-2 text-xs font-semibold rounded-lg bg-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-text-color,#ffffff)]"
        >
          返回看板
        </Link>
      </div>
    );
  }

  const isWorkflow = Boolean(
    detail.workflow_run ||
      detail.event.workflow_run_id ||
      detail.event.kind === "workflow_run",
  );
  const isPR = Boolean(
    detail.work_item ||
      detail.event.subject_number ||
      detail.event.kind === "pull_request",
  );
  // Show all if not strictly determined
  const showWorkflowAction = isWorkflow || !isPR;
  const showPRAction = isPR || !isWorkflow;

  return (
    <div className="space-y-4">
      {/* Top Navigation */}
      <div className="flex items-center justify-between">
        <Link
          to="/tma/dashboard"
          className="text-xs text-[var(--tg-theme-link-color,#38bdf8)] flex items-center space-x-1 hover:underline"
        >
          <span>‹</span>
          <span>返回看板</span>
        </Link>
        <span className="text-[11px] font-mono text-[var(--tg-theme-hint-color,#94a3b8)]">
          {detail.repository.full_name}
        </span>
      </div>

      {/* Event Header Card */}
      <section className="rounded-xl p-3.5 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20">
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="flex items-center space-x-1.5 flex-wrap">
            <span
              className={`text-[10px] font-bold uppercase px-1.5 py-0.5 rounded ${
                detail.event.severity === "error"
                  ? "bg-rose-500/20 text-rose-300"
                  : detail.event.severity === "warning"
                    ? "bg-amber-500/20 text-amber-300"
                    : "bg-sky-500/20 text-sky-300"
              }`}
            >
              {detail.event.severity}
            </span>
            <span className="text-[10px] uppercase font-semibold px-1.5 py-0.5 rounded bg-slate-800 text-slate-300">
              {detail.event.kind}
            </span>
            <span className="text-[10px] text-[var(--tg-theme-hint-color,#94a3b8)]">
              {detail.event.action}
            </span>
          </div>
          {detail.event.html_url && (
            <a
              href={detail.event.html_url}
              target="_blank"
              rel="noreferrer"
              className="text-xs text-[var(--tg-theme-link-color,#38bdf8)]"
            >
              GitHub ↗
            </a>
          )}
        </div>

        <h1 className="text-base font-bold text-[var(--tg-theme-text-color,#f8fafc)] leading-snug">
          {detail.event.title}
        </h1>

        <div className="mt-2 text-[11px] text-[var(--tg-theme-hint-color,#94a3b8)] flex items-center justify-between">
          <span>触发者: {detail.event.actor}</span>
          <span>{new Date(detail.event.occurred_at).toLocaleTimeString()}</span>
        </div>
      </section>

      {/* Feedback Toast Banner */}
      {feedback && (
        <div
          className={`p-3 text-xs rounded-lg border flex items-center justify-between ${
            feedback.type === "success"
              ? "bg-emerald-500/20 text-emerald-300 border-emerald-500/30"
              : feedback.type === "warning"
                ? "bg-amber-500/20 text-amber-300 border-amber-500/30"
                : "bg-rose-500/20 text-rose-300 border-rose-500/30"
          }`}
        >
          <span>{feedback.message}</span>
          <button
            type="button"
            onClick={() => setFeedback(null)}
            className="font-bold ml-2 opacity-70 hover:opacity-100"
          >
            ×
          </button>
        </div>
      )}

      {/* Action Buttons Area */}
      <section className="rounded-xl p-3.5 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 space-y-2.5">
        <div className="flex items-center justify-between">
          <h2 className="text-xs font-semibold text-[var(--tg-theme-hint-color,#94a3b8)] uppercase tracking-wider">
            快速处置操作
          </h2>
        </div>

        <div className="flex flex-wrap gap-2">
          {showWorkflowAction && (
            <button
              type="button"
              disabled={
                runningAction === "workflow_rerun" ||
                Boolean(lockedActions["workflow_rerun"])
              }
              onClick={() => handleAction("workflow_rerun")}
              className="flex-1 min-w-[130px] px-3 py-2 text-xs font-semibold rounded-lg bg-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-text-color,#ffffff)] disabled:opacity-50 transition-opacity"
            >
              {runningAction === "workflow_rerun" ? "⏳ 处理中…" : "🔄 重新运行"}
            </button>
          )}

          {showPRAction && (
            <>
              <button
                type="button"
                disabled={
                  runningAction === "pr_quick_approve" ||
                  Boolean(lockedActions["pr_quick_approve"])
                }
                onClick={() => handleAction("pr_quick_approve")}
                className="flex-1 min-w-[130px] px-3 py-2 text-xs font-semibold rounded-lg bg-emerald-600 text-white disabled:opacity-50 transition-opacity"
              >
                {runningAction === "pr_quick_approve"
                  ? "⏳ 处理中…"
                  : "✅ 提交批准审查"}
              </button>

              <button
                type="button"
                disabled={
                  runningAction === "pr_snooze" ||
                  Boolean(lockedActions["pr_snooze"])
                }
                onClick={() => handleAction("pr_snooze")}
                className="flex-1 min-w-[130px] px-3 py-2 text-xs font-semibold rounded-lg bg-slate-700 text-slate-200 disabled:opacity-50 transition-opacity"
              >
                {runningAction === "pr_snooze" ? "⏳ 处理中…" : "💤 稍后提醒 (2h)"}
              </button>
            </>
          )}
        </div>

        {/* PR Comment Area */}
        {showPRAction && (
          <div className="pt-2 border-t border-[var(--tg-theme-hint-color,#334155)]/20">
            <div className="flex gap-2">
              <input
                type="text"
                placeholder="输入评论内容…"
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
                disabled={
                  runningAction === "pr_comment" ||
                  Boolean(lockedActions["pr_comment"])
                }
                className="flex-1 text-xs rounded-lg px-3 py-2 bg-slate-900 border border-slate-700 text-slate-200 placeholder-slate-500 focus:outline-none focus:border-sky-500"
              />
              <button
                type="button"
                disabled={
                  !commentText.trim() ||
                  runningAction === "pr_comment" ||
                  Boolean(lockedActions["pr_comment"])
                }
                onClick={() =>
                  handleAction("pr_comment", { comment_body: commentText })
                }
                className="px-3 py-2 text-xs font-semibold rounded-lg bg-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-text-color,#ffffff)] disabled:opacity-50"
              >
                {runningAction === "pr_comment" ? "发送中…" : "发表评论"}
              </button>
            </div>
          </div>
        )}
      </section>

      {/* Tabs View */}
      <section className="rounded-xl overflow-hidden bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20">
        <div className="flex border-b border-[var(--tg-theme-hint-color,#334155)]/20" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === "logs"}
            onClick={() => setActiveTab("logs")}
            className={`flex-1 py-2.5 text-xs font-semibold text-center transition-colors ${
              activeTab === "logs"
                ? "border-b-2 border-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-color,#38bdf8)]"
                : "text-[var(--tg-theme-hint-color,#94a3b8)]"
            }`}
          >
            CI 日志
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === "diff"}
            onClick={() => setActiveTab("diff")}
            className={`flex-1 py-2.5 text-xs font-semibold text-center transition-colors ${
              activeTab === "diff"
                ? "border-b-2 border-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-color,#38bdf8)]"
                : "text-[var(--tg-theme-hint-color,#94a3b8)]"
            }`}
          >
            文件改动 (Diff)
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeTab === "checks"}
            onClick={() => setActiveTab("checks")}
            className={`flex-1 py-2.5 text-xs font-semibold text-center transition-colors ${
              activeTab === "checks"
                ? "border-b-2 border-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-color,#38bdf8)]"
                : "text-[var(--tg-theme-hint-color,#94a3b8)]"
            }`}
          >
            检查项
          </button>
        </div>

        <div className="p-3">
          {tabLoading ? (
            <div className="py-8 text-center text-xs text-[var(--tg-theme-hint-color,#94a3b8)]">
              加载内容中…
            </div>
          ) : activeTab === "logs" ? (
            <div className="space-y-2">
              <div className="flex items-center justify-between text-[11px] text-[var(--tg-theme-hint-color,#94a3b8)]">
                <span>
                  {logsData?.job_name
                    ? `失败步骤: ${logsData.job_name}`
                    : "CI 执行输出"}
                </span>
                {logsData?.truncated && (
                  <span className="text-amber-400 font-medium">
                    [日志已截断至尾部 200 行]
                  </span>
                )}
              </div>
              {logsData?.logs ? (
                <pre className="font-mono text-xs p-3 rounded-lg bg-black/60 overflow-x-auto text-slate-300 whitespace-pre-wrap leading-relaxed max-h-96">
                  {logsData.logs}
                </pre>
              ) : (
                <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)] py-4 text-center">
                  暂无日志或该事件非 CI 运行
                </p>
              )}
            </div>
          ) : activeTab === "diff" ? (
            <div className="space-y-2">
              {diffData?.diff ? (
                <pre className="font-mono text-xs p-3 rounded-lg bg-black/60 overflow-x-auto text-slate-300 whitespace-pre leading-relaxed max-h-96">
                  {diffData.diff.split("\n").map((line, idx) => {
                    let color = "text-slate-300";
                    if (line.startsWith("+")) color = "text-emerald-400 bg-emerald-950/30";
                    else if (line.startsWith("-")) color = "text-rose-400 bg-rose-950/30";
                    else if (line.startsWith("@")) color = "text-sky-400";
                    return (
                      <div key={idx} className={`${color} px-1 rounded-sm`}>
                        {line || " "}
                      </div>
                    );
                  })}
                </pre>
              ) : (
                <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)] py-4 text-center">
                  暂无文件改动或该事件非 Pull Request
                </p>
              )}
            </div>
          ) : (
            <div className="space-y-2 text-xs">
              <div className="grid grid-cols-2 gap-2 text-[var(--tg-theme-hint-color,#94a3b8)]">
                <div>仓库: {detail.repository.full_name}</div>
                <div>类型: {detail.event.kind}</div>
                <div>事件 ID: {detail.event.id}</div>
                <div>
                  时间: {new Date(detail.event.occurred_at).toLocaleString()}
                </div>
              </div>

              {detail.workflow_run && (
                <div className="mt-2 pt-2 border-t border-[var(--tg-theme-hint-color,#334155)]/20 space-y-1">
                  <p className="font-semibold text-[var(--tg-theme-text-color,#f8fafc)]">
                    工作流运行详情
                  </p>
                  <p>名称: {detail.workflow_run.workflow_name}</p>
                  <p>分支: {detail.workflow_run.head_branch}</p>
                  <p>Commit: {detail.workflow_run.head_sha.slice(0, 7)}</p>
                  <p>
                    状态: {detail.workflow_run.status} (结论:{" "}
                    {detail.workflow_run.conclusion || "unknown"})
                  </p>
                  <p>重试次数: 第 {detail.workflow_run.run_attempt} 次</p>
                </div>
              )}

              {detail.work_item && (
                <div className="mt-2 pt-2 border-t border-[var(--tg-theme-hint-color,#334155)]/20 space-y-1">
                  <p className="font-semibold text-[var(--tg-theme-text-color,#f8fafc)]">
                    关联待办工单
                  </p>
                  <p>
                    #{detail.work_item.number}: {detail.work_item.title}
                  </p>
                  <p>状态: {detail.work_item.state}</p>
                </div>
              )}
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

export default TmaEventDetailPage;
