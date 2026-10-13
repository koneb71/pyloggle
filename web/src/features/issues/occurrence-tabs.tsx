import { useMemo, useState } from "react";
import { Link } from "react-router";
import { ExternalLink, GitBranch } from "lucide-react";
import type { Occurrence } from "@/api/types";
import { Badge, EmptyState, JsonView, KeyValueList, LinkButton, SegmentedControl } from "@/components/ui";
import { RequestBody, expectsBody } from "@/components/request-body";
import { term } from "@/lib/query";
import { sourceLabel } from "@/lib/fields";
import { ExceptionView } from "./exception-view";
import { Breadcrumbs } from "./breadcrumbs";
import { LogList } from "@/features/logs/log-list";

export const OCCURRENCE_TABS = [
  { id: "exception", label: "What went wrong" },
  { id: "breadcrumbs", label: "Steps before the error" },
  { id: "request", label: "Web request" },
  { id: "context", label: "User & system" },
  { id: "tags", label: "Labels & details" },
  { id: "logs", label: "Messages" },
  { id: "activity", label: "Activity" },
  { id: "raw", label: "Full data" },
] as const;

/** The content of one tab for one time the issue happened. */
export function OccurrenceTab({ tab, occ, onFilterLogs }: { tab: string; occ: Occurrence; onFilterLogs: (q: string) => void }) {
  const filterActions = useMemo(
    () => ({
      onFilter: (k: string, v: string) => onFilterLogs(term(k, v)),
      onExclude: (k: string, v: string) => onFilterLogs(term(k, v, true)),
    }),
    [onFilterLogs],
  );
  switch (tab) {
    case "breadcrumbs":
      return <Breadcrumbs items={occ.breadcrumbs} anchorTs={occ.ts} />;
    case "request":
      return <RequestTab occ={occ} onFilterLogs={onFilterLogs} />;
    case "context":
      return (
        <div className="grid gap-6 md:grid-cols-2">
          <Block title="Person">
            <KeyValueList
              entries={{ "user.id": occ.user?.id, "user.email": occ.user?.email, "user.username": occ.user?.username, "user.ip": occ.user?.ip, id_hash: occ.user_id_hash || occ.user?.id_hash }}
              emptyText="No signed-in person was attached to this error."
              actions={{ onFilter: (k, v) => onFilterLogs(term(k === "id_hash" ? "user" : `attrs.${k}`, v)) }}
            />
          </Block>
          <Block title="System">
            <KeyValueList
              entries={{
                "runtime.name": occ.runtime?.name,
                "runtime.version": occ.runtime?.version,
                "runtime.platform": occ.runtime?.platform,
                "runtime.pid": occ.runtime?.pid,
                "runtime.thread": occ.runtime?.thread,
                hostname: occ.hostname,
                release: occ.release,
                "sdk.name": occ.sdk ? `${occ.sdk.name} ${occ.sdk.version}` : undefined,
                source: sourceLabel(occ.source),
                "mechanism.type": occ.mechanism ? `${occ.mechanism.type}${occ.mechanism.handled === false ? " (not caught: the app crashed)" : ""}` : undefined,
              }}
              actions={{ onFilter: (k, v) => (k === "hostname" || k === "release" ? onFilterLogs(term(k === "hostname" ? "host" : k, v)) : undefined) }}
            />
          </Block>
        </div>
      );
    case "tags":
      return (
        <div className="grid gap-6 md:grid-cols-2">
          <Block title="Labels" hint="Tags your code set (for example a team or a feature flag).">
            <KeyValueList entries={occ.tags} keyPrefix="tags." actions={filterActions} emptyText="No labels were set." />
          </Block>
          <Block title="Details" hint="Extra fields sent with the error.">
            <KeyValueList entries={occ.attrs} keyPrefix="attrs." actions={filterActions} emptyText="No extra details were sent." />
          </Block>
          {occ.extra !== undefined && occ.extra !== null && (
            <Block title="Extra data" className="md:col-span-2">
              <JsonView value={occ.extra} />
            </Block>
          )}
        </div>
      );
    case "logs":
      return <IssueLogs occ={occ} />;
    case "raw":
      return (
        <div>
          <p className="mb-3 text-[13px] text-ink-3">Everything recorded for this time, as stored. Useful when reporting a bug.</p>
          <JsonView value={occ} defaultOpen={1} />
        </div>
      );
    default:
      return <ExceptionView occ={occ} />;
  }
}

