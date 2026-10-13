import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { LogEvent } from "@/api/types";
import { LogDetailPanel } from "./log-detail";

afterEach(() => vi.unstubAllGlobals());

const line: LogEvent = {
  ts: "2026-10-13T08:00:00Z",
  event_id: "ev-1",
  service: "shop",
  env: "prod",
  level: "info",
  logger: "pyloggle.flask",
  message: "request",
  trace_id: "",
  span_id: "",
  request_id: "r1",
  project_id: "shop",
  http_method: "POST",
  http_route: "/orders",
  http_status: 201,
  duration_ms: 12,
  attrs: {},
};

function renderPanel(event: LogEvent, body?: string) {
  const fetch = vi.fn(async (url: string) => new Response(JSON.stringify(url.includes("/logs/ev-1") ? { ...event, http_body: body } : {}), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <LogDetailPanel event={event} onClose={() => {}} onFilter={() => {}} onExclude={() => {}} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return fetch;
}

describe("Message details", () => {
  it("fetches and shows the body of a request line", async () => {
    renderPanel(line, '{"item":3}');
    expect(await screen.findByText("Request body")).toBeInTheDocument();
    expect(await screen.findByText("item:")).toBeInTheDocument();
  });

  it("does not look for a body on ordinary messages", async () => {
    const fetch = renderPanel({ ...line, message: "order saved", duration_ms: undefined });
    await screen.findByText("About this message");
    expect(fetch.mock.calls.some(([url]) => String(url).includes("/logs/ev-1"))).toBe(false);
    expect(screen.queryByText("Request body")).toBeNull();
  });
});
