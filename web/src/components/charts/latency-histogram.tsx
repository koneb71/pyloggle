import { useMemo } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import type { LatencyBucket } from "@/api/types";
import { fmtNumber } from "@/lib/time";
import { fmtUs } from "@/features/traces/waterfall-layout";

/**
 * Distribution of entry-span durations over power-of-two buckets: one series (no legend),
 * thin bars with a surface gap, categorical x axis labeled with the bucket's lower edge, and a
 * per-bar hover tooltip naming the range.
 */
export function LatencyHistogram({ buckets, height = 140, loading, className, color = "var(--accent)" }: { buckets?: LatencyBucket[]; height?: number; loading?: boolean; className?: string; color?: string }) {
  const rows = useMemo(() => (buckets ?? []).map((b) => ({ label: fmtUs(b.from_us), from: b.from_us, to: b.to_us, count: b.count })), [buckets]);
  const total = rows.reduce((n, r) => n + r.count, 0);
  const empty = !loading && total === 0;
  return (
    <div className={clsx("relative", className)} style={{ height }}>
      {empty && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-ink-3">No traces in this range</div>}
      {loading && rows.length === 0 && <div className="skeleton absolute inset-x-8 bottom-6 top-2" />}
      {rows.length > 0 && (
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={rows} margin={{ top: 6, right: 8, bottom: 0, left: 0 }} barCategoryGap={2}>
            <CartesianGrid vertical={false} strokeDasharray="0" />
            <XAxis dataKey="label" tickLine={false} axisLine={{ stroke: "var(--border-strong)" }} height={18} minTickGap={12} interval="preserveStartEnd" />
            <YAxis width={40} tickLine={false} axisLine={false} tickFormatter={(v: number) => fmtNumber(v)} allowDecimals={false} />
            <Tooltip
              cursor={{ fill: "color-mix(in oklab, var(--text) 6%, transparent)" }}
              isAnimationActive={false}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const r = payload[0].payload as (typeof rows)[number];
                return (
                  <div className="rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs text-ink shadow-[var(--shadow)]">
                    <div className="font-semibold tabular">{r.count.toLocaleString()} traces</div>
                    <div className="text-ink-2 tabular">
                      {fmtUs(r.from)} – {fmtUs(r.to)}
                    </div>
                  </div>
                );
              }}
            />
            <Bar dataKey="count" fill={color} stroke="var(--surface)" strokeWidth={1} isAnimationActive={false} radius={[2, 2, 0, 0]} maxBarSize={32} />
          </BarChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}
