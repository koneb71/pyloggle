import { useCallback, useMemo } from "react";
import { ScrollText } from "lucide-react";
import { useChartFields } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import type { ChartSource } from "@/api/types";
import { Card, LinkButton, SegmentedControl } from "@/components/ui";
import { useUrlState } from "@/lib/url";
import { addTerm } from "@/lib/query";
import type { Range } from "@/lib/time";
import { SearchBar, type SearchExample } from "@/features/logs/search-bar";
import { ChartBuilder, useChart } from "./chart-builder";
import { chartFromParams, chartParams, chartStateOf, DEFAULT_CHART, SOURCES, SOURCE_WORDS, type ChartState } from "./chart-model";
import { SavedChartsMenu } from "./saved-charts";
import type { AnalyticsScope } from "./errors-tab";

const EXAMPLES: Partial<Record<ChartSource, SearchExample[]>> = {
  errors: [
    { query: "exception:ValueError", says: "One type of error" },
    { query: "release:1.4.0", says: "Errors in one version" },
    { query: "http.route:/checkout*", says: "Errors on some pages" },
    { query: "handled:0", says: "Errors nothing caught (crashes)" },
    { query: "timed out", says: "Errors whose message has these words" },
  ],
  requests: [
    { query: 'endpoint:"POST /orders"', says: "One endpoint" },
    { query: "route:/api/*", says: "Every route under /api" },
    { query: "method:POST", says: "Only POST requests" },
    { query: "version:1.4.0", says: "Requests one version answered" },
  ],
};

/** Analytics → Charts: any chart of messages, errors or requests, saved and pinned to Dashboards. */
export function ChartsTab({ scope, range, onZoom }: { scope: AnalyticsScope; range: Range; onZoom: (from: Date, to: Date) => void }) {
  const { get, set } = useUrlState();
  const state = useMemo(() => chartFromParams(get), [get]);
  const fields = useChartFields(state.source);
  // The builder runs the same query (the cache shares it): a search it cannot read is shown here.
  const agg = useChart(state, scope);
  const queryError = agg.error instanceof ApiRequestError && agg.error.status === 400 && agg.error.body.error === "bad_query" ? { message: agg.error.body.message || agg.error.message, pos: agg.error.body.pos } : null;
  const activeId = get("chart");
  const setState = useCallback(
    (patch: Partial<ChartState>) => {
      const next = { ...state, ...patch };
      set({ source: undefined, q: undefined, metric: undefined, of: undefined, group_by: undefined, top: undefined, chart: undefined, ...chartParams(next) });
    },
    [state, set],
  );
  const switchSource = (source: ChartSource) => setState({ ...DEFAULT_CHART[source], q: "" });
  const searchFields = state.source === "messages" ? undefined : (fields.data?.fields ?? []).map((f) => f.name);
  const logsHref = `/logs?${new URLSearchParams(Object.entries({ q: state.q, from: range.from, to: range.to }).filter(([, v]) => v) as [string, string][]).toString()}`;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentedControl
          label="What to chart"
          value={state.source}
          onChange={switchSource}
          options={SOURCES.map((s) => ({ value: s, label: SOURCE_WORDS[s].label, title: SOURCE_WORDS[s].hint }))}
        />
        <div className="ml-auto flex items-center gap-2">
          {state.source === "messages" && (
            <LinkButton to={logsHref} variant="ghost" icon={<ScrollText size={14} />} title="Read these messages on Logs">
              See the messages
            </LinkButton>
          )}
          <SavedChartsMenu
            current={state}
            range={range}
            activeId={activeId || undefined}
            onOpen={(chart, view) => set({ ...{ source: undefined, q: undefined, metric: undefined, of: undefined, group_by: undefined, top: undefined }, ...chartParams(chartStateOf(chart)), from: chart.from, to: chart.to, chart: view.id })}
          />
        </div>
      </div>
      <SearchBar
        value={state.q}
        onChange={(q) => setState({ q })}
        range={range}
        fields={searchFields}
        examples={EXAMPLES[state.source]}
        placeholder={state.source === "requests" ? "Filter by app, environment, method, route, endpoint or version" : `Search ${SOURCE_WORDS[state.source].many}`}
        ariaLabel={`Search ${SOURCE_WORDS[state.source].many}`}
        error={queryError}
      />
      <Card className="overflow-hidden">
        <ChartBuilder state={state} scope={scope} onState={setState} onFilter={(field, value) => setState({ q: addTerm(state.q, field, value) })} onZoom={onZoom} />
      </Card>
    </div>
  );
}
