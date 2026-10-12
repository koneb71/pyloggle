import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import clsx from "clsx";
import { AlertTriangle, ChevronDown, ChevronRight } from "lucide-react";
import type { Span } from "@/api/types";
import { SERIES_VARS } from "@/components/charts/palette";
import { useElementWidth, useSmallScreen } from "@/lib/use-media";
import { fmtUs, layoutSpans, rulerTicks, visibleRows, type WaterfallRow } from "./waterfall-layout";

export interface WaterfallProps {
  spans: Span[];
  selectedId?: string;
  onSelect?: (span: Span) => void;
  className?: string;
  /** Services in color order (the page shows the same order in its legend). */
  services: string[];
}

const ROW_H = 28;
const LEFT_W = 340;
/** On a narrow screen each step takes two lines: its name, then its bar across the full width. */
const STACKED_ROW_H = 46;

export function serviceColor(services: string[], service: string): string {
  const i = services.indexOf(service);
  return SERIES_VARS[(i < 0 ? services.length : i) % SERIES_VARS.length];
}

/**
 * Trace waterfall as plain HTML rows (virtualized): a name column with collapse toggles and
 * a track column where each span is a positioned bar, colored by service, red when it failed.
 * On a phone the name sits above the bar, so the bars keep the full width of the screen.
 */
