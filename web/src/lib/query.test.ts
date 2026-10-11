import { describe, expect, it } from "vitest";
import { addTerm, chips, hasTerm, removeTerm, term, tokenize } from "./query";

describe("tokenize", () => {
  it("splits on whitespace and keeps quoted phrases and groups together", () => {
    expect(tokenize('service:api level:error "timed out" -attrs.path:/health level:(error OR fatal)')).toEqual([
      "service:api",
      "level:error",
      '"timed out"',
      "-attrs.path:/health",
      "level:(error OR fatal)",
    ]);
  });
  it("handles escaped quotes inside phrases", () => {
    expect(tokenize('msg:"say \\"hi\\" now" x')).toEqual(['msg:"say \\"hi\\" now"', "x"]);
  });
});

describe("chips", () => {
  it("parses field, value and negation; free text keeps raw", () => {
    expect(chips('service:api -level:debug "boom" OR x')).toEqual([
      { raw: "service:api", negate: false, field: "service", value: "api" },
      { raw: "-level:debug", negate: true, field: "level", value: "debug" },
      { raw: '"boom"', negate: false, field: null, value: '"boom"' },
      { raw: "OR", negate: false, field: null, value: "OR" },
      { raw: "x", negate: false, field: null, value: "x" },
    ]);
  });
});

describe("term helpers", () => {
  it("quotes values that need it", () => {
    expect(term("http.route", "/api/orders")).toBe("http.route:/api/orders");
    expect(term("message", "timed out")).toBe('message:"timed out"');
    expect(term("service", "", true)).toBe('-service:""');
  });
  it("addTerm is idempotent and replaces the opposite sign", () => {
    const q = addTerm("", "service", "api");
    expect(q).toBe("service:api");
    expect(addTerm(q, "service", "api")).toBe(q);
    expect(addTerm(q, "service", "api", true)).toBe("-service:api");
  });
  it("removeTerm drops the token and dangling OR", () => {
    expect(removeTerm("a:1 OR b:2", "b:2")).toBe("a:1");
    expect(removeTerm("a:1 OR b:2", "a:1")).toBe("b:2");
    expect(hasTerm("level:error x", "level", "error")).toBe(true);
    expect(hasTerm("level:error x", "level", "warn")).toBe(false);
  });
});
