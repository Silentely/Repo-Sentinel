import type { FC, ReactNode } from "react";

/**
 * PixelArt SVG helper components.
 * Designed according to restyle-retro-brutalist guidelines:
 * - Restrained, sparse pixel-art scenery in peripheral background margins.
 * - Non-intrusive: aria-hidden="true", pointer-events: none, user-select: none.
 * - Crisp pixelated edges (shape-rendering: crispEdges).
 * - Adaptive to light cream paper and dark arcade terminal palettes via --pixel-ink.
 */

export const PixelTree: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 32 44"
    width="32"
    height="44"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Outline & Shadow */}
    <rect x="10" y="2" width="12" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="6" y="6" width="20" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="4" y="10" width="24" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="2" y="16" width="28" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="4" y="22" width="24" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="8" y="28" width="16" height="4" fill="var(--pixel-ink, #17140f)" />

    {/* Foliage Fill */}
    <rect x="12" y="4" width="8" height="4" fill="#34a853" />
    <rect x="8" y="8" width="16" height="4" fill="#34a853" />
    <rect x="6" y="12" width="20" height="4" fill="#2b8a3e" />
    <rect x="4" y="16" width="24" height="6" fill="#237032" />
    <rect x="6" y="22" width="20" height="6" fill="#1b5e20" />
    <rect x="10" y="28" width="12" height="3" fill="#144618" />

    {/* Highlights */}
    <rect x="12" y="6" width="4" height="2" fill="#81c784" />
    <rect x="8" y="10" width="6" height="2" fill="#81c784" />
    <rect x="6" y="14" width="4" height="2" fill="#66bb6a" />

    {/* Trunk */}
    <rect x="13" y="32" width="6" height="10" fill="var(--pixel-ink, #17140f)" />
    <rect x="14" y="32" width="4" height="10" fill="#795548" />
    <rect x="14" y="34" width="1" height="6" fill="#a1887f" />
    <rect x="11" y="41" width="10" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="12" y="42" width="8" height="2" fill="#4e342e" />
  </svg>
);

export const PixelHouse: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 44 40"
    width="44"
    height="40"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Chimney */}
    <rect x="29" y="4" width="6" height="8" fill="var(--pixel-ink, #17140f)" />
    <rect x="30" y="5" width="4" height="7" fill="#c0392b" />
    <rect x="28" y="2" width="8" height="2" fill="var(--pixel-ink, #17140f)" />

    {/* Roof Outline */}
    <rect x="20" y="6" width="4" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="16" y="8" width="12" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="12" y="10" width="20" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="8" y="12" width="28" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="4" y="14" width="36" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="2" y="16" width="40" height="4" fill="var(--pixel-ink, #17140f)" />

    {/* Roof Fill */}
    <rect x="18" y="8" width="8" height="2" fill="#ff7043" />
    <rect x="14" y="10" width="16" height="2" fill="#ff7043" />
    <rect x="10" y="12" width="24" height="2" fill="#f4511e" />
    <rect x="6" y="14" width="32" height="2" fill="#e64a19" />
    <rect x="4" y="16" width="36" height="2" fill="#d84315" />

    {/* Body Walls */}
    <rect x="5" y="19" width="34" height="20" fill="var(--pixel-ink, #17140f)" />
    <rect x="7" y="19" width="30" height="18" fill="var(--bg-panel-warm)" />

    {/* Window */}
    <rect x="10" y="23" width="9" height="9" fill="var(--pixel-ink, #17140f)" />
    <rect x="11" y="24" width="7" height="7" fill="var(--info-bg)" />
    <rect x="14" y="24" width="1" height="7" fill="var(--pixel-ink, #17140f)" />
    <rect x="11" y="27" width="7" height="1" fill="var(--pixel-ink, #17140f)" />

    {/* Door */}
    <rect x="24" y="25" width="9" height="12" fill="var(--pixel-ink, #17140f)" />
    <rect x="25" y="26" width="7" height="11" fill="#8d6e63" />
    <rect x="30" y="31" width="1" height="2" fill="#ffd54f" />

    {/* Ground base */}
    <rect x="3" y="38" width="38" height="2" fill="var(--pixel-ink, #17140f)" />
  </svg>
);

