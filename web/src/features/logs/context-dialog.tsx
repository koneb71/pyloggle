import { useMemo, useState } from "react";
import { useLogContext } from "@/api/hooks";
import type { LogEvent } from "@/api/types";
import { Dialog, ErrorState, Skeleton } from "@/components/ui";
import { LogTable } from "./log-list";
import { LogDetailPanel } from "./log-detail";

/** The messages the same app wrote just before and after one message, oldest first. */
export function ContextDialog({ eventId, onClose, onFilter }: { eventId: string; onClose: () => void; onFilter: (t: string) => void }) {
  const ctx = useLogContext(eventId);
  const [sel, setSel] = useState<LogEvent | null>(null);
  const items = useMemo(() => (ctx.data ? [...ctx.data.before, ctx.data.anchor, ...ctx.data.after] : []), [ctx.data]);
  return (
    <Dialog open onClose={onClose} title={ctx.data ? `Surrounding messages from ${ctx.data.anchor.service}` : "Surrounding messages"} wide className="max-w-6xl">
      <p className="mb-3 text-[13px] text-ink-3">The highlighted row is the message you opened. Click a row to see its details.</p>
      {ctx.isError ? (
        <ErrorState error={ctx.error} retry={() => ctx.refetch()} />
      ) : ctx.isPending ? (
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
