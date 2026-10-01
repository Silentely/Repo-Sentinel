import type { FC } from "react";

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

/**
 * PixelSheep: 悠闲可爱的草地小绵羊，支持站立观望与低头吃草两种形态。
 */
export const PixelSheep: FC<{ className?: string; facing?: "left" | "right"; variant?: "standing" | "grazing" }> = ({
  className,
  facing = "left",
  variant = "standing",
}) => {
  const isGrazing = variant === "grazing";
  const flip = facing === "right" ? "scaleX(-1)" : undefined;

  return (
    <svg
      viewBox="0 0 28 20"
      width="28"
      height="20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      shapeRendering="crispEdges"
      className={className}
      style={flip ? { transform: flip, transformOrigin: "center" } : undefined}
      aria-hidden="true"
    >
      {/* Wool Body Outline */}
      <rect x="7" y="3" width="13" height="11" fill="var(--pixel-ink, #17140f)" />
      <rect x="5" y="5" width="17" height="8" fill="var(--pixel-ink, #17140f)" />
      {/* Fluffy White Wool */}
      <rect x="8" y="4" width="11" height="9" fill="#ffffff" />
      <rect x="6" y="6" width="15" height="6" fill="#ffffff" />
      {/* Wool shadow */}
      <rect x="8" y="11" width="12" height="2" fill="#e2e8f0" />
      <rect x="18" y="6" width="2" height="3" fill="#f1f5f9" />

      {/* Little Tail */}
      <rect x="21" y="6" width="3" height="3" fill="var(--pixel-ink, #17140f)" />
      <rect x="21" y="7" width="2" height="2" fill="#ffffff" />

      {/* Head & Ear */}
      {isGrazing ? (
        <>
          <rect x="2" y="9" width="6" height="7" fill="var(--pixel-ink, #17140f)" />
          <rect x="3" y="10" width="4" height="5" fill="#374151" />
          <rect x="4" y="11" width="1" height="1" fill="#ffffff" />
          <rect x="6" y="8" width="2" height="2" fill="var(--pixel-ink, #17140f)" />
          <rect x="6" y="8" width="1" height="2" fill="#f48fb1" />
          <rect x="1" y="14" width="2" height="2" fill="#4caf50" />
        </>
      ) : (
        <>
          <rect x="2" y="4" width="6" height="7" fill="var(--pixel-ink, #17140f)" />
          <rect x="3" y="5" width="4" height="5" fill="#374151" />
          <rect x="4" y="6" width="1" height="1" fill="#ffffff" />
          <rect x="6" y="3" width="2" height="2" fill="var(--pixel-ink, #17140f)" />
          <rect x="6" y="3" width="1" height="2" fill="#f48fb1" />
        </>
      )}

      {/* 4 Legs */}
      <rect x="8" y="14" width="2" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="12" y="14" width="2" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="16" y="14" width="2" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="19" y="14" width="2" height="5" fill="var(--pixel-ink, #17140f)" />
      {/* Hooves */}
      <rect x="8" y="18" width="2" height="1" fill="#1f2937" />
      <rect x="12" y="18" width="2" height="1" fill="#1f2937" />
      <rect x="16" y="18" width="2" height="1" fill="#1f2937" />
      <rect x="19" y="18" width="2" height="1" fill="#1f2937" />
    </svg>
  );
};

/**
 * PixelCow: 草地黑白花斑奶牛，呆萌温顺。
 */
