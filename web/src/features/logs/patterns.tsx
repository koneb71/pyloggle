import { Filter } from "lucide-react";
import { useLogPatterns } from "@/api/hooks";
import type { LogPattern } from "@/api/types";
import { Button, DataTable, EmptyState, IconButton, LevelBadge, MetaLine, type Column } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { patternPhrase } from "./log-fields";

const PLACEHOLDER: Record<string, string> = { "<n>": "number", "<s>": "text", "<hex>": "id", "<uuid>": "id" };

/** Messages grouped by their shape, so a flood of near-identical lines reads as one row. */
export function PatternsView({ q, range, onFilter }: { q: string; range: { from: string; to: string }; onFilter: (phrase: string) => void }) {
  const p = useLogPatterns({ q, from: range.from, to: range.to });
  const items = p.data?.items ?? [];
  const total = items.reduce((n, i) => n + i.count, 0) || 1;
  const columns: Column<LogPattern>[] = [
    {
      id: "count",
      header: "Times",
      width: "88px",
      align: "right",
      cell: (it) => (
        <div className="w-full">
          <div className="font-medium text-ink">{fmtNumber(it.count)}</div>
          <div className="mt-1 h-1 w-full rounded-full bg-surface-3">
            <div className="h-1 rounded-full bg-accent" style={{ width: `${Math.max(3, (it.count / total) * 100)}%` }} />
          </div>
          {/* On a phone the Severity column steps out; its word sits under the count. */}
          <LevelBadge level={it.level} className="mt-1.5 justify-end text-xs sm:hidden" />
        </div>
      ),
      className: "self-start",
    },
    { id: "level", header: "Severity", width: "96px", hideBelow: "sm", cell: (it) => <LevelBadge level={it.level} />, className: "self-start" },
    {
      id: "pattern",
      header: "Message shape",
      cell: (it) => (
        <div className="min-w-0">
          <div className="whitespace-pre-wrap break-words leading-6 text-ink">{renderPattern(it.pattern)}</div>
          {it.sample && it.sample !== it.pattern && (
            <div className="mt-0.5 truncate text-xs text-ink-3" title={it.sample}>
              For example: {it.sample}
            </div>
          )}
          <MetaLine className="mt-0.5">{it.services.length > 3 ? `${it.services.slice(0, 3).join(", ")} and ${it.services.length - 3} more` : it.services.join(", ")}</MetaLine>
        </div>
      ),
    },
    {
      id: "go",
      header: <span className="sr-only">Show</span>,
      width: "92px",
      align: "right",
      hideBelow: "sm",
      cell: (it) => (
        <Button size="xs" variant="ghost" icon={<Filter size={13} />} onClick={() => onFilter(patternPhrase(it.pattern))} title="Show only messages with this shape">
          Show these
        </Button>
      ),
      className: "self-start",
    },
    {
      id: "go-compact",
      header: <span className="sr-only">Show</span>,
      width: "32px",
      align: "right",
      showBelow: "sm",
      cell: (it) => <IconButton size="xs" icon={<Filter size={14} />} onClick={() => onFilter(patternPhrase(it.pattern))} label="Show only messages with this shape" />,
      className: "self-start",
    },
  ];
  return (
    <div className="min-h-0 flex-1 overflow-auto bg-surface">
      <p className="border-b border-border px-4 py-2.5 text-[13px] text-ink-3">Messages grouped by their shape. Numbers, ids and quoted text are blanked out, so lines that differ only in those count as one.</p>
      <DataTable
        label="Similar messages"
        columns={columns}
        rows={p.data ? items : undefined}
        rowKey={(it) => it.pattern + it.level}
        loading={p.isPending}
        error={p.isError ? p.error : undefined}
        onRetry={() => p.refetch()}
        rowClassName={() => "items-start py-3"}
        empty={<EmptyState title="No messages to group" hint="Widen the time range or remove a filter." />}
      />
    </div>
  );
}

function renderPattern(p: string) {
  return p.split(/(<(?:n|s|hex|uuid)>)/).map((x, i) =>
    PLACEHOLDER[x] ? (
      <span key={i} className="mx-0.5 rounded bg-accent-soft px-1 py-px text-xs font-medium text-accent" title={`Any ${PLACEHOLDER[x]} (${x})`}>
        {PLACEHOLDER[x]}
      </span>
    ) : (
      <span key={i}>{x}</span>
    ),
  );
}
