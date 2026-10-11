import { useMemo } from "react";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import clsx from "clsx";
import { bucketLabel, fmtAxisTime, fmtNumber, fmtTs } from "@/lib/time";
import { SERIES_VARS } from "./palette";

export interface LineSeries {
  name: string;
  values: (number | null)[];
  /** CSS color; defaults to the categorical slot in fixed order. */
  color?: string;
}

export interface MultiLineChartProps {
  buckets: string[];
  series: LineSeries[];
  bucketSeconds?: number;
  height?: number;
  format?: (v: number) => string;
  loading?: boolean;
  className?: string;
  emptyText?: string;
  /** Hide the legend (e.g. the caller renders one). */
  hideLegend?: boolean;
}

interface Row {
  ts: string;
  t: number;
  [series: string]: number | string | null;
}

export function lineColor(s: LineSeries, i: number): string {
  if (s.color) return s.color;
  if (s.name === "other" || s.name === "Other") return "var(--lvl-other)";
  return SERIES_VARS[i % SERIES_VARS.length];
}

/**
 * Multi-series line chart: one axis, 2px lines in fixed categorical order, gaps where a bucket
 * has no value, a crosshair tooltip that lists every series at that X, and a legend for 2+
 * series. Callers keep it to 8 series (fold the rest into "other").
 */
export function MultiLineChart({ buckets, series, bucketSeconds = 60, height = 180, format = fmtNumber, loading, className, emptyText = "No data in this range", hideLegend }: MultiLineChartProps) {
  const rows = useMemo<Row[]>(
    () =>
      buckets.map((ts, i) => {
        const r: Row = { ts, t: new Date(ts).getTime() };
        for (const s of series) r[s.name] = s.values[i] ?? null;
        return r;
      }),
    [buckets, series],
  );
  const spanMs = rows.length > 1 ? rows[rows.length - 1].t - rows[0].t : 0;
  const hasData = series.some((s) => s.values.some((v) => v !== null && v !== undefined));
  const empty = !loading && !hasData;

  return (
    <div className={clsx("relative", className)} style={{ height }}>
      {empty && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-xs text-ink-3">{emptyText}</div>}
      {loading && !hasData && <div className="skeleton absolute inset-x-8 bottom-6 top-2" />}
      {rows.length > 0 && (
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={rows} margin={{ top: 8, right: 12, bottom: 0, left: 0 }}>
            <CartesianGrid vertical={false} strokeDasharray="0" />
            <XAxis dataKey="ts" tickFormatter={(v: string) => fmtAxisTime(new Date(v), spanMs)} minTickGap={48} tickLine={false} axisLine={{ stroke: "var(--border-strong)" }} height={18} />
            <YAxis width={46} tickLine={false} axisLine={false} tickFormatter={(v: number) => format(v)} domain={[0, "auto"]} />
            <Tooltip
              isAnimationActive={false}
              cursor={{ stroke: "var(--border-strong)" }}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const r = payload[0].payload as Row;
                return <LineTooltip row={r} series={series} format={format} bucketSeconds={bucketSeconds} />;
              }}
            />
            {series.map((s, i) => (
              <Line
                key={s.name}
                dataKey={s.name}
                type="monotone"
                stroke={lineColor(s, i)}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4, stroke: "var(--surface)", strokeWidth: 2, fill: lineColor(s, i) }}
                connectNulls={false}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      )}
      {!hideLegend && series.length >= 2 && (
        <div className="pointer-events-none absolute right-2 top-0 flex flex-wrap justify-end gap-x-3 gap-y-0.5 text-[11px] text-ink-2">
          {series.map((s, i) => (
            <span key={s.name} className="inline-flex items-center gap-1">
              <span className="inline-block h-0.5 w-3 rounded-sm" style={{ background: lineColor(s, i) }} aria-hidden />
              <span className="max-w-40 truncate">{s.name === "" ? "(empty)" : s.name}</span>
            </span>
          ))}
          <span className="text-ink-3">· {bucketLabel(bucketSeconds)} buckets</span>
        </div>
      )}
    </div>
  );
}

function LineTooltip({ row, series, format, bucketSeconds }: { row: Row; series: LineSeries[]; format: (v: number) => string; bucketSeconds: number }) {
  return (
    <div className="rounded-md border border-border bg-surface px-2.5 py-2 text-xs text-ink shadow-[var(--shadow)]">
      <div className="mb-1 text-ink-3 tabular">{fmtTs(row.ts, { seconds: bucketSeconds < 60 })}</div>
      <table className="tabular">
        <tbody>
          {series.map((s, i) => {
            const v = row[s.name];
            return (
              <tr key={s.name}>
                <td className="pr-3 text-right font-semibold">{typeof v === "number" ? format(v) : "–"}</td>
                <td className="text-ink-2">
                  <span className="mr-1.5 inline-block h-0.5 w-3 align-middle" style={{ background: lineColor(s, i) }} />
                  {s.name === "" ? "(empty)" : s.name}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
