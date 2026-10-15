import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ErrorAnalytics, LogAggregate, Me, SavedView, UsageAnalytics } from "@/api/types";
import { AnalyticsPage } from "./analytics-page";
import { LogsPage } from "@/features/logs/logs-page";
import { PinnedCharts } from "@/features/dashboards/pinned-charts";
import { personLabel, perThousand } from "./errors-tab";

afterEach(() => vi.unstubAllGlobals());

const me: Me = {
  version: "1.1.0",
  auth_mode: "disabled",
  is_admin: true,
  project: { id: "shop", name: "Shop" },
  projects: [{ id: "shop", name: "Shop" }],
  features: { alerting: true, traces: true, docs_url: "https://koneb71.github.io/pyloggle/" },
};

const errors: ErrorAnalytics = {
  from: "2026-09-15T00:00:00Z",
  to: "2026-10-15T00:00:00Z",
  bucket_seconds: 86400,
  points: [{ ts: "2026-10-14T00:00:00Z", opened: 3, fixed: 2, came_back: 1 }],
  totals: { opened: 3, fixed: 2, came_back: 1 },
  previous: { opened: 6, fixed: 1, came_back: 0 },
  time_to_fix: { fixed: 2, median_hours: 5, within_hour: 1, within_day: 1, within_week: 0, longer: 0 },
  releases: [
    { release: "1.4.0", first_seen: "2026-10-10T00:00:00Z", errors: 40, problems: 3, new_problems: 2, people: 5, requests: 10000 },
    { release: "1.3.0", first_seen: "2026-10-01T00:00:00Z", errors: 10, problems: 1, new_problems: 1, people: 2, requests: 10000 },
  ],
  types: [{ key: "ValueError", errors: 30, problems: 2, people: 4 }, { key: "", errors: 20, problems: 1, people: 1 }],
  apps: [{ key: "shop", errors: 50, problems: 3, people: 5 }],
  people: [{ id_hash: "9f86d081884c7d65", errors: 7, problems: 2, last_seen: "2026-10-14T00:00:00Z" }, { id_hash: "aa", label: "ada@example.com", errors: 3, problems: 1, last_seen: "2026-10-14T00:00:00Z" }],
  details_kept_days: 30,
};

const usage = (requests: number): UsageAnalytics => ({
  from: "2026-09-15T00:00:00Z",
  to: "2026-10-15T00:00:00Z",
  bucket_seconds: 86400,
  totals: { requests, errors: requests ? 20 : 0, people: requests ? 40 : 0 },
  previous: { requests: 0, errors: 0, people: 0 },
  points: [{ ts: "2026-10-14T00:00:00Z", requests, errors: 0, people: 40 }],
  weeks: [{ ts: "2026-10-12T00:00:00Z", requests, errors: 0, people: 40 }],
  endpoints: requests ? [{ method: "POST", route: "/orders", requests, errors: 20, people: 40 }] : [],
  versions: requests ? [{ release: "1.4.0", first_seen: "2026-10-10T00:00:00Z", requests, errors: 20, people: 40 }] : [],
  version_series: requests ? [{ name: "1.4.0", values: [requests] }] : [],
});

const aggregate: LogAggregate = {
  bucket_seconds: 3600,
  group_by: "endpoint",
  metric: "p95",
  buckets: ["2026-10-14T00:00:00Z"],
  total: { key: "total", rows: 120, value: 85 },
  groups: [{ key: "POST /orders", rows: 120, value: 85, series: [85] }],
};

const chartView: SavedView = {
  id: "c1",
  project_id: "shop",
  user_id: "anonymous",
  name: "Checkout speed",
  description: "",
  query: JSON.stringify({ kind: "chart", source: "requests", q: "", metric: "p95", group_by: "endpoint", limit: 8, from: "-24h" }),
  is_public: true,
  is_pinned: true,
  created_at: "",
  updated_at: "",
};

interface Call {
  method: string;
  path: string;
  params: URLSearchParams;
  body?: unknown;
}

