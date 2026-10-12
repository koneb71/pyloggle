import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { DOCS, docsUrl } from "./docs";

const docsDir = resolve(__dirname, "../../../docs");

describe("docs links", () => {
  it("point at pages that exist", () => {
    for (const [key, page] of Object.entries(DOCS)) {
      const file = resolve(docsDir, page === "" ? "index.md" : `${page}.md`);
      expect(existsSync(file), `${key}: ${file}`).toBe(true);
    }
  });
  it("build addresses from the server's docs root", () => {
    expect(docsUrl("https://koneb71.github.io/pyloggle/", "alerting")).toBe("https://koneb71.github.io/pyloggle/self-hosting/alerting");
    expect(docsUrl("https://docs.internal.example/pyloggle", "search", "fields-on-the-logs-page")).toBe("https://docs.internal.example/pyloggle/app/search#fields-on-the-logs-page");
    expect(docsUrl("https://koneb71.github.io/pyloggle/", "home")).toBe("https://koneb71.github.io/pyloggle/");
  });
  it("are hidden when the server has no docs address", () => {
    expect(docsUrl(undefined, "alerting")).toBeNull();
    expect(docsUrl("", "alerting")).toBeNull();
  });
});
