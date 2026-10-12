import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import clsx from "clsx";
import { useLogs } from "@/api/hooks";
import { get } from "@/api/client";
import type { LogEvent, LogsResponse } from "@/api/types";
import { EmptyState, LevelBadge, LoadMore, QueryState, Skeleton } from "@/components/ui";
import { fmtTs, fmtWhen } from "@/lib/time";
import { useElementWidth, useSmallScreen } from "@/lib/use-media";
import { columnFor, type ColumnDef } from "./log-fields";

export interface LogTableProps {
  items: LogEvent[];
  columns?: string[];
  selectedId?: string;
  highlightEventId?: string;
  onSelect?: (e: LogEvent) => void;
  wrap?: boolean;
  dense?: boolean;
  className?: string;
  /** Rendered after the last row (e.g. "Show earlier"). */
  footer?: React.ReactNode;
  scrollRef?: React.RefObject<HTMLDivElement | null>;
  onScrollTop?: (atTop: boolean) => void;
}

/**
 * Virtualized message rows. The header and every row share one grid template and one gap, so
 * columns line up exactly. Keyboard: j / k move, Enter opens, when the page has focus.
 *
 * Where the columns don't fit (a phone, or a tablet with the filters open) each message takes
 * two lines instead: when, how severe and the chosen columns on the first, the message on the
 * second. A desk with many columns keeps the table and scrolls sideways.
 */
