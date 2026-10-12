import { describe, expect, it } from "vitest";
import { ago, bucketLabel, durationWords, fmtNumber, fmtWhen, previousLabel, rangeLabel, rangePhrase, resolveRange } from "./time";

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
    expect(rangeLabel({ from: "-15m", to: "" })).toBe("Last 15 minutes");
    expect(rangeLabel({ from: "-1h", to: "" })).toBe("Last hour");
    expect(rangeLabel({ from: "-30d", to: "" })).toBe("Last 30 days");
    expect(rangePhrase({ from: "-24h", to: "" })).toBe("the last 24 hours");
    expect(rangeLabel({ from: "-3d", to: "" })).toBe("Last 3 days");
    expect(rangeLabel({ from: "-1w", to: "" })).toBe("Last week");
    expect(previousLabel({ from: "-1h", to: "" })).toBe("vs previous hour");
    expect(previousLabel({ from: "2026-10-01T00:00:00Z", to: "2026-10-02T00:00:00Z" })).toBe("vs the period before");
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
    expect(ago(new Date(now - 30_000).toISOString(), now)).toBe("30 sec ago");
    expect(ago(new Date(now - 90_000).toISOString(), now)).toBe("2 min ago");
    expect(ago(new Date(now - 3600_000).toISOString(), now)).toBe("1 hour ago");
    expect(ago(new Date(now - 3 * 3600_000).toISOString(), now)).toBe("3 hours ago");
    expect(ago(new Date(now - 5 * 864e5).toISOString(), now)).toBe("5 days ago");
  });
  it("fmtWhen", () => {
    const now = new Date(2026, 9, 12, 15, 0, 0);
    expect(fmtWhen(new Date(2026, 9, 12, 9, 28, 56), { seconds: true }, now)).toBe("Today 09:28:56");
    expect(fmtWhen(new Date(2026, 9, 11, 18, 2, 0), {}, now)).toBe("Yesterday 18:02");
    expect(fmtWhen(new Date(2026, 9, 3, 18, 2, 0), {}, now)).toBe("Oct 3 18:02");
    expect(fmtWhen(new Date(2025, 0, 3, 8, 0, 0), {}, now)).toBe("Jan 3, 2025 08:00");
  });
  it("durationWords", () => {
    expect(durationWords(30)).toBe("30 seconds");
    expect(durationWords(60)).toBe("1 minute");
    expect(durationWords(1800)).toBe("30 minutes");
    expect(durationWords(3600)).toBe("1 hour");
    expect(durationWords(86400 * 2)).toBe("2 days");
  });
});
