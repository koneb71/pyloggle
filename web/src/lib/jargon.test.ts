import { describe, expect, it } from "vitest";

/**
 * The UI speaks plain language. These words were on screen before the redesign; this test keeps
 * them from coming back. It reads every string literal and JSX text in the app (not identifiers
 * or object keys), so code may still use the technical names.
 */
const BANNED = [
  "request_stats_minutely",
  "CORE",
  "unset",
  "entry spans",
  "Mutations pending",
  "power-of-two",
  "0.2 key",
  "Ingest streams",
  "Load older",
  "Recent firings",
  "Affected users",
  "5xx rate",
  "Add facet",
  "View trace",
  "In-app",
  "buckets",
  "Regressed",
  "Culprit",
];

const sources = import.meta.glob(["../**/*.tsx", "!../**/*.test.tsx"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;

function visibleText(src: string): string[] {
  const out: string[] = [];
  // Double-quoted and template string literals (minus class lists), and JSX text between tags.
  for (const m of src.matchAll(/"((?:[^"\\\n]|\\.)*)"/g)) out.push(m[1]);
  for (const m of src.matchAll(/`((?:[^`\\]|\\.)*)`/g)) out.push(m[1].replace(/\$\{[^}]*\}/g, " "));
  for (const m of src.matchAll(/>([^<>{}]+)</g)) out.push(m[1]);
  return out.filter((s) => !/^[\w\s:/[\]().%-]*\b(px|py|text|bg|border|flex|grid|rounded|gap|font)-/.test(s));
}

describe("plain language", () => {
  it("reads the app's sources", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(40);
  });
  it("never shows the old technical words", () => {
    const hits: string[] = [];
    for (const [file, src] of Object.entries(sources)) {
      for (const text of visibleText(src)) {
        for (const word of BANNED) {
          if (new RegExp(`(^|[^A-Za-z_])${word.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}($|[^A-Za-z_])`).test(text)) hits.push(`${file}: "${word}" in ${JSON.stringify(text.slice(0, 80))}`);
        }
      }
    }
    expect(hits).toEqual([]);
  });
});
