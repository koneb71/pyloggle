import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LevelBadge, StatusBadge, UnhandledBadge } from "./badge";

describe("badges", () => {
  it("level badge names the level in words (never color alone)", () => {
    render(<LevelBadge level="warn" />);
    expect(screen.getByText("Warning")).toBeInTheDocument();
  });
  it("compact level badge keeps the name for screen readers", () => {
    render(<LevelBadge level="error" compact />);
    expect(screen.getByText("Error")).toHaveClass("sr-only");
  });
  it("status badge uses plain words with the technical name in the title", () => {
    render(<StatusBadge status="open" regressed />);
    const b = screen.getByText("Came back");
    expect(b.closest("span")?.getAttribute("title")).toContain("regressed");
  });
  it("open reads as Unresolved and unhandled as Crashed", () => {
    render(
      <>
        <StatusBadge status="open" />
        <UnhandledBadge />
      </>,
    );
    expect(screen.getByText("Unresolved")).toBeInTheDocument();
    expect(screen.getByText("Crashed")).toBeInTheDocument();
  });
});
