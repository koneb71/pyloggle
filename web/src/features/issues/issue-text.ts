import type { IssueSummary } from "@/api/types";

/**
 * Splits an issue into what to show in bold and the line under it: the error type and its
 * message for crashes ("KeyError" / "'currency'"), or the logged message alone for issues that
 * come from error messages.
 */
export function issueHeadline(s: Pick<IssueSummary, "title" | "exception_type" | "last_message">): { headline: string; message: string } {
  const title = (s.title || "").trim();
  const type = (s.exception_type || "").trim();
  const last = (s.last_message || "").trim();
  if (type) {
    const prefix = `${type}: `;
    const message = title.startsWith(prefix) ? title.slice(prefix.length) : last || (title !== type ? title : "");
    return { headline: type, message };
  }
  const headline = title || last || "Unknown error";
  return { headline, message: last && last !== headline ? last : "" };
}

/** Within the last 24 hours: the list's "New" badge. */
export function isNewIssue(s: Pick<IssueSummary, "first_seen">, now = Date.now()): boolean {
  const t = new Date(s.first_seen).getTime();
  return !isNaN(t) && now - t < 24 * 3600_000;
}
