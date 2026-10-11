// Screenshots every page in light and dark mode against a running ingestd.
//   PYLOGGLE_URL=http://127.0.0.1:8080 node scripts/screenshots.mjs [outDir]
// Uses the pre-installed Chromium (no download) through playwright-core.
import { chromium } from "playwright-core";
import { mkdirSync } from "node:fs";
import path from "node:path";

const base = process.env.PYLOGGLE_URL ?? "http://127.0.0.1:8080";
const out = process.argv[2] ?? path.resolve("screenshots");
const exe = process.env.CHROMIUM ?? "/opt/pw-browsers/chromium";
mkdirSync(out, { recursive: true });

const pages = [
  ["issues", "/ui/issues?from=-7d"],
  ["issue", "/ui/issues/__FIRST_ISSUE__"],
  ["issue-breadcrumbs", "/ui/issues/__FIRST_ISSUE__?tab=breadcrumbs"],
  ["logs", "/ui/logs?from=-24h"],
  ["logs-filtered", "/ui/logs?from=-24h&q=level%3Aerror&sel=__FIRST_LOG__"],
  ["logs-patterns", "/ui/logs?from=-24h&view=patterns"],
  ["logs-analytics", "/ui/logs?from=-24h&view=analytics&group_by_a=http.route&metric=p95"],
  ["traces", "/ui/traces?from=-24h"],
  ["trace", "/ui/traces/__FIRST_TRACE__?at=__FIRST_TRACE_AT__"],
  ["dashboards", "/ui/dashboards?from=-24h"],
  ["alerts", "/ui/alerts"],
  ["projects", "/ui/projects"],
  ["login", "/ui/login"],
];

async function firstIds() {
  const h = { Accept: "application/json" };
  const issues = await fetch(`${base}/ui/api/issues?from=-30d&status=all&limit=1`, { headers: h }).then((r) => r.json());
  const logs = await fetch(`${base}/ui/api/logs?from=-30d&q=level:error&limit=1`, { headers: h }).then((r) => r.json());
  const traces = await fetch(`${base}/ui/api/traces?from=-7d&limit=1`, { headers: h }).then((r) => r.json()).catch(() => ({}));
  return { issue: issues.items?.[0]?.issue_id ?? "", log: logs.items?.[0]?.event_id ?? "", trace: traces.items?.[0]?.trace_id ?? "", traceAt: traces.items?.[0]?.start_ts ?? "" };
}

const ids = await firstIds();
const browser = await chromium.launch({ executablePath: exe, args: ["--no-sandbox"] });
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: scheme, deviceScaleFactor: 1 });
  await ctx.addInitScript((t) => localStorage.setItem("pyloggle.theme", t), scheme);
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
  for (const [name, p] of pages) {
    const url = base + p.replace("__FIRST_ISSUE__", ids.issue).replace("__FIRST_LOG__", ids.log).replace("__FIRST_TRACE__", ids.trace).replace("__FIRST_TRACE_AT__", encodeURIComponent(ids.traceAt));
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForTimeout(400);
    const file = path.join(out, `${name}-${scheme}.png`);
    await page.screenshot({ path: file, fullPage: false });
    console.log("saved", file);
  }
  if (errors.length) {
    console.error(`[${scheme}] browser errors:\n` + errors.map((e) => "  " + e).join("\n"));
  }
  await ctx.close();
}
await browser.close();
