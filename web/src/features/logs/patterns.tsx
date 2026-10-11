import { useLogPatterns } from "@/api/hooks";
import { Badge, Button, EmptyState, ErrorState, Skeleton } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { levelVar } from "@/lib/levels";
import { Filter } from "lucide-react";
import { patternPhrase } from "./log-fields";

export function PatternsView({ q, range, onFilter }: { q: string; range: { from: string; to: string }; onFilter: (phrase: string) => void }) {
  const p = useLogPatterns({ q, from: range.from, to: range.to });
  if (p.isError) return <ErrorState error={p.error} retry={() => p.refetch()} />;
  if (p.isPending) return <Skeleton rows={8} className="p-3" />;
  const items = p.data?.items ?? [];
  if (items.length === 0) return <EmptyState title="No patterns" hint="Patterns cluster messages by template: numbers, hex ids, uuids and quoted strings are collapsed." />;
  const total = items.reduce((n, i) => n + i.count, 0) || 1;
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <table className="w-full border-collapse text-xs">
        <thead className="sticky top-0 z-10 bg-surface text-[10px] font-medium uppercase tracking-wide text-ink-3 shadow-[0_1px_0_var(--border)]">
          <tr>
            <th className="w-24 px-3 py-1.5 text-right">Count</th>
            <th className="w-16 px-2 py-1.5 text-left">Level</th>
            <th className="px-2 py-1.5 text-left">Pattern</th>
            <th className="w-40 px-2 py-1.5 text-left">Services</th>
            <th className="w-20 px-2 py-1.5" />
          </tr>
        </thead>
        <tbody>
          {items.map((it, i) => (
            <tr key={i} className="group border-b border-border hover:bg-surface-2">
              <td className="px-3 py-1.5 text-right align-top">
                <div className="font-medium tabular text-ink">{fmtNumber(it.count)}</div>
                <div className="mt-0.5 h-1 w-full rounded bg-surface-3">
                  <div className="h-1 rounded bg-accent" style={{ width: `${Math.max(2, (it.count / total) * 100)}%` }} />
                </div>
              </td>
              <td className="px-2 py-1.5 align-top">
                <span className="inline-flex items-center gap-1.5 font-mono uppercase text-ink-2">
                  <span className="inline-block h-2 w-2 rounded-sm" style={{ background: levelVar(it.level) }} />
                  {it.level}
                </span>
              </td>
              <td className="px-2 py-1.5 align-top">
                <div className="whitespace-pre-wrap break-words font-mono text-ink">{highlightPlaceholders(it.pattern)}</div>
                {it.sample && it.sample !== it.pattern && <div className="mt-0.5 truncate font-mono text-[11px] text-ink-3" title={it.sample}>{it.sample}</div>}
              </td>
              <td className="px-2 py-1.5 align-top">
                <div className="flex flex-wrap gap-1">
                  {it.services.slice(0, 4).map((s) => (
                    <Badge key={s}>{s}</Badge>
                  ))}
                  {it.services.length > 4 && <Badge tone="outline">+{it.services.length - 4}</Badge>}
                </div>
              </td>
              <td className="px-2 py-1.5 text-right align-top">
                <Button size="xs" variant="ghost" className="invisible group-hover:visible" icon={<Filter size={11} />} onClick={() => onFilter(patternPhrase(it.pattern))} title="Filter the list to this pattern">
                  Filter
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function highlightPlaceholders(p: string) {
  const parts = p.split(/(<(?:n|s|hex|uuid)>)/);
  return parts.map((x, i) => (/^<(?:n|s|hex|uuid)>$/.test(x) ? <span key={i} className="rounded bg-accent-soft px-0.5 text-accent">{x}</span> : <span key={i}>{x}</span>));
}
