import { describe, expect, it } from "vitest";
import { kindWords, requestLabel, requestResult, statusWords } from "./trace-words";

describe("request words", () => {
  it("decides the outcome of a request", () => {
    expect(requestResult({ status: "error", errors: 0 })).toBe("failed");
    expect(requestResult({ status: "unset", errors: 2 })).toBe("failed");
    expect(requestResult({ status: "unset", errors: 0, http_status: 503 })).toBe("failed");
    expect(requestResult({ status: "unset", errors: 0, http_status: 200 })).toBe("ok");
    expect(requestResult({ status: "unset", errors: 0 })).toBe("unknown");
  });
  it("names requests and steps", () => {
    expect(requestLabel({ http_method: "GET", http_route: "/orders", name: "x" })).toBe("GET /orders");
    expect(requestLabel({ name: "SELECT shop.orders" })).toBe("SELECT shop.orders");
    expect(kindWords("client")).toBe("Calls another service");
    expect(statusWords("unset")).toBe("Not reported");
  });
});
