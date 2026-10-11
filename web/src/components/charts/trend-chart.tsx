import { useMemo } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import type { TimeSeriesPoint } from "@/api/types";
import { fmtAxisTime, fmtNumber, fmtTs } from "@/lib/time";

/** Single-series area chart (no legend: the title names the series). */
export function TrendChart({ points, height = 140, color = "var(--lvl-error)", className, label = "events" }: { points?: TimeSeriesPoint[]; height?: number; color?: string; className?: string; label?: string }) {
  const rows = useMemo(() => (points ?? []).map((p) => ({ ts: p.ts, t: new Date(p.ts).getTime(), count: p.count })), [points]);
  const spanMs = rows.length > 1 ? rows[rows.length - 1].t - rows[0].t : 0;
  const total = rows.reduce((n, r) => n + r.count, 0);
  return (
    <div className={clsx("relative", className)} style={{ height }}>
      {rows.length > 0 && total === 0 && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-ink-3">No {label} in this range</div>}
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="trend-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.25} />
              <stop offset="100%" stopColor={color} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="ts" tickFormatter={(v: string) => fmtAxisTime(new Date(v), spanMs)} minTickGap={48} tickLine={false} axisLine={{ stroke: "var(--border-strong)" }} height={18} />
          <YAxis width={40} tickLine={false} axisLine={false} tickFormatter={(v: number) => fmtNumber(v)} allowDecimals={false} />
          <Tooltip
            isAnimationActive={false}
            cursor={{ stroke: "var(--border-strong)" }}
            content={({ active, payload }) => {
              if (!active || !payload?.length) return null;
              const r = payload[0].payload as { ts: string; count: number };
              return (
                <div className="rounded-md border border-border bg-surface px-2.5 py-1.5 text-xs text-ink shadow-[var(--shadow)]">
                  <div className="text-ink-3 tabular">{fmtTs(r.ts)}</div>
                  <div className="font-medium tabular">
                    {r.count.toLocaleString()} {label}
                  </div>
                </div>
              );
            }}
          />
          <Area type="monotone" dataKey="count" stroke={color} strokeWidth={2} fill="url(#trend-fill)" isAnimationActive={false} dot={false} activeDot={{ r: 4, stroke: "var(--surface)", strokeWidth: 2 }} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
