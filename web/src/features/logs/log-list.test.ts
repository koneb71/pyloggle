import { describe, expect, it } from "vitest";
import { columnFor, type ColumnDef } from "./log-fields";
import { stacksAt } from "./log-list";

const cols = (...ids: string[]) => ids.map(columnFor).filter((c): c is ColumnDef => !!c);

describe("stacksAt", () => {
  it("stacks the default list once its columns no longer fit", () => {
    // Time 150 + severity 84 + app 90 + message 240, three gaps of 16 and 32 of padding: 644.
    expect(stacksAt(643, cols("service"))).toBe(true);
    expect(stacksAt(644, cols("service"))).toBe(false);
    expect(stacksAt(390, cols("service"))).toBe(true);
  });
  it("keeps a desk's wide table, scrolling sideways, rather than stacking it", () => {
    // Five columns need more than 720px; a 900px list keeps them, a 700px one stacks.
    const many = cols("service", "logger", "host", "http", "duration");
    expect(stacksAt(900, many)).toBe(false);
    expect(stacksAt(700, many)).toBe(true);
  });
});
