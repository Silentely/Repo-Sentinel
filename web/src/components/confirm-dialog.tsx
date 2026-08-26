import type { ReactNode } from "react";
import { X } from "lucide-react";

import { useModalLayer } from "../lib/use-modal-layer";

/**
 * 样式化确认对话框：替代原生 window.confirm，视觉与操作反馈与整体 UI 一致。
 * 行为与投递详情抽屉对齐：焦点循环、Escape / 点击遮罩取消、打开锁背景滚动、
 * 关闭后焦点归还触发元素（统一由 useModalLayer 承担）。danger 确认（删除类）用实色危险按钮。
 */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  cancelLabel = "取消",
  danger = false,
  busy = false,
  busyLabel,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  busy?: boolean;
  busyLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const panelRef = useModalLayer<HTMLDivElement>({ open, onClose: onCancel });

  if (!open) {
    return null;
  }

  return (
    <div className="dialog-overlay" onClick={onCancel} role="presentation">
      <div
        ref={panelRef}
        className="confirm-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="confirm-dialog__header">
          <h2>{title}</h2>
          <button className="quiet-button" type="button" onClick={onCancel} aria-label="取消">
            <X size={18} aria-hidden="true" />
          </button>
        </div>
        <div className="confirm-dialog__body">{message}</div>
        <div className="confirm-dialog__actions">
          <button className="secondary-button" type="button" onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </button>
          <button
            className={danger ? "primary-button primary-button--danger" : "primary-button"}
            type="button"
            onClick={onConfirm}
            disabled={busy}
          >
            {busy && busyLabel ? busyLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
