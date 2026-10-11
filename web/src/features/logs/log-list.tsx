import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import clsx from "clsx";
import { useLogs } from "@/api/hooks";
import { get } from "@/api/client";
import type { LogEvent, LogsResponse } from "@/api/types";
import { Button, EmptyState, ErrorState, Skeleton } from "@/components/ui";
import { fmtTs } from "@/lib/time";
import { levelVar } from "@/lib/levels";
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
  /** Rendered after the last row (e.g. "load older"). */
  footer?: React.ReactNode;
  scrollRef?: React.RefObject<HTMLDivElement | null>;
  onScrollTop?: (atTop: boolean) => void;
}

/** Virtualized log rows. Keyboard: j/k move, Enter opens, when the table has focus. */
export function LogTable({ items, columns = [], selectedId, highlightEventId, onSelect, wrap, dense, className, footer, scrollRef, onScrollTop }: LogTableProps) {
  const localRef = useRef<HTMLDivElement>(null);
  const parentRef = scrollRef ?? localRef;
  const cols = useMemo(() => columns.map(columnFor).filter((c): c is ColumnDef => !!c), [columns]);
  const rowH = dense ? 26 : 30;
  const v = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowH,
    overscan: 20,
    measureElement: wrap ? (el) => el.getBoundingClientRect().height : undefined,
  });
  const grid: CSSProperties = { gridTemplateColumns: `150px 64px ${cols.map((c) => c.width).join(" ")} minmax(200px,1fr)` };

  // keyboard navigation
  useEffect(() => {
    const el = parentRef.current;
    if (!el || !onSelect) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "j" && e.key !== "k" && e.key !== "Enter") return;
      const t = e.target as HTMLElement;
      if (t.tagName === "INPUT" || t.tagName === "TEXTAREA") return;
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
      className={clsx("relative min-h-0 flex-1 overflow-auto font-mono text-xs", className)}
      onScroll={(e) => onScrollTop?.((e.target as HTMLDivElement).scrollTop < 4)}
      tabIndex={0}
      role="table"
      aria-rowcount={items.length}
    >
      <div className="sticky top-0 z-10 grid border-b border-border bg-surface px-2 font-sans text-[10px] font-medium uppercase tracking-wide text-ink-3" style={grid} role="row">
        <div className="py-1">Time</div>
        <div className="py-1">Level</div>
        {cols.map((c) => (
          <div key={c.id} className={clsx("truncate py-1", c.align === "right" && "text-right")}>
            {c.label}
          </div>
        ))}
        <div className="py-1">Message</div>
      </div>
      <div style={{ height: v.getTotalSize(), position: "relative" }}>
        {v.getVirtualItems().map((vi) => {
          const e = items[vi.index];
          const selected = e.event_id === selectedId;
          const highlighted = e.event_id === highlightEventId;
          return (
            <div
              key={e.event_id}
              ref={wrap ? v.measureElement : undefined}
              data-index={vi.index}
              role="row"
              aria-selected={selected}
              onClick={() => onSelect?.(e)}
              className={clsx(
                "absolute left-0 grid w-full cursor-pointer items-start gap-x-2 border-b border-border/60 px-2 hover:bg-surface-2",
                selected && "bg-accent-soft hover:bg-accent-soft",
                highlighted && !selected && "bg-surface-3",
                dense ? "leading-[26px]" : "leading-[30px]",
              )}
              style={{ ...grid, top: 0, transform: `translateY(${vi.start}px)`, minHeight: rowH }}
            >
              <div className="flex items-center gap-1.5 truncate tabular text-ink-2">
                <span className="lvl-bar" style={{ background: levelVar(e.level), height: dense ? 16 : 18 }} aria-hidden />
                {fmtTs(e.ts, { ms: true }).slice(5)}
              </div>
              <div className={clsx("truncate uppercase", e.level === "error" || e.level === "fatal" ? "text-ink" : "text-ink-2")}>{e.level}</div>
              {cols.map((c) => {
                const val = c.value(e);
                return (
                  <div key={c.id} className={clsx("truncate", c.mono ? "" : "font-sans", c.align === "right" && "text-right", val ? "text-ink-2" : "text-ink-3")} title={val}>
                    {val || "—"}
                  </div>
                );
              })}
              <div className={clsx("min-w-0 text-ink", wrap ? "whitespace-pre-wrap break-words py-1 leading-[18px]" : "truncate")} title={wrap ? undefined : e.message}>
                {e.message}
                {!wrap && e.exception_type && <span className="ml-2 text-serious">{e.exception_type}</span>}
              </div>
            </div>
          );
        })}
      </div>
      {footer}
    </div>
  );
}

/** Self-fetching list (used in issue detail and context views). Newest first, "load older" appends. */
export function LogList({ q, from, to, limit = 200, highlightEventId, dense, columns, onSelect, selectedId }: { q: string; from: string; to: string; limit?: number; highlightEventId?: string; dense?: boolean; columns?: string[]; onSelect?: (e: LogEvent) => void; selectedId?: string }) {
  const first = useLogs({ q, from, to, limit });
  const [extra, setExtra] = useState<LogEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  useEffect(() => {
    setExtra([]);
    setCursor(first.data?.next_cursor);
  }, [first.data]);
  const items = useMemo(() => [...(first.data?.items ?? []), ...extra], [first.data, extra]);
  const loadMore = async () => {
    if (!cursor) return;
    setLoadingMore(true);
    try {
      const r = await get<LogsResponse>("/logs", { q, from, to, limit, cursor });
      setExtra((x) => [...x, ...r.items]);
      setCursor(r.next_cursor);
    } finally {
      setLoadingMore(false);
    }
  };
  if (first.isError) return <ErrorState error={first.error} retry={() => first.refetch()} />;
  if (first.isPending) return <Skeleton rows={6} />;
  if (items.length === 0) return <EmptyState title="No log lines" hint={`Nothing matched ${q} in this range.`} className="py-6" />;
  return (
    <div className="flex h-96 flex-col overflow-hidden rounded-md border border-border">
      <LogTable
        items={items}
        dense={dense}
        columns={columns ?? ["service", "logger"]}
        highlightEventId={highlightEventId}
        onSelect={onSelect}
        selectedId={selectedId}
        footer={
          cursor ? (
            <div className="flex justify-center py-2">
              <Button size="xs" onClick={loadMore} loading={loadingMore}>
                Load older
              </Button>
            </div>
          ) : null
        }
      />
    </div>
  );
}