function RequestTab({ occ, onFilterLogs }: { occ: Occurrence; onFilterLogs: (q: string) => void }) {
  const h = occ.http;
  if (!h && !occ.http_method && !occ.http_route)
    return <EmptyState title="No web request attached" hint="This happened outside a web request (for example in a background job), or the app doesn't report request details." className="py-6" />;
  const method = h?.method || occ.http_method;
  const route = h?.route || occ.http_route;
  const status = h?.status || occ.http_status;
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="accent">{method}</Badge>
        <span className="min-w-0 break-all font-mono text-[13px] text-ink">{h?.url || route}</span>
        {status ? (
          <Badge tone={status >= 500 ? "critical" : status >= 400 ? "warning" : "good"} title={status >= 500 ? "The server failed to answer this request" : undefined}>
            {status >= 500 ? `Failed · ${status}` : status}
          </Badge>
        ) : null}
        {occ.trace_id && (
          <LinkButton to={`/traces/${occ.trace_id}?at=${encodeURIComponent(occ.ts)}`} size="xs" icon={<GitBranch size={13} />} className="ml-auto">
            See request timeline
          </LinkButton>
        )}
      </div>
      <KeyValueList
        entries={{
          "http.route": route,
          "http.query": h?.query,
          ip: h?.client_ip,
          remote_addr: h?.remote_addr,
          "server.address": h?.server_addr,
          user_agent: h?.user_agent,
          request_id: occ.request_id,
          trace_id: occ.trace_id,
          span_id: occ.span_id,
        }}
        actions={{ onFilter: (k, v) => onFilterLogs(term(k, v)) }}
      />
      {h?.headers && Object.keys(h.headers).length > 0 && (
        <Block title="Headers">
          <KeyValueList entries={h.headers} humanize={false} mono />
        </Block>
      )}
      {(h?.body || expectsBody(method)) && (
        <Block title="Request body">
          <RequestBody body={h?.body} truncated={occ.attrs?.request_body_truncated === "true"} method={method} />
        </Block>
      )}
    </div>
  );
}

function IssueLogs({ occ }: { occ: Occurrence }) {
  const [mode, setMode] = useState<"request" | "issue">(occ.request_id ? "request" : "issue");
  const q = mode === "request" && occ.request_id ? `request_id:${occ.request_id}` : `issue:${occ.issue_id}`;
  const center = new Date(occ.ts).getTime();
  const range = mode === "request" ? { from: new Date(center - 10 * 60_000).toISOString(), to: new Date(center + 10 * 60_000).toISOString() } : { from: "-7d", to: "" };
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <SegmentedControl
          label="Which messages"
          value={mode}
          onChange={setMode}
          options={[
            { value: "request", label: "From this request", title: occ.request_id ? "Messages logged while handling the same request" : "No request was recorded for this time" },
            { value: "issue", label: "All times of this issue" },
          ]}
        />
        <Link to={`/logs?q=${encodeURIComponent(q)}&from=${encodeURIComponent(range.from)}${range.to ? `&to=${encodeURIComponent(range.to)}` : ""}`} className="inline-flex items-center gap-1 text-[13px] font-medium text-accent hover:underline">
          Open in Logs <ExternalLink size={13} />
        </Link>
      </div>
      {mode === "request" && !occ.request_id ? (
        <EmptyState title="No request recorded for this time" hint="Switch to all times of this issue to see the messages logged with it." className="py-6" />
      ) : (
        <LogList q={q} from={range.from} to={range.to} highlightEventId={occ.event_id} limit={200} dense />
      )}
    </div>
  );
}

function Block({ title, hint, children, className }: { title: string; hint?: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={className}>
      <h4 className="text-[13px] font-semibold text-ink">{title}</h4>
      {hint && <p className="mb-2 text-xs text-ink-3">{hint}</p>}
      <div className={hint ? "" : "mt-2"}>{children}</div>
    </section>
  );
}