export const PixelCow: FC<{ className?: string; facing?: "left" | "right" }> = ({
  className,
  facing = "left",
}) => {
  const flip = facing === "right" ? "scaleX(-1)" : undefined;

  return (
    <svg
      viewBox="0 0 36 24"
      width="36"
      height="24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      shapeRendering="crispEdges"
      className={className}
      style={flip ? { transform: flip, transformOrigin: "center" } : undefined}
      aria-hidden="true"
    >
      {/* Body Outline */}
      <rect x="8" y="5" width="21" height="13" fill="var(--pixel-ink, #17140f)" />
      <rect x="6" y="7" width="25" height="10" fill="var(--pixel-ink, #17140f)" />

      {/* Body White base */}
      <rect x="9" y="6" width="19" height="11" fill="#ffffff" />
      <rect x="7" y="8" width="23" height="8" fill="#ffffff" />

      {/* Black Spots */}
      <rect x="11" y="7" width="5" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="19" y="9" width="6" height="6" fill="var(--pixel-ink, #17140f)" />
      <rect x="23" y="7" width="4" height="4" fill="var(--pixel-ink, #17140f)" />
      <rect x="9" y="14" width="4" height="2" fill="#e2e8f0" />

      {/* Udder touch */}
      <rect x="21" y="16" width="4" height="2" fill="#f8bbd0" />

      {/* Tail */}
      <rect x="29" y="9" width="3" height="2" fill="var(--pixel-ink, #17140f)" />
      <rect x="31" y="11" width="1" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="30" y="15" width="2" height="2" fill="var(--pixel-ink, #17140f)" />

      {/* Cow Head */}
      <rect x="2" y="5" width="8" height="9" fill="var(--pixel-ink, #17140f)" />
      <rect x="3" y="6" width="6" height="7" fill="#ffffff" />
      <rect x="5" y="6" width="3" height="3" fill="var(--pixel-ink, #17140f)" />
      {/* Muzzle */}
      <rect x="1" y="9" width="4" height="5" fill="var(--pixel-ink, #17140f)" />
      <rect x="2" y="10" width="3" height="3" fill="#f8bbd0" />
      <rect x="2" y="11" width="1" height="1" fill="#c2185b" />
      <rect x="5" y="7" width="1" height="1" fill="#212121" />

      {/* Horns */}
      <rect x="6" y="3" width="2" height="3" fill="var(--pixel-ink, #17140f)" />
      <rect x="6" y="3" width="1" height="2" fill="#fbc02d" />
      <rect x="8" y="4" width="2" height="2" fill="var(--pixel-ink, #17140f)" />
      <rect x="8" y="4" width="1" height="1" fill="#fbc02d" />

      {/* Legs */}
      <rect x="9" y="17" width="2" height="6" fill="var(--pixel-ink, #17140f)" />
      <rect x="13" y="17" width="2" height="6" fill="var(--pixel-ink, #17140f)" />
      <rect x="22" y="17" width="2" height="6" fill="var(--pixel-ink, #17140f)" />
      <rect x="26" y="17" width="2" height="6" fill="var(--pixel-ink, #17140f)" />
      {/* Hooves */}
      <rect x="9" y="22" width="2" height="1" fill="#374151" />
      <rect x="13" y="22" width="2" height="1" fill="#374151" />
      <rect x="22" y="22" width="2" height="1" fill="#374151" />
      <rect x="26" y="22" width="2" height="1" fill="#374151" />
    </svg>
  );
};

/**
 * PixelMountain: 积雪高山远峦，提供深远静谧的背景意境。
 */
