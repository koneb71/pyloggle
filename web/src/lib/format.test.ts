import { describe, expect, it } from "vitest";
import { fmtDelta } from "./format";

describe("fmtDelta", () => {
  it("words changes for people", () => {
    expect(fmtDelta(112, 100)).toEqual({ text: "+12%", direction: "up" });
    expect(fmtDelta(92, 100)).toEqual({ text: "−8%", direction: "down" });
    expect(fmtDelta(310, 100)).toEqual({ text: "3.1×", direction: "up" });
    expect(fmtDelta(1500, 100)).toEqual({ text: "15×", direction: "up" });
    expect(fmtDelta(100.2, 100)).toEqual({ text: "No change", direction: "flat" });
  });
  it("handles an empty previous period", () => {
    expect(fmtDelta(0, 0)).toEqual({ text: "No change", direction: "flat" });
    expect(fmtDelta(4, 0)).toEqual({ text: "New", direction: "up" });
    expect(fmtDelta(NaN, 3)).toBeNull();
  });
});
