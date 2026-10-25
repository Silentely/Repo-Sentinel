import { useState } from "react";
import { Ban, Check, CheckCircle, RotateCw, Send, AlertTriangle } from "lucide-react";
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
    <div className="chatops-action-bar">
      <div className="chatops-action-bar__buttons">
        {suggestedReply && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("issue_reply_draft", suggestedReply)}
            disabled={executing !== null}
            aria-label="采纳发送首响应"
          >
            <Send size={13} aria-hidden="true" />
            <span>{executing === "issue_reply_draft" ? "发送中…" : "采纳首响应"}</span>
          </button>
        )}

        {isPR && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("pr_quick_approve")}
            disabled={executing !== null}
            aria-label="快速通过审批"
          >
            <CheckCircle size={13} aria-hidden="true" />
            <span>{executing === "pr_quick_approve" ? "提交中…" : "快速批准"}</span>
          </button>
        )}

        {!isPR && issueNumber !== undefined && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => setConfirmCloseOpen(true)}
            disabled={executing !== null}
            aria-label="判定垃圾并关闭"
          >
            <Ban size={13} aria-hidden="true" />
            <span>{executing === "issue_mark_spam_and_close" ? "处理中…" : "判定垃圾并关闭"}</span>
          </button>
        )}

        {runId && (
          <button
            type="button"
            className="quiet-button quiet-button--compact"
            onClick={() => handleAction("workflow_rerun")}
            disabled={executing !== null}
            aria-label="重新运行流水线"
          >
            <RotateCw size={13} className={executing === "workflow_rerun" ? "spin" : ""} aria-hidden="true" />
            <span>{executing === "workflow_rerun" ? "重跑中…" : "重跑流水线"}</span>
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
          className={`chatops-action-bar__feedback ${
            feedback.type === "success" ? "chatops-action-bar__feedback--success" : "chatops-action-bar__feedback--error"
          }`}
        >
          {feedback.type === "success" ? <Check size={14} aria-hidden="true" /> : <AlertTriangle size={14} aria-hidden="true" />}
          <span>{feedback.text}</span>
        </p>
      )}
    </div>
  );
}
