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