export const PixelFlowerPatch: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 64 24"
    width="64"
    height="24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Flower 1 - Yellow Tulip */}
    <rect x="6" y="8" width="6" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="7" y="9" width="4" height="4" fill="#fbc02d" />
    <rect x="8" y="14" width="2" height="8" fill="#2e7d32" />
    <rect x="5" y="16" width="3" height="2" fill="#388e3c" />

    {/* Grass Tufts */}
    <rect x="18" y="12" width="2" height="10" fill="#2e7d32" />
    <rect x="20" y="14" width="2" height="8" fill="#43a047" />
    <rect x="16" y="16" width="2" height="6" fill="#388e3c" />

    {/* Flower 2 - Orange Blossom */}
    <rect x="34" y="6" width="8" height="8" fill="var(--pixel-ink, #17140f)" />
    <rect x="35" y="7" width="6" height="6" fill="#ff7043" />
    <rect x="37" y="9" width="2" height="2" fill="#fff59d" />
    <rect x="37" y="14" width="2" height="8" fill="#2e7d32" />
    <rect x="39" y="16" width="3" height="2" fill="#388e3c" />

    {/* Grass 2 */}
    <rect x="50" y="13" width="2" height="9" fill="#2e7d32" />
    <rect x="52" y="11" width="2" height="11" fill="#43a047" />
    <rect x="48" y="15" width="2" height="7" fill="#388e3c" />
    <rect x="54" y="16" width="2" height="6" fill="#2e7d32" />

    {/* Ground baseline */}
    <rect x="0" y="22" width="64" height="2" fill="var(--border-subtle)" />
  </svg>
);

export const PixelCloud: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 54 26"
    width="54"
    height="26"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Cloud Outline */}
    <rect x="16" y="2" width="18" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="12" y="4" width="26" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="8" y="6" width="36" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="4" y="8" width="44" height="12" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="2" y="12" width="48" height="8" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="4" y="20" width="44" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.35" />
    <rect x="8" y="22" width="36" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.35" />

    {/* Cloud White Fill */}
    <rect x="16" y="4" width="18" height="2" fill="#ffffff" opacity="0.85" />
    <rect x="12" y="6" width="26" height="2" fill="#ffffff" opacity="0.85" />
    <rect x="8" y="8" width="36" height="12" fill="#ffffff" opacity="0.85" />
    <rect x="4" y="12" width="44" height="8" fill="#ffffff" opacity="0.85" />
    <rect x="6" y="18" width="40" height="2" fill="#f0f4f8" opacity="0.85" />
  </svg>
);

/**
 * PixelCampfire: 哨兵篝火，象征持续守望与值守陪伴。
 */
export const PixelCampfire: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 32 32"
    width="32"
    height="32"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Embers / Sparks */}
    <rect x="14" y="2" width="2" height="2" fill="#ffa726" />
    <rect x="9" y="6" width="2" height="2" fill="#ff7043" />
    <rect x="21" y="5" width="2" height="2" fill="#ffa726" />

    {/* Flame Outer Layer (Orange-Red) */}
    <rect x="13" y="6" width="6" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="10" y="10" width="12" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="8" y="16" width="16" height="6" fill="var(--pixel-ink, #17140f)" />

    <rect x="14" y="7" width="4" height="4" fill="#ff5722" />
    <rect x="11" y="11" width="10" height="6" fill="#f4511e" />
    <rect x="9" y="17" width="14" height="5" fill="#e64a19" />

    {/* Flame Inner Layer (Warm Yellow & Light) */}
    <rect x="13" y="12" width="6" height="8" fill="#ffca28" />
    <rect x="14" y="15" width="4" height="4" fill="#fff9c4" />

    {/* Firewood Logs */}
    <rect x="4" y="24" width="24" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="5" y="25" width="22" height="2" fill="#6d4c41" />
    <rect x="6" y="22" width="6" height="4" fill="#8d6e63" />
    <rect x="20" y="22" width="6" height="4" fill="#8d6e63" />
    {/* Ashes */}
    <rect x="2" y="27" width="28" height="2" fill="#424242" opacity="0.6" />
  </svg>
);

/**
 * PixelMailbox: 像素邮筒/投递箱，呼应通知分发与 Outbox 队列。
 */
