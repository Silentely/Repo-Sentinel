import React, { useEffect, useId, useRef, useState } from "react";
import "./status-tooltip.css";

export interface StatusTooltipRow {
  label: string;
  value: React.ReactNode;
}

export interface StatusTooltipAlert {
  type: "danger" | "warning" | "info";
  message: string;
}

export interface StatusTooltipProps {
  children: React.ReactNode;
  title: string;
  badge?: {
    text: string;
    variant: "success" | "warning" | "danger" | "neutral";
  };
  rows?: StatusTooltipRow[];
  alert?: StatusTooltipAlert;
  hint?: string;
  align?: "left" | "right" | "center";
}

export function StatusTooltip({
  children,
  title,
  badge,
  rows = [],
  alert,
  hint,
  align = "left",
}: StatusTooltipProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [isPinned, setIsPinned] = useState(false);
  const isPinnedRef = useRef(isPinned);
  useEffect(() => {
    isPinnedRef.current = isPinned;
  }, [isPinned]);
  const tooltipId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const enterTimerRef = useRef<number | null>(null);
  const leaveTimerRef = useRef<number | null>(null);

  function clearTimers() {
    if (enterTimerRef.current !== null) {
      window.clearTimeout(enterTimerRef.current);
      enterTimerRef.current = null;
    }
    if (leaveTimerRef.current !== null) {
      window.clearTimeout(leaveTimerRef.current);
      leaveTimerRef.current = null;
    }
  }

  function handleMouseEnter() {
    clearTimers();
    enterTimerRef.current = window.setTimeout(() => {
      setIsOpen(true);
    }, 100);
  }

  function handleMouseLeave() {
    clearTimers();
    leaveTimerRef.current = window.setTimeout(() => {
      if (!isPinnedRef.current) {
        setIsOpen(false);
      }
    }, 150);
  }

  function handleFocus() {
    clearTimers();
    setIsOpen(true);
  }

  function handleBlur(e: React.FocusEvent) {
    clearTimers();
    if (!containerRef.current?.contains(e.relatedTarget as Node)) {
      if (!isPinnedRef.current) {
        setIsOpen(false);
      }
    }
  }

  function handleClick(e: React.MouseEvent) {
    // 允许通过点击锁定/关闭浮层，对触控板和移动端友好
    clearTimers();
    e.stopPropagation();
    if (isPinned) {
      setIsPinned(false);
      setIsOpen(false);
    } else {
      setIsPinned(true);
      setIsOpen(true);
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") {
      clearTimers();
      setIsPinned(false);
      setIsOpen(false);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      clearTimers();
      if (isPinned) {
        setIsPinned(false);
        setIsOpen(false);
      } else {
        setIsPinned(true);
        setIsOpen(true);
      }
    }
  }

  // 点击外部自动收起已锁定的浮层
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setIsPinned(false);
        setIsOpen(false);
      }
    }

    if (isOpen || isPinned) {
      document.addEventListener("mousedown", handleClickOutside);
    }
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [isOpen, isPinned]);

  useEffect(() => {
    return () => clearTimers();
  }, []);

  const alignClass =
    align === "right"
      ? "status-tooltip-panel--right"
      : align === "center"
        ? "status-tooltip-panel--center"
        : "status-tooltip-panel--left";

  return (
    <div
      ref={containerRef}
      className="status-tooltip-wrapper"
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      onFocus={handleFocus}
      onBlur={handleBlur}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      role="group"
      aria-expanded={isOpen || isPinned}
      aria-controls={isOpen || isPinned ? tooltipId : undefined}
    >
      {children}

      {(isOpen || isPinned) && (
        <div
          id={tooltipId}
          className={`status-tooltip-panel ${alignClass}`}
          role="tooltip"
          data-testid="status-tooltip-panel"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="status-tooltip-header">
            <span className="status-tooltip-title">{title}</span>
            {badge && (
              <span
                className={`system-badge system-badge--${badge.variant} status-tooltip-header-badge`}
              >
                {badge.text}
              </span>
            )}
          </div>

          <div className="status-tooltip-body">
            {rows.map((row, idx) => (
              <div key={idx} className="status-tooltip-row">
                <span className="status-tooltip-label">{row.label}</span>
                <span className="status-tooltip-value">{row.value}</span>
              </div>
            ))}

            {alert && (
              <div
                className={`status-tooltip-alert status-tooltip-alert--${alert.type}`}
              >
                {alert.message}
              </div>
            )}

            {hint && <div className="status-tooltip-footer">{hint}</div>}
          </div>
        </div>
      )}
    </div>
  );
}
