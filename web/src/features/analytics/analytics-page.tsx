import { useIsFetching, useQueryClient } from "@tanstack/react-query";
import { Page, Tabs } from "@/components/ui";
import { TimeRangePicker } from "@/components/shell/time-range-picker";
import { ScopeFilter } from "@/components/shell/scope-filter";
import { LearnMore } from "@/components/learn-more";
import { useRange } from "@/lib/use-range";
import { useUrlState } from "@/lib/url";
import { ErrorsTab } from "./errors-tab";
import { UsageTab } from "./usage-tab";
import { ChartsTab } from "./charts-tab";

export const ANALYTICS_DEFAULT = { from: "-30d", to: "" };

const TABS = [
  { id: "errors", label: "Errors", title: "How your apps' health changes: problems opened, fixed and coming back" },
  { id: "usage", label: "Usage", title: "How your apps are used: requests and people, by page, endpoint and version" },
  { id: "charts", label: "Charts", title: "Chart messages, errors or requests any way you like, save and pin to Dashboards" },
];

/** Analytics: trends over days and weeks, in three tabs. */
export function AnalyticsPage() {
  const { range, setRange, zoom } = useRange(ANALYTICS_DEFAULT);
  const { get, set } = useUrlState();
  const qc = useQueryClient();
  const tab = TABS.some((t) => t.id === get("tab")) ? get("tab") : "errors";
  const scope = { from: range.from, to: range.to, service: get("service"), env: get("env") };
  const refresh = () => qc.invalidateQueries({ predicate: (q) => String(q.queryKey[0]).startsWith("analytics-") });
  const fetching = useIsFetching({ predicate: (q) => String(q.queryKey[0]).startsWith("analytics-") }) > 0;
  return (
    <Page
      title="Analytics"
      description={
        <>
          How your apps' health and use change over time. <LearnMore doc="analytics">About Analytics</LearnMore>
        </>
      }
      actions={<TimeRangePicker value={range} onChange={setRange} onRefresh={refresh} refreshing={fetching} />}
      toolbar={<ScopeFilter range={range} service={scope.service} env={scope.env} onChange={(p) => set(p)} />}
    >
      <Tabs items={TABS} value={tab} onChange={(t) => set({ tab: t === "errors" ? undefined : t }, true)} />
      {tab === "errors" && <ErrorsTab scope={scope} range={range} />}
      {tab === "usage" && <UsageTab scope={scope} range={range} />}
      {tab === "charts" && <ChartsTab scope={scope} range={range} onZoom={zoom} />}
    </Page>
  );
}
