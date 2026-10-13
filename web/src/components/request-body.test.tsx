import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RequestBody, expectsBody, parseBody } from "./request-body";

afterEach(() => vi.unstubAllGlobals());

function renderBody(props: Parameters<typeof RequestBody>[0]) {
  // LearnMore asks the server where the docs live.
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ features: { docs_url: "https://docs.example/" } }), { status: 200 })));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RequestBody {...props} />
    </QueryClientProvider>,
  );
}

describe("RequestBody", () => {
  it("shows JSON and forms as fields, with a plain-text view and a copy button", () => {
    renderBody({ body: '{"item":3,"user":{"name":"ada"},"password":"[REDACTED]"}', method: "POST" });
    expect(screen.getByText("item:")).toBeInTheDocument();
    expect(screen.getByText("name:")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy body" })).toBeInTheDocument();
    expect(screen.getByText(/were blanked out/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Plain text" }));
    expect(screen.getByText(/"name": "ada"/)).toBeInTheDocument();
  });

  it("shows other bodies as text, and says when the body was cut short", () => {
    renderBody({ body: '{"item":3,"note":"abc', truncated: true, method: "POST" });
    expect(screen.queryByRole("radio")).toBeNull(); // no fields to show
    expect(screen.getByText('{"item":3,"note":"abc')).toBeInTheDocument();
    expect(screen.getByText(/Only the start of this body was recorded/)).toBeInTheDocument();
    expect(screen.queryByText(/blanked out/)).toBeNull();
  });

  it("says when a request that usually has a body has none, and stays quiet otherwise", () => {
    const { container, unmount } = renderBody({ method: "GET" });
    expect(container).toBeEmptyDOMElement();
    unmount();
    renderBody({ method: "POST" });
    expect(screen.getByText(/No body was recorded/)).toBeInTheDocument();
  });

  it("knows which bodies are fields and which methods send one", () => {
    expect(parseBody('[1, {"a": 2}]')).toEqual([1, { a: 2 }]);
    expect(parseBody('"text"')).toBeNull();
    expect(parseBody("{not json")).toBeNull();
    expect(parseBody("[image/png body, 1.2 MB, not recorded]")).toBeNull();
    expect(expectsBody("patch")).toBe(true);
    expect(expectsBody("DELETE")).toBe(false);
    expect(expectsBody(undefined)).toBe(false);
  });
});
