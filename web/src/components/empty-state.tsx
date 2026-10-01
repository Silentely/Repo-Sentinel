import { ArrowRight } from "lucide-react";
import type { ReactNode } from "react";
import { PixelCampfire, PixelMailbox, PixelPeacefulClearing, PixelTree, PixelWatchtower } from "./pixel-scenery";

export type EmptyStateIllustration = "campfire" | "mailbox" | "tree" | "watchtower" | "clearing" | "none" | ReactNode;

export interface EmptyStateProps {
  eyebrow?: string;
  title: string;
  description: string;
  action?: ReactNode;
  /** action 为按钮等非导航元素时设为 false，避免误配右箭头暗示「跳转」。 */
  actionArrow?: boolean;
  /** 顶部微型像素小景插图，默认展示正在静默值守的篝火 */
  illustration?: EmptyStateIllustration;
}

function renderIllustration(ill: EmptyStateIllustration) {
  if (ill === "none") return null;
  if (ill === "mailbox") return <PixelMailbox />;
  if (ill === "tree") return <PixelTree />;
  if (ill === "watchtower") return <PixelWatchtower />;
  if (ill === "clearing") return <PixelPeacefulClearing />;
  if (ill === "campfire" || ill === undefined) return <PixelCampfire />;
  return ill;
}

export function EmptyState({
  eyebrow,
  title,
  description,
  action,
  actionArrow = true,
  illustration = "campfire",
}: EmptyStateProps) {
  const illNode = renderIllustration(illustration);

  return (
    <section className="empty-state" aria-label={title || "空状态"}>
      {illNode ? (
        <div className="empty-state__illustration" aria-hidden="true">
          {illNode}
        </div>
      ) : null}
      {eyebrow ? <p className="eyebrow">{eyebrow}</p> : null}
      <h2>{title}</h2>
      {description ? <p>{description}</p> : null}
      {action ? (
        <div className="empty-state__action">
          {action}
          {actionArrow ? <ArrowRight aria-hidden="true" size={16} /> : null}
        </div>
      ) : null}
    </section>
  );
}
