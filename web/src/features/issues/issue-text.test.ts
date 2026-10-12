import { describe, expect, it } from "vitest";
import { isNewIssue, issueHeadline } from "./issue-text";

describe("issueHeadline", () => {
  it("splits a crash into its type and message", () => {
    expect(issueHeadline({ title: "KeyError: 'currency'", exception_type: "KeyError", last_message: "'currency'" })).toEqual({ headline: "KeyError", message: "'currency'" });
  });
  it("uses the message for issues that come from logged errors", () => {
    expect(issueHeadline({ title: "GET /orders failed", exception_type: "", last_message: "GET /orders failed" })).toEqual({ headline: "GET /orders failed", message: "" });
    expect(issueHeadline({ title: "", exception_type: "", last_message: "" }).headline).toBe("Unknown error");
  });
  it("falls back to the last message when the title has no type prefix", () => {
    expect(issueHeadline({ title: "boom", exception_type: "RuntimeError", last_message: "boom later" })).toEqual({ headline: "RuntimeError", message: "boom later" });
  });
});

describe("isNewIssue", () => {
  it("is new for 24 hours", () => {
    const now = Date.UTC(2026, 9, 12, 12);
    expect(isNewIssue({ first_seen: new Date(now - 3600_000).toISOString() }, now)).toBe(true);
    expect(isNewIssue({ first_seen: new Date(now - 25 * 3600_000).toISOString() }, now)).toBe(false);
  });
});
