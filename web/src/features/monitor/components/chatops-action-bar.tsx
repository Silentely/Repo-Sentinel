import { useState } from "react";
import { ConfirmDialog } from "../../../components/confirm-dialog";
import { executeChatOpsWebAction, type ChatOpsWebActionResult } from "../api";

export interface ChatOpsActionBarProps {
  repoId: string;
  issueNumber?: number;
  runId?: string;
  suggestedReply?: string;
  isPR?: boolean;
  onSuccess?: (result: ChatOpsWebActionResult) => void;
}

export function ChatOpsActionBar({
  repoId,
  issueNumber,
  runId,
  suggestedReply,
  isPR,
  onSuccess,
}: ChatOpsActionBarProps) {
  const [executing, setExecuting] = useState<string | null>(null);
  const [feedback, setFeedback] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [confirmCloseOpen, setConfirmCloseOpen] = useState(false);

  const handleAction = async (action: string, payload?: string) => {
    setExecuting(action);
    setFeedback(null);
    try {
      const res = await executeChatOpsWebAction({
        action,
        repo_id: repoId,
        issue_number: issueNumber,
        run_id: runId,
        payload,
      });
      setFeedback({ type: "success", text: res.message || "操作已成功执行" });
      onSuccess?.(res);
    } catch (err) {
      setFeedback({
        type: "error",
        text: err instanceof Error ? err.message : "执行失败，请稍后重试",
      });
    } finally {
      setExecuting(null);
    }
  };

  return (
    <div className="chatops-action-bar" style={{ marginTop: "0.5rem" }}>
      <div style={{ display: "flex", alignItems: "center", gap: "0.4rem", flexWrap: "wrap" }}>
        {suggestedReply && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("issue_reply_draft", suggestedReply)}
            disabled={executing !== null}
            style={{ fontSize: "0.75rem", fontWeight: 600 }}
          >
            {executing === "issue_reply_draft" ? "发送中…" : "🚀 采纳发送首响应"}
          </button>
        )}

        {isPR && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("pr_quick_approve")}
            disabled={executing !== null}
            style={{ fontSize: "0.75rem" }}
          >
            {executing === "pr_quick_approve" ? "提交中…" : "👍 快速通过审批"}
          </button>
        )}

        {!isPR && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => setConfirmCloseOpen(true)}
            disabled={executing !== null}
            style={{ fontSize: "0.75rem" }}
          >
            {executing === "issue_mark_spam_and_close" ? "处理中…" : "🚫 判定垃圾并关闭"}
          </button>
        )}

        {runId && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("workflow_rerun")}
            disabled={executing !== null}
            style={{ fontSize: "0.75rem" }}
          >
            {executing === "workflow_rerun" ? "重跑中…" : "🔄 重新运行流水线"}
          </button>
        )}
      </div>

      <ConfirmDialog
        open={confirmCloseOpen}
        title="标记并关闭 Issue"
        message={
          issueNumber !== undefined
            ? `确定将 #${issueNumber} 判定为垃圾/无效工单并在 GitHub 上自动关闭吗？此操作将直接同步远端。`
            : "确定将此工单判定为垃圾/无效并在 GitHub 上自动关闭吗？"
        }
        confirmLabel="确认关闭"
        danger={true}
        busy={executing === "issue_mark_spam_and_close"}
        onConfirm={async () => {
          setConfirmCloseOpen(false);
          await handleAction("issue_mark_spam_and_close");
        }}
        onCancel={() => setConfirmCloseOpen(false)}
      />

      {feedback && (
        <p
          style={{
            margin: "0.4rem 0 0",
            fontSize: "0.75rem",
            color: feedback.type === "success" ? "var(--success-fg, #10b981)" : "var(--danger-fg, #ef4444)",
          }}
        >
          {feedback.type === "success" ? "✓ " : "⚠ "}
          {feedback.text}
        </p>
      )}
    </div>
  );
}
