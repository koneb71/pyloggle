import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiRequestError } from "@/api/client";
import { QueryState, type QueryLike } from "./query-state";

const q = <T,>(over: Partial<QueryLike<T>>): QueryLike<T> => ({ data: undefined, isPending: false, isError: false, error: null, fetchStatus: "idle", refetch: () => undefined, ...over });

const renderState = (query: QueryLike<string[]>) =>
  render(
    <QueryState query={query} isEmpty={(d) => d.length === 0} empty={<p>nothing here</p>} skeleton={<p>loading</p>}>
      {(d) => <p>rows: {d.join(",")}</p>}
    </QueryState>,
  );

describe("QueryState", () => {
  it("shows the error and never the empty state when the first load fails", () => {
    renderState(q({ isError: true, error: new ApiRequestError(500, { error: "boom", message: "boom" }) }));
    expect(screen.getByText("The server had a problem loading this")).toBeInTheDocument();
    expect(screen.getByText("Try again")).toBeInTheDocument();
    expect(screen.queryByText("nothing here")).toBeNull();
  });
  it("shows the skeleton while the first load runs", () => {
    renderState(q({ isPending: true, fetchStatus: "fetching" }));
    expect(screen.getByText("loading")).toBeInTheDocument();
  });
  it("treats a switched-off query as empty instead of loading forever", () => {
    renderState(q({ isPending: true, fetchStatus: "idle" }));
    expect(screen.getByText("nothing here")).toBeInTheDocument();
  });
  it("shows empty or content once data is there", () => {
    const { unmount } = renderState(q({ data: [] }));
    expect(screen.getByText("nothing here")).toBeInTheDocument();
    unmount();
    renderState(q({ data: ["a", "b"] }));
    expect(screen.getByText("rows: a,b")).toBeInTheDocument();
  });
  it("keeps data on screen when a refresh fails", () => {
    renderState(q({ data: ["a"], isError: true, error: new Error("x") }));
    expect(screen.getByText("rows: a")).toBeInTheDocument();
    expect(screen.getByText("Couldn't refresh. Showing earlier data.")).toBeInTheDocument();
  });
});
