import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Me } from "@/api/types";
import { LogsMoreMenu } from "./clear-logs-dialog";

afterEach(() => vi.unstubAllGlobals());

const me: Me = {
  auth_mode: "disabled",
  is_admin: true,
  project: { id: "shop", name: "Shop" },
  projects: [{ id: "shop", name: "Shop" }],
  features: { alerting: true },
};

function renderMenu(who: Me) {
  const calls: { url: string; method: string; body?: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, method: init?.method ?? "GET", body: init?.body as string | undefined });
      const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200 });
      if (url.endsWith("/me")) return json(who);
      if (url.includes("/projects/shop/usage")) return json({ by_source: { logs: { rows: 1234 } } });
      if (url.includes("/logs?project_id=shop") && init?.method === "DELETE") return json({ status: "cleared", id: "shop" });
      return json({});
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <LogsMoreMenu />
    </QueryClientProvider>,
  );
  return calls;
}

describe("Clear all messages", () => {
  it("asks for the project id, then deletes the project's messages", async () => {
    const calls = renderMenu(me);
    fireEvent.click(await screen.findByRole("button", { name: "More" }));
    fireEvent.click(screen.getByRole("button", { name: /Clear all messages/ }));
    expect(await screen.findByText(/about 1,234 of them/)).toBeInTheDocument();
    const confirm = screen.getByRole("button", { name: "Clear all messages" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Type shop to confirm"), { target: { value: "Shop" } });
    expect(confirm).toBeDisabled(); // the id, not the name
    fireEvent.change(screen.getByLabelText("Type shop to confirm"), { target: { value: "shop" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE")).toBe(true));
    const del = calls.find((c) => c.method === "DELETE")!;
    expect(del.url).toContain("/logs?project_id=shop");
    expect(JSON.parse(del.body!)).toEqual({ confirm: "shop" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("is only offered to admins", async () => {
    const calls = renderMenu({ ...me, is_admin: false });
    await waitFor(() => expect(calls.some((c) => c.url.endsWith("/me"))).toBe(true));
    expect(screen.queryByRole("button", { name: "More" })).toBeNull();
  });
});
