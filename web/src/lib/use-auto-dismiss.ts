import { useCallback, useEffect, useRef, useState } from "react";

/**
 * 短暂提示消息：show(msg) 后 timeoutMs 自动清空。
 * 再次 show 会重置计时（连续提交时提示持续存在）；组件卸载时清理定时器。
 */
export function useAutoDismiss(timeoutMs = 3000): [string, (msg: string) => void] {
  const [message, setMessage] = useState("");
  const timer = useRef<number | undefined>(undefined);
  const show = useCallback(
    (msg: string) => {
      setMessage(msg);
      if (timer.current !== undefined) window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setMessage(""), timeoutMs);
    },
    [timeoutMs],
  );
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return [message, show];
}