function stubApi(over: { usageRequests?: number; views?: SavedView[] } = {}) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const u = new URL(String(input), "http://x");
      const path = u.pathname.replace("/ui/api", "");
      calls.push({ method: init?.method ?? "GET", path, params: u.searchParams, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (path === "/me") return json(me);
      if (path === "/analytics/errors") return json(errors);
      if (path === "/analytics/usage") return json(usage(over.usageRequests ?? 1200));
      if (path === "/analytics/aggregate") return json(aggregate);
      if (path === "/analytics/fields") return json({ source: u.searchParams.get("source"), fields: [{ name: "endpoint", kind: "string" }], metrics: [], attrs: false });
      if (path === "/saved-views") return json({ items: over.views ?? [] });
      if (path.endsWith("/pin")) return json({ view: { ...chartView, is_pinned: false } });
      if (path.startsWith("/logs")) return json({ fields: {}, attr_keys: [], tag_keys: [], items: [], buckets: [], series: [] });
      return json({});
    }),
  );
  return calls;
}

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function renderAt(url: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/analytics" element={<><AnalyticsPage /><Where /></>} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/dashboards" element={<PinnedCharts scope={{ from: "-24h", to: "", service: "shop", env: "" }} />} />
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Analytics", () => {
  it("opens on Errors with opened, fixed and came back against the period before", async () => {
    const calls = stubApi();
    renderAt("/analytics?service=shop");
    expect(screen.getByRole("heading", { name: "Analytics" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Errors" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("5 hours")).toBeInTheDocument(); // typical time to fix
    const call = calls.find((c) => c.path === "/analytics/errors")!;
    expect(call.params.get("service")).toBe("shop");
    expect(call.params.get("from")).toBe("-30d");
    // 1.4.0 fails four times as often as 1.3.0 per 1,000 requests: a red arrow.
    const row = screen.getByText("1.4.0").closest('[role="row"]') as HTMLElement;
    expect(within(row).getByText("4.00")).toBeInTheDocument();
    expect(within(row).getByText(/4\.0×/).closest("span")).toHaveClass("text-critical-text");
    expect(screen.getByText("Logged errors without an exception")).toBeInTheDocument();
    expect(screen.getByText("Person 9f86d0")).toBeInTheDocument();
    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    expect(screen.getByText(/kept for 30 days/)).toBeInTheDocument();
  });

  it("shows usage by endpoint and version, and how to start when nothing is counted", async () => {
    stubApi();
    renderAt("/analytics?tab=usage");
    expect(await screen.findByText("/orders")).toBeInTheDocument();
    expect(screen.getAllByText("1.4.0").length).toBeGreaterThan(0);
    expect(screen.getByText("30")).toBeInTheDocument(); // requests per person
    fireEvent.click(screen.getByRole("radio", { name: "People" }));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("by=people"));
  });

  it("explains request counting when there are none yet", async () => {
    stubApi({ usageRequests: 0 });
    renderAt("/analytics?tab=usage");
    expect(await screen.findByText("Requests are counted as soon as your app answers them")).toBeInTheDocument();
  });

  it("charts requests from the URL and switches what it charts", async () => {
    const calls = stubApi();
    renderAt("/analytics?tab=charts&source=requests&metric=p95");
    expect(await screen.findAllByText("Slow response time (p95)")).not.toHaveLength(0);
    const call = calls.find((c) => c.path === "/analytics/aggregate")!;
    expect(call.params.get("source")).toBe("requests");
    expect(call.params.get("group_by")).toBe("endpoint");
    expect(call.params.get("of")).toBeNull();
    fireEvent.click(screen.getByRole("radio", { name: "Errors" }));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("source=errors"));
    expect(screen.getByTestId("where").textContent).not.toContain("metric=p95");
  });

  it("opens a saved chart and pins it", async () => {
    const calls = stubApi({ views: [chartView, { ...chartView, id: "s1", name: "A Logs search", query: JSON.stringify({ q: "level:error", from: "-1h" }) }] });
    renderAt("/analytics?tab=charts");
    fireEvent.click(await screen.findByRole("button", { name: "Saved charts" }));
    expect(await screen.findByText("Checkout speed")).toBeInTheDocument();
    expect(screen.queryByText("A Logs search")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Unpin Checkout speed from Dashboards" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/saved-views/c1/pin" && (c.body as { pinned: boolean }).pinned === false)).toBe(true));
    fireEvent.click(screen.getByText("Checkout speed"));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("chart=c1"));
    expect(screen.getByTestId("where").textContent).toContain("source=requests");
  });
});

describe("Charts moved from Logs", () => {
  it("sends an old Logs chart link to Analytics with its settings", async () => {
    stubApi();
    renderAt("/logs?view=analytics&group_by_a=http.route&metric=p95&of=duration_ms&q=level%3Aerror&from=-6h");
    const where = (await screen.findByTestId("where")).textContent!;
    expect(where.startsWith("/analytics?tab=charts")).toBe(true);
    expect(where).toContain("group_by=http.route");
    expect(where).toContain("metric=p95");
    expect(where).toContain("from=-6h");
  });
});

describe("Pinned charts on Dashboards", () => {
  it("draws each pinned chart with the dashboard's range and app", async () => {
    const calls = stubApi({ views: [chartView] });
    renderAt("/dashboards");
    expect(await screen.findByText("Checkout speed")).toBeInTheDocument();
    await waitFor(() => expect(calls.some((c) => c.path === "/analytics/aggregate")).toBe(true));
    const call = calls.find((c) => c.path === "/analytics/aggregate")!;
    expect(call.params.get("service")).toBe("shop");
    expect(call.params.get("metric")).toBe("p95");
    expect(calls.find((c) => c.path === "/saved-views")!.params.get("pinned")).toBe("1");
    expect(screen.getByRole("link", { name: /Open in Analytics/ }).getAttribute("href")).toContain("chart=c1");
  });

  it("offers to build one when nothing is pinned", async () => {
    stubApi({ views: [] });
    renderAt("/dashboards");
    expect(await screen.findByText(/Pin a chart from Analytics/)).toBeInTheDocument();
  });
});

describe("errors tab helpers", () => {
  it("names people and rates versions", () => {
    expect(personLabel({ id_hash: "abcdef1234", errors: 1, problems: 1, last_seen: "" })).toBe("Person abcdef");
    expect(perThousand(errors.releases[0])).toBe(4);
    expect(perThousand({ ...errors.releases[0], requests: 0 })).toBeNull();
  });
});
