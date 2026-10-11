import { describe, expect, it } from "vitest";
import { columnFor, patternPhrase } from "./log-fields";

describe("patternPhrase", () => {
  it("picks the longest literal run around placeholders", () => {
    expect(patternPhrase("request <s> took <n>ms for user <uuid>")).toBe("ms for user");
    expect(patternPhrase("connection to database timed out after <n>s")).toBe("connection to database timed out after");
    expect(patternPhrase("<n> retries")).toBe("<n> retries");
  });
});

describe("columnFor", () => {
  it("builds attribute and tag columns on the fly", () => {
    const c = columnFor("attrs.user_id")!;
    expect(c.label).toBe("user_id");
    expect(c.value({ attrs: { user_id: "42" } } as never)).toBe("42");
    expect(columnFor("tags.team")!.label).toBe("#team");
    expect(columnFor("nope")).toBeUndefined();
  });
});
