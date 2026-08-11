import { useCallback, useEffect, useRef, useState } from "react";

type CopyFeedback = { key: string; state: "ok" | "fail" } | null;

/**
 * 复制到剪贴板并给出按 key 区分的短暂反馈（「已复制」/「复制失败」）。
 * 收敛三处（渠道目标 / 投递 ID / Webhook URL）重复的 writeText + 定时器恢复结构：
 * - 反馈定时器在卸载时清理，避免组件卸载后 setState；
 * - 失败也给出短暂反馈（如「复制失败」），非安全上下文不打断使用。
 * isCopied / isFailed 供按钮按 key 判断当前展示文案。
 */
export function useCopyFeedback(okMs = 1500, failMs = 2200) {
  const [feedback, setFeedback] = useState<CopyFeedback>(null);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    };
  }, []);

  const copy = useCallback(
    async (key: string, text: string) => {
      try {
        await navigator.clipboard.writeText(text);
        setFeedback({ key, state: "ok" });
        if (timerRef.current !== null) window.clearTimeout(timerRef.current);
        timerRef.current = window.setTimeout(() => setFeedback(null), okMs);
      } catch {
        setFeedback({ key, state: "fail" });
        if (timerRef.current !== null) window.clearTimeout(timerRef.current);
        timerRef.current = window.setTimeout(() => setFeedback(null), failMs);
      }
    },
    [okMs, failMs],
  );

  return {
    isCopied: (key: string) => feedback?.key === key && feedback.state === "ok",
    isFailed: (key: string) => feedback?.key === key && feedback.state === "fail",
    copy,
  };
}
