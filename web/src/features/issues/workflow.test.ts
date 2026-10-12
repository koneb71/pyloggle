import { describe, expect, it } from "vitest";
import type { IssueActivity } from "@/api/types";
import { activitySentence, snoozeWords } from "./workflow";

const act = (over: Partial<IssueActivity>): IssueActivity => ({ id: "a", issue_id: "i", ts: "2026-10-12T10:00:00Z", kind: "status", actor: "ana", ...over }) as IssueActivity;

describe("activity in plain words", () => {
  it("names status changes as actions", () => {
    expect(activitySentence(act({ kind: "status", to: "resolved" }))).toEqual({ who: "ana", what: "resolved it" });
    expect(activitySentence(act({ kind: "status", to: "open" })).what).toBe("reopened it");
    expect(activitySentence(act({ kind: "assign", to: "bo" })).what).toBe("assigned it to bo");
  });
  it("says why Pyloggle reopened an issue", () => {
    expect(activitySentence(act({ kind: "regression", actor: "system", release: "1.4.1" }))).toEqual({ who: "Pyloggle", what: "reopened it: it came back in version 1.4.1" });
  });
  it("rewrites stored ignore text", () => {
    expect(snoozeWords("until 140 occurrences")).toBe("until it has happened 140 times");
    expect(snoozeWords("until 2026-10-12T14:00:00Z")).toMatch(/^until (Today|Yesterday|Oct 12)/);
  });
});