export const PixelMountain: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 112 56"
    width="112"
    height="56"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Peak 1 (High left peak outline) */}
    <rect x="36" y="4" width="6" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="32" y="7" width="14" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="28" y="11" width="22" height="5" fill="var(--pixel-ink, #17140f)" />
    <rect x="22" y="16" width="34" height="6" fill="var(--pixel-ink, #17140f)" />
    <rect x="16" y="22" width="46" height="7" fill="var(--pixel-ink, #17140f)" />
    <rect x="10" y="29" width="58" height="8" fill="var(--pixel-ink, #17140f)" />
    <rect x="4" y="37" width="70" height="9" fill="var(--pixel-ink, #17140f)" />
    <rect x="0" y="46" width="78" height="9" fill="var(--pixel-ink, #17140f)" />

    {/* Snowcap on Peak 1 */}
    <rect x="37" y="5" width="4" height="2" fill="#ffffff" />
    <rect x="34" y="7" width="9" height="3" fill="#ffffff" />
    <rect x="30" y="10" width="14" height="3" fill="#ffffff" />
    <rect x="30" y="13" width="7" height="3" fill="#ffffff" />
    <rect x="37" y="13" width="6" height="3" fill="#cbd5e1" />
    <rect x="32" y="16" width="4" height="3" fill="#ffffff" />

    {/* Mountain Body Peak 1 */}
    <rect x="24" y="17" width="9" height="5" fill="#8ca0b3" />
    <rect x="18" y="22" width="16" height="7" fill="#8ca0b3" />
    <rect x="12" y="29" width="22" height="8" fill="#7a8f9f" />
    <rect x="6" y="37" width="28" height="9" fill="#7a8f9f" />
    <rect x="2" y="46" width="32" height="8" fill="#687d8d" />

    <rect x="38" y="16" width="16" height="6" fill="#4d5f70" />
    <rect x="36" y="22" width="24" height="7" fill="#4d5f70" />
    <rect x="34" y="29" width="32" height="8" fill="#405060" />
    <rect x="34" y="37" width="38" height="9" fill="#354452" />
    <rect x="34" y="46" width="42" height="8" fill="#2d3a46" />

    {/* Peak 2 (Lower right peak outline) */}
    <rect x="74" y="18" width="6" height="3" fill="var(--pixel-ink, #17140f)" />
    <rect x="70" y="21" width="14" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="66" y="25" width="22" height="5" fill="var(--pixel-ink, #17140f)" />
    <rect x="62" y="30" width="32" height="7" fill="var(--pixel-ink, #17140f)" />
    <rect x="58" y="37" width="42" height="9" fill="var(--pixel-ink, #17140f)" />
    <rect x="54" y="46" width="56" height="9" fill="var(--pixel-ink, #17140f)" />

    {/* Snowcap on Peak 2 */}
    <rect x="75" y="19" width="4" height="2" fill="#ffffff" />
    <rect x="72" y="21" width="9" height="3" fill="#ffffff" />
    <rect x="70" y="24" width="7" height="3" fill="#ffffff" />
    <rect x="77" y="24" width="5" height="3" fill="#cbd5e1" />

    {/* Mountain Body Peak 2 */}
    <rect x="68" y="26" width="5" height="4" fill="#9ab0c2" />
    <rect x="64" y="30" width="10" height="7" fill="#9ab0c2" />
    <rect x="60" y="37" width="14" height="9" fill="#889dae" />
    <rect x="56" y="46" width="18" height="8" fill="#768b9d" />
    <rect x="76" y="27" width="10" height="3" fill="#546677" />
    <rect x="74" y="30" width="18" height="7" fill="#465666" />
    <rect x="74" y="37" width="24" height="9" fill="#394856" />
    <rect x="74" y="46" width="34" height="8" fill="#2d3a46" />

    {/* Foothill Grass */}
    <rect x="0" y="52" width="112" height="4" fill="#388e3c" />
    <rect x="8" y="50" width="24" height="2" fill="#4caf50" />
    <rect x="60" y="50" width="30" height="2" fill="#4caf50" />
  </svg>
);

/**
 * PixelStream: 蜿蜒清澈的流水小溪。
 */
export const PixelStream: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 76 30"
    width="76"
    height="30"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Meadow banks */}
    <rect x="0" y="0" width="24" height="12" fill="#388e3c" />
    <rect x="0" y="12" width="18" height="18" fill="#2e7d32" />
    <rect x="52" y="0" width="24" height="10" fill="#388e3c" />
    <rect x="56" y="10" width="20" height="20" fill="#2e7d32" />

    {/* Highlights */}
    <rect x="0" y="0" width="22" height="2" fill="#4caf50" />
    <rect x="54" y="0" width="22" height="2" fill="#4caf50" />
    <rect x="18" y="12" width="2" height="16" fill="#1b5e20" />
    <rect x="54" y="10" width="2" height="18" fill="#1b5e20" />

    {/* Pebbles */}
    <rect x="16" y="6" width="3" height="3" fill="#78909c" />
    <rect x="17" y="7" width="1" height="1" fill="#cfd8dc" />
    <rect x="57" y="14" width="4" height="3" fill="#607d8b" />
    <rect x="58" y="15" width="2" height="1" fill="#b0bec5" />

    {/* Water Steps */}
    <rect x="24" y="0" width="28" height="8" fill="#1e88e5" />
    <rect x="22" y="8" width="34" height="8" fill="#1e88e5" />
    <rect x="19" y="16" width="36" height="7" fill="#1976d2" />
    <rect x="18" y="23" width="37" height="7" fill="#1565c0" />

    {/* Ripples & Foam */}
    <rect x="28" y="3" width="12" height="2" fill="#90caf9" />
    <rect x="42" y="5" width="6" height="2" fill="#ffffff" />
    <rect x="26" y="10" width="8" height="2" fill="#ffffff" />
    <rect x="36" y="11" width="14" height="2" fill="#90caf9" />
    <rect x="23" y="18" width="16" height="2" fill="#90caf9" />
    <rect x="42" y="19" width="10" height="2" fill="#ffffff" />
    <rect x="24" y="25" width="14" height="2" fill="#ffffff" />
    <rect x="40" y="26" width="12" height="2" fill="#90caf9" />
  </svg>
);