export function LogTable({ items, columns = [], selectedId, highlightEventId, onSelect, wrap, dense, className, footer, scrollRef, onScrollTop }: LogTableProps) {
  const localRef = useRef<HTMLDivElement>(null);
  const parentRef = scrollRef ?? localRef;
  const cols = useMemo(() => columns.map(columnFor).filter((c): c is ColumnDef => !!c), [columns]);
  const width = useElementWidth(parentRef);
  const small = useSmallScreen();
  const stacked = width > 0 ? stacksAt(width, cols, dense) : small;
  // Two-line rows: padding (12 or 16) + a 20px line of facts + a 20px line of message.
  const rowH = stacked ? (dense ? 52 : 56) : dense ? 28 : 34;
  const v = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowH,
    overscan: 20,
    measureElement: wrap ? (el) => el.getBoundingClientRect().height : undefined,
  });
  // Turning a tablet or resizing across the phone width changes every row's height.
  useEffect(() => v.measure(), [stacked, v]);
  const grid: CSSProperties = { gridTemplateColumns: `${dense ? 128 : 150}px 84px ${cols.map((c) => c.width).join(" ")} minmax(240px,1fr)` };
  const rowClass = "grid gap-x-4 px-4";

  // keyboard navigation
  useEffect(() => {
    const el = parentRef.current;
    if (!el || !onSelect) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "j" && e.key !== "k" && e.key !== "Enter") return;
      const t = e.target as HTMLElement;
      if (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT") return;
      const idx = items.findIndex((i) => i.event_id === selectedId);
      let next = idx;
      if (e.key === "j") next = Math.min(items.length - 1, idx + 1);
      if (e.key === "k") next = Math.max(0, idx - 1);
      if (e.key === "Enter" && idx >= 0) next = idx;
      if (next >= 0 && next !== idx) {
        e.preventDefault();
        onSelect(items[next]);
        v.scrollToIndex(next, { align: "auto" });
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [items, selectedId, onSelect, v, parentRef]);

  useEffect(() => {
    if (!highlightEventId) return;
    const i = items.findIndex((x) => x.event_id === highlightEventId);
    if (i >= 0) v.scrollToIndex(i, { align: "center" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [highlightEventId, items.length]);

  return (
    <div
      ref={parentRef}
      className={clsx("relative min-h-0 flex-1 overflow-auto bg-surface text-[13px]", className)}
      onScroll={(e) => onScrollTop?.((e.target as HTMLDivElement).scrollTop < 4)}
      tabIndex={0}
      role="table"
      aria-label="Messages"
      aria-rowcount={items.length}
    >
      <div className={stacked ? "sr-only" : clsx(rowClass, "sticky top-0 z-10 h-8 items-center border-b border-border bg-surface text-xs font-medium text-ink-3")} style={stacked ? undefined : grid} role="row">
        <div role="columnheader">Time</div>
        <div role="columnheader">Severity</div>
        {cols.map((c) => (
          <div key={c.id} role="columnheader" className={clsx("truncate", c.align === "right" && "text-right")} title={c.technical !== c.label ? `Field: ${c.technical}` : undefined}>
            {c.label}
          </div>
        ))}
        <div role="columnheader">Message</div>
      </div>
      <div style={{ height: v.getTotalSize(), position: "relative" }}>
        {v.getVirtualItems().map((vi) => {
          const e = items[vi.index];
          const selected = e.event_id === selectedId;
          const highlighted = e.event_id === highlightEventId;
          if (stacked)
            return (
              <div
                key={e.event_id}
                ref={wrap ? v.measureElement : undefined}
                data-index={vi.index}
                role="row"
                aria-selected={selected}
                onClick={() => onSelect?.(e)}
                className={clsx(
                  "absolute left-0 w-full cursor-pointer border-b border-border/70 px-4 hover:bg-surface-2",
                  dense ? "py-1.5" : "py-2",
                  selected && "bg-accent-soft hover:bg-accent-soft",
                  highlighted && !selected && "bg-warning/10",
                )}
                style={{ top: 0, transform: `translateY(${vi.start}px)`, minHeight: rowH }}
              >
                <div className="flex h-5 min-w-0 items-center gap-2 text-xs text-ink-3">
                  <span role="cell" className="flex-none tabular text-ink-2" title={fmtTs(e.ts, { ms: true })}>
                    {fmtWhen(e.ts, { seconds: true })}
                  </span>
                  <span role="cell" className="flex-none">
                    <LevelBadge level={e.level} className="text-xs" />
                  </span>
                  {cols.map((c) => {
                    const val = c.value(e);
                    return val ? (
                      <span key={c.id} role="cell" className={clsx("min-w-0 truncate", c.mono && "font-mono text-[11px]")} title={`${c.label}: ${val}`}>
                        {val}
                      </span>
                    ) : null;
                  })}
                </div>
                <div role="cell" className={clsx("min-w-0 leading-5 text-ink", wrap ? "whitespace-pre-wrap break-words" : "truncate")}>
                  {e.message}
                  {e.exception_type && <span className="ml-2 text-xs font-medium text-critical-text">{e.exception_type}</span>}
                </div>
              </div>
            );
          return (
            <div
              key={e.event_id}
              ref={wrap ? v.measureElement : undefined}
              data-index={vi.index}
              role="row"
              aria-selected={selected}
              onClick={() => onSelect?.(e)}
              className={clsx(
                rowClass,
                "absolute left-0 w-full cursor-pointer items-start border-b border-border/70 hover:bg-surface-2",
                selected && "bg-accent-soft hover:bg-accent-soft",
                highlighted && !selected && "bg-warning/10",
              )}
              style={{ ...grid, top: 0, transform: `translateY(${vi.start}px)`, minHeight: rowH, lineHeight: `${rowH - 1}px` }}
            >
              <div role="cell" className="truncate tabular text-ink-2" title={fmtTs(e.ts, { ms: true })}>
                {fmtWhen(e.ts, { seconds: true })}
              </div>
              <div role="cell" className="truncate">
                <LevelBadge level={e.level} />
              </div>
              {cols.map((c) => {
                const val = c.value(e);
                return (
                  <div key={c.id} role="cell" className={clsx("truncate", c.mono && "font-mono text-[12px]", c.align === "right" && "text-right tabular", val ? "text-ink-2" : "text-ink-3")} title={val}>
                    {val || "—"}
                  </div>
                );
              })}
              <div role="cell" className={clsx("min-w-0 text-ink", wrap ? "whitespace-pre-wrap break-words py-1.5 leading-5" : "truncate")} title={wrap ? undefined : e.message}>
                {e.message}
                {e.exception_type && <span className="ml-2 text-xs font-medium text-critical-text">{e.exception_type}</span>}
              </div>
            </div>
          );
        })}
      </div>
      {footer}
    </div>
  );
}

/** Narrower than this, a list stacks even when its columns would just fit. */
const STACK_BELOW = 720;

/** Whether a list this wide shows two-line rows: when the table wouldn't fit, up to STACK_BELOW. */
export function stacksAt(width: number, cols: ColumnDef[], dense?: boolean): boolean {
  return width < Math.min(STACK_BELOW, minTableWidth(cols, dense));
}

/** The narrowest the table can be: every column at its minimum, the gaps and the padding. */
function minTableWidth(cols: ColumnDef[], dense?: boolean): number {
  const least = (track: string) => Number(/(\d+)px/.exec(track)?.[1] ?? 0);
  return (dense ? 128 : 150) + 84 + cols.reduce((n, c) => n + least(c.width), 0) + 240 + 16 * (cols.length + 2) + 32;
}

/** Self-fetching list (issue detail, request timeline). Newest first; "Show earlier" appends. */
export function LogList({ q, from, to, limit = 200, highlightEventId, dense, columns, onSelect, selectedId }: { q: string; from: string; to: string; limit?: number; highlightEventId?: string; dense?: boolean; columns?: string[]; onSelect?: (e: LogEvent) => void; selectedId?: string }) {
  const first = useLogs({ q, from, to, limit });
  const [extra, setExtra] = useState<LogEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<unknown>(null);
  useEffect(() => {
    setExtra([]);
    setCursor(first.data?.next_cursor);
    setMoreError(null);
  }, [first.data]);
  const items = useMemo(() => [...(first.data?.items ?? []), ...extra], [first.data, extra]);
  const loadMore = async () => {
    if (!cursor) return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const r = await get<LogsResponse>("/logs", { q, from, to, limit, cursor });
      setExtra((x) => [...x, ...r.items]);
      setCursor(r.next_cursor);
    } catch (e) {
      setMoreError(e);
    } finally {
      setLoadingMore(false);
    }
  };
  return (
    <QueryState query={first} isEmpty={(d) => d.items.length === 0} skeleton={<Skeleton rows={6} />} empty={<EmptyState title="No messages" hint="Nothing matched in this time range." className="py-6" />}>
      {() => (
        <div className="flex h-96 flex-col overflow-hidden rounded-md border border-border">
          <LogTable
            items={items}
            dense={dense}
            columns={columns ?? ["service", "logger"]}
            highlightEventId={highlightEventId}
            onSelect={onSelect}
            selectedId={selectedId}
            footer={<LoadMore shown={items.length} noun="messages shown" hasMore={!!cursor} loading={loadingMore} error={moreError} onMore={loadMore} label="Show earlier" />}
          />
        </div>
      )}
    </QueryState>
  );
}
