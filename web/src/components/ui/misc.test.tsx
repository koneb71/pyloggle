import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MetaItem, MetaLine } from "./misc";

describe("MetaLine", () => {
  it("skips empty items and gives each the dot that a wrapped line hides", () => {
    const { container } = render(
      <MetaLine>
        {"api"}
        {""}
        {"production"}
        <MetaItem className="sm:hidden">17:58</MetaItem>
      </MetaLine>,
    );
    const items = container.querySelectorAll(".pl-3");
    expect(items).toHaveLength(3);
    // Every item carries its own dot; the row is clipped so a line never starts with one.
    expect(container.firstElementChild?.className).toContain("overflow-x-clip");
    expect(container.querySelectorAll("[aria-hidden]")).toHaveLength(3);
    expect(screen.getByText("17:58").closest(".pl-3")?.className).toContain("sm:hidden");
  });
});
