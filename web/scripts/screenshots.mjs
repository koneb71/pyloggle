// Screenshots every page in light and dark mode against a running ingestd (UI sign-in off).
//   PYLOGGLE_URL=http://127.0.0.1:8080 node scripts/screenshots.mjs [outDir]
//   WIDTH=1024   adds "-1024" to the file names and uses that viewport width.
//   SHOTS=a,b    takes only those views (names as in the list below, plus "login").
//   OUT=dir      output folder (same as the first argument).
//   CHROMIUM=…   a Chromium binary; by default Playwright's own browser
//                (install it once with `npx playwright install chromium`).
//   ISSUE_Q=…    search that picks the issue to show (default: the latest one).
//   TRACE_Q=…    search that picks the request to show (default: the latest one).
//   The view "phone" shows Overview, an issue and Logs side by side as a phone shows them.
import { chromium } from "playwright-core";
import { mkdirSync } from "node:fs";
import path from "node:path";

const base = process.env.PYLOGGLE_URL ?? "http://127.0.0.1:8080";
const out = process.argv[2] ?? process.env.OUT ?? path.resolve("screenshots");
const exe = process.env.CHROMIUM || undefined;
const only = new Set((process.env.SHOTS ?? "").split(",").map((s) => s.trim()).filter(Boolean));
const wanted = (name) => only.size === 0 || only.has(name);
const width = Number(process.env.WIDTH ?? 1440);
const suffix = width === 1440 ? "" : `-${width}`;
mkdirSync(out, { recursive: true });

// [name, path, optional action run before the screenshot]
const pages = [
  ["overview", "/ui/overview"],
  ["issues", "/ui/issues?from=-7d"],
  ["issue", "/ui/issues/__FIRST_ISSUE__?from=-7d"],
  ["issue-steps", "/ui/issues/__FIRST_ISSUE__?from=-7d&tab=breadcrumbs"],
  ["logs", "/ui/logs?from=-24h"],
  ["logs-details", "/ui/logs?from=-24h&q=level%3Aerror&sel=__FIRST_LOG__"],
  ["logs-add-filter", "/ui/logs?from=-24h", (p) => p.getByRole("button", { name: "Add filter" }).click()],
  ["logs-similar", "/ui/logs?from=-24h&view=patterns"],
  ["logs-charts", "/ui/logs?from=-24h&view=analytics&group_by_a=http.route&metric=p95"],
  ["requests", "/ui/traces?from=-24h"],
  ["request", "/ui/traces/__FIRST_TRACE__?at=__FIRST_TRACE_AT__"],
  ["dashboards", "/ui/dashboards?from=-24h"],
  ["alerts", "/ui/alerts"],
  ["alert-form", "/ui/alerts", (p) => p.getByRole("button", { name: "New alert" }).click()],
  ["projects", "/ui/projects"],
  ["project-settings", "/ui/projects", (p) => p.getByRole("button", { name: "Settings" }).last().click()],
  ["system", "/ui/system"],
  ["shortcuts", "/ui/overview", (p) => p.keyboard.press("Shift+Slash")],
  ["help", "/ui/help"],
];

async function firstIds() {
  const h = { Accept: "application/json" };
  const q = (env) => (process.env[env] ? `&q=${encodeURIComponent(process.env[env])}` : "");
  const issues = await fetch(`${base}/ui/api/issues?from=-30d&status=all&limit=1${q("ISSUE_Q")}`, { headers: h }).then((r) => r.json());
  const logs = await fetch(`${base}/ui/api/logs?from=-24h&q=level:error&limit=1`, { headers: h }).then((r) => r.json());
  const traces = await fetch(`${base}/ui/api/traces?from=-7d&limit=1${q("TRACE_Q")}`, { headers: h }).then((r) => r.json()).catch(() => ({}));
  return { issue: issues.items?.[0]?.issue_id ?? "", log: logs.items?.[0]?.event_id ?? "", trace: traces.items?.[0]?.trace_id ?? "", traceAt: traces.items?.[0]?.start_ts ?? "" };
}

const ids = await firstIds();
const browser = await chromium.launch({ executablePath: exe, args: ["--no-sandbox"] });
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: scheme, deviceScaleFactor: 1 });
  await ctx.addInitScript((t) => localStorage.setItem("pyloggle.theme", t), scheme);
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  page.on("console", (m) => m.type() === "error" && errors.push(m.text()));
  for (const [name, p, action] of pages) {
    if (!wanted(name)) continue;
    const url = base + p.replace("__FIRST_ISSUE__", ids.issue).replace("__FIRST_LOG__", ids.log).replace("__FIRST_TRACE__", ids.trace).replace("__FIRST_TRACE_AT__", encodeURIComponent(ids.traceAt));
    await page.goto(url, { waitUntil: "networkidle" });
    await page.waitForTimeout(400);
    if (action) {
      await action(page);
      await page.waitForTimeout(500);
    }
    const file = path.join(out, `${name}${suffix}-${scheme}.png`);
    await page.screenshot({ path: file, fullPage: false });
    console.log("saved", file);
  }
  // The sign-in screen: answer /me with "not signed in" so the server can keep sign-in off.
  if (wanted("login")) {
    await page.route("**/ui/api/me", (r) => r.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "login_required" }) }));
    await page.goto(`${base}/ui/overview`, { waitUntil: "networkidle" });
    await page.screenshot({ path: path.join(out, `login${suffix}-${scheme}.png`) });
    await page.unroute("**/ui/api/me");
  }
  if (wanted("phone")) await phoneSheet(scheme);
  if (errors.length) {
    console.error(`[${scheme}] browser errors:\n` + errors.map((e) => "  " + e).join("\n"));
  }
  await ctx.close();
}
await browser.close();

/** Three screens from a phone-sized touch browser, framed side by side on one image. */
async function phoneSheet(scheme) {
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: scheme, deviceScaleFactor: 1, isMobile: true, hasTouch: true });
  await phone.addInitScript((t) => localStorage.setItem("pyloggle.theme", t), scheme);
  const page = await phone.newPage();
  const shots = [];
  for (const p of ["/ui/overview", `/ui/issues/${ids.issue}?from=-7d`, "/ui/logs?from=-24h"]) {
    await page.goto(base + p, { waitUntil: "networkidle" });
    await page.waitForTimeout(400);
    shots.push((await page.screenshot()).toString("base64"));
  }
  await phone.close();
  const [bg, edge] = scheme === "dark" ? ["#0e0e0d", "#3a3a37"] : ["#f6f6f4", "#d2d1ca"];
  const sheet = await browser.newPage({ viewport: { width: 3 * 390 + 4 * 32, height: 844 + 2 * 32 } });
  const imgs = shots.map((b64) => `<img src="data:image/png;base64,${b64}" width="390" height="844" style="border-radius:24px;border:1px solid ${edge}">`).join("");
  await sheet.setContent(`<body style="margin:0;padding:32px;display:flex;gap:32px;background:${bg}">${imgs}</body>`);
  const file = path.join(out, `phone-${scheme}.png`);
  await sheet.screenshot({ path: file });
  await sheet.close();
  console.log("saved", file);
}
