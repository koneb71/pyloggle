import { describe, expect, it } from "vitest";
import { exportUrl } from "./export-url";

describe("exportUrl", () => {
  it("carries the selection and omits empty params", () => {
    const u = exportUrl({ q: "level:error service:api", from: "-1h", to: "", format: "csv", limit: 10000 });
    expect(u.startsWith("/ui/api/logs/export?")).toBe(true);
    expect(u).toContain("q=level%3Aerror+service%3Aapi");
    expect(u).toContain("from=-1h");
    expect(u).not.toContain("to=");
    expect(u).toContain("format=csv");
    expect(u).toContain("limit=10000");
  });
  it("joins columns", () => {
    expect(exportUrl({ q: "", from: "-1h", to: "2026-01-01T00:00:00Z", format: "ndjson", limit: 1000, cols: ["ts", "message"] })).toContain("cols=ts%2Cmessage");
  });
});
