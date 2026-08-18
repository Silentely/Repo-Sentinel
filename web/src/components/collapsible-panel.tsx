import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";

/**
 * 可折叠面板：点击标题行展开/收起内容区，仪表盘各面板与设置页「仓库与基线对账」共用。
 * 语义：section 以 sr-only h2 承载可访问名称与 region 角色（button 内容模型不允许 h2），
 * 视觉标题由按钮内 span 呈现，配合 aria-expanded / aria-controls 提供完整键盘与读屏支持。
 */
export function CollapsiblePanel({
  id,
  title,
  count,
  open,
  onToggle,
  headerExtra,
  children,
}: {
  id: string;
  title: string;
  count?: ReactNode;
  open: boolean;
  onToggle: () => void;
  headerExtra?: ReactNode;
  children: ReactNode;
}) {
  const headingId = `${id}-title`;
  const panelId = `${id}-panel`;
  return (
    <section className={`onboarding-card collapsible-panel${open ? " is-open" : ""}`} aria-labelledby={headingId}>
      <div className="collapsible-panel__header">
        {/* 标题以 sr-only 承载语义与可访问名称；视觉标题由按钮内 span 呈现（button 内容模型不允许 h2）。 */}
        <h2 id={headingId} className="sr-only">
          {title}
        </h2>
        <button
          type="button"
          className="collapsible-panel__toggle"
          aria-expanded={open}
          // 收起时 body 不在 DOM：此时不输出 aria-controls，避免指向不存在的元素。
          aria-controls={open ? panelId : undefined}
          aria-labelledby={headingId}
          onClick={onToggle}
        >
          <ChevronDown className="collapsible-panel__chevron" size={18} aria-hidden="true" />
          <span className="collapsible-panel__title">{title}</span>
          {count != null ? <span className="collapsible-panel__count">{count}</span> : null}
        </button>
        {headerExtra ? <div className="collapsible-panel__extra">{headerExtra}</div> : null}
      </div>
      {open ? (
        <div id={panelId} className="collapsible-panel__body">
          {children}
        </div>
      ) : null}
    </section>
  );
}
