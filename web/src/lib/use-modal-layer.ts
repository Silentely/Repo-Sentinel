import { useEffect, useRef, type RefObject } from "react";

/** 模态层内可聚焦元素选择器：焦点循环共用单一来源，新增模态层不再复制。 */
export const MODAL_FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])';

/**
 * 模态层通用行为（确认对话框 / 详情抽屉 / 移动端导航抽屉共用）：
 * - open 期间锁定背景滚动；
 * - Escape 触发 onClose；
 * - Tab 焦点在容器内循环，避免焦点逃逸到遮罩之后的主内容；
 * - 打开时焦点移入容器（initialFocusSelector 指定首个聚焦元素，缺省聚焦容器本身），
 *   关闭后归还打开前的焦点元素（覆盖任意关闭路径）。
 * 返回挂到模态容器上的 ref；容器需 tabIndex={-1} 以承接缺省聚焦。
 */
export function useModalLayer<T extends HTMLElement>({
  open,
  onClose,
  initialFocusSelector,
}: {
  open: boolean;
  onClose: () => void;
  initialFocusSelector?: string;
}): RefObject<T | null> {
  const containerRef = useRef<T>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  // onClose 以 ref 跟随最新值：调用方传内联箭头时，重渲染不会触发 effect 重挂
  //（否则清理阶段会把焦点抢先归还触发元素，造成打开期间焦点抖动）。
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) {
      return;
    }
    triggerRef.current = document.activeElement as HTMLElement | null;
    const panel = containerRef.current;
    const initial = initialFocusSelector
      ? panel?.querySelector<HTMLElement>(initialFocusSelector)
      : null;
    (initial ?? panel)?.focus();

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onCloseRef.current();
        return;
      }
      if (event.key !== "Tab" || !panel) {
        return;
      }
      const focusables = panel.querySelectorAll<HTMLElement>(MODAL_FOCUSABLE_SELECTOR);
      if (focusables.length === 0) {
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (!first || !last) {
        return;
      }
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !panel.contains(active))) {
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
  }, [open, initialFocusSelector]);

  return containerRef;
}