export const PixelMailbox: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 26 34"
    width="26"
    height="34"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Post / Pole */}
    <rect x="11" y="18" width="4" height="14" fill="var(--pixel-ink, #17140f)" />
    <rect x="12" y="18" width="2" height="13" fill="#8d6e63" />
    <rect x="8" y="31" width="10" height="2" fill="var(--pixel-ink, #17140f)" />

    {/* Mailbox Box Body Outline */}
    <rect x="4" y="4" width="18" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="2" y="6" width="22" height="12" fill="var(--pixel-ink, #17140f)" />

    {/* Red Body Fill */}
    <rect x="4" y="6" width="18" height="10" fill="#e53935" />
    <rect x="4" y="6" width="18" height="2" fill="#ef5350" />
    <rect x="4" y="14" width="18" height="2" fill="#c62828" />

    {/* Letter Slot */}
    <rect x="6" y="10" width="10" height="2" fill="var(--pixel-ink, #17140f)" />

    {/* Signal Flag (Upright) */}
    <rect x="19" y="0" width="2" height="8" fill="var(--pixel-ink, #17140f)" />
    <rect x="21" y="0" width="4" height="4" fill="#ffd54f" />
    <rect x="20" y="7" width="2" height="2" fill="var(--pixel-ink, #17140f)" />
  </svg>
);

/**
 * PixelWatchtower: 哨兵瞭望塔，象征 RepoSentinel 哨兵全天候监控与值守。
 */
export const PixelWatchtower: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 36 46"
    width="36"
    height="46"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Flag on top */}
    <rect x="17" y="2" width="2" height="8" fill="var(--pixel-ink, #17140f)" />
    <rect x="19" y="3" width="7" height="4" fill="#ff5722" />
    <rect x="24" y="4" width="2" height="2" fill="#ffd54f" />

    {/* Roof */}
    <rect x="10" y="8" width="16" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="6" y="10" width="24" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="8" y="10" width="20" height="2" fill="#d84315" />

    {/* Cabin Observation Deck */}
    <rect x="9" y="13" width="18" height="9" fill="var(--pixel-ink, #17140f)" />
    <rect x="11" y="13" width="14" height="7" fill="var(--bg-panel-warm)" />
    {/* Lookouts/Windows */}
    <rect x="13" y="15" width="3" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="20" y="15" width="3" height="3" fill="var(--pixel-ink, #17140f)" />

    {/* Deck Floor Railing */}
    <rect x="5" y="21" width="26" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="6" y="22" width="24" height="1" fill="#8d6e63" />

    {/* Stilts / Legs */}
    <rect x="8" y="24" width="3" height="18" fill="var(--pixel-ink, #17140f)" />
    <rect x="25" y="24" width="3" height="18" fill="var(--pixel-ink, #17140f)" />
    {/* Cross Bracing */}
    <rect x="11" y="29" width="14" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.8" />
    <rect x="11" y="36" width="14" height="2" fill="var(--pixel-ink, #17140f)" opacity="0.8" />

    {/* Ground Footings */}
    <rect x="6" y="42" width="7" height="2" fill="var(--pixel-ink, #17140f)" />
    <rect x="23" y="42" width="7" height="2" fill="var(--pixel-ink, #17140f)" />
  </svg>
);

/**
 * PixelPeacefulClearing: 空状态专用治愈系微缩地块（小草坪 + 跳动篝火 + 小木桩）。
 */
export const PixelPeacefulClearing: FC<{ className?: string }> = ({ className }) => (
  <div className={`pixel-peaceful-clearing ${className || ""}`} aria-hidden="true">
    <div className="pixel-peaceful-clearing__tree">
      <PixelTree />
    </div>
    <div className="pixel-peaceful-clearing__center">
      <PixelCampfire />
    </div>
    <div className="pixel-peaceful-clearing__mailbox">
      <PixelMailbox />
    </div>
  </div>
);

/**
 * PixelSceneryBackdrop renders a charming, sparse pixel scenery in peripheral margins.
 * Ideal for auth screens (login, setup) and dashboard bottom margins.
 */
export const PixelSceneryGround: FC<{ className?: string }> = ({ className }) => {
  return (
    <div className={`pixel-scenery-ground ${className || ""}`} aria-hidden="true">
      <div className="pixel-scenery-ground__item pixel-scenery-ground__item--tree">
        <PixelTree />
      </div>
      <div className="pixel-scenery-ground__item pixel-scenery-ground__item--house">
        <PixelHouse />
      </div>
      <div className="pixel-scenery-ground__strip">
        <PixelFlowerPatch />
        <PixelFlowerPatch />
        <PixelFlowerPatch />
      </div>
    </div>
  );
};

/**
 * PixelScenerySidebarDecor renders a miniature house, tree, and campfire at the bottom of the sidebar.
 */
export const PixelScenerySidebarDecor: FC<{ className?: string }> = ({ className }) => {
  return (
    <div className={`pixel-scenery-sidebar ${className || ""}`} aria-hidden="true">
      <PixelTree />
      <PixelCampfire />
      <PixelHouse />
    </div>
  );
};
