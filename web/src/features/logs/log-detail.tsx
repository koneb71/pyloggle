import { useMemo, useState } from "react";
import { Link } from "react-router";
import { AlertOctagon, Braces, GitBranch, ListTree, Route } from "lucide-react";
import type { LogEvent } from "@/api/types";
import { Badge, Button, CopyButton, JsonView, KeyValueList, LevelBadge, SidePanel, Tabs } from "@/components/ui";
import { fmtTs } from "@/lib/time";
import { term } from "@/lib/query";
import { ContextDialog } from "./context-dialog";

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
  const core: Record<string, string | number | undefined> = {
    service: event.service,
    env: event.env,
    logger: event.logger,
    host: event.hostname,
    release: event.release,
    source: event.source,
    request_id: event.request_id,
    trace_id: event.trace_id,
    span_id: event.span_id,
    user: event.user_id_hash,
    exception: event.exception_type,
    "http.method": event.http_method,
    "http.route": event.http_route,
    "http.status": event.http_status || undefined,
    "http.url": event.http_url,
    duration_ms: event.duration_ms || undefined,
  };
  return (
    <SidePanel
      open
      width={width}
      onClose={onClose}
      title={
        <span className="flex items-center gap-2">
          <LevelBadge level={event.level} />
          <span className="tabular text-xs font-normal text-ink-2">{fmtTs(event.ts, { ms: true })}</span>
        </span>
      }
      actions={<CopyButton text={JSON.stringify(event, null, 2)} label="Copy JSON" size="sm" />}
    >
      <div className="border-b border-border px-3 py-2">
        <div className="whitespace-pre-wrap break-words font-mono text-[13px] leading-5 text-ink">{event.message}</div>
        <div className="mt-2 flex flex-wrap gap-1">
          {event.request_id && (
            <Button size="xs" icon={<Route size={11} />} onClick={() => onFilter(term("request_id", event.request_id))} title="Only lines from this request">
              Same request
            </Button>
          )}
          {event.trace_id && (
            <Button size="xs" icon={<GitBranch size={11} />} onClick={() => onFilter(term("trace_id", event.trace_id))}>
              Same trace
            </Button>
          )}
          <Button size="xs" icon={<ListTree size={11} />} onClick={() => setCtxOpen(true)} title="Surrounding lines from the same service">
            View in context
          </Button>
          {event.issue_id && (
            <Link to={`/issues/${event.issue_id}`} className="inline-flex h-6 items-center gap-1 rounded-md border border-border bg-surface px-1.5 text-[11px] font-medium text-ink hover:bg-surface-2">
              <AlertOctagon size={11} /> View issue
            </Link>
          )}
        </div>
      </div>
      <Tabs
        size="sm"
        className="px-2"
        value={tab}
        onChange={(t) => setTab(t as "fields" | "raw")}
        items={[
          { id: "fields", label: "Fields" },
          { id: "raw", label: <span className="inline-flex items-center gap-1"><Braces size={11} /> JSON</span> },
        ]}
      />
      <div className="space-y-4 p-3">
        {tab === "raw" ? (
          <JsonView value={event} defaultOpen={2} />
        ) : (
          <>
            <Section title="Event">
              <KeyValueList entries={core} actions={actions} />
            </Section>
            <Section title="Attributes" badge={Object.keys(event.attrs ?? {}).length}>
              <KeyValueList entries={event.attrs} keyPrefix="attrs." actions={actions} emptyText="no attributes" />
              {onPickColumn && Object.keys(event.attrs ?? {}).length > 0 && (
                <div className="mt-1 flex flex-wrap gap-1">
                  {Object.keys(event.attrs).slice(0, 12).map((k) => (
                    <button key={k} type="button" className="rounded border border-border px-1 text-[10px] text-ink-3 hover:text-ink" onClick={() => onPickColumn(`attrs.${k}`)} title={`Add ${k} as a column`}>
                      + col {k}
                    </button>
                  ))}
                </div>
              )}
            </Section>
            {event.tags && Object.keys(event.tags).length > 0 && (
              <Section title="Tags">
                <KeyValueList entries={event.tags} keyPrefix="tags." actions={actions} />
              </Section>
            )}
            <Section title="Ids">
              <div className="flex flex-wrap gap-1">
                <Badge tone="outline" className="font-mono">
                  event {event.event_id.slice(0, 12)} <CopyButton text={event.event_id} className="h-3.5 px-0.5" />
                </Badge>
                {event.issue_id && (
                  <Badge tone="outline" className="font-mono">
                    issue {event.issue_id.slice(0, 12)} <CopyButton text={event.issue_id} className="h-3.5 px-0.5" />
                  </Badge>
                )}
              </div>
            </Section>
          </>
        )}
      </div>
      {ctxOpen && <ContextDialog eventId={event.event_id} onClose={() => setCtxOpen(false)} onFilter={onFilter} />}
    </SidePanel>
  );
}

function Section({ title, children, badge }: { title: string; children: React.ReactNode; badge?: number }) {
  return (
    <section>
      <h4 className="mb-1 flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-ink-3">
        {title}
        {badge !== undefined && <span className="rounded bg-surface-3 px-1 text-[10px] normal-case">{badge}</span>}
      </h4>
      {children}
    </section>
  );
}
