import type { FC } from "react";

/**
 * PixelArt SVG helper components.
 * Designed according to restyle-retro-brutalist guidelines:
 * - Restrained, sparse pixel-art scenery in peripheral background margins.
 * - Non-intrusive: aria-hidden="true", pointer-events: none, user-select: none.
 * - Crisp pixelated edges (shape-rendering: crispEdges).
 * - Adaptive to light cream paper and dark arcade terminal palettes.
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
    <rect x="10" y="2" width="12" height="4" fill="var(--ink)" />
    <rect x="6" y="6" width="20" height="4" fill="var(--ink)" />
    <rect x="4" y="10" width="24" height="6" fill="var(--ink)" />
    <rect x="2" y="16" width="28" height="6" fill="var(--ink)" />
    <rect x="4" y="22" width="24" height="6" fill="var(--ink)" />
    <rect x="8" y="28" width="16" height="4" fill="var(--ink)" />

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
    <rect x="13" y="32" width="6" height="10" fill="var(--ink)" />
    <rect x="14" y="32" width="4" height="10" fill="#795548" />
    <rect x="14" y="34" width="1" height="6" fill="#a1887f" />
    <rect x="11" y="41" width="10" height="3" fill="var(--ink)" />
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
    <rect x="29" y="4" width="6" height="8" fill="var(--ink)" />
    <rect x="30" y="5" width="4" height="7" fill="#c0392b" />
    <rect x="28" y="2" width="8" height="2" fill="var(--ink)" />

    {/* Roof Outline */}
    <rect x="20" y="6" width="4" height="2" fill="var(--ink)" />
    <rect x="16" y="8" width="12" height="2" fill="var(--ink)" />
    <rect x="12" y="10" width="20" height="2" fill="var(--ink)" />
    <rect x="8" y="12" width="28" height="2" fill="var(--ink)" />
    <rect x="4" y="14" width="36" height="2" fill="var(--ink)" />
    <rect x="2" y="16" width="40" height="4" fill="var(--ink)" />

    {/* Roof Fill */}
    <rect x="18" y="8" width="8" height="2" fill="#ff7043" />
    <rect x="14" y="10" width="16" height="2" fill="#ff7043" />
    <rect x="10" y="12" width="24" height="2" fill="#f4511e" />
    <rect x="6" y="14" width="32" height="2" fill="#e64a19" />
    <rect x="4" y="16" width="36" height="2" fill="#d84315" />

    {/* Body Walls */}
    <rect x="5" y="19" width="34" height="20" fill="var(--ink)" />
    <rect x="7" y="19" width="30" height="18" fill="var(--bg-panel-warm)" />

    {/* Window */}
    <rect x="10" y="23" width="9" height="9" fill="var(--ink)" />
    <rect x="11" y="24" width="7" height="7" fill="var(--info-bg)" />
    <rect x="14" y="24" width="1" height="7" fill="var(--ink)" />
    <rect x="11" y="27" width="7" height="1" fill="var(--ink)" />

    {/* Door */}
    <rect x="24" y="25" width="9" height="12" fill="var(--ink)" />
    <rect x="25" y="26" width="7" height="11" fill="#8d6e63" />
    <rect x="30" y="31" width="1" height="2" fill="#ffd54f" />

    {/* Ground base */}
    <rect x="3" y="38" width="38" height="2" fill="var(--ink)" />
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
    <rect x="6" y="8" width="6" height="6" fill="var(--ink)" />
    <rect x="7" y="9" width="4" height="4" fill="#fbc02d" />
    <rect x="8" y="14" width="2" height="8" fill="#2e7d32" />
    <rect x="5" y="16" width="3" height="2" fill="#388e3c" />

    {/* Grass Tufts */}
    <rect x="18" y="12" width="2" height="10" fill="#2e7d32" />
    <rect x="20" y="14" width="2" height="8" fill="#43a047" />
    <rect x="16" y="16" width="2" height="6" fill="#388e3c" />

    {/* Flower 2 - Orange Blossom */}
    <rect x="34" y="6" width="8" height="8" fill="var(--ink)" />
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
    <rect x="16" y="2" width="18" height="2" fill="var(--ink)" opacity="0.35" />
    <rect x="12" y="4" width="26" height="2" fill="var(--ink)" opacity="0.35" />
    <rect x="8" y="6" width="36" height="2" fill="var(--ink)" opacity="0.35" />
    <rect x="4" y="8" width="44" height="12" fill="var(--ink)" opacity="0.35" />
    <rect x="2" y="12" width="48" height="8" fill="var(--ink)" opacity="0.35" />
    <rect x="4" y="20" width="44" height="2" fill="var(--ink)" opacity="0.35" />
    <rect x="8" y="22" width="36" height="2" fill="var(--ink)" opacity="0.35" />

    {/* Cloud White Fill */}
    <rect x="16" y="4" width="18" height="2" fill="#ffffff" opacity="0.85" />
    <rect x="12" y="6" width="26" height="2" fill="#ffffff" opacity="0.85" />
    <rect x="8" y="8" width="36" height="12" fill="#ffffff" opacity="0.85" />
    <rect x="4" y="12" width="44" height="8" fill="#ffffff" opacity="0.85" />
    <rect x="6" y="18" width="40" height="2" fill="#f0f4f8" opacity="0.85" />
  </svg>
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
 * PixelScenerySidebarDecor renders a miniature house and tree at the bottom of the sidebar.
 */
export const PixelScenerySidebarDecor: FC<{ className?: string }> = ({ className }) => {
  return (
    <div className={`pixel-scenery-sidebar ${className || ""}`} aria-hidden="true">
      <PixelTree />
      <PixelHouse />
    </div>
  );
};
