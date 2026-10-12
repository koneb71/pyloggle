import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "@/api/types";
import { SMALL_SCREEN } from "@/lib/use-media";
import { AppShell } from "./app-shell";

const me: Me = {
  version: "1.0.0",
  auth_mode: "disabled",
  is_admin: true,
  project: { id: "shop", name: "Shop" },
  projects: [{ id: "shop", name: "Shop" }],
  features: { alerting: true, traces: true },
};

/** jsdom has no screen: answer the shell's media queries as a phone or a desk would. */
function screenIs(kind: "phone" | "desk") {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({ matches: kind === "phone" && (query === SMALL_SCREEN || query === "(max-width: 1199px)"), media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false }) as MediaQueryList,
  );
}

function Where() {
  return <p data-testid="where">{useLocation().pathname}</p>;
}

function renderShell() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={["/overview"]}>
        <Routes>
          <Route element={<AppShell me={me} />}>
            <Route path="*" element={<Where />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe("AppShell on a phone", () => {
  it("opens the navigation as a menu, focused on its close button, and closes it after a choice", () => {
    screenIs("phone");
    renderShell();
    expect(screen.queryByRole("dialog", { name: "Main navigation" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open menu" }));
    const menu = screen.getByRole("dialog", { name: "Main navigation" });
    expect(within(menu).getByRole("button", { name: "Close menu" })).toHaveFocus();
    expect(within(menu).getByRole("link", { name: "Help" })).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("link", { name: "Logs" }));
    expect(screen.getByTestId("where")).toHaveTextContent("/logs");
    expect(screen.queryByRole("dialog", { name: "Main navigation" })).toBeNull();
  });

  it("closes the menu on Escape and gives the focus back to the menu button", () => {
    screenIs("phone");
    renderShell();
    const button = screen.getByRole("button", { name: "Open menu" });
    button.focus();
    fireEvent.click(button);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Main navigation" })).toBeNull();
    expect(button).toHaveFocus();
  });

  it("names the current project in the top bar", () => {
    screenIs("phone");
    renderShell();
    expect(screen.getByTitle("Current project")).toHaveTextContent("Shop");
  });
});

describe("AppShell on a desk", () => {
  it("keeps the sidebar and never opens the phone menu", () => {
    screenIs("desk");
    renderShell();
    expect(screen.getByRole("complementary", { name: "Main navigation" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open menu" }));
    expect(screen.queryByRole("dialog", { name: "Main navigation" })).toBeNull();
  });
});
