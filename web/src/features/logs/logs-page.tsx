import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowUp, Columns3, PanelLeft, Radio, WrapText } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useLogHistogram, useLogs } from "@/api/hooks";
import { get, ApiRequestError } from "@/api/client";
import type { LogEvent, LogsResponse } from "@/api/types";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { Button, Checkbox, EmptyState, ErrorState, IconButton, Input, LoadMore, Page, Popover, SegmentedControl, Select, Skeleton, UpdatingDot, friendlyError } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { addTerm } from "@/lib/query";
import { pluralize } from "@/lib/format";
import { fieldLabel } from "@/lib/fields";
import { useMediaQuery } from "@/lib/use-media";
import { SearchBar } from "./search-bar";
import { FacetsPanel } from "./facets-panel";
import { LogTable } from "./log-list";
import { LogDetailPanel } from "./log-detail";
import { PatternsView } from "./patterns";
import { SavedViewsMenu, type ViewState } from "./saved-views";
import { BUILTIN_COLUMNS, DEFAULT_COLUMNS, FACET_TO_FIELD } from "./log-fields";
import { AnalyticsView } from "./analytics";
import { ExportMenu } from "./export-menu";
import { DEFAULT_ANALYTICS, isMetric, type AnalyticsState } from "./analytics-model";

/** What the chart can be colored by (sidebar field names). */
const GROUPS = ["level", "service", "env", "logger", "http_status", "hostname", "release", "source"];
const PAGE = 300;

