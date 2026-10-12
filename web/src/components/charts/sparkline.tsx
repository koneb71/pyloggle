/** Tiny inline bar sparkline for list rows. Pure SVG, no axes; the row text carries the numbers. */
export function Sparkline({ values, width = 96, height = 20, color = "var(--lvl-error)", title }: { values?: number[]; width?: number; height?: number; color?: string; title?: string }) {
  const v = values ?? [];
  const max = Math.max(1, ...v);
  const n = Math.max(1, v.length);
  const gap = 1;
  const bw = Math.max(1, (width - gap * (n - 1)) / n);
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={title ?? "trend"} className="block">
      {title && <title>{title}</title>}
      <line x1={0} x2={width} y1={height - 0.5} y2={height - 0.5} stroke="var(--grid)" />
      {v.map((x, i) => {
        const h = x > 0 ? Math.max(1.5, (x / max) * (height - 2)) : 0;
        return <rect key={i} x={i * (bw + gap)} y={height - 1 - h} width={bw} height={h} fill={color} rx={0.5} opacity={x > 0 ? 1 : 0} />;
      })}
    </svg>
  );
}

/**
 * A small area trend for stat tiles: a 1.5px line over a faint fill, stretched to the tile's
 * width. No axes; the tile's number and delta carry the reading.
 */
export function MiniTrend({ values, height = 28, color = "var(--series-1)", title }: { values: number[]; height?: number; color?: string; title?: string }) {
  const n = values.length;
  if (n < 2) return null;
  const w = 100;
  const max = Math.max(1, ...values);
  const pts = values.map((v, i) => [(i / (n - 1)) * w, height - 1 - (v / max) * (height - 3)] as const);
  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(2)},${y.toFixed(2)}`).join("");
  return (
    <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" role="img" aria-label={title ?? "trend"} className="block w-full" style={{ height }}>
      {title && <title>{title}</title>}
      <path d={`${line}L${w},${height}L0,${height}Z`} fill={color} opacity={0.12} />
      <path d={line} fill="none" stroke={color} strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}
