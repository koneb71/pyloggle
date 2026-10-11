import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LevelBadge, StatusBadge } from "./badge";

describe("badges", () => {
  it("level badge names the level in text (never color alone)", () => {
    render(<LevelBadge level="error" />);
    expect(screen.getByText("error")).toBeInTheDocument();
  });
  it("status badge shows regressed for open+regressed", () => {
    render(<StatusBadge status="open" regressed />);
    expect(screen.getByText("Regressed")).toBeInTheDocument();
  });
});