export function LogsPage() {
  const { range, setRange, zoom, isLive } = useRange();
  const { get: g, set } = useUrlState();
  const q = g("q");
  const groupBy = g("group_by", "level");
  const view = g("view", "list");
  const live = g("live") === "1" && isLive;
  const wrap = g("wrap") === "1";
  const showFacets = g("facets_panel") !== "0";
  const cols = useMemo(() => (g("cols") ? g("cols").split(",").filter(Boolean) : DEFAULT_COLUMNS), [g]);
  const pinnedFacets = useMemo(() => (g("facets") ? g("facets").split(",").filter(Boolean) : []), [g]);
  const selId = g("sel");
  const viewName = g("view_name");
  const qc = useQueryClient();
  const analytics = useMemo<AnalyticsState>(() => {
    const m = g("metric", DEFAULT_ANALYTICS.metric);
    const gb = g("group_by_a", DEFAULT_ANALYTICS.groupBy);
    return {
      groupBy: gb === "none" ? "" : gb,
      metric: isMetric(m) ? m : DEFAULT_ANALYTICS.metric,
      of: g("of", DEFAULT_ANALYTICS.of),
      limit: Number(g("top", String(DEFAULT_ANALYTICS.limit))) || DEFAULT_ANALYTICS.limit,
    };
  }, [g]);
  const setAnalytics = useCallback(
    (p: Partial<AnalyticsState>) =>
      set({
        ...(p.groupBy !== undefined ? { group_by_a: p.groupBy === DEFAULT_ANALYTICS.groupBy ? undefined : p.groupBy || "none" } : {}),
        ...(p.metric !== undefined ? { metric: p.metric === DEFAULT_ANALYTICS.metric ? undefined : p.metric } : {}),
        ...(p.of !== undefined ? { of: p.of === DEFAULT_ANALYTICS.of ? undefined : p.of } : {}),
        ...(p.limit !== undefined ? { top: p.limit === DEFAULT_ANALYTICS.limit ? undefined : p.limit } : {}),
        view_name: undefined,
      }),
    [set],
  );

  const setQuery = useCallback((nq: string) => set({ q: nq || undefined, sel: undefined, view_name: undefined }), [set]);
  const addFilter = useCallback((t: string) => setQuery(addTerm(q, ...splitTerm(t))), [q, setQuery]);

  const hist = useLogHistogram({ q, from: range.from, to: range.to, group_by: groupBy, buckets: 90 }, view !== "analytics");
  const logs = useLogs({ q, from: range.from, to: range.to, limit: PAGE }, view === "list");
  const queryError = useMemo(() => {
    const e = hist.error ?? logs.error;
    if (e instanceof ApiRequestError && e.status === 400) return { message: e.body.message || e.message, pos: e.body.pos };
    return null;
  }, [hist.error, logs.error]);

  // --- rows: base page + older pages + live-tail prepends ---
  const [older, setOlder] = useState<LogEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [fresh, setFresh] = useState<LogEvent[]>([]);
  const [pendingFresh, setPendingFresh] = useState<LogEvent[]>([]);
  const [atTop, setAtTop] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [liveError, setLiveError] = useState("");
  const newest = useRef<string | undefined>(undefined);
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setOlder([]);
    setFresh([]);
    setPendingFresh([]);
    setCursor(logs.data?.next_cursor);
    setMoreError(null);
    newest.current = logs.data?.newest_ts;
  }, [logs.data]);

  useEffect(() => {
    if (!live || view !== "list") return;
    let stop = false;
    const tick = async () => {
      if (stop || !newest.current) return;
      try {
        const r = await get<LogsResponse>("/logs", { q, from: range.from, after: newest.current, limit: 500 });
        setLiveError("");
        if (stop || r.items.length === 0) return;
        const rows = [...r.items].reverse(); // server returns ascending for `after`
        newest.current = r.newest_ts ?? newest.current;
        if (atTop) setFresh((f) => [...rows, ...f].slice(0, 5000));
        else setPendingFresh((p) => [...rows, ...p].slice(0, 5000));
        qc.invalidateQueries({ queryKey: ["logs-histogram"] });
      } catch (e) {
        setLiveError(friendlyError(e).title);
      }
    };
    const id = setInterval(tick, 3000);
    return () => {
      stop = true;
      clearInterval(id);
    };
  }, [live, view, q, range.from, atTop, qc]);

  const items = useMemo(() => [...fresh, ...(logs.data?.items ?? []), ...older], [fresh, logs.data, older]);
  const selected = useMemo(() => items.find((i) => i.event_id === selId), [items, selId]);
  // Below 1280px there is room for the list and one side panel: details win over filters.
  const roomy = useMediaQuery("(min-width: 1280px)");
  const facetsVisible = showFacets && (roomy || !selected);
  const toggleFacets = () => {
    if (facetsVisible) set({ facets_panel: "0" });
    else set({ facets_panel: undefined, ...(roomy ? {} : { sel: undefined }) });
  };
  const loadMore = async () => {
    if (!cursor || loadingMore) return;
    setLoadingMore(true);
    setMoreError(null);
    try {
      const r = await get<LogsResponse>("/logs", { q, from: range.from, to: range.to, limit: PAGE, cursor });
      setOlder((o) => [...o, ...r.items]);
      setCursor(r.next_cursor);
    } catch (e) {
      setMoreError(e);
    } finally {
      setLoadingMore(false);
    }
  };
  const total = hist.data?.buckets.reduce((n, b) => n + b.total, 0) ?? 0;
  const currentView: ViewState = {
    q,
    from: range.from,
    to: range.to,
    group_by: groupBy,
    cols: cols.join(","),
    facets: pinnedFacets.join(","),
    view: view === "list" ? undefined : view,
    group_by_a: view === "analytics" ? analytics.groupBy || "none" : undefined,
    metric: view === "analytics" ? analytics.metric : undefined,
    of: view === "analytics" ? analytics.of : undefined,
  };

  const histTotal = hist.data ? total : undefined;
  return (
    <Page
      width="full"
      title="Logs"
      description="Every message your apps wrote. Search, filter and chart them."
      actions={
        <>
          <SavedViewsMenu
            current={currentView}
            activeName={viewName || undefined}
            onApply={(v, name) =>
              set({
                q: v.q || undefined,
                from: v.from,
                to: v.to || undefined,
                group_by: v.group_by,
                cols: v.cols,
                facets: v.facets,
                view: v.view,
                group_by_a: v.group_by_a,
                metric: v.metric,
                of: v.of,
                view_name: name,
                sel: undefined,
              })
            }
          />
          <ExportMenu q={q} range={range} />
          <Button
            icon={live ? <span className="live-dot" aria-hidden /> : <Radio size={14} />}
            active={live}
            disabled={!isLive}
            aria-pressed={live}
            title={isLive ? (live ? "Stop showing new messages as they arrive" : "Show new messages as they arrive (every 3 seconds)") : "Live updates need a recent time range, like Last 15 minutes"}
            onClick={() => set({ live: live ? undefined : "1" })}
          >
            {live ? "Live" : "Go live"}
          </Button>
          <TimeRangePicker value={range} onChange={setRange} onRefresh={() => (hist.refetch(), logs.refetch())} refreshing={hist.isFetching || logs.isFetching} />
        </>
      }
      toolbar={<SearchBar value={q} onChange={setQuery} error={queryError} range={range} />}
    >
      <div className="flex min-h-0 flex-1">
        {facetsVisible && (
          <aside className="w-64 flex-none border-r border-border bg-surface" aria-label="Filters">
            <FacetsPanel q={q} range={range} onChange={setQuery} extraFacets={pinnedFacets} onExtraFacets={(f) => set({ facets: f.join(",") || undefined })} />
          </aside>
        )}
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex-none border-b border-border bg-surface px-4 pb-2 pt-2.5">
            <div className="flex flex-wrap items-center gap-2">
              <IconButton label={facetsVisible ? "Hide filters" : "Show filters"} icon={<PanelLeft size={15} />} active={facetsVisible} onClick={toggleFacets} />
              <SegmentedControl
                label="View"
                value={view as "list" | "patterns" | "analytics"}
                onChange={(v) => set({ view: v === "list" ? undefined : v, sel: undefined })}
                options={[
                  { value: "list", label: "Messages" },
                  { value: "patterns", label: "Similar messages", title: "Messages grouped by their shape" },
                  { value: "analytics", label: "Charts", title: "Count or measure messages over time" },
                ]}
              />
              {view !== "analytics" && (
                <label className="ml-2 flex items-center gap-2 text-[13px] text-ink-3">
                  Color chart by
                  <Select value={groupBy} onChange={(e) => set({ group_by: e.target.value === "level" ? undefined : e.target.value })} className="w-auto min-w-32" aria-label="Color the chart by">
                    {GROUPS.map((x) => (
                      <option key={x} value={x}>
                        {fieldLabel(FACET_TO_FIELD[x] ?? x)}
                      </option>
                    ))}
                  </Select>
                </label>
              )}
              <div className="ml-auto flex items-center gap-2">
                <UpdatingDot active={(hist.isFetching && !!hist.data) || (logs.isFetching && !!logs.data)} />
                {view !== "analytics" && histTotal !== undefined && <span className="text-[13px] tabular text-ink-2">{pluralize(histTotal, "message")}</span>}
                {view === "list" && (
                  <>
                    <span className="mx-1 h-5 w-px bg-border" aria-hidden />
                    <IconButton label={wrap ? "Show messages on one line" : "Show long messages in full"} icon={<WrapText size={15} />} active={wrap} onClick={() => set({ wrap: wrap ? undefined : "1" })} />
                    <ColumnPicker cols={cols} items={items} onChange={(c) => set({ cols: c.join(",") || undefined })} />
                  </>
                )}
              </div>
            </div>
            {view !== "analytics" &&
              !queryError &&
              (hist.isError && !hist.data ? (
                <ErrorState compact error={hist.error} retry={() => hist.refetch()} title="Couldn't load the chart" />
              ) : (
                <TimelineHistogram data={hist.data} loading={hist.isPending} height={140} onZoom={zoom} className="mt-2.5" />
              ))}
          </div>
          {view === "analytics" ? (
            <AnalyticsView q={q} range={range} state={analytics} onState={setAnalytics} onFilter={(f, v) => setQuery(addTerm(q, f, v))} onZoom={zoom} />
          ) : view === "patterns" ? (
            <PatternsView q={q} range={range} onFilter={(phrase) => setQuery(addTerm(q, "message", phrase))} />
          ) : queryError ? (
            <EmptyState title="Fix the search above to see messages" hint="Click the ? in the search box for examples of what you can type." />
          ) : logs.isError && !logs.data ? (
            <ErrorState error={logs.error} retry={() => logs.refetch()} />
          ) : logs.isPending ? (
            <Skeleton rows={12} className="bg-surface p-4" />
          ) : items.length === 0 ? (
            <EmptyState title="No messages" hint={q ? "Nothing matches this search. Try a wider time range or remove a filter." : "Nothing was received in this time range."} />
          ) : (
            <div className="relative flex min-h-0 flex-1 flex-col">
              {live && liveError && (
                <div role="status" className="flex-none border-b border-border bg-warning/10 px-4 py-1.5 text-[13px] text-warning-text">
                  Live updates paused ({liveError}). Trying again every few seconds.
                </div>
              )}
              {pendingFresh.length > 0 && (
                <button
                  type="button"
                  className="absolute left-1/2 top-10 z-20 inline-flex -translate-x-1/2 items-center gap-1 rounded-full bg-accent-strong px-3 py-1 text-[13px] font-medium text-accent-ink shadow-[var(--shadow)]"
                  onClick={() => {
                    setFresh((f) => [...pendingFresh, ...f]);
                    setPendingFresh([]);
                    scrollRef.current?.scrollTo({ top: 0 });
                  }}
                >
                  <ArrowUp size={13} />
                  {pluralize(pendingFresh.length, "new message")}
                </button>
              )}
              <LogTable
                items={items}
                columns={cols}
                selectedId={selId}
                onSelect={(e) => set({ sel: e.event_id }, true)}
                wrap={wrap}
                scrollRef={scrollRef}
                onScrollTop={setAtTop}
                footer={<LoadMore shown={items.length} noun="messages shown" hasMore={!!cursor} loading={loadingMore} error={moreError} onMore={loadMore} label="Show earlier" />}
              />
            </div>
          )}
        </div>
        {selected && (
          <LogDetailPanel event={selected} onClose={() => set({ sel: undefined }, true)} onFilter={addFilter} onExclude={addFilter} onPickColumn={(c) => !cols.includes(c) && set({ cols: [...cols, c].join(",") })} />
        )}
      </div>
    </Page>
  );
}

