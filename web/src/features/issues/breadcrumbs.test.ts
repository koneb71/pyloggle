import { describe, expect, it } from "vitest";
import { beforeLabel, categoryLabel } from "./breadcrumbs";

describe("breadcrumb words", () => {
  it("names categories", () => {
    expect(categoryLabel({ ts: "", message: "", category: "httpx" })).toBe("Web call");
    expect(categoryLabel({ ts: "", message: "", category: "django.db.query" })).toBe("Database query");
    expect(categoryLabel({ ts: "", message: "", category: "logging" })).toBe("Log message");
    expect(categoryLabel({ ts: "", message: "" })).toBe("Step");
  });
  it("says how long before the error", () => {
    expect(beforeLabel("2026-10-12T10:00:00.000Z", "2026-10-12T10:00:01.250Z")).toBe("1.25 s before");
    expect(beforeLabel("2026-10-12T10:00:01.250Z", "2026-10-12T10:00:01.250Z")).toBe("at the same moment");
  });
});
