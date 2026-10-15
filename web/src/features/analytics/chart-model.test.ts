import { describe, expect, it } from "vitest";
import type { LogAggregate } from "@/api/types";
import { chartFromParams, chartHref, chartHrefFromLogs, chartParams, chartSeries, DEFAULT_CHART, metricFormat, metricLabel, needsField, parseSavedChart, savedChart, sharePct } from "./chart-model";

function agg(n: number, other = false, groupBy = "service"): LogAggregate {
  const keys = groupBy === "level" ? ["error", "info", "warn"] : [];
  const groups = Array.from({ length: n }, (_, i) => ({ key: keys[i] ?? `svc-${i}`, rows: 10 - i, value: 10 - i, series: [1, null, 3] }));
  return { bucket_seconds: 60, group_by: groupBy, metric: "count", buckets: ["a", "b", "c"], total: { key: "total", rows: 100, value: 100, series: [5, 5, 5] }, groups, other: other ? { key: "other", rows: 1, value: 1, series: [0, 1, 0] } : undefined };
}

const params = (o: Record<string, string>) => (k: string) => o[k] ?? "";

describe("chart model", () => {
  it("keeps at most 8 lines and only shows an exact other fold", () => {
    expect(chartSeries(agg(3, true)).map((s) => s.name)).toEqual(["svc-0", "svc-1", "svc-2", "Other"]);
    expect(chartSeries(agg(8, true)).map((s) => s.name)).toHaveLength(8);
    expect(chartSeries(agg(8, true)).some((s) => s.name === "Other")).toBe(false);
    expect(chartSeries(agg(15, true))).toHaveLength(8);
  });

  it("colors level groups by level and names an unsplit line after what it counts", () => {
    const s = chartSeries(agg(2, false, "level"));
    expect(s.map((x) => x.color)).toEqual(["var(--lvl-error)", "var(--lvl-info)"]);
    const t = chartSeries({ ...agg(0), group_by: "" });
    expect(t).toHaveLength(1);
    expect(t[0].color).toBe("var(--accent)");
    expect(t[0].name).toBe("Number of messages");
    expect(chartSeries({ ...agg(0), group_by: "" }, "requests")[0].name).toBe("Number of requests");
  });

  it("labels and formats metrics per source", () => {
    expect(metricLabel("messages", "count")).toBe("Number of messages");
    expect(metricLabel("errors", "problems")).toBe("Problems");
    expect(metricLabel("messages", "p95", "duration_ms")).toBe("Slow (p95) duration");
    expect(metricLabel("requests", "p95", "duration_ms")).toBe("Slow response time (p95)");
    expect(metricLabel("messages", "avg", "attrs.items")).toBe("Average items");
    expect(metricFormat("messages", "p95", "duration_ms")(12.34)).toBe("12ms");
    expect(metricFormat("requests", "p99")(250)).toBe("250ms");
    expect(metricFormat("requests", "failure_rate")(2.5)).toContain("%");
    expect(metricFormat("messages", "avg", "attrs.items")(2.5)).toBe("2.50");
    expect(metricFormat("messages", "count")(12000)).toBe("12.0k");
    expect(metricFormat("errors", "people")(3)).toBe("3");
    expect(sharePct(25, 100)).toBe(25);
    expect(sharePct(1, 0)).toBe(0);
  });

  it("measures fields only where a field is measured", () => {
    expect(needsField("messages", "avg")).toBe(true);
    expect(needsField("messages", "people")).toBe(false);
    expect(needsField("errors", "problems")).toBe(false);
    expect(needsField("requests", "p95")).toBe(false);
  });

  it("round-trips a chart through the URL, leaving defaults out", () => {
    const s = { ...DEFAULT_CHART.requests, q: "route:/api/*", metric: "p95" as const, groupBy: "release" };
    const p = chartParams(s);
    expect(p).toEqual({ source: "requests", q: "route:/api/*", metric: "p95", of: undefined, group_by: "release", top: undefined });
    expect(chartFromParams(params(p as Record<string, string>))).toEqual(s);
    expect(chartParams(DEFAULT_CHART.messages)).toEqual({ source: undefined, q: undefined, metric: undefined, of: undefined, group_by: undefined, top: undefined });
    expect(chartFromParams(params({ source: "requests", metric: "avg" })).metric).toBe("count"); // requests have no average
    expect(chartFromParams(params({ group_by: "none" })).groupBy).toBe("");
    expect(chartFromParams(params({ source: "spans" })).source).toBe("messages");
  });

  it("sends old Logs chart links to Analytics", () => {
    const href = chartHrefFromLogs(new URLSearchParams("view=analytics&group_by_a=http.route&metric=p95&of=duration_ms&q=level:error&from=-6h"));
    const u = new URL(href, "http://x");
    expect(u.pathname).toBe("/analytics");
    expect(Object.fromEntries(u.searchParams)).toEqual({ tab: "charts", q: "level:error", metric: "p95", group_by: "http.route", from: "-6h" });
    expect(chartHref(DEFAULT_CHART.errors, { from: "-7d" }, { service: "shop" })).toBe("/analytics?tab=charts&source=errors&from=-7d&service=shop");
  });

  it("saves a chart and reads it back; a Logs search is not a chart", () => {
    const c = savedChart({ ...DEFAULT_CHART.messages, metric: "avg", of: "attrs.items" }, { from: "-24h", to: "" });
    expect(c).toMatchObject({ kind: "chart", source: "messages", metric: "avg", of: "attrs.items", from: "-24h" });
    expect(parseSavedChart(JSON.stringify(c))).toEqual({ ...c, to: undefined });
    expect(parseSavedChart(JSON.stringify({ q: "level:error", from: "-1h" }))).toBeNull();
    expect(parseSavedChart("not json")).toBeNull();
    expect(parseSavedChart(JSON.stringify({ kind: "chart", source: "requests", metric: "avg" }))?.metric).toBe("count");
  });
});
