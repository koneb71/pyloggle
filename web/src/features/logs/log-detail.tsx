import { useMemo, useState } from "react";
import { AlertOctagon, GitBranch, ListTree, Route } from "lucide-react";
import type { LogEvent } from "@/api/types";
import { Button, CopyButton, JsonView, KeyValueList, LevelBadge, LinkButton, SidePanel, Tabs } from "@/components/ui";
import { fmtTs, fmtWhen } from "@/lib/time";
import { term } from "@/lib/query";
import { sourceLabel } from "@/lib/fields";
import { ContextDialog } from "./context-dialog";

/** Everything about one message, with ways to follow it: its request, its timeline, its neighbours. */
export function LogDetailPanel({ event, onClose, onFilter, onExclude, onPickColumn, width }: { event: LogEvent; onClose: () => void; onFilter: (t: string) => void; onExclude: (t: string) => void; onPickColumn?: (col: string) => void; width?: number }) {
  const [tab, setTab] = useState<"fields" | "raw">("fields");
  const [ctxOpen, setCtxOpen] = useState(false);
  const actions = useMemo(
    () => ({
      onFilter: (k: string, v: string) => onFilter(term(k, v)),
      onExclude: (k: string, v: string) => onExclude(term(k, v, true)),
    }),
    [onFilter, onExclude],
  );
  const about: Record<string, string | number | undefined> = {
    service: event.service,
    env: event.env,
    logger: event.logger,
    host: event.hostname,
    release: event.release,
    source: sourceLabel(event.source),
    exception: event.exception_type,
    "http.method": event.http_method,
    "http.route": event.http_route,
    "http.status": event.http_status || undefined,
    "http.url": event.http_url,
    duration_ms: event.duration_ms ? `${event.duration_ms.toFixed(event.duration_ms < 10 ? 1 : 0)} ms` : undefined,
    user: event.user_id_hash,
    request_id: event.request_id,
    trace_id: event.trace_id,
    span_id: event.span_id,
  };
  const attrCount = Object.keys(event.attrs ?? {}).length;
  return (
    <SidePanel
      open
      width={width ?? 480}
      onClose={onClose}
      title={
        <span className="flex items-center gap-2.5">
          <span>Message details</span>
          <LevelBadge level={event.level} />
        </span>
      }
      actions={<CopyButton text={JSON.stringify(event, null, 2)} label="Copy raw data" size="sm" />}
    >
      <div className="border-b border-border px-4 py-3">
        <div className="text-xs text-ink-3 tabular" title={fmtTs(event.ts, { ms: true })}>
          {fmtWhen(event.ts, { seconds: true })} · {event.service}
          {event.env ? ` · ${event.env}` : ""}
        </div>
        <div className="mt-1.5 whitespace-pre-wrap break-words text-[14px] leading-6 text-ink">{event.message}</div>
        {event.exception_type && <div className="mt-1 text-[13px] font-medium text-critical-text">{event.exception_type}</div>}
        <div className="mt-3 flex flex-wrap gap-1.5">
          {event.request_id && (
            <Button size="xs" icon={<Route size={13} />} onClick={() => onFilter(term("request_id", event.request_id))} title="Only the messages logged while handling the same request">
              All messages from this request
            </Button>
          )}
          {event.trace_id && (
            <LinkButton to={`/traces/${event.trace_id}?at=${encodeURIComponent(event.ts)}`} size="xs" icon={<GitBranch size={13} />} title="Every step of this request, across apps">
              See request timeline
            </LinkButton>
          )}
          <Button size="xs" icon={<ListTree size={13} />} onClick={() => setCtxOpen(true)} title="The messages the same app wrote just before and after">
            Show surrounding messages
          </Button>
          {event.issue_id && (
            <LinkButton to={`/issues/${event.issue_id}`} size="xs" icon={<AlertOctagon size={13} />}>
              Open issue
            </LinkButton>
          )}
        </div>
      </div>
      <Tabs
        className="px-4"
        value={tab}
        onChange={(t) => setTab(t as "fields" | "raw")}
        items={[
          { id: "fields", label: "Details" },
          { id: "raw", label: "Raw data" },
        ]}
      />
      <div className="space-y-5 p-4">
        {tab === "raw" ? (
          <JsonView value={event} defaultOpen={2} />
        ) : (
          <>
            <Block title="About this message">
              <KeyValueList entries={about} actions={actions} />
            </Block>
            <Block title="Extra details" hint={attrCount ? "Fields your app sent with this message. Hover a row to filter by it or show it as a column." : undefined}>
              <KeyValueList entries={event.attrs} keyPrefix="attrs." actions={{ ...actions, onColumn: onPickColumn }} emptyText="No extra details were sent." />
            </Block>
            {event.tags && Object.keys(event.tags).length > 0 && (
              <Block title="Labels">
                <KeyValueList entries={event.tags} keyPrefix="tags." actions={{ ...actions, onColumn: onPickColumn }} />
              </Block>
            )}
            <Block title="Identifiers">
              <KeyValueList entries={{ event_id: event.event_id, issue_id: event.issue_id }} />
            </Block>
          </>
        )}
      </div>
      {ctxOpen && <ContextDialog eventId={event.event_id} onClose={() => setCtxOpen(false)} onFilter={onFilter} />}
    </SidePanel>
  );
}

function Block({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section>
      <h4 className="text-[13px] font-semibold text-ink">{title}</h4>
      {hint && <p className="mt-0.5 text-xs text-ink-3">{hint}</p>}
      <div className="mt-2">{children}</div>
    </section>
  );
}
