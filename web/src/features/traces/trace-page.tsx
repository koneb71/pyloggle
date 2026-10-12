import { useCallback, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { AlertOctagon, ScrollText } from "lucide-react";
import { useTrace } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import type { Span } from "@/api/types";
import { Badge, CopyButton, EmptyState, ErrorState, KeyValueList, LevelBadge, LinkButton, MetaLine, Page, SidePanel, Skeleton, Swatch, Tabs } from "@/components/ui";
import { useUrlState } from "@/lib/url";
import { fmtTs, fmtWhen } from "@/lib/time";
import { pluralize } from "@/lib/format";
import { useSmallScreen } from "@/lib/use-media";
import { LogTable } from "@/features/logs/log-list";
import { LogDetailPanel } from "@/features/logs/log-detail";
import { issueHeadline } from "@/features/issues/issue-text";
import { Waterfall, serviceColor } from "./waterfall";
import { fmtUs, microFraction } from "./waterfall-layout";
import { kindWords, requestLabel, statusWords } from "./trace-words";

export function TracePage() {
  const { id } = useParams<{ id: string }>();
  const { get, set } = useUrlState();
  const nav = useNavigate();
  const at = get("at") || undefined;
  const selId = get("span");
  const logSel = get("log");
  const [tab, setTab] = useState<"logs" | "errors">("logs");
  const small = useSmallScreen();
  const trace = useTrace(id, at);
  const d = trace.data;
  const selected = useMemo(() => d?.spans.find((s) => s.span_id === selId), [d, selId]);
  const selectedLog = useMemo(() => d?.logs.find((l) => l.event_id === logSel), [d, logSel]);
  const root = useMemo(() => d?.spans.find((s) => !s.parent_span_id) ?? d?.spans[0], [d]);
  const notFound = trace.isError && trace.error instanceof ApiRequestError && trace.error.status === 404;
  // Messages around this request, as a Logs search: five minutes either side.
  const logsWindow = useMemo(() => {
    if (!d) return "";
    const start = new Date(d.start_ts).getTime();
    return `from=${encodeURIComponent(new Date(start - 300_000).toISOString())}&to=${encodeURIComponent(new Date(start + d.duration_us / 1000 + 300_000).toISOString())}`;
  }, [d]);
  const openLogs = useCallback((extra: string) => nav(`/logs?q=${encodeURIComponent([`trace_id:${id}`, extra].filter(Boolean).join(" "))}&${logsWindow}`), [nav, id, logsWindow]);

  return (
    <Page
      width="full"
      back={{ to: "/traces", label: "Requests" }}
      title={root ? <span className="break-all font-mono text-[17px]">{requestLabel(root)}</span> : notFound || trace.isError ? "Request" : <span className="skeleton inline-block h-6 w-72 align-middle" aria-label="Loading" />}
      description={
        d && (
          <MetaLine className="text-[13px]">
            <span title={fmtTs(d.start_ts, { ms: true })}>{fmtWhen(d.start_ts, { seconds: true })}</span>
            <span>took {fmtUs(d.duration_us)}</span>
            {pluralize(d.spans.length, "step")}
            {d.errors.length > 0 && <span className="text-critical-text">{pluralize(d.errors.length, "error")}</span>}
            <span className="inline-flex items-center gap-1" title="Request timeline ID (trace id)">
              <span className="font-mono text-[11px]">{id}</span>
              <CopyButton text={id ?? ""} label="Copy the request timeline ID" />
            </span>
          </MetaLine>
        )
      }
      actions={
        d && (
          <LinkButton to={`/logs?q=${encodeURIComponent(`trace_id:${id}`)}&${logsWindow}`} icon={<ScrollText size={14} />}>
            Messages from this request
          </LinkButton>
        )
      }
    >
      {notFound ? (
        <EmptyState title="This request isn't stored" hint={(trace.error as ApiRequestError).body.message || "Its steps may have been cleaned up by the storage limits."} action={<LinkButton to="/traces">Back to requests</LinkButton>} />
      ) : trace.isError ? (
        <ErrorState error={trace.error} retry={() => trace.refetch()} />
      ) : trace.isPending || !d ? (
        <Skeleton rows={12} className="bg-surface p-4" />
      ) : (
        <>
          <div className="flex flex-none flex-wrap items-center gap-x-4 gap-y-1 border-b border-border bg-surface px-4 py-2 text-xs text-ink-2 sm:px-5">
            <span className="text-ink-3">{d.services.length === 1 ? "App" : "Apps in this request"}</span>
            {d.services.map((s) => (
              <span key={s} className="inline-flex items-center gap-1.5">
                <Swatch color={serviceColor(d.services, s)} />
                {s}
              </span>
            ))}
            <span className="ml-auto text-ink-3 max-md:hidden">Click a step to see its details · the arrow hides the steps inside it</span>
          </div>
          <div className="flex min-h-0 flex-1">
            <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-surface">
              <Waterfall spans={d.spans} services={d.services} selectedId={selId} onSelect={(s) => set({ span: s.span_id, log: undefined }, true)} className="min-h-0 flex-[3]" />
              <div className="flex min-h-0 flex-[2] flex-col border-t border-border">
                <Tabs
                  className="px-4"
                  value={tab}
                  onChange={(t) => setTab(t as "logs" | "errors")}
                  items={[
                    { id: "logs", label: small ? "Messages" : "Messages logged during this request", count: d.logs.length },
                    { id: "errors", label: "Errors", count: d.errors.length },
                  ]}
                />
                {tab === "logs" ? (
                  d.logs.length === 0 ? (
                    <EmptyState title="No messages were logged during this request" hint="Messages are linked to a request when they are logged inside it with the SDK or OpenTelemetry." className="py-6" />
                  ) : (
                    <LogTable items={d.logs} columns={["service", "logger"]} dense selectedId={logSel} onSelect={(e) => set({ log: e.event_id, span: undefined }, true)} className="min-h-0 flex-1" />
                  )
                ) : d.errors.length === 0 ? (
                  <EmptyState title="No errors during this request" className="py-6" />
                ) : (
                  <ul className="min-h-0 flex-1 divide-y divide-border overflow-y-auto">
                    {d.errors.map((e) => {
                      const { headline, message } = issueHeadline({ title: e.title, exception_type: e.exception_type, last_message: e.exception_message || e.message });
                      return (
                        <li key={e.event_id}>
                          <Link to={`/issues/${e.issue_id}?event=${e.event_id}`} className="flex items-center gap-3 px-4 py-2.5 text-[13px] hover:bg-surface-2">
                            <LevelBadge level={e.level} compact />
                            <span className="min-w-0 flex-1 truncate">
                              <span className="font-semibold text-ink">{headline}</span>
                              {message && <span className="ml-2 text-ink-2">{message}</span>}
                            </span>
                            <span className="flex-none text-xs text-ink-3 max-sm:hidden">{e.service}</span>
                            <span className="flex-none text-xs tabular text-ink-3 max-sm:hidden">{fmtWhen(e.ts, { seconds: true })}</span>
                            <span className="inline-flex flex-none items-center gap-1 text-xs font-medium text-accent">
                              <AlertOctagon size={13} /> <span className="max-sm:sr-only">Open issue</span>
                            </span>
                          </Link>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </div>
            </div>
            {selected && <StepPanel span={selected} traceStart={d.start_ts} onClose={() => set({ span: undefined }, true)} />}
            {selectedLog && <LogDetailPanel event={selectedLog} onClose={() => set({ log: undefined }, true)} onFilter={openLogs} onExclude={openLogs} width={460} />}
          </div>
        </>
      )}
    </Page>
  );
}

function StepPanel({ span, traceStart, onClose }: { span: Span; traceStart: string; onClose: () => void }) {
  const offsetUs = (new Date(span.start_ts).getTime() - new Date(traceStart).getTime()) * 1000 + microFraction(span.start_ts) - microFraction(traceStart);
  const about: Record<string, string | number | undefined> = {
    service: span.service,
    env: span.env,
    kind: kindWords(span.kind),
    status: statusWords(span.status),
    status_message: span.status_message,
    "http.method": span.http_method,
    "http.route": span.http_route,
    "http.status": span.http_status || undefined,
    release: span.release,
    host: span.hostname,
    scope: span.scope,
    span_id: span.span_id,
    parent_span_id: span.parent_span_id,
  };
  const failed = span.status === "error";
  return (
    <SidePanel open width={460} onClose={onClose} title="Step details" actions={<CopyButton text={JSON.stringify(span, null, 2)} label="Copy raw data" size="sm" />}>
      <div className="border-b border-border px-4 py-3">
        <div className="break-words font-mono text-[13px] font-medium text-ink">{requestLabel(span)}</div>
        <MetaLine className="mt-1 text-[13px]">
          <span>took {fmtUs(span.duration_us)}</span>
          <span>started {fmtUs(Math.max(0, offsetUs))} into the request</span>
        </MetaLine>
        {failed && (
          <Badge tone="critical" className="mt-2">
            Failed{span.status_message ? `: ${span.status_message}` : ""}
          </Badge>
        )}
      </div>
      <Block title="About this step">
        <KeyValueList entries={about} />
      </Block>
      <Block title="Details" hint="Fields recorded with this step.">
        <KeyValueList entries={span.attrs} emptyText="No details were recorded." />
      </Block>
      {(span.events?.length ?? 0) > 0 && (
        <Block title="What happened during this step">
          <ol className="space-y-2">
            {span.events!.map((e, i) => (
              <li key={i} className="rounded-md border border-border bg-surface-2 px-3 py-2 text-[13px]">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-ink">{e.name === "exception" ? "An error was raised" : e.name}</span>
                  <span className="text-xs tabular text-ink-3">{fmtWhen(e.ts, { seconds: true })}</span>
                </div>
                {e.attrs && Object.keys(e.attrs).length > 0 && (
                  <div className="mt-1.5">
                    {e.name === "exception" && e.attrs["exception.stacktrace"] ? (
                      <>
                        <KeyValueList entries={{ "exception.type": e.attrs["exception.type"], "exception.message": e.attrs["exception.message"] }} />
                        <pre className="mt-2 max-h-60 overflow-auto whitespace-pre-wrap rounded border border-border bg-surface p-2 font-mono text-[11px] text-ink">{e.attrs["exception.stacktrace"]}</pre>
                      </>
                    ) : (
                      <KeyValueList entries={e.attrs} />
                    )}
                  </div>
                )}
              </li>
            ))}
          </ol>
        </Block>
      )}
    </SidePanel>
  );
}

function Block({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="border-b border-border px-4 py-3">
      <h4 className="text-[13px] font-semibold text-ink">{title}</h4>
      {hint && <p className="mt-0.5 text-xs text-ink-3">{hint}</p>}
      <div className="mt-2">{children}</div>
    </section>
  );
}
