import { describe, expect, it } from "vitest";
import { orderSeries, seriesColor } from "./palette";

describe("series ordering and colors", () => {
  it("keeps the fixed level order whatever the server returns", () => {
    expect(orderSeries("level", ["error", "info", "debug", "fatal", "warn"]).kept).toEqual(["debug", "info", "warn", "error", "fatal"]);
    expect(orderSeries("level", ["info", "custom"]).kept).toEqual(["info", "custom"]);
  });
  it("folds a 9th+ series into Other instead of inventing a hue", () => {
    const s = Array.from({ length: 11 }, (_, i) => `svc${i}`);
    const { kept, folded } = orderSeries("service", s);
    expect(kept).toHaveLength(8);
    expect(kept[7]).toBe("Other");
    expect(folded).toEqual(s.slice(7));
  });
  it("colors follow the entity, not its rank", () => {
    expect(seriesColor("level", "error", 3)).toBe("var(--lvl-error)");
    expect(seriesColor("level", "error", 0)).toBe("var(--lvl-error)");
    expect(seriesColor("service", "Other", 7)).toBe("var(--lvl-other)");
    expect(seriesColor("service", "api", 0)).toBe("var(--series-1)");
  });
});
