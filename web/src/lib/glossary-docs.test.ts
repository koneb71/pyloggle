import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { GLOSSARY } from "./glossary";

/** The concepts page in the docs lists every word the app uses, with its technical name. */
describe("glossary in the docs", () => {
  const page = readFileSync(resolve(__dirname, "../../../docs/introduction/concepts.md"), "utf8");
  it.each(Object.values(GLOSSARY).map((g) => [g.label, g.technical]))("lists %s (%s)", (label, technical) => {
    expect(page).toContain(`| ${label} | ${technical} |`);
  });
});
