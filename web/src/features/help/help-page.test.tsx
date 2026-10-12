import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "@/api/types";
import { App } from "@/app";
import { HelpPage, signInWords } from "./help-page";

afterEach(() => vi.unstubAllGlobals());

const base: Me = {
  version: "1.0.0",
  auth_mode: "oidc",
  is_admin: false,
  project: { id: "shop", name: "Shop" },
  projects: [{ id: "shop", name: "Shop" }],
  features: { alerting: true, traces: true, auth_provider: "Okta", docs_url: "https://koneb71.github.io/pyloggle/" },
};

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function renderHelp(me: Me) {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(me), { status: 200, headers: { "Content-Type": "application/json" } })));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/help"]}>
        <Routes>
          <Route path="/help" element={<HelpPage me={me} />} />
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Help page", () => {
  it("gathers the guides, sending data, search, shortcuts, words and this server", async () => {
    renderHelp(base);
    expect(screen.getByRole("heading", { name: "Help" })).toBeInTheDocument();
    const guides = screen.getByRole("list", { name: "Guides" });
    expect(guides.querySelector('a[href="https://koneb71.github.io/pyloggle/introduction/quick-start"]')).not.toBeNull();
    expect(screen.getByText("Connect your app")).toBeInTheDocument();
    expect(screen.getByText("Keyboard shortcuts")).toBeInTheDocument();
    expect(screen.getByText("Go to Help")).toBeInTheDocument();
    expect(screen.getByText("Came back")).toBeInTheDocument();
    expect(screen.getByText("1.0.0")).toBeInTheDocument();
    expect(screen.getByText("With Okta accounts")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open-source licenses/ })).toHaveAttribute("href", "/ui/licenses.txt");
    expect(await screen.findByRole("link", { name: /Full search syntax/ })).toHaveAttribute("href", "https://koneb71.github.io/pyloggle/app/search");
  });

  it("opens a search example on its page", () => {
    renderHelp(base);
    // "service:api" is an example on both lists: the first opens Logs, the second Requests.
    fireEvent.click(screen.getAllByText("service:api", { selector: "code" })[0]);
    expect(screen.getByTestId("where").textContent).toBe("/logs?from=-24h&q=service%3Aapi");
  });

  it("hides every link to the docs when the server has no docs address", async () => {
    const me = { ...base, features: { ...base.features, docs_url: undefined } };
    renderHelp(me);
    await waitFor(() => expect(screen.getByText("Connect your app")).toBeInTheDocument());
    expect(screen.queryByRole("list", { name: "Guides" })).toBeNull();
    expect(screen.queryByText(/Full search syntax/)).toBeNull();
    expect(screen.queryByText(/Learn more/)).toBeNull();
  });
});

describe("sign-in in words", () => {
  it("names the provider", () => {
    expect(signInWords({ auth_mode: "azure", features: { alerting: false } })).toBe("With Microsoft accounts");
    expect(signInWords({ auth_mode: "oidc", features: { alerting: false } })).toBe("With company accounts");
    expect(signInWords({ auth_mode: "disabled", features: { alerting: false } })).toMatch(/^Off/);
  });
});

describe("Help in the app", () => {
  it("is in the sidebar and opens with g then h", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(base), { status: 200, headers: { "Content-Type": "application/json" } })));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/projects"]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("link", { name: /Help/ })).toHaveAttribute("href", "/help");
    fireEvent.keyDown(document, { key: "g" });
    fireEvent.keyDown(document, { key: "h" });
    expect(await screen.findByRole("heading", { name: "Help" })).toBeInTheDocument();
  });
});
