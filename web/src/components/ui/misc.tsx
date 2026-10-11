import { useEffect, useState, type ReactNode } from "react";
import clsx from "clsx";
import { Check, Copy, Inbox } from "lucide-react";
import { ago, fmtTs } from "@/lib/time";
import { Button } from "./button";

export function EmptyState({ title, hint, icon, action, className }: { title: ReactNode; hint?: ReactNode; icon?: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={clsx("flex flex-col items-center justify-center gap-1.5 px-4 py-10 text-center", className)}>
      <div className="text-ink-3">{icon ?? <Inbox size={22} />}</div>
      <div className="text-[13px] font-medium text-ink">{title}</div>
      {hint && <div className="max-w-md text-xs text-ink-3">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function ErrorState({ error, retry }: { error: unknown; retry?: () => void }) {
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <div className="m-3 rounded-md border border-critical/40 bg-surface px-3 py-2 text-xs text-critical">
      <span className="font-medium">Request failed:</span> {msg}
      {retry && (
        <Button size="xs" className="ml-2" onClick={retry}>
          Retry
        </Button>
      )}
    </div>
  );
}

export function Skeleton({ className, rows = 1 }: { className?: string; rows?: number }) {
  return (
    <div className={clsx("flex flex-col gap-1.5", className)} aria-hidden>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton h-3.5" style={{ width: `${70 + ((i * 13) % 30)}%` }} />
      ))}
    </div>
  );
}

export function CopyButton({ text, label = "Copy", size = "xs", className }: { text: string; label?: string; size?: "xs" | "sm"; className?: string }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      size={size}
      variant="ghost"
      className={className}
      title={label}
      aria-label={label}
      icon={done ? <Check size={12} /> : <Copy size={12} />}
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
    />
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
    <time dateTime={iso} title={fmtTs(iso, { seconds: true, ms: true })} className={clsx("tabular", className)}>
      {ago(iso)}
      {withAbsolute && <span className="ml-1 text-ink-3">· {fmtTs(iso)}</span>}
    </time>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded border border-border bg-surface-2 px-1 py-px font-mono text-[10px] text-ink-2">{children}</kbd>;
}

export function Spinner({ className }: { className?: string }) {
  return <span className={clsx("inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-ink-3 border-t-transparent", className)} aria-label="Loading" />;
}

export function Divider({ className }: { className?: string }) {
  return <div className={clsx("h-px w-full bg-border", className)} />;
}

/** Simple local pagination for offset-based lists. */
export function Pagination({ offset, limit, count, onChange }: { offset: number; limit: number; count: number; onChange: (offset: number) => void }) {
  const page = Math.floor(offset / limit) + 1;
  const hasNext = count >= limit;
  return (
    <div className="flex items-center justify-end gap-2 px-3 py-2 text-xs text-ink-3">
      <span className="tabular">Page {page}</span>
      <Button size="xs" disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>
        Prev
      </Button>
      <Button size="xs" disabled={!hasNext} onClick={() => onChange(offset + limit)}>
        Next
      </Button>
    </div>
  );
}
