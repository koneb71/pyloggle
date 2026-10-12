import { describe, expect, it } from "vitest";
import { auditWords, limitsWords, scopeLabel } from "./project-words";

describe("project words", () => {
  it("names permissions and limits", () => {
    expect(["ingest", "read", "write"].map(scopeLabel)).toEqual(["Send data", "View", "Make changes"]);
    expect(limitsWords({ max_events_per_second: 500, max_storage_gb: 10, retention_days: 14 })).toBe("Up to 500 messages a second · 10 GB of storage · keeps 14 days");
    expect(limitsWords({ max_events_per_second: 0, max_storage_gb: 0, retention_days: 0 })).toMatch(/^No limits/);
  });
  it("words admin actions", () => {
    expect(auditWords({ action: "key.revoke" })).toBe("disabled an access key");
    expect(auditWords({ action: "something.new" })).toBe("something new");
  });
});
