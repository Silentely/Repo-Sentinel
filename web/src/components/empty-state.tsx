import { ArrowRight } from "lucide-react";
import type { ReactNode } from "react";

export interface EmptyStateProps {
  eyebrow?: string;
  title: string;
  description: string;
  action?: ReactNode;
}

export function EmptyState({ eyebrow, title, description, action }: EmptyStateProps) {
  return (
    <section className="empty-state">
      {eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}
      <h2>{title}</h2>
      <p>{description}</p>
      {action ? (
        <div className="empty-state__action">
          {action}
          <ArrowRight aria-hidden="true" size={16} />
        </div>
      ) : null}
    </section>
  );
}
