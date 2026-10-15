/** One group's share of the whole: a thin bar and its percentage, for breakdown tables. */
export function ShareBar({ pct, color = "var(--series-1)", label }: { pct: number; color?: string; label?: string }) {
  const clamped = Math.max(0, Math.min(100, pct));
  return (
    <span className="flex w-full items-center gap-2" title={label}>
      <span className="relative flex h-1.5 flex-1 rounded-full bg-surface-3" aria-hidden>
        <span className="h-1.5 rounded-full" style={{ width: `${Math.max(1, clamped)}%`, background: color }} />
      </span>
      <span className="w-12 text-right text-xs tabular text-ink-3">{clamped.toFixed(1)}%</span>
    </span>
  );
}
