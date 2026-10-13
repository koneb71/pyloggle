import { useMemo, useState } from "react";
import { CopyButton, JsonView, SegmentedControl } from "@/components/ui";
import { LearnMore } from "@/components/learn-more";

const REDACTED = "[REDACTED]";
const SENDS_A_BODY = new Set(["POST", "PUT", "PATCH"]);

/** Whether a request of this method normally carries a body, so a missing one is worth saying. */
export function expectsBody(method?: string): boolean {
  return !!method && SENDS_A_BODY.has(method.toUpperCase());
}

/** A stored body as a tree when it is a JSON object or list (forms are recorded that way too). */
export function parseBody(body: string): object | null {
  const t = body.trimStart();
  if (!t.startsWith("{") && !t.startsWith("[")) return null;
  try {
    const v: unknown = JSON.parse(body);
    return typeof v === "object" && v !== null ? v : null;
  } catch {
    return null;
  }
}

/**
 * The body of a web request, as the app recorded it: fields as a tree (JSON and forms), anything
 * else as text. Says when the body was cut short, and when secrets in it were blanked out.
 */
export function RequestBody({ body, truncated, method }: { body?: string; truncated?: boolean; method?: string }) {
  const tree = useMemo(() => (body ? parseBody(body) : null), [body]);
  const [view, setView] = useState<"tree" | "text">("tree");
  if (!body) {
    if (!expectsBody(method)) return null;
    return (
      <p className="text-[13px] text-ink-3">
        No body was recorded. <LearnMore doc="sdkConfiguration" anchor="request-bodies">What gets recorded</LearnMore>
      </p>
    );
  }
  const text = tree ? JSON.stringify(tree, null, 2) : body;
  const showTree = tree !== null && view === "tree";
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-end gap-1">
        {tree && (
          <SegmentedControl
            label="How to show the body"
            value={view}
            onChange={setView}
            options={[
              { value: "tree", label: "Fields", title: "Each field on its own line; fold the nested ones" },
              { value: "text", label: "Plain text", title: "The body as text, ready to copy" },
            ]}
          />
        )}
        <CopyButton text={text} label="Copy body" size="sm" showLabel />
      </div>
      <div className="max-h-96 overflow-auto rounded-md border border-border bg-surface-2 p-3">
        {showTree ? <JsonView value={tree} defaultOpen={3} /> : <pre className="whitespace-pre-wrap break-all font-mono text-xs leading-5 text-ink">{text}</pre>}
      </div>
      {(truncated || body.includes(REDACTED)) && (
        <ul className="space-y-0.5 text-xs text-ink-3">
          {truncated && <li>Only the start of this body was recorded: it was longer than the limit the app sends.</li>}
          {body.includes(REDACTED) && <li>Passwords, tokens and keys were blanked out ({REDACTED}) before they left the app.</li>}
        </ul>
      )}
    </div>
  );
}
