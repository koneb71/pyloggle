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
  it("gives each screen size its own tracks when columns step out or step in", () => {
    const responsive: Column<R>[] = [
      { id: "name", header: "Name", cell: (r) => r.name },
      { id: "n", header: "Times", cell: (r) => r.n, width: "80px", align: "right", hideBelow: "md" },
      { id: "m", header: "Compact", cell: () => "+", width: "32px", showBelow: "sm" },
    ];
    render(
      <MemoryRouter>
        <DataTable columns={responsive} rows={[{ id: "a", name: "A", n: 1 }]} rowKey={(r) => r.id} />
      </MemoryRouter>,
    );
    const [head, row] = screen.getAllByRole("row");
    // Phones: the compact column, not Times. From sm: neither. From md up: Times.
    expect(head.style.getPropertyValue("--cols-base")).toBe("minmax(0,1fr) 32px");
    expect(head.style.getPropertyValue("--cols-sm")).toBe("minmax(0,1fr)");
    expect(head.style.getPropertyValue("--cols-md")).toBe("minmax(0,1fr) 80px");
    expect(head.style.getPropertyValue("--cols-xl")).toBe("minmax(0,1fr) 80px");
    expect(row.style.getPropertyValue("--cols-base")).toBe(head.style.getPropertyValue("--cols-base"));
    expect(row.className).toContain("md:grid-cols-(--cols-md)");
    expect(screen.getByRole("columnheader", { name: "Times" }).className).toContain("max-md:hidden");
    const cells = screen.getAllByRole("cell");
    expect(cells[1].className).toContain("max-md:hidden");
    expect(cells[2].className).toContain("sm:hidden");
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