export function Waterfall({ spans, selectedId, onSelect, className, services }: WaterfallProps) {
  const box = useRef<HTMLDivElement>(null);
  const width = useElementWidth(box);
  const small = useSmallScreen();
  // The name column takes 340px; below 600px the bars would be too short to compare.
  const stacked = width > 0 ? width < LEFT_W + 260 : small;
  const rowH = stacked ? STACKED_ROW_H : ROW_H;
  const rows = useMemo(() => layoutSpans(spans), [spans]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const visible = useMemo(() => visibleRows(rows, collapsed), [rows, collapsed]);
  const totalUs = useMemo(() => Math.max(1, ...rows.map((r) => r.endUs)), [rows]);
  const ticks = useMemo(() => rulerTicks(totalUs), [totalUs]);
  const parentRef = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({ count: visible.length, getScrollElement: () => parentRef.current, estimateSize: () => rowH, overscan: 20 });
  useEffect(() => v.measure(), [rowH, v]);
  const toggle = (id: string) =>
    setCollapsed((c) => {
      const n = new Set(c);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const grid: CSSProperties = { gridTemplateColumns: `${LEFT_W}px 1fr` };

  const stepCount = (
    <div className={clsx("flex items-center justify-between px-3", stacked ? "pt-1.5" : "py-1.5")}>
      <span>
        {rows.length} steps{collapsed.size > 0 && ` · ${rows.length - visible.length} hidden`}
      </span>
      {collapsed.size > 0 && (
        <button type="button" className="font-medium text-accent hover:underline" onClick={() => setCollapsed(new Set())}>
          Show all
        </button>
      )}
    </div>
  );
  // A phone has room for every other label (always keeping the last one, the request's length).
  const ruler = ticks.map((t, i) =>
    stacked && i % 2 === 1 && i !== ticks.length - 1 ? null : (
      <span key={i} className={clsx("absolute top-1 tabular", i === ticks.length - 1 ? "-translate-x-full pr-1" : "pl-1")} style={{ left: `${t.pct}%` }}>
        {t.label}
      </span>
    ),
  );

  return (
    <div ref={box} className={clsx("flex min-h-0 flex-col", className)}>
      {stacked ? (
        <div className="flex-none border-b border-border bg-surface-2 text-xs text-ink-3">
          {stepCount}
          <div className="relative mx-3 h-6">{ruler}</div>
        </div>
      ) : (
        <div className="grid flex-none border-b border-border bg-surface-2 text-xs text-ink-3" style={grid}>
          {stepCount}
          <div className="relative h-6 border-l border-border">{ruler}</div>
        </div>
      )}
      <div ref={parentRef} className="min-h-0 flex-1 overflow-auto">
        <div style={{ height: v.getTotalSize(), position: "relative" }}>
          {v.getVirtualItems().map((vi) => {
            const r = visible[vi.index];
            const props = { row: r, top: vi.start, selected: r.span.span_id === selectedId, collapsed: collapsed.has(r.span.span_id), onToggle: () => toggle(r.span.span_id), onSelect: () => onSelect?.(r.span), color: serviceColor(services, r.span.service), ticks };
            return stacked ? <StackedRow key={r.span.span_id} {...props} /> : <Row key={r.span.span_id} {...props} grid={grid} />;
          })}
        </div>
      </div>
    </div>
  );
}

function Row({ row, top, grid, selected, collapsed, onToggle, onSelect, color, ticks }: RowProps & { grid: CSSProperties }) {
  const s = row.span;
  const failed = s.status === "error";
  const label = s.http_method && s.http_route ? `${s.http_method} ${s.http_route}` : s.name;
  return (
    <div
      role="row"
      aria-selected={selected}
      className={clsx("absolute left-0 right-0 grid cursor-pointer border-b border-border text-[13px] hover:bg-surface-2", selected && "bg-accent-soft hover:bg-accent-soft")}
      style={{ ...grid, top, height: ROW_H }}
      onClick={onSelect}
    >
      <div className="flex min-w-0 items-center gap-1 pr-2" style={{ paddingLeft: 6 + row.depth * 14 }}>
        <Toggle row={row} collapsed={collapsed} onToggle={onToggle} />
        <span className="inline-block h-2.5 w-2.5 flex-none rounded-sm" style={{ background: color }} aria-hidden />
        <span className={clsx("min-w-0 flex-1 truncate", failed ? "font-medium text-critical-text" : "text-ink")} title={`${s.service} · ${s.name}${failed ? " · failed" : ""}`}>
          {label}
        </span>
        {collapsed && row.descendants > 0 && <span className="flex-none rounded bg-surface-3 px-1 text-[11px] tabular text-ink-2">+{row.descendants}</span>}
        {row.hasError && !failed && <AlertTriangle size={12} className="flex-none text-warning-text" aria-label="A step inside failed" />}
        <span className="flex-none text-[11px] text-ink-3">{s.service}</span>
      </div>
      <div className="relative border-l border-border">
        {ticks.slice(1, -1).map((t, i) => (
          <span key={i} className="absolute bottom-0 top-0 w-px bg-grid" style={{ left: `${t.pct}%` }} aria-hidden />
        ))}
        <span
          className={clsx("absolute top-[7px] h-[14px] rounded-sm", failed && "ring-1 ring-[var(--lvl-error)]")}
          style={{ left: `${row.left}%`, width: `${row.width}%`, background: failed ? "var(--lvl-error)" : color, opacity: failed ? 0.9 : 0.85 }}
          title={`${label}: took ${fmtUs(s.duration_us)}, started ${fmtUs(row.startUs)} into the request`}
        />
        <DurationLabel row={row} />
      </div>
    </div>
  );
}

type RowProps = { row: WaterfallRow; top: number; selected: boolean; collapsed: boolean; onToggle: () => void; onSelect: () => void; color: string; ticks: { pct: number }[] };

/** A step on a phone: the name line (indented by depth, with the duration at the end), then the bar. */
function StackedRow({ row, top, selected, collapsed, onToggle, onSelect, color, ticks }: RowProps) {
  const s = row.span;
  const failed = s.status === "error";
  const label = s.http_method && s.http_route ? `${s.http_method} ${s.http_route}` : s.name;
  return (
    <div
      role="row"
      aria-selected={selected}
      className={clsx("absolute left-0 right-0 cursor-pointer border-b border-border px-3 text-[13px] hover:bg-surface-2", selected && "bg-accent-soft hover:bg-accent-soft")}
      style={{ top, height: STACKED_ROW_H }}
      onClick={onSelect}
    >
      <div className="flex h-7 min-w-0 items-center gap-1" style={{ paddingLeft: Math.min(row.depth, 8) * 10 }}>
        <Toggle row={row} collapsed={collapsed} onToggle={onToggle} />
        <span className="inline-block h-2.5 w-2.5 flex-none rounded-sm" style={{ background: color }} aria-hidden />
        <span className={clsx("min-w-0 flex-1 truncate", failed ? "font-medium text-critical-text" : "text-ink")} title={`${s.service} · ${s.name}${failed ? " · failed" : ""}`}>
          {label}
        </span>
        {collapsed && row.descendants > 0 && <span className="flex-none rounded bg-surface-3 px-1 text-[11px] tabular text-ink-2">+{row.descendants}</span>}
        {row.hasError && !failed && <AlertTriangle size={12} className="flex-none text-warning-text" aria-label="A step inside failed" />}
        <span className="flex-none text-xs tabular text-ink-2">{fmtUs(s.duration_us)}</span>
      </div>
      <div className="relative h-3">
        {ticks.slice(1, -1).map((t, i) => (
          <span key={i} className="absolute bottom-0 top-0 w-px bg-grid" style={{ left: `${t.pct}%` }} aria-hidden />
        ))}
        <span
          className={clsx("absolute top-0.5 h-2 min-w-[2px] rounded-sm", failed && "ring-1 ring-[var(--lvl-error)]")}
          style={{ left: `${row.left}%`, width: `${row.width}%`, background: failed ? "var(--lvl-error)" : color, opacity: failed ? 0.9 : 0.85 }}
          aria-hidden
        />
      </div>
    </div>
  );
}

function Toggle({ row, collapsed, onToggle }: { row: WaterfallRow; collapsed: boolean; onToggle: () => void }) {
  if (row.children === 0) return <span className="inline-block h-4 w-4 flex-none" />;
  return (
    <button
      type="button"
      className="flex h-4 w-4 flex-none items-center justify-center rounded text-ink-3 hover:bg-surface-3 hover:text-ink pointer-coarse:h-6 pointer-coarse:w-6"
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      aria-label={collapsed ? `Show ${row.descendants} steps inside` : "Hide the steps inside"}
      title={collapsed ? `Show ${row.descendants} steps inside` : "Hide the steps inside"}
    >
      {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
    </button>
  );
}

/**
 * The span's duration sits right after its bar; when the bar reaches the right edge the label
 * moves before the bar, and a bar that fills the track carries the label inside its end.
 */
function DurationLabel({ row }: { row: WaterfallRow }) {
  const end = row.left + row.width;
  const text = fmtUs(row.span.duration_us);
  const base = "pointer-events-none absolute top-[6px] whitespace-nowrap text-[11px] leading-4 tabular";
  if (end <= 85) return <span className={clsx(base, "text-ink-2")} style={{ left: `${end}%`, paddingLeft: 4 }}>{text}</span>;
  if (row.left >= 15) return <span className={clsx(base, "text-ink-2")} style={{ right: `${100 - row.left}%`, paddingRight: 4 }}>{text}</span>;
  return (
    <span className={clsx(base, "font-medium text-white [text-shadow:0_0_2px_rgba(0,0,0,.45)]")} style={{ right: `${Math.max(0, 100 - end)}%`, paddingRight: 4 }}>
      {text}
    </span>
  );
}
