import { describe, expect, it } from "vitest";
import type { LogAggregate } from "@/api/types";
import { chartSeries, metricFormat, metricLabel, sharePct } from "./analytics-model";

function agg(n: number, other = false, groupBy = "service"): LogAggregate {
  const keys = groupBy === "level" ? ["error", "info", "warn"] : [];
  const groups = Array.from({ length: n }, (_, i) => ({ key: keys[i] ?? `svc-${i}`, rows: 10 - i, value: 10 - i, series: [1, null, 3] }));
  return { bucket_seconds: 60, group_by: groupBy, metric: "count", buckets: ["a", "b", "c"], total: { key: "total", rows: 100, value: 100, series: [5, 5, 5] }, groups, other: other ? { key: "other", rows: 1, value: 1, series: [0, 1, 0] } : undefined };
}

describe("analytics model", () => {
  it("keeps at most 8 lines and only shows an exact other fold", () => {
    expect(chartSeries(agg(3, true)).map((s) => s.name)).toEqual(["svc-0", "svc-1", "svc-2", "Other"]);
    expect(chartSeries(agg(8, true)).map((s) => s.name)).toHaveLength(8);
    expect(chartSeries(agg(8, true)).some((s) => s.name === "Other")).toBe(false);
    expect(chartSeries(agg(15, true))).toHaveLength(8);
  });
  it("colors level groups by level and ungrouped totals by accent", () => {
    const s = chartSeries(agg(2, false, "level"));
    expect(s.map((x) => x.color)).toEqual(["var(--lvl-error)", "var(--lvl-info)"]);
    const t = chartSeries({ ...agg(0), group_by: "" });
    expect(t).toHaveLength(1);
    expect(t[0].color).toBe("var(--accent)");
    expect(t[0].name).toBe("Number of messages");
  });
  it("labels and formats metrics", () => {
    expect(metricLabel("count")).toBe("Number of messages");
    expect(metricLabel("p95", "duration_ms")).toBe("Slow (p95) duration");
    expect(metricLabel("avg", "attrs.items")).toBe("Average items");
    expect(metricFormat("p95", "duration_ms")(12.34)).toBe("12ms");
    expect(metricFormat("avg", "attrs.items")(2.5)).toBe("2.50");
    expect(metricFormat("count")(12000)).toBe("12.0k");
    expect(sharePct(25, 100)).toBe(25);
    expect(sharePct(1, 0)).toBe(0);
  });
});
