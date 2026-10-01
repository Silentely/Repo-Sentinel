import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Bell,
  Check,
  FolderGit2,
  GitPullRequest,
  Info,
  ListTodo,
  RefreshCw,
  Search,
  Settings,
  Shield,
  Star,
  Workflow,
  X,
} from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { reconcileAll, retryAllDeadOutbox } from "../features/monitor/api";
import { useModalLayer } from "../lib/use-modal-layer";

export interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
}

interface PaletteAction {
  id: string;
  category: "导航" | "视图" | "危险动作";
  title: string;
  subtitle?: string;
  shortcut?: string;
  destructive?: boolean;
  confirmPrompt?: string;
  icon?: React.ReactNode;
  perform: () => void | Promise<void>;
}

export function CommandPalette({ open, onClose }: CommandPaletteProps) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [pendingAction, setPendingAction] = useState<PaletteAction | null>(null);
  const [actionLoading, setActionLoading] = useState(false);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const successTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (successTimerRef.current) {
        clearTimeout(successTimerRef.current);
      }
    };
  }, []);

  const containerRef = useModalLayer<HTMLDivElement>({
    open,
    onClose: () => {
      if (pendingAction) {
        setPendingAction(null);
        return;
      }
      onClose();
    },
    initialFocusSelector: ".command-palette-input",
  });

  // 重置状态
  useEffect(() => {
    if (open) {
      setQuery("");
      setSelectedIndex(0);
      setPendingAction(null);
      setActionLoading(false);
      setActionSuccess(null);
      setActionError(null);
    }
  }, [open]);

  const actions = useMemo<PaletteAction[]>(() => {
    return [
      {
        id: "nav-dashboard",
        category: "导航",
        title: "仪表盘",
        subtitle: "系统核心指标态势、活跃仓库与新鲜度",
        shortcut: "G D",
        icon: <Activity size={16} />,
        perform: () => {
          navigate({ to: "/" });
          onClose();
        },
      },
      {
        id: "nav-issues",
        category: "导航",
        title: "Issues",
        subtitle: "收件箱与分诊流，查看与管理未关闭/已归档 Issue",
        shortcut: "G I",
        icon: <ListTodo size={16} />,
        perform: () => {
          navigate({ to: "/issues" });
          onClose();
        },
      },
      {
        id: "nav-prs",
        category: "导航",
        title: "Pull Requests",
        subtitle: "合并请求、审核状态与自动化检查",
        shortcut: "G P",
        icon: <GitPullRequest size={16} />,
        perform: () => {
          navigate({ to: "/pull-requests" });
          onClose();
        },
      },
      {
        id: "nav-actions",
        category: "导航",
        title: "Actions 运行",
        subtitle: "CI/CD 工作流构建状态与耗时洞察",
        shortcut: "G R",
        icon: <Workflow size={16} />,
        perform: () => {
          navigate({ to: "/actions" });
          onClose();
        },
      },
      {
        id: "nav-security",
        category: "导航",
        title: "安全告警",
        subtitle: "Dependabot、Code Scanning 与 Secret Scanning",
        shortcut: "G A",
        icon: <Shield size={16} />,
        perform: () => {
          navigate({ to: "/security" });
          onClose();
        },
      },
      {
        id: "nav-releases",
        category: "导航",
        title: "Star Releases",
        subtitle: "已 Star 仓库的发布动态追踪",
        icon: <Star size={16} />,
        perform: () => {
          navigate({ to: "/starred-releases" });
          onClose();
        },
      },
      {
        id: "nav-repos",
        category: "导航",
        title: "仓库管理",
        subtitle: "已纳管的自有仓与外部公开仓",
        icon: <FolderGit2 size={16} />,
        perform: () => {
          navigate({ to: "/repos" });
          onClose();
        },
      },
      {
        id: "nav-notifications",
        category: "导航",
        title: "通知渠道配置",
        subtitle: "Telegram、飞书、企业微信与钉钉推送及免打扰",
        shortcut: "G N",
        icon: <Bell size={16} />,
        perform: () => {
          navigate({ to: "/notifications" });
          onClose();
        },
      },
      {
        id: "nav-settings",
        category: "导航",
        title: "系统设置",
        subtitle: "数据保留策略、时区与全局偏好",
        shortcut: "G S",
        icon: <Settings size={16} />,
        perform: () => {
          navigate({ to: "/settings" });
          onClose();
        },
      },
      {
        id: "nav-about",
        category: "导航",
        title: "关于 Repo-Sentinel",
        subtitle: "系统架构、版本构建信息与文档",
        icon: <Info size={16} />,
        perform: () => {
          navigate({ to: "/about" });
          onClose();
        },
      },
      {
        id: "act-reconcile-all",
        category: "危险动作",
        title: "强制全量对账",
        subtitle: "对所有活跃仓库立即发起全量对账拉取",
        destructive: true,
        confirmPrompt:
          "该操作将对所有活跃仓库触发并发全量同步，可能消耗较多 GitHub API 配额。确定继续吗？",
        icon: <RefreshCw size={16} />,
        perform: async () => {
          await reconcileAll();
        },
      },
      {
        id: "act-retry-dead",
        category: "危险动作",
        title: "重试全部死信投递",
        subtitle: "将所有处于 dead 失败状态的通知投递恢复为待重发",
        destructive: true,
        confirmPrompt: "确定要重试所有处于死信（dead）状态的投递消息吗？",
        icon: <RefreshCw size={16} />,
        perform: async () => {
          await retryAllDeadOutbox();
        },
      },
    ];
  }, [navigate, onClose]);

  const filteredActions = useMemo(() => {
    if (!query.trim()) return actions;
    const lower = query.trim().toLowerCase();
    return actions.filter(
      (a) =>
        a.title.toLowerCase().includes(lower) ||
        (a.subtitle && a.subtitle.toLowerCase().includes(lower)) ||
        a.category.toLowerCase().includes(lower),
    );
  }, [actions, query]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  const handleSelect = (action: PaletteAction) => {
    if (action.destructive) {
      setPendingAction(action);
      setActionError(null);
      setActionSuccess(null);
    } else {
      try {
        void action.perform();
      } catch (err) {
        console.error("Failed to perform palette action:", err);
      }
    }
  };

  const handleConfirmAction = async () => {
    if (!pendingAction) return;
    setActionLoading(true);
    setActionError(null);
    try {
      await pendingAction.perform();
      setActionSuccess("操作执行成功");
      if (successTimerRef.current) clearTimeout(successTimerRef.current);
      successTimerRef.current = setTimeout(() => {
        setPendingAction(null);
        onClose();
      }, 700);
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : "执行失败");
    } finally {
      setActionLoading(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (pendingAction) {
      if (e.key === "Enter" && !actionLoading) {
        e.preventDefault();
        handleConfirmAction();
      } else if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        setPendingAction(null);
      }
      return;
    }

    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev + 1) % Math.max(1, filteredActions.length));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((prev) => (prev - 1 + filteredActions.length) % Math.max(1, filteredActions.length));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const current = filteredActions[selectedIndex];
      if (current) {
        handleSelect(current);
      }
    }
  };

  if (!open) return null;

  return (
    <div className="dialog-overlay command-palette-overlay" onClick={onClose}>
      <div
        ref={containerRef}
        className="command-palette-dialog"
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label="全局指令面板"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        {pendingAction ? (
          <div className="command-palette-confirm">
            <div className="command-palette-confirm__header">
              <AlertTriangle className="text-warning" size={24} />
              <h3>二次确认危险操作</h3>
            </div>
            <p className="command-palette-confirm__body">
              {pendingAction.confirmPrompt || "确定要执行此操作吗？"}
            </p>
            {actionError && <div className="command-palette-error">{actionError}</div>}
            {actionSuccess && (
              <div className="command-palette-success">
                <Check size={16} /> {actionSuccess}
              </div>
            )}
            <div className="command-palette-confirm__actions">
              <button
                type="button"
                className="quiet-button"
                onClick={() => setPendingAction(null)}
                disabled={actionLoading}
              >
                取消
              </button>
              <button
                type="button"
                className="pixel-btn pixel-btn--danger"
                onClick={handleConfirmAction}
                disabled={actionLoading}
              >
                {actionLoading ? "执行中…" : "确认执行"}
              </button>
            </div>
          </div>
        ) : (
          <>
            <div className="command-palette-header">
              <Search className="command-palette-search-icon" size={18} />
              <input
                type="text"
                className="command-palette-input"
                placeholder="输入指令或搜索… (↑↓ 选择，Enter 执行，Esc 关闭)"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                autoFocus
              />
              <button
                type="button"
                className="command-palette-close"
                onClick={onClose}
                aria-label="关闭指令面板"
              >
                <X size={18} />
              </button>
            </div>

            <div className="command-palette-list">
              {filteredActions.length === 0 ? (
                <div className="command-palette-empty">没有匹配的指令</div>
              ) : (
                filteredActions.map((action, index) => {
                  const isSelected = index === selectedIndex;
                  return (
                    <div
                      key={action.id}
                      className={`command-palette-item ${isSelected ? "selected" : ""} ${
                        action.destructive ? "destructive" : ""
                      }`}
                      onClick={() => handleSelect(action)}
                      onMouseEnter={() => setSelectedIndex(index)}
                      role="button"
                      tabIndex={0}
                    >
                      <div className="command-palette-item__icon">
                        {action.icon ?? <ArrowRight size={16} />}
                      </div>
                      <div className="command-palette-item__content">
                        <div className="command-palette-item__title">
                          {action.title}
                          <span className="command-palette-item__badge">{action.category}</span>
                        </div>
                        {action.subtitle && (
                          <div className="command-palette-item__subtitle">{action.subtitle}</div>
                        )}
                      </div>
                      {action.shortcut && (
                        <div className="command-palette-item__shortcut">
                          <kbd>{action.shortcut}</kbd>
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>

            <div className="command-palette-footer">
              <span>
                <kbd>↑</kbd> <kbd>↓</kbd> 切换选项
              </span>
              <span>
                <kbd>Enter</kbd> 确认执行
              </span>
              <span>
                <kbd>Esc</kbd> 关闭
              </span>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

/**
 * 全局热键 Hook：监听 Cmd+K / Ctrl+K 以及 G 序列导航。
 */
export function useGlobalHotkeys({
  onTogglePalette,
  onNavigate,
}: {
  onTogglePalette: () => void;
  onNavigate: (path: string) => void;
}) {
  const gPressedRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // 检查 Cmd+K / Ctrl+K
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        onTogglePalette();
        return;
      }

      // 如果聚焦在输入框内，不拦截单个字符快捷键
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }

      // 按 / 键聚焦搜索框
      if (e.key === "/" && !e.metaKey && !e.ctrlKey) {
        const searchInput = document.querySelector<HTMLInputElement>(
          'input[type="search"], .search-input',
        );
        if (searchInput) {
          e.preventDefault();
          searchInput.focus();
          return;
        }
      }

      // 序列键：G 随后 D, I, P, R, A, S, N
      if (e.key.toLowerCase() === "g" && !e.metaKey && !e.ctrlKey && !gPressedRef.current) {
        gPressedRef.current = true;
        if (timerRef.current) clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => {
          gPressedRef.current = false;
        }, 1000);
        return;
      }

      if (gPressedRef.current && !e.metaKey && !e.ctrlKey) {
        gPressedRef.current = false;
        if (timerRef.current) clearTimeout(timerRef.current);

        const key = e.key.toLowerCase();
        if (key === "d") {
          e.preventDefault();
          onNavigate("/");
        } else if (key === "i") {
          e.preventDefault();
          onNavigate("/issues");
        } else if (key === "p") {
          e.preventDefault();
          onNavigate("/pull-requests");
        } else if (key === "r") {
          e.preventDefault();
          onNavigate("/actions");
        } else if (key === "a") {
          e.preventDefault();
          onNavigate("/security");
        } else if (key === "s") {
          e.preventDefault();
          onNavigate("/settings");
        } else if (key === "n") {
          e.preventDefault();
          onNavigate("/notifications");
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [onTogglePalette, onNavigate]);
}
