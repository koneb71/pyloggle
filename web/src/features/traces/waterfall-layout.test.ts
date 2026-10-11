import { describe, expect, it } from "vitest";
import type { Span } from "@/api/types";
import { fmtUs, layoutSpans, microFraction, visibleRows } from "./waterfall-layout";

const span = (id: string, parent: string | undefined, startMs: number, durUs: number, extra: Partial<Span> = {}): Span => ({
  trace_id: "t",
  span_id: id,
  parent_span_id: parent,
  service: "api",
  env: "prod",
  name: id,
  kind: "internal",
  status: "ok",
  start_ts: new Date(1700000000000 + startMs).toISOString(),
  duration_us: durUs,
  attrs: {},
  ...extra,
});

describe("waterfall layout", () => {
  it("orders depth-first by start and computes offsets against the trace span", () => {
    const rows = layoutSpans([span("db", "root", 10, 20_000), span("root", undefined, 0, 100_000), span("http", "root", 40, 50_000, { status: "error" }), span("inner", "http", 45, 1)]);
    expect(rows.map((r) => r.span.span_id)).toEqual(["root", "db", "http", "inner"]);
    expect(rows.map((r) => r.depth)).toEqual([0, 1, 1, 2]);
    expect(rows[0].left).toBe(0);
    expect(rows[0].width).toBe(100);
    expect(rows[1].left).toBeCloseTo(10);
    expect(rows[1].width).toBeCloseTo(20);
    expect(rows[3].width).toBeGreaterThanOrEqual(0.3); // sliver stays clickable
    expect(rows[0].descendants).toBe(3);
    expect(rows[0].hasError).toBe(true); // bubbled up from http
    expect(rows[1].hasError).toBe(false);
    expect(rows[2].children).toBe(1);
  });
  it("puts orphans at depth 0 and survives cycles", () => {
    const rows = layoutSpans([span("a", "missing", 0, 10), span("b", "c", 1, 10), span("c", "b", 2, 10)]);
    expect(rows).toHaveLength(3);
    expect(rows[0].span.span_id).toBe("a");
    expect(rows[0].depth).toBe(0);
  });
  it("collapses subtrees", () => {
    const rows = layoutSpans([span("root", undefined, 0, 100), span("x", "root", 1, 10), span("y", "x", 2, 5), span("z", "root", 20, 5)]);
    expect(visibleRows(rows, new Set(["x"])).map((r) => r.span.span_id)).toEqual(["root", "x", "z"]);
    expect(visibleRows(rows, new Set(["root"])).map((r) => r.span.span_id)).toEqual(["root"]);
  });
  it("formats durations and keeps microseconds", () => {
    expect(fmtUs(420)).toBe("420µs");
    expect(fmtUs(1234)).toBe("1.23ms");
    expect(fmtUs(45_600)).toBe("45.6ms");
    expect(fmtUs(2_500_000)).toBe("2.50s");
    expect(microFraction("2023-11-14T22:13:20.123456Z")).toBe(456);
    expect(microFraction("2023-11-14T22:13:20.123Z")).toBe(0);
    expect(microFraction("2023-11-14T22:13:20Z")).toBe(0);
  });
});
