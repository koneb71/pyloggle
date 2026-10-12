import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./app";

afterEach(() => vi.unstubAllGlobals());

function renderWithMe(status: number, body: unknown) {
  const fetchMock = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetchMock);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/overview"]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return fetchMock;
}

describe("sign-in", () => {
  it("shows company sign-in when the server says the session ended, without refetching forever", async () => {
    const fetchMock = renderWithMe(401, { error: "login_required", login_url: "/ui/login" });
    expect(await screen.findByText(/Sign in with your company account/)).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(fetchMock.mock.calls.length).toBeLessThanOrEqual(2);
  });
  it("shows the access key form when the server has no company sign-in", async () => {
    renderWithMe(401, { error: "login_required" });
    await waitFor(() => expect(screen.getByLabelText(/Access key/)).toBeInTheDocument());
  });
});

describe("outages", () => {
  it("says the database is down instead of offering to create a first project", async () => {
    renderWithMe(503, { error: "unavailable", message: "the database is not answering: connection refused" });
    expect(await screen.findByText("Pyloggle can't reach its database")).toBeInTheDocument();
    expect(screen.queryByText(/Create a project/)).not.toBeInTheDocument();
  });
  it("does not treat 'no project selected' as signed out (no reload loop)", async () => {
    const me = { auth_mode: "disabled", is_admin: true, project: { id: "default", name: "Default" }, projects: [{ id: "default", name: "Default" }], features: { alerting: false, traces: true } };
    const fetchMock = vi.fn(async (url: string) =>
      url.endsWith("/me")
        ? new Response(JSON.stringify(me), { status: 200, headers: { "Content-Type": "application/json" } })
        : new Response(JSON.stringify({ error: "no_project", message: "no project selected" }), { status: 401, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/overview"]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect((await screen.findAllByText("No project is open")).length).toBeGreaterThan(0);
    await new Promise((r) => setTimeout(r, 100));
    expect(fetchMock.mock.calls.filter(([u]) => String(u).endsWith("/me")).length).toBe(1);
  });
});
