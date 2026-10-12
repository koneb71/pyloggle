import { Children, useEffect, useState, type ReactNode } from "react";
import clsx from "clsx";
import { Check, Copy, Inbox, X } from "lucide-react";
import { ago, fmtTs } from "@/lib/time";
import { Button } from "./button";

export function EmptyState({ title, hint, icon, action, className }: { title: ReactNode; hint?: ReactNode; icon?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={clsx("flex flex-col items-center justify-center gap-1 px-4 py-12 text-center", className)}>
      <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-surface-2 text-ink-3">{icon ?? <Inbox size={18} />}</div>
      <div className="text-sm font-medium text-ink">{title}</div>
      {hint && <div className="max-w-md text-[13px] text-ink-3">{hint}</div>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

export function Skeleton({ className, rows = 1 }: { className?: string; rows?: number }) {
  return (
    <div className={clsx("flex flex-col gap-2", className)} aria-hidden>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton h-4" style={{ width: `${70 + ((i * 13) % 30)}%` }} />
      ))}
    </div>
  );
}

export function CopyButton({ text, label = "Copy", size = "xs", className, showLabel }: { text: string; label?: string; size?: "xs" | "sm"; className?: string; showLabel?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      size={size}
      variant="ghost"
      square={!showLabel}
      className={className}
      title={label}
      aria-label={label}
      icon={done ? <Check size={13} className="text-good-text" /> : <Copy size={13} />}
      onClick={async (e) => {
        e.stopPropagation();
        try {
          await navigator.clipboard.writeText(text);
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        } catch {
          /* clipboard unavailable */
        }
      }}
    >
      {showLabel ? (done ? "Copied" : label) : null}
    </Button>
  );
}

export function TimeAgo({ iso, className, withAbsolute }: { iso: string; className?: string; withAbsolute?: boolean }) {
  const [, tick] = useState(0);
  useEffect(() => {
    const t = setInterval(() => tick((n) => n + 1), 30_000);
    return () => clearInterval(t);
  }, []);
  if (!iso) return null;
  return (
    <time dateTime={iso} title={fmtTs(iso, { seconds: true })} className={clsx("tabular", className)}>
      {ago(iso)}
      {withAbsolute && <span className="ml-1 text-ink-3">· {fmtTs(iso)}</span>}
    </time>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-flex min-w-5 justify-center rounded border border-border-strong bg-surface-2 px-1 py-px font-mono text-[11px] leading-4 text-ink-2">{children}</kbd>;
}

export function Spinner({ className }: { className?: string }) {
  return <span role="status" className={clsx("inline-block h-4 w-4 animate-spin rounded-full border-2 border-ink-3/60 border-t-transparent", className)} aria-label="Loading" />;
}

export function Divider({ className }: { className?: string }) {
  return <div className={clsx("h-px w-full bg-border", className)} />;
}

/** Offset pagination: "Page 2 · Previous · Next". */
export function Pagination({ offset, limit, count, onChange }: { offset: number; limit: number; count: number; onChange: (offset: number) => void }) {
  const page = Math.floor(offset / limit) + 1;
  const hasNext = count >= limit;
  if (page === 1 && !hasNext) return null;
  return (
    <div className="flex items-center justify-end gap-2 px-4 py-2.5 text-[13px] text-ink-3">
      <span className="tabular">Page {page}</span>
      <Button size="xs" disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>
        Previous
      </Button>
      <Button size="xs" disabled={!hasNext} onClick={() => onChange(offset + limit)}>
        Next
      </Button>
    </div>
  );
}

/** Secondary facts in one muted line: "api · production · v1.4.0 · 2 min ago". Empty items are skipped. */
export function MetaLine({ children, className }: { children: ReactNode; className?: string }) {
  const items = Children.toArray(children).filter((c) => c !== "");
  return (
    <div className={clsx("flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-ink-3", className)}>
      {items.map((c, i) => (
        <span key={i} className="inline-flex min-w-0 items-center gap-1.5">
          {i > 0 && <span aria-hidden className="text-ink-3/70">·</span>}
          <span className="min-w-0 truncate">{c}</span>
        </span>
      ))}
    </div>
  );
}

/** The footer of a growing list: "120 shown · Show earlier", with its own error and loading state. */
export function LoadMore({ shown, noun = "shown", hasMore, loading, error, onMore, label = "Show more", className }: { shown: number; noun?: string; hasMore: boolean; loading?: boolean; error?: unknown; onMore: () => void; label?: string; className?: string }) {
  return (
    <div className={clsx("flex items-center justify-center gap-3 px-4 py-3 text-[13px] text-ink-3", className)}>
      <span className="tabular">
        {shown.toLocaleString()} {noun}
      </span>
      {error ? (
        <span className="text-critical-text">
          Couldn't load more.{" "}
          <button type="button" className="font-medium underline underline-offset-2" onClick={onMore}>
            Try again
          </button>
        </span>
      ) : hasMore ? (
        <Button size="xs" onClick={onMore} loading={loading}>
          {label}
        </Button>
      ) : shown > 0 ? (
        <span>That's everything in this time range.</span>
      ) : null}
    </div>
  );
}

/** Bulk-action bar shown while rows are selected. */
export function SelectionBar({ count, noun = "selected", onClear, children, className }: { count: number; noun?: string; onClear: () => void; children: ReactNode; className?: string }) {
  if (count === 0) return null;
  return (
    <div className={clsx("flex items-center gap-2 border-b border-border bg-accent-soft px-4 py-2 text-[13px]", className)} role="region" aria-label="Selection actions">
      <span className="font-medium text-ink tabular">
        {count.toLocaleString()} {noun}
      </span>
      <button type="button" onClick={onClear} className="inline-flex items-center gap-0.5 rounded px-1 text-ink-2 hover:text-ink" aria-label="Clear selection">
        <X size={13} /> Clear
      </button>
      <div className="ml-auto flex items-center gap-1.5">{children}</div>
    </div>
  );
}

/** A legend key: the mark a series uses on the chart. */
export function Swatch({ color, shape = "square", className }: { color: string; shape?: "square" | "dot" | "line"; className?: string }) {
  return <span aria-hidden className={clsx("inline-block flex-none", shape === "line" ? "h-0.5 w-3 rounded-full" : "h-2.5 w-2.5", shape === "dot" && "rounded-full", shape === "square" && "rounded-[3px]", className)} style={{ background: color }} />;
}

/** "Updating…" while a query refreshes data that is already on screen. */
export function UpdatingDot({ active, className }: { active: boolean; className?: string }) {
  if (!active) return null;
  return (
    <span className={clsx("inline-flex items-center gap-1.5 text-xs text-ink-3", className)} role="status">
      <span className="h-3 w-3 animate-spin rounded-full border-[1.5px] border-ink-3/70 border-t-transparent" aria-hidden />
      Updating…
    </span>
  );
}
