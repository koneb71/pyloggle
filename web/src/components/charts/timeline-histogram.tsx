import { useMemo, useState } from "react";
import { Bar, BarChart, CartesianGrid, ReferenceArea, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import type { Histogram } from "@/api/types";
import { fmtAxisNumber, fmtAxisTime, fmtWhen } from "@/lib/time";
import { orderSeries, seriesColor, seriesLabel } from "./palette";
import { ChartLegend } from "./legend";

export interface TimelineHistogramProps {
  data?: Histogram;
  loading?: boolean;
  height?: number;
  onZoom?: (from: Date, to: Date) => void;
  onBucketClick?: (from: Date, to: Date) => void;
  className?: string;
  /** Hide the legend (e.g. when the legend is rendered elsewhere). */
  hideLegend?: boolean;
}

interface Row {
  ts: string;
  t: number;
  total: number;
  [series: string]: number | string;
}

/**
 * Stacked timeline (Datadog-style). One axis, thin bars with a surface gap between segments,
 * fixed series order and colors, hover tooltip, legend for 2+ series and drag-to-zoom.
 */
export function TimelineHistogram({ data, loading, height = 160, onZoom, onBucketClick, className, hideLegend }: TimelineHistogramProps) {
  const groupBy = data?.group_by ?? "level";
  const { kept, folded } = useMemo(() => orderSeries(groupBy, data?.series ?? []), [groupBy, data?.series]);
  const rows = useMemo<Row[]>(() => {
    if (!data) return [];
    return data.buckets.map((b) => {
      const r: Row = { ts: b.ts, t: new Date(b.ts).getTime(), total: b.total };
      for (const s of kept) r[s] = s === "Other" ? folded.reduce((n, f) => n + (b.counts[f] ?? 0), 0) : (b.counts[s] ?? 0);
      return r;
    });
  }, [data, kept, folded]);
  const spanMs = rows.length > 1 ? rows[rows.length - 1].t - rows[0].t : 0;
  const bucketMs = (data?.bucket_seconds ?? 60) * 1000;

  const [drag, setDrag] = useState<{ a: string; b: string } | null>(null);
  const finishDrag = () => {
    if (drag && drag.a !== drag.b && onZoom) {
      const ta = new Date(drag.a).getTime();
      const tb = new Date(drag.b).getTime();
      const from = new Date(Math.min(ta, tb));
      const to = new Date(Math.max(ta, tb) + bucketMs);
      onZoom(from, to);
    } else if (drag && drag.a === drag.b && onBucketClick) {
      const t = new Date(drag.a).getTime();
      onBucketClick(new Date(t), new Date(t + bucketMs));
    }
    setDrag(null);
  };
  const total = rows.reduce((n, r) => n + r.total, 0);
  const empty = !loading && (!data || total === 0);

  return (
    <div className={clsx("select-none", className)}>
      {!hideLegend && data && <ChartLegend items={kept.map((s, i) => ({ name: seriesLabel(groupBy, s), color: seriesColor(groupBy, s, i) }))} bucketSeconds={data.bucket_seconds} />}
      <div className="relative" style={{ height }}>
        {empty && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-[13px] text-ink-3">Nothing in this time range</div>}
        {loading && !data && <div className="skeleton absolute inset-x-8 bottom-6 top-2" />}
        {data && (
          <ResponsiveContainer width="100%" height="100%">
            <BarChart
              data={rows}
              margin={{ top: 6, right: 8, bottom: 0, left: 0 }}
              barCategoryGap={1}
              onMouseDown={(s) => s?.activeLabel !== undefined && setDrag({ a: String(s.activeLabel), b: String(s.activeLabel) })}
              onMouseMove={(s) => {
                if (s?.activeLabel !== undefined && drag) setDrag({ ...drag, b: String(s.activeLabel) });
              }}
              onMouseUp={finishDrag}
              onMouseLeave={() => {
                if (drag) finishDrag();
              }}
              style={{ cursor: onZoom ? "crosshair" : "default" }}
            >
              <CartesianGrid vertical={false} strokeDasharray="0" />
              <XAxis dataKey="ts" tickFormatter={(v: string) => fmtAxisTime(new Date(v), spanMs)} minTickGap={48} tickLine={false} axisLine={{ stroke: "var(--border-strong)" }} height={18} />
              <YAxis width={44} tickLine={false} axisLine={false} tickFormatter={(v: number) => fmtAxisNumber(v)} allowDecimals={false} />
              <Tooltip
                cursor={{ fill: "color-mix(in oklab, var(--text) 6%, transparent)" }}
                isAnimationActive={false}
                content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const r = payload[0].payload as Row;
                  return <HistogramTooltip row={r} series={kept} groupBy={groupBy} bucketSeconds={data.bucket_seconds} />;
                }}
              />
              {kept.map((s, i) => (
                <Bar key={s} dataKey={s} stackId="a" fill={seriesColor(groupBy, s, i)} stroke="var(--surface)" strokeWidth={1} isAnimationActive={false} maxBarSize={28} radius={i === kept.length - 1 ? [2, 2, 0, 0] : 0} />
              ))}
              {drag && drag.a !== drag.b && <ReferenceArea x1={drag.a} x2={drag.b} fill="var(--accent)" fillOpacity={0.15} stroke="var(--accent)" strokeOpacity={0.6} />}
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}

function HistogramTooltip({ row, series, groupBy, bucketSeconds }: { row: Row; series: string[]; groupBy: string; bucketSeconds: number }) {
  const end = new Date(row.t + bucketSeconds * 1000);
  return (
    <div className="rounded-md border border-border bg-surface px-2.5 py-2 text-xs text-ink shadow-[var(--shadow)]">
      <div className="mb-1 text-ink-3 tabular">
        {fmtWhen(row.ts, { seconds: bucketSeconds < 60 })} – {fmtWhen(end, { seconds: bucketSeconds < 60 }).replace(/^(Today|Yesterday) /, "")}
      </div>
      <table className="tabular">
        <tbody>
          {series
            .filter((s) => (row[s] as number) > 0)
            .map((s, i) => (
              <tr key={s}>
                <td className="pr-2">
                  <span className="mr-1.5 inline-block h-2 w-2 rounded-sm align-middle" style={{ background: seriesColor(groupBy, s, series.indexOf(s) >= 0 ? series.indexOf(s) : i) }} />
                  {seriesLabel(groupBy, s)}
                </td>
                <td className="text-right font-medium">{(row[s] as number).toLocaleString()}</td>
              </tr>
            ))}
          <tr className="border-t border-border">
            <td className="pr-2 pt-0.5 text-ink-2">Total</td>
            <td className="pt-0.5 text-right font-semibold">{row.total.toLocaleString()}</td>
          </tr>
        </tbody>
      </table>
    </div>
  );
}
