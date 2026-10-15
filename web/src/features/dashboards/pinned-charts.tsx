import { ArrowRight, Pin } from "lucide-react";
import type { SavedView } from "@/api/types";
import { Card, CardHeader, ErrorState, LinkButton } from "@/components/ui";
import { ChartPlot, useChart, type ChartScope } from "@/features/analytics/chart-builder";
import { chartHref, chartStateOf, metricLabel, SOURCE_WORDS, type SavedChart } from "@/features/analytics/chart-model";
import { useSavedCharts } from "@/features/analytics/saved-charts";
import { fmtNumber } from "@/lib/time";

/** Charts pinned from Analytics, drawn with the dashboard's time range, app and environment. */
export function PinnedCharts({ scope }: { scope: ChartScope }) {
  const { charts, isPending } = useSavedCharts(true);
  if (isPending) return null;
  if (charts.length === 0) {
    return (
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-dashed border-border px-4 py-3 text-[13px] text-ink-3">
        <Pin size={14} aria-hidden />
        <span className="flex-1">Pin a chart from Analytics to keep it here, next to everything else.</span>
        <LinkButton to="/analytics?tab=charts" size="xs" variant="ghost" icon={<ArrowRight size={13} />}>
          Build a chart
        </LinkButton>
      </div>
    );
  }
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {charts.map(({ view, chart }) => (
        <PinnedChart key={view.id} view={view} chart={chart} scope={scope} />
      ))}
    </div>
  );
}

function PinnedChart({ view, chart, scope }: { view: SavedView; chart: SavedChart; scope: ChartScope }) {
  const state = chartStateOf(chart);
  const q = useChart(state, scope, 40);
  const words = SOURCE_WORDS[chart.source];
  const total = q.data?.total.rows;
  return (
    <Card className="min-w-0">
      <CardHeader
        title={view.name}
        sub={
          <>
            {metricLabel(chart.source, chart.metric, chart.of)}
            {total !== undefined ? ` · ${fmtNumber(total)} ${total === 1 ? words.one : words.many}` : chart.metric !== "count" ? ` · ${words.many}` : ""}
            {view.description && ` · ${view.description}`}
          </>
        }
        actions={
          <LinkButton to={`${chartHref(state, { from: scope.from, to: scope.to }, scope)}&chart=${encodeURIComponent(view.id)}`} size="xs" variant="ghost" icon={<ArrowRight size={13} />}>
            Open in Analytics
          </LinkButton>
        }
      />
      <div className="p-4">{q.isError && !q.data ? <ErrorState compact error={q.error} retry={() => q.refetch()} title="Couldn't load this chart" /> : <ChartPlot state={state} data={q.data} loading={q.isPending} height={180} />}</div>
    </Card>
  );
}
