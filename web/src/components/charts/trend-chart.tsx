import { useMemo } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import type { TimeSeriesPoint } from "@/api/types";
import { fmtAxisNumber, fmtAxisTime, fmtWhen } from "@/lib/time";

/**
 * One series over time, no legend (the title names it). "bars" suits counts per bucket (how
 * many times something happened); "area" suits a continuous rate.
 */
export function TrendChart({ points, height = 140, color = "var(--lvl-error)", className, label = "events", variant = "area" }: { points?: TimeSeriesPoint[]; height?: number; color?: string; className?: string; label?: string; variant?: "area" | "bars" }) {
  const rows = useMemo(() => (points ?? []).map((p) => ({ ts: p.ts, t: new Date(p.ts).getTime(), count: p.count })), [points]);
  const spanMs = rows.length > 1 ? rows[rows.length - 1].t - rows[0].t : 0;
  const total = rows.reduce((n, r) => n + r.count, 0);
  const axes = (
    <>
      <CartesianGrid vertical={false} />
      <XAxis dataKey="ts" tickFormatter={(v: string) => fmtAxisTime(new Date(v), spanMs)} minTickGap={56} tickLine={false} axisLine={{ stroke: "var(--border-strong)" }} height={20} />
      <YAxis width={44} tickLine={false} axisLine={false} tickFormatter={(v: number) => fmtAxisNumber(v)} allowDecimals={false} />
      <Tooltip
        isAnimationActive={false}
        cursor={variant === "bars" ? { fill: "color-mix(in oklab, var(--text) 6%, transparent)" } : { stroke: "var(--border-strong)" }}
        content={({ active, payload }) => {
          if (!active || !payload?.length) return null;
          const r = payload[0].payload as { ts: string; count: number };
          return (
            <div className="rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs text-ink shadow-[var(--shadow)]">
              <div className="text-ink-3 tabular">{fmtWhen(r.ts)}</div>
              <div className="font-medium tabular">
                {r.count.toLocaleString()} {label}
              </div>
            </div>
          );
        }}
      />
    </>
  );
  return (
    <div className={clsx("relative", className)} style={{ height }}>
      {rows.length > 0 && total === 0 && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-[13px] text-ink-3">Nothing in this time range</div>}
      <ResponsiveContainer width="100%" height="100%">
        {variant === "bars" ? (
          <BarChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 0 }} barCategoryGap={1}>
            {axes}
            <Bar dataKey="count" fill={color} radius={[2, 2, 0, 0]} isAnimationActive={false} />
          </BarChart>
        ) : (
          <AreaChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
            <defs>
              <linearGradient id="trend-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity={0.25} />
                <stop offset="100%" stopColor={color} stopOpacity={0.02} />
              </linearGradient>
            </defs>
            {axes}
            <Area type="monotone" dataKey="count" stroke={color} strokeWidth={2} fill="url(#trend-fill)" isAnimationActive={false} dot={false} activeDot={{ r: 4, stroke: "var(--surface)", strokeWidth: 2 }} />
          </AreaChart>
        )}
      </ResponsiveContainer>
    </div>
  );
}