function ColumnPicker({ cols, items, onChange }: { cols: string[]; items: LogEvent[]; onChange: (c: string[]) => void }) {
  const toggle = (id: string) => onChange(cols.includes(id) ? cols.filter((c) => c !== id) : [...cols, id]);
  const [custom, setCustom] = useState("");
  // Detail keys from the first rows on screen, most common first.
  const attrKeys = useMemo(() => {
    const n = new Map<string, number>();
    for (const e of items.slice(0, 300)) for (const k of Object.keys(e.attrs ?? {})) n.set(k, (n.get(k) ?? 0) + 1);
    return [...n.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => k);
  }, [items]);
  return (
    <Popover align="end" width={300} trigger={({ toggle: t, open }) => <IconButton label="Choose columns" icon={<Columns3 size={15} />} onClick={t} active={open} />}>
      <div className="p-1.5">
        <div className="px-1 pb-1 text-xs font-medium text-ink-3">Columns</div>
        <div className="grid grid-cols-2 gap-x-2 gap-y-0.5">
          {BUILTIN_COLUMNS.map((c) => (
            <Checkbox key={c.id} label={c.label} checked={cols.includes(c.id)} onChange={() => toggle(c.id)} className="px-1 py-1" />
          ))}
        </div>
        {attrKeys.length > 0 && (
          <>
            <div className="mt-2 px-1 pb-1 text-xs font-medium text-ink-3">Details in these messages</div>
            <div className="max-h-40 overflow-y-auto">
              {attrKeys.slice(0, 40).map((k) => (
                <Checkbox key={k} label={<span title={`attrs.${k}`}>{fieldLabel(k)}</span>} checked={cols.includes(`attrs.${k}`)} onChange={() => toggle(`attrs.${k}`)} className="flex px-1 py-1" />
              ))}
            </div>
          </>
        )}
        <form
          className="mt-2 flex gap-1.5 border-t border-border px-1 pt-2"
          onSubmit={(e) => {
            e.preventDefault();
            const k = custom.trim();
            if (!k) return;
            const id = k.startsWith("attrs.") || k.startsWith("tags.") ? k : `attrs.${k}`;
            if (!cols.includes(id)) onChange([...cols, id]);
            setCustom("");
          }}
        >
          <Input value={custom} onChange={(e) => setCustom(e.target.value)} placeholder="Add a detail by name" aria-label="Detail name" />
          <Button type="submit">Add</Button>
        </form>
        <button type="button" className="mt-1.5 px-1 text-xs text-ink-3 hover:text-ink" onClick={() => onChange(DEFAULT_COLUMNS)}>
          Reset columns
        </button>
      </div>
    </Popover>
  );
}

/** "field:value" / "-field:value" → [field, value, negate] for addTerm. */
function splitTerm(t: string): [string, string, boolean] {
  const negate = t.startsWith("-");
  const body = negate ? t.slice(1) : t;
  const i = body.indexOf(":");
  if (i < 0) return ["message", unquote(body), negate];
  return [body.slice(0, i), unquote(body.slice(i + 1)), negate];
}

function unquote(v: string): string {
  if (v.length >= 2 && v.startsWith('"') && v.endsWith('"')) return v.slice(1, -1).replace(/\\"/g, '"');
  return v;
}

