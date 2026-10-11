import type { Breadcrumb } from "@/api/types";
import { fmtTs } from "@/lib/time";
import { levelVar } from "@/lib/levels";
import { EmptyState } from "@/components/ui";

export function Breadcrumbs({ items, anchorTs }: { items?: Breadcrumb[]; anchorTs?: string }) {
  if (!items || items.length === 0) return <EmptyState title="No breadcrumbs" hint="The SDK records log lines, HTTP requests and manual breadcrumbs leading up to an error." className="py-6" />;
  const anchor = anchorTs ? new Date(anchorTs).getTime() : NaN;
  return (
    <ol className="relative ml-2 border-l border-border pl-4">
      {items.map((b, i) => {
        const t = new Date(b.ts).getTime();
        const d = Math.round((t - anchor) / 10) / 100;
        const rel = isNaN(anchor) || isNaN(t) ? "" : `${d > 0 ? "+" : ""}${d.toFixed(2)}s`;
        return (
          <li key={i} className="relative py-1.5">
            <span className="absolute -left-[21px] top-[11px] h-2.5 w-2.5 rounded-full border-2 border-surface" style={{ background: levelVar((b.level || "info").toLowerCase()) }} aria-hidden />
            <div className="flex flex-wrap items-baseline gap-x-2 text-xs">
              <span className="tabular text-ink-3" title={fmtTs(b.ts, { ms: true })}>
                {fmtTs(b.ts, { ms: true }).slice(11)}
              </span>
              {rel && <span className="tabular text-ink-3">({rel})</span>}
              <span className="font-mono text-[11px] text-ink-2">{b.category || b.type || "default"}</span>
              {b.level && <span className="font-mono text-[10px] uppercase text-ink-3">{b.level}</span>}
            </div>
            <div className="whitespace-pre-wrap break-words text-[13px] text-ink">{b.message}</div>
            {b.data && Object.keys(b.data).length > 0 && (
              <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-[11px] text-ink-3">
                {Object.entries(b.data).map(([k, v]) => (
                  <span key={k}>
                    <span className="text-ink-2">{k}</span>={v}
                  </span>
                ))}
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
