import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { ArrowUp, Columns3, LayoutList, PanelLeft, Radio, Regex, WrapText } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useLogHistogram, useLogs } from "@/api/hooks";
import { get, ApiRequestError } from "@/api/client";
import type { LogEvent, LogsResponse } from "@/api/types";
import { PageHeader } from "@/components/shell/app-shell";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { TimelineHistogram } from "@/components/charts/timeline-histogram";
import { Button, EmptyState, ErrorState, MenuItem, Popover, Select, Skeleton, Checkbox } from "@/components/ui";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { addTerm } from "@/lib/query";
import { fmtNumber } from "@/lib/time";
import { SearchBar } from "./search-bar";
import { FacetsPanel } from "./facets-panel";
import { LogTable } from "./log-list";
import { LogDetailPanel } from "./log-detail";
import { PatternsView } from "./patterns";
import { SavedViewsMenu, type ViewState } from "./saved-views";
import { BUILTIN_COLUMNS, DEFAULT_COLUMNS } from "./log-fields";

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

  const setQuery = useCallback((nq: string) => set({ q: nq || undefined, sel: undefined, view_name: undefined }), [set]);
  const addFilter = useCallback((t: string) => setQuery(addTerm(q, ...splitTerm(t))), [q, setQuery]);

  const hist = useLogHistogram({ q, from: range.from, to: range.to, group_by: groupBy, buckets: 90 });
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
  const newest = useRef<string | undefined>(undefined);
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setOlder([]);
    setFresh([]);
    setPendingFresh([]);
    setCursor(logs.data?.next_cursor);
    newest.current = logs.data?.newest_ts;
  }, [logs.data]);

  useEffect(() => {
    if (!live || view !== "list") return;
    let stop = false;
    const tick = async () => {
      if (stop || !newest.current) return;
      try {
        const r = await get<LogsResponse>("/logs", { q, from: range.from, after: newest.current, limit: 500 });
        if (stop || r.items.length === 0) return;
        const rows = [...r.items].reverse(); // server returns ascending for `after`
        newest.current = r.newest_ts ?? newest.current;
        if (atTop) setFresh((f) => [...rows, ...f].slice(0, 5000));
        else setPendingFresh((p) => [...rows, ...p].slice(0, 5000));
        qc.invalidateQueries({ queryKey: ["logs-histogram"] });
      } catch {
        /* keep polling */
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
  const loadMore = async () => {
    if (!cursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const r = await get<LogsResponse>("/logs", { q, from: range.from, to: range.to, limit: PAGE, cursor });
      setOlder((o) => [...o, ...r.items]);
      setCursor(r.next_cursor);
    } finally {
      setLoadingMore(false);
    }
  };
  const total = hist.data?.buckets.reduce((n, b) => n + b.total, 0) ?? 0;
  const currentView: ViewState = { q, from: range.from, to: range.to, group_by: groupBy, cols: cols.join(","), facets: pinnedFacets.join(",") };

  return (
    <>
      <PageHeader title="Logs" sub={hist.data ? `${fmtNumber(total)} events in range` : undefined}>
        <SavedViewsMenu current={currentView} activeName={viewName || undefined} onApply={(v, name) => set({ q: v.q || undefined, from: v.from, to: v.to || undefined, group_by: v.group_by, cols: v.cols, facets: v.facets, view_name: name, sel: undefined })} />
        <Button
          icon={<Radio size={13} className={live ? "text-good" : ""} />}
          active={live}
          disabled={!isLive}
          title={isLive ? "Tail new lines every 3s" : "Live tail needs a relative range (e.g. Last 15m)"}
          onClick={() => set({ live: live ? undefined : "1" })}
        >
          Live {live && <span className="live-dot" />}
        </Button>
        <TimeRangePicker value={range} onChange={setRange} onRefresh={() => (hist.refetch(), logs.refetch())} refreshing={hist.isFetching || logs.isFetching} />
      </PageHeader>
      <div className="flex-none border-b border-border bg-surface px-4 py-2">
        <SearchBar value={q} onChange={setQuery} error={queryError} range={range} />
      </div>
      <div className="flex min-h-0 flex-1">
        {showFacets && (
          <div className="w-60 flex-none border-r border-border bg-surface">
            <FacetsPanel q={q} range={range} onChange={setQuery} extraFacets={pinnedFacets} onExtraFacets={(f) => set({ facets: f.join(",") || undefined })} />
          </div>
        )}
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex-none border-b border-border bg-surface px-3 pt-2">
            <div className="flex items-center gap-2">
              <Button size="xs" variant="ghost" icon={<PanelLeft size={13} />} onClick={() => set({ facets_panel: showFacets ? "0" : undefined })} title={showFacets ? "Hide facets" : "Show facets"} aria-label="Toggle facets" />
              <span className="text-[11px] text-ink-3">Group by</span>
              <Select value={groupBy} onChange={(e) => set({ group_by: e.target.value === "level" ? undefined : e.target.value })} className="h-6 w-32 py-0 text-xs" aria-label="Group histogram by">
                {GROUPS.map((x) => (
                  <option key={x} value={x}>
                    {x}
                  </option>
                ))}
              </Select>
              {hist.isFetching && <span className="text-[11px] text-ink-3">updating…</span>}
              <div className="ml-auto flex items-center gap-1">
                <Button size="xs" variant={view === "list" ? "subtle" : "ghost"} icon={<LayoutList size={12} />} onClick={() => set({ view: undefined })}>
                  List
                </Button>
                <Button size="xs" variant={view === "patterns" ? "subtle" : "ghost"} icon={<Regex size={12} />} onClick={() => set({ view: "patterns", sel: undefined })}>
                  Patterns
                </Button>
                <span className="mx-1 h-4 w-px bg-border" />
                <Button size="xs" variant="ghost" icon={<WrapText size={12} />} active={wrap} onClick={() => set({ wrap: wrap ? undefined : "1" })} title="Wrap long messages" aria-label="Wrap messages" />
                <ColumnPicker cols={cols} attrKeys={Object.keys(items[0]?.attrs ?? {})} onChange={(c) => set({ cols: c.join(",") || undefined })} />
              </div>
            </div>
            <TimelineHistogram data={hist.data} loading={hist.isPending} height={150} onZoom={zoom} className="mt-1" />
          </div>
          {view === "patterns" ? (
            <PatternsView q={q} range={range} onFilter={(phrase) => setQuery(addTerm(q, "message", phrase))} />
          ) : logs.isError && !queryError ? (
            <ErrorState error={logs.error} retry={() => logs.refetch()} />
          ) : logs.isPending ? (
            <Skeleton rows={12} className="p-4" />
          ) : items.length === 0 ? (
            <EmptyState title="No log lines" hint={q ? "Try widening the time range or removing a filter." : "Nothing was ingested for this project in the selected range."} />
          ) : (
            <div className="relative flex min-h-0 flex-1 flex-col">
              {pendingFresh.length > 0 && (
                <button
                  type="button"
                  className="absolute left-1/2 top-2 z-20 -translate-x-1/2 rounded-full bg-accent px-3 py-1 text-xs font-medium text-accent-ink shadow-[var(--shadow)]"
                  onClick={() => {
                    setFresh((f) => [...pendingFresh, ...f]);
                    setPendingFresh([]);
                    scrollRef.current?.scrollTo({ top: 0 });
                  }}
                >
                  <ArrowUp size={12} className="mr-1 inline" />
                  {pendingFresh.length} new line{pendingFresh.length === 1 ? "" : "s"}
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
                footer={
                  <div className="flex items-center justify-center gap-2 py-2 font-sans text-[11px] text-ink-3">
                    {items.length.toLocaleString()} loaded
                    {cursor && (
                      <Button size="xs" onClick={loadMore} loading={loadingMore}>
                        Load older
                      </Button>
                    )}
                  </div>
                }
              />
            </div>
          )}
        </div>
        {selected && (
          <LogDetailPanel event={selected} onClose={() => set({ sel: undefined }, true)} onFilter={addFilter} onExclude={addFilter} onPickColumn={(c) => !cols.includes(c) && set({ cols: [...cols, c].join(",") })} />
        )}
      </div>
    </>
  );
}

function ColumnPicker({ cols, attrKeys, onChange }: { cols: string[]; attrKeys: string[]; onChange: (c: string[]) => void }) {
  const toggle = (id: string) => onChange(cols.includes(id) ? cols.filter((c) => c !== id) : [...cols, id]);
  const [custom, setCustom] = useState("");
  return (
    <Popover
      align="end"
      width={260}
      trigger={({ toggle: t }) => <Button size="xs" variant="ghost" icon={<Columns3 size={12} />} onClick={t} title="Columns" aria-label="Choose columns" />}
    >
      <div className="p-1">
        <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Columns</div>
        <div className="grid grid-cols-2 gap-x-2">
          {BUILTIN_COLUMNS.map((c) => (
            <Checkbox key={c.id} label={c.label} checked={cols.includes(c.id)} onChange={() => toggle(c.id)} className="px-1 py-0.5 text-xs" />
          ))}
        </div>
        {attrKeys.length > 0 && (
          <>
            <div className="mt-1 px-1 py-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Attributes (from selected rows)</div>
            <div className="max-h-40 overflow-y-auto">
              {attrKeys.slice(0, 40).map((k) => (
                <Checkbox key={k} label={<span className="font-mono">{k}</span>} checked={cols.includes(`attrs.${k}`)} onChange={() => toggle(`attrs.${k}`)} className="block px-1 py-0.5 text-xs" />
              ))}
            </div>
          </>
        )}
        <form
          className="mt-1 flex gap-1 px-1"
          onSubmit={(e) => {
            e.preventDefault();
            const k = custom.trim();
            if (!k) return;
            const id = k.startsWith("attrs.") || k.startsWith("tags.") ? k : `attrs.${k}`;
            if (!cols.includes(id)) onChange([...cols, id]);
            setCustom("");
          }}
        >
          <input value={custom} onChange={(e) => setCustom(e.target.value)} placeholder="attrs.key or tags.key" className="h-6 flex-1 rounded border border-border bg-surface px-1.5 font-mono text-[11px] text-ink" />
          <Button size="xs" type="submit">
            Add
          </Button>
        </form>
        <div className={clsx("mt-1 px-1 text-[10px] text-ink-3")}>
          <MenuItem onClick={() => onChange(DEFAULT_COLUMNS)}>Reset to default</MenuItem>
        </div>
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

