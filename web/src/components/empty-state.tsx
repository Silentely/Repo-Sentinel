import { ArrowRight } from "lucide-react";
import type { ReactNode } from "react";

export interface EmptyStateProps {
  eyebrow?: string;
  title: string;
  description: string;
  action?: ReactNode;
  /** action 为按钮等非导航元素时设为 false，避免误配右箭头暗示「跳转」。 */
  actionArrow?: boolean;
}

export function EmptyState({ eyebrow, title, description, action, actionArrow = true }: EmptyStateProps) {
  return (
    <section className="empty-state">
      {eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}
      <h2>{title}</h2>
      <p>{description}</p>
      {action ? (
        <div className="empty-state__action">
          {action}
          {actionArrow ? <ArrowRight aria-hidden="true" size={16} /> : null}
        </div>
      ) : null}
    </section>
  );
}
