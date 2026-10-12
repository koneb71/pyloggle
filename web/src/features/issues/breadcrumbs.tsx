import type { Breadcrumb } from "@/api/types";
import { fmtTs } from "@/lib/time";
import { levelLabel, levelVar } from "@/lib/levels";
import { EmptyState } from "@/components/ui";

const CATEGORY: [RegExp, string][] = [
  [/^(http|httpx|requests|urllib|fetch|xhr)/i, "Web call"],
  [/(query|sql|db|database|psycopg|sqlalchemy|django\.db)/i, "Database query"],
  [/^(log|logging)/i, "Log message"],
  [/(navigation|route)/i, "Page change"],
  [/(redis|cache)/i, "Cache"],
  [/(celery|task|queue)/i, "Background job"],
];

/** "http" → "Web call": breadcrumb categories in plain words, technical name kept on hover. */
export function categoryLabel(b: Breadcrumb): string {
  const c = b.category || b.type || "";
  for (const [re, label] of CATEGORY) if (re.test(c)) return label;
  return c ? c.charAt(0).toUpperCase() + c.slice(1) : "Step";
}

/** "1.25 s before", "just before": how long before the error each step happened. */
export function beforeLabel(stepTs: string, errorTs?: string): string {
  const t = new Date(stepTs).getTime();
  const e = errorTs ? new Date(errorTs).getTime() : NaN;
  if (isNaN(t) || isNaN(e)) return "";
  const d = (e - t) / 1000;
  if (Math.abs(d) < 0.005) return "at the same moment";
  if (d < 0) return `${(-d).toFixed(2)} s after`;
  return `${d.toFixed(2)} s before`;
}

export function Breadcrumbs({ items, anchorTs }: { items?: Breadcrumb[]; anchorTs?: string }) {
  if (!items || items.length === 0)
    return <EmptyState title="No steps were recorded" hint="The SDK records the log messages, web calls and database queries that happen just before an error." className="py-6" />;
  return (
    <ol className="relative ml-2 border-l border-border pl-5">
      {items.map((b, i) => {
        const lvl = (b.level || "info").toLowerCase();
        return (
          <li key={i} className="relative py-2">
            <span className="absolute -left-[25px] top-[14px] h-2.5 w-2.5 rounded-full border-2 border-surface" style={{ background: levelVar(lvl) }} aria-hidden />
            <div className="flex flex-wrap items-baseline gap-x-2 text-xs text-ink-3">
              <span className="font-medium text-ink-2" title={b.category || b.type || undefined}>
                {categoryLabel(b)}
              </span>
              {lvl !== "info" && <span>{levelLabel(lvl)}</span>}
              <span className="tabular" title={fmtTs(b.ts, { ms: true })}>
                {beforeLabel(b.ts, anchorTs) || fmtTs(b.ts, { ms: true }).slice(11)}
              </span>
            </div>
            <div className="mt-0.5 whitespace-pre-wrap break-words text-[13px] text-ink">{b.message}</div>
            {b.data && Object.keys(b.data).length > 0 && (
              <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-[11px] text-ink-3">
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
