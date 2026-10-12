import { describe, expect, it } from "vitest";
import { fieldLabel, isCodeLike } from "./fields";
import { GLOSSARY, labelWithTechnical, term } from "./glossary";

describe("fieldLabel", () => {
  it("uses the known plain names", () => {
    expect(fieldLabel("http.status")).toBe("Response code");
    expect(fieldLabel("service")).toBe("App");
    expect(fieldLabel("env")).toBe("Environment");
    expect(fieldLabel("release")).toBe("Version");
    expect(fieldLabel("hostname")).toBe("Server");
    expect(fieldLabel("user_agent")).toBe("Browser");
    expect(fieldLabel("id_hash")).toBe("Person (anonymized)");
  });
  it("strips attribute and tag prefixes", () => {
    expect(fieldLabel("attrs.http.route")).toBe("Route");
    expect(fieldLabel("tags.team")).toBe("Team");
  });
  it("humanizes unknown keys with acronyms", () => {
    expect(fieldLabel("order_id")).toBe("Order ID");
    expect(fieldLabel("payment.retryCount")).toBe("Payment retry count");
    expect(fieldLabel("db.query_ms")).toBe("Database query (ms)");
    expect(fieldLabel("api_url")).toBe("API URL");
    expect(fieldLabel("")).toBe("");
  });
});

describe("isCodeLike", () => {
  it("treats identifiers, paths and code as code", () => {
    for (const k of ["trace_id", "request_id", "user.id_hash", "code.filepath", "http.route", "db.statement", "attrs.order_id", "culprit", "exception_type"]) {
      expect(isCodeLike(k), k).toBe(true);
    }
  });
  it("keeps ordinary fields in the text face", () => {
    for (const k of ["service", "env", "level", "message", "user.email", "http.status", "team", "release"]) {
      expect(isCodeLike(k), k).toBe(false);
    }
  });
  it("looks at the value when the key is ordinary", () => {
    expect(isCodeLike("customer", "3f2b1c9e-8d4a-4b6f-9c1e-2a7d5e8f0b13")).toBe(true);
    expect(isCodeLike("checksum", "deadbeefcafe1234")).toBe(true);
    expect(isCodeLike("template", "/srv/app/templates/base.html")).toBe(true);
    expect(isCodeLike("plan", "enterprise")).toBe(false);
  });
});

describe("glossary", () => {
  it("has a label, a technical name and an explanation for every term", () => {
    for (const [k, e] of Object.entries(GLOSSARY)) {
      expect(e.label, k).not.toBe("");
      expect(e.technical, k).not.toBe("");
      expect(e.explain.endsWith("."), k).toBe(true);
    }
  });
  it("pairs the plain label with the technical name", () => {
    expect(term("regressed").label).toBe("Came back");
    expect(labelWithTechnical("regressed")).toBe("Came back (regressed)");
    expect(labelWithTechnical("issue")).toBe("Issue");
  });
});