/**
 * PixelFence: 田园小木栅栏。
 */
export const PixelFence: FC<{ className?: string }> = ({ className }) => (
  <svg
    viewBox="0 0 44 20"
    width="44"
    height="20"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    shapeRendering="crispEdges"
    className={className}
    aria-hidden="true"
  >
    {/* Post 1 */}
    <rect x="4" y="2" width="4" height="17" fill="var(--pixel-ink, #17140f)" />
    <rect x="5" y="3" width="2" height="15" fill="#a1887f" />
    <rect x="5" y="2" width="2" height="1" fill="#d7ccc8" />

    {/* Post 2 */}
    <rect x="20" y="2" width="4" height="17" fill="var(--pixel-ink, #17140f)" />
    <rect x="21" y="3" width="2" height="15" fill="#a1887f" />
    <rect x="21" y="2" width="2" height="1" fill="#d7ccc8" />

    {/* Post 3 */}
    <rect x="36" y="2" width="4" height="17" fill="var(--pixel-ink, #17140f)" />
    <rect x="37" y="3" width="2" height="15" fill="#a1887f" />
    <rect x="37" y="2" width="2" height="1" fill="#d7ccc8" />

    {/* Upper rail */}
    <rect x="1" y="6" width="42" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="2" y="7" width="40" height="2" fill="#8d6e63" />
    <rect x="2" y="7" width="40" height="1" fill="#bcaaa4" />

    {/* Lower rail */}
    <rect x="1" y="12" width="42" height="4" fill="var(--pixel-ink, #17140f)" />
    <rect x="2" y="13" width="40" height="2" fill="#8d6e63" />
    <rect x="2" y="13" width="40" height="1" fill="#bcaaa4" />

    {/* Baseline */}
    <rect x="0" y="19" width="44" height="1" fill="var(--border-subtle)" />
  </svg>
);

/**
 * PixelAuthFlanks: 专为宽屏认证页（LoginPage / SetupPage）两侧宽阔留白区定制的左右田园景致翼。
 * 左翼：高山流水与山林牧场（远山 PixelMountain、松树 PixelTree、吃草小白羊 PixelSheep、花丛 PixelFlowerPatch）
 * 右翼：溪流田园与哨所农庄（小木屋 PixelHouse、瞭望塔 PixelWatchtower、木栅栏 PixelFence、奶牛 PixelCow、流水 PixelStream、营火 PixelCampfire）
 */
export const PixelAuthFlanks: FC = () => {
  return (
    <div className="auth-flanks" aria-hidden="true">
      {/* 左侧翼：高山、松林与吃草小羊 */}
      <aside className="auth-flank auth-flank--left">
        <div className="auth-flank__mountain">
          <PixelMountain />
        </div>
        <div className="auth-flank__cluster">
          <div className="auth-flank__tree">
            <PixelTree />
          </div>
          <div className="auth-flank__animals">
            <PixelSheep variant="grazing" facing="left" />
            <PixelSheep variant="standing" facing="right" />
          </div>
          <div className="auth-flank__patch">
            <PixelFlowerPatch />
          </div>
        </div>
      </aside>

      {/* 右侧翼：小溪流水、木屋、木栅栏与奶牛 */}
      <aside className="auth-flank auth-flank--right">
        <div className="auth-flank__buildings">
          <PixelWatchtower />
          <PixelHouse />
        </div>
        <div className="auth-flank__cluster">
          <div className="auth-flank__stream">
            <PixelStream />
          </div>
          <div className="auth-flank__animals">
            <PixelFence />
            <PixelCow facing="left" />
          </div>
          <div className="auth-flank__fire">
            <PixelCampfire />
          </div>
        </div>
      </aside>
    </div>
  );
};
