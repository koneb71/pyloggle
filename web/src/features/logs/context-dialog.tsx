import { useMemo, useState } from "react";
import { useLogContext } from "@/api/hooks";
import type { LogEvent } from "@/api/types";
import { Dialog, ErrorState, Skeleton } from "@/components/ui";
import { LogTable } from "./log-list";
import { LogDetailPanel } from "./log-detail";

/** Surrounding lines (same project/service) around one event, oldest first. */
export function ContextDialog({ eventId, onClose, onFilter }: { eventId: string; onClose: () => void; onFilter: (t: string) => void }) {
  const ctx = useLogContext(eventId);
  const [sel, setSel] = useState<LogEvent | null>(null);
  const items = useMemo(() => (ctx.data ? [...ctx.data.before, ctx.data.anchor, ...ctx.data.after] : []), [ctx.data]);
  return (
    <Dialog open onClose={onClose} title={`Context · ${ctx.data?.anchor.service ?? ""} · ${items.length} lines`} wide className="max-w-6xl">
      {ctx.isError && <ErrorState error={ctx.error} />}
      {ctx.isPending ? (
        <Skeleton rows={8} />
      ) : (
        <div className="flex h-[70vh] overflow-hidden rounded-md border border-border">
          <LogTable items={items} columns={["logger", "request_id"]} highlightEventId={eventId} selectedId={sel?.event_id} onSelect={setSel} dense />
          {sel && (
            <LogDetailPanel
              event={sel}
              width={420}
              onClose={() => setSel(null)}
              onFilter={(t) => {
                onFilter(t);
                onClose();
              }}
              onExclude={(t) => {
                onFilter(t);
                onClose();
              }}
            />
          )}
        </div>
      )}
    </Dialog>
  );
}
