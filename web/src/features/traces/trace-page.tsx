import { useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { AlertOctagon, ArrowLeft, ScrollText } from "lucide-react";
import { useTrace } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import type { Span } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { Badge, Button, CopyButton, EmptyState, ErrorState, KeyValueList, LevelBadge, SidePanel, Skeleton, Tabs } from "@/components/ui";
import { useUrlState } from "@/lib/url";
import { fmtTs } from "@/lib/time";
import { LogTable } from "@/features/logs/log-list";
import { LogDetailPanel } from "@/features/logs/log-detail";
import { Waterfall, serviceColor } from "./waterfall";
import { fmtUs, microFraction } from "./waterfall-layout";

export function TracePage() {
  const { id } = useParams<{ id: string }>();
  const { get, set } = useUrlState();
  const at = get("at") || undefined;
  const selId = get("span");
  const logSel = get("log");
  const [tab, setTab] = useState<"logs" | "errors">("logs");
  const trace = useTrace(id, at);
  const d = trace.data;
  const selected = useMemo(() => d?.spans.find((s) => s.span_id === selId), [d, selId]);
  const selectedLog = useMemo(() => d?.logs.find((l) => l.event_id === logSel), [d, logSel]);
  const root = useMemo(() => d?.spans.find((s) => !s.parent_span_id) ?? d?.spans[0], [d]);
  const notFound = trace.isError && trace.error instanceof ApiRequestError && trace.error.status === 404;
  const title = root ? (root.http_method && root.http_route ? `${root.http_method} ${root.http_route}` : root.name) : "Trace";

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            <Link to="/traces" className="text-ink-3 hover:text-ink" title="Back to traces">
              <ArrowLeft size={16} />
            </Link>
            <span className="font-mono">{title}</span>
          </span>
        }
        sub={
          d ? (
            <span className="flex flex-wrap items-center gap-2">
              <span className="tabular">{fmtTs(d.start_ts, { ms: true })}</span>
              <span>· {fmtUs(d.duration_us)}</span>
              <span>· {d.spans.length} spans</span>
              {d.errors.length > 0 && <span className="text-critical">· {d.errors.length} errors</span>}
              <span className="inline-flex items-center gap-1 font-mono text-ink-3">
                {id}
                <CopyButton text={id ?? ""} label="Copy trace id" />
              </span>
            </span>
          ) : undefined
        }
      >
        {d && (
          <Link to={`/logs?q=${encodeURIComponent(`trace_id:${id}`)}&from=${encodeURIComponent(new Date(new Date(d.start_ts).getTime() - 300000).toISOString())}&to=${encodeURIComponent(new Date(new Date(d.start_ts).getTime() + d.duration_us / 1000 + 300000).toISOString())}`} className="inline-flex h-7 items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 text-xs font-medium text-ink hover:bg-surface-2">
            <ScrollText size={13} /> Open in Logs
          </Link>
        )}
      </PageHeader>
      {notFound ? (
        <EmptyState title="Trace not found" hint={(trace.error as ApiRequestError).body.message ?? "No spans are stored for this trace id."} action={<Button onClick={() => history.back()}>Go back</Button>} />
      ) : trace.isError ? (
        <ErrorState error={trace.error} retry={() => trace.refetch()} />
      ) : trace.isPending || !d ? (
        <Skeleton rows={12} className="p-4" />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-none flex-wrap items-center gap-3 border-b border-border bg-surface px-4 py-1.5 text-[11px] text-ink-2">
            {d.services.map((s) => (
              <span key={s} className="inline-flex items-center gap-1">
                <span className="inline-block h-2 w-2 rounded-sm" style={{ background: serviceColor(d.services, s) }} aria-hidden />
                {s}
              </span>
            ))}
            <span className="ml-auto text-ink-3">click a span for its attributes · ▸ collapses a subtree</span>
          </div>
          <div className="flex min-h-0 flex-1">
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              <Waterfall spans={d.spans} services={d.services} selectedId={selId} onSelect={(s) => set({ span: s.span_id, log: undefined }, true)} className="min-h-0 flex-[3]" />
              <div className="flex min-h-0 flex-[2] flex-col border-t border-border">
                <Tabs
                  size="sm"
                  className="px-2"
                  value={tab}
                  onChange={(t) => setTab(t as "logs" | "errors")}
                  items={[
                    { id: "logs", label: "Logs", count: d.logs.length },
                    { id: "errors", label: "Errors", count: d.errors.length },
                  ]}
                />
                {tab === "logs" ? (
                  d.logs.length === 0 ? (
                    <EmptyState title="No log lines carry this trace id" hint="The SDK stamps trace_id on every line logged inside an OpenTelemetry span." className="py-6" />
                  ) : (
                    <LogTable items={d.logs} columns={["service", "logger"]} dense selectedId={logSel} onSelect={(e) => set({ log: e.event_id, span: undefined }, true)} className="min-h-0 flex-1" />
                  )
                ) : d.errors.length === 0 ? (
                  <EmptyState title="No error occurrences in this trace" className="py-6" />
                ) : (
                  <ul className="min-h-0 flex-1 overflow-y-auto">
                    {d.errors.map((e) => (
                      <li key={e.event_id} className="flex items-center gap-2 border-b border-border px-3 py-1.5 text-xs">
                        <LevelBadge level={e.level} compact />
                        <Link to={`/issues/${e.issue_id}?occurrence=${e.event_id}`} className="min-w-0 flex-1 truncate font-mono text-ink hover:text-accent">
                          {e.title || `${e.exception_type}: ${e.exception_message}`}
                        </Link>
                        <Badge>{e.service}</Badge>
                        <span className="tabular text-ink-3">{fmtTs(e.ts, { ms: true })}</span>
                        <Link to={`/issues/${e.issue_id}`} className="inline-flex items-center gap-1 text-accent hover:underline">
                          <AlertOctagon size={11} /> issue
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
            {selected && <SpanPanel span={selected} traceStart={d.start_ts} onClose={() => set({ span: undefined }, true)} />}
            {selectedLog && <LogDetailPanel event={selectedLog} onClose={() => set({ log: undefined }, true)} onFilter={() => {}} onExclude={() => {}} width={460} />}
          </div>
        </div>
      )}
    </>
  );
}

function SpanPanel({ span, traceStart, onClose }: { span: Span; traceStart: string; onClose: () => void }) {
  const offsetUs = (new Date(span.start_ts).getTime() - new Date(traceStart).getTime()) * 1000 + microFraction(span.start_ts) - microFraction(traceStart);
  const core: Record<string, string | number | undefined> = {
    service: span.service,
    env: span.env,
    kind: span.kind,
    status: span.status,
    status_message: span.status_message,
    scope: span.scope,
    release: span.release,
    host: span.hostname,
    span_id: span.span_id,
    parent_span_id: span.parent_span_id,
    "http.method": span.http_method,
    "http.route": span.http_route,
    "http.status": span.http_status || undefined,
  };
  return (
    <SidePanel
      open
      width={440}
      onClose={onClose}
      title={
        <span className="flex items-center gap-2 font-mono">
          <span className="truncate">{span.name}</span>
          <span className="text-xs font-normal tabular text-ink-2">{fmtUs(span.duration_us)}</span>
        </span>
      }
      actions={<CopyButton text={JSON.stringify(span, null, 2)} label="Copy JSON" size="sm" />}
    >
      <div className="border-b border-border px-3 py-2 text-xs text-ink-2">
        starts at <span className="tabular text-ink">+{fmtUs(Math.max(0, offsetUs))}</span> · {fmtTs(span.start_ts, { ms: true })}
        {span.status === "error" && <span className="ml-2 rounded bg-surface-3 px-1.5 py-px text-[11px] font-medium text-critical">error{span.status_message ? `: ${span.status_message}` : ""}</span>}
      </div>
      <Section title="Span">
        <KeyValueList entries={core} />
      </Section>
      <Section title={`Attributes (${Object.keys(span.attrs ?? {}).length})`}>
        <KeyValueList entries={span.attrs} emptyText="no attributes" />
      </Section>
      {(span.events?.length ?? 0) > 0 && (
        <Section title={`Events (${span.events!.length})`}>
          <ol className="space-y-2">
            {span.events!.map((e, i) => (
              <li key={i} className="rounded-md border border-border bg-surface-2 px-2 py-1.5 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-ink">{e.name}</span>
                  <span className="tabular text-ink-3">{fmtTs(e.ts, { ms: true })}</span>
                </div>
                {e.attrs && Object.keys(e.attrs).length > 0 && (
                  <div className="mt-1">
                    {e.name === "exception" && e.attrs["exception.stacktrace"] ? (
                      <>
                        <KeyValueList entries={{ type: e.attrs["exception.type"], message: e.attrs["exception.message"] }} />
                        <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap rounded border border-border bg-surface p-1.5 font-mono text-[11px] text-ink">{e.attrs["exception.stacktrace"]}</pre>
                      </>
                    ) : (
                      <KeyValueList entries={e.attrs} />
                    )}
                  </div>
                )}
              </li>
            ))}
          </ol>
        </Section>
      )}
    </SidePanel>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border-b border-border px-3 py-2">
      <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">{title}</div>
      {children}
    </div>
  );
}
