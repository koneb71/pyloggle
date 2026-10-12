import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it } from "vitest";
import { DataTable, type Column } from "./data-table";

type R = { id: string; name: string; n: number };
const cols: Column<R>[] = [
  { id: "name", header: "Name", cell: (r) => r.name },
  { id: "n", header: "Times", cell: (r) => r.n, width: "80px", align: "right" },
];

describe("DataTable", () => {
  it("gives the header and every row the same grid template", () => {
    render(
      <MemoryRouter>
        <DataTable columns={cols} rows={[{ id: "a", name: "A", n: 1 }]} rowKey={(r) => r.id} />
      </MemoryRouter>,
    );
    const rows = screen.getAllByRole("row");
    expect(rows).toHaveLength(2);
    expect(rows[0].style.gridTemplateColumns).toBe("minmax(0,1fr) 80px");
    expect(rows[1].style.gridTemplateColumns).toBe(rows[0].style.gridTemplateColumns);
    expect(rows[1].className).toContain("gap-x-4");
    expect(rows[0].className).toContain("gap-x-4");
  });
  it("scrolls sideways below its minimum width instead of squeezing columns", () => {
    render(
      <MemoryRouter>
        <DataTable columns={cols} rows={[{ id: "a", name: "A", n: 1 }]} rowKey={(r) => r.id} minWidth={600} />
      </MemoryRouter>,
    );
    const table = screen.getByRole("table");
    expect(table.style.minWidth).toBe("600px");
    expect(table.parentElement?.className).toContain("overflow-x-auto");
    expect(screen.getAllByRole("rowgroup")[0].className).not.toContain("sticky");
  });
  it("shows the error instead of the empty text", () => {
    render(
      <MemoryRouter>
        <DataTable columns={cols} rows={[]} rowKey={(r) => r.id} error={new Error("down")} empty="No issues" />
      </MemoryRouter>,
    );
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.queryByText("No issues")).toBeNull();
  });
  it("opens a row's link on click", () => {
    render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route path="/" element={<DataTable columns={cols} rows={[{ id: "a", name: "A", n: 1 }]} rowKey={(r) => r.id} rowHref={(r) => `/x/${r.id}`} />} />
          <Route path="/x/:id" element={<p>opened</p>} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText("A"));
    expect(screen.getByText("opened")).toBeInTheDocument();
  });
});
