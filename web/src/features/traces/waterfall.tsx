import { useMemo, useRef, useState, type CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import clsx from "clsx";
import { AlertTriangle, ChevronDown, ChevronRight } from "lucide-react";
import type { Span } from "@/api/types";
import { SERIES_VARS } from "@/components/charts/palette";
import { fmtUs, layoutSpans, rulerTicks, visibleRows, type WaterfallRow } from "./waterfall-layout";

export interface WaterfallProps {
  spans: Span[];
  selectedId?: string;
  onSelect?: (span: Span) => void;
  className?: string;
  /** Services in color order (the page shows the same order in its legend). */
  services: string[];
}

const ROW_H = 26;
const LEFT_W = 320;

export function serviceColor(services: string[], service: string): string {
  const i = services.indexOf(service);
  return SERIES_VARS[(i < 0 ? services.length : i) % SERIES_VARS.length];
}

/**
 * Trace waterfall as plain HTML rows (virtualized): a name column with collapse toggles and
 * a track column where each span is a positioned bar, colored by service, red when it failed.
 */
export function Waterfall({ spans, selectedId, onSelect, className, services }: WaterfallProps) {
  const rows = useMemo(() => layoutSpans(spans), [spans]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const visible = useMemo(() => visibleRows(rows, collapsed), [rows, collapsed]);
  const totalUs = useMemo(() => Math.max(1, ...rows.map((r) => r.endUs)), [rows]);
  const ticks = useMemo(() => rulerTicks(totalUs), [totalUs]);
  const parentRef = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({ count: visible.length, getScrollElement: () => parentRef.current, estimateSize: () => ROW_H, overscan: 20 });
  const toggle = (id: string) =>
    setCollapsed((c) => {
      const n = new Set(c);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const grid: CSSProperties = { gridTemplateColumns: `${LEFT_W}px 1fr` };

  return (
    <div className={clsx("flex min-h-0 flex-col", className)}>
      <div className="grid flex-none border-b border-border bg-surface-2 text-[11px] text-ink-3" style={grid}>
        <div className="flex items-center justify-between px-2 py-1">
          <span>
            {rows.length} spans{collapsed.size > 0 && ` · ${rows.length - visible.length} hidden`}
          </span>
          {collapsed.size > 0 && (
            <button type="button" className="text-accent hover:underline" onClick={() => setCollapsed(new Set())}>
              expand all
            </button>
          )}
        </div>
        <div className="relative h-6 border-l border-border">
          {ticks.map((t, i) => (
            <span key={i} className={clsx("absolute top-1 tabular", i === ticks.length - 1 ? "-translate-x-full pr-1" : "pl-1")} style={{ left: `${t.pct}%` }}>
              {t.label}
            </span>
          ))}
        </div>
      </div>
      <div ref={parentRef} className="min-h-0 flex-1 overflow-auto">
        <div style={{ height: v.getTotalSize(), position: "relative" }}>
          {v.getVirtualItems().map((vi) => {
            const r = visible[vi.index];
            return <Row key={r.span.span_id} row={r} top={vi.start} grid={grid} selected={r.span.span_id === selectedId} collapsed={collapsed.has(r.span.span_id)} onToggle={() => toggle(r.span.span_id)} onSelect={() => onSelect?.(r.span)} color={serviceColor(services, r.span.service)} ticks={ticks} />;
          })}
        </div>
      </div>
    </div>
  );
}

function Row({ row, top, grid, selected, collapsed, onToggle, onSelect, color, ticks }: { row: WaterfallRow; top: number; grid: CSSProperties; selected: boolean; collapsed: boolean; onToggle: () => void; onSelect: () => void; color: string; ticks: { pct: number }[] }) {
  const s = row.span;
  const failed = s.status === "error";
  const label = s.http_method && s.http_route ? `${s.http_method} ${s.http_route}` : s.name;
  return (
    <div
      role="row"
      aria-selected={selected}
      className={clsx("absolute left-0 right-0 grid cursor-pointer border-b border-border text-xs hover:bg-surface-2", selected && "bg-accent-soft")}
      style={{ ...grid, top, height: ROW_H }}
      onClick={onSelect}
    >
      <div className="flex min-w-0 items-center gap-1 pr-2" style={{ paddingLeft: 6 + row.depth * 14 }}>
        {row.children > 0 ? (
          <button
            type="button"
            className="flex h-4 w-4 flex-none items-center justify-center rounded text-ink-3 hover:bg-surface-3 hover:text-ink"
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
            aria-label={collapsed ? `Expand ${row.descendants} spans` : "Collapse"}
            title={collapsed ? `Expand ${row.descendants} spans` : "Collapse"}
          >
            {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
          </button>
        ) : (
          <span className="inline-block h-4 w-4 flex-none" />
        )}
        <span className="inline-block h-2.5 w-2.5 flex-none rounded-sm" style={{ background: color }} aria-hidden />
        <span className={clsx("min-w-0 flex-1 truncate font-mono", failed ? "text-critical" : "text-ink")} title={`${s.service} · ${s.name}`}>
          {label}
        </span>
        {collapsed && row.descendants > 0 && <span className="flex-none rounded bg-surface-3 px-1 text-[10px] tabular text-ink-2">+{row.descendants}</span>}
        {row.hasError && !failed && <AlertTriangle size={11} className="flex-none text-serious" aria-label="A child span failed" />}
        <span className="flex-none text-[10px] text-ink-3">{s.service}</span>
      </div>
      <div className="relative border-l border-border">
        {ticks.slice(1, -1).map((t, i) => (
          <span key={i} className="absolute bottom-0 top-0 w-px bg-grid" style={{ left: `${t.pct}%` }} aria-hidden />
        ))}
        <span
          className={clsx("absolute top-[6px] h-[14px] rounded-sm", failed && "ring-1 ring-[var(--lvl-error)]")}
          style={{ left: `${row.left}%`, width: `${row.width}%`, background: failed ? "var(--lvl-error)" : color, opacity: failed ? 0.9 : 0.85 }}
          title={`${label}: ${fmtUs(s.duration_us)} (starts at +${fmtUs(row.startUs)})`}
        />
        <DurationLabel row={row} />
      </div>
    </div>
  );
}

/**
 * The span's duration sits right after its bar; when the bar reaches the right edge the label
 * moves before the bar, and a bar that fills the track carries the label inside its end.
 */
function DurationLabel({ row }: { row: WaterfallRow }) {
  const end = row.left + row.width;
  const text = fmtUs(row.span.duration_us);
  const base = "pointer-events-none absolute top-[5px] whitespace-nowrap text-[10px] tabular";
  if (end <= 85) return <span className={clsx(base, "text-ink-2")} style={{ left: `${end}%`, paddingLeft: 4 }}>{text}</span>;
  if (row.left >= 15) return <span className={clsx(base, "text-ink-2")} style={{ right: `${100 - row.left}%`, paddingRight: 4 }}>{text}</span>;
  return (
    <span className={clsx(base, "font-medium text-white [text-shadow:0_0_2px_rgba(0,0,0,.45)]")} style={{ right: `${Math.max(0, 100 - end)}%`, paddingRight: 4 }}>
      {text}
    </span>
  );
}
