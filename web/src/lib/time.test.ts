import { describe, expect, it } from "vitest";
import { ago, bucketLabel, fmtNumber, rangeLabel, resolveRange } from "./time";

describe("resolveRange", () => {
  const now = Date.UTC(2026, 9, 11, 12, 0, 0);
  it("resolves relative ranges against now", () => {
    const { from, to } = resolveRange({ from: "-15m", to: "" }, now);
    expect(to.getTime()).toBe(now);
    expect(from.getTime()).toBe(now - 15 * 60_000);
  });
  it("keeps absolute ranges", () => {
    const r = resolveRange({ from: "2026-10-01T00:00:00Z", to: "2026-10-02T00:00:00Z" }, now);
    expect(r.from.toISOString()).toBe("2026-10-01T00:00:00.000Z");
    expect(r.to.toISOString()).toBe("2026-10-02T00:00:00.000Z");
  });
  it("labels quick ranges", () => {
    expect(rangeLabel({ from: "-1h", to: "" })).toBe("Last 1h");
    expect(rangeLabel({ from: "-30d", to: "" })).toBe("Last 30d");
  });
});

describe("formatting", () => {
  it("bucketLabel", () => {
    expect(bucketLabel(10)).toBe("10s");
    expect(bucketLabel(300)).toBe("5m");
    expect(bucketLabel(7200)).toBe("2h");
    expect(bucketLabel(86400)).toBe("1d");
  });
  it("fmtNumber", () => {
    expect(fmtNumber(999)).toBe("999");
    expect(fmtNumber(12_345)).toBe("12.3k");
    expect(fmtNumber(2_500_000)).toBe("2.5M");
  });
  it("ago", () => {
    const now = Date.now();
    expect(ago(new Date(now - 2000).toISOString(), now)).toBe("just now");
    expect(ago(new Date(now - 90_000).toISOString(), now)).toBe("2m ago");
    expect(ago(new Date(now - 3 * 3600_000).toISOString(), now)).toBe("3h ago");
  });
});
