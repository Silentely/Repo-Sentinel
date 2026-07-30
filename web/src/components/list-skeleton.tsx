/** 列表页通用骨架屏，加载中占位。 */
export function ListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <ul className="skeleton-list" aria-label="加载中" role="status">
      {Array.from({ length: rows }, (_, i) => (
        <li key={i} className="skeleton-row">
          <span className="skeleton-row__badge" />
          <span className="skeleton-row__text skeleton-row__text--lg" />
          <span className="skeleton-row__text skeleton-row__text--sm" />
          <span className="skeleton-row__text skeleton-row__text--xs" />
        </li>
      ))}
    </ul>
  );
}
