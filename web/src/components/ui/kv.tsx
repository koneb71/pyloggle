import type { ReactNode } from "react";
import clsx from "clsx";
import { Filter, FilterX } from "lucide-react";
import { CopyButton } from "./misc";

export interface KVAction {
  onFilter?: (key: string, value: string) => void;
  onExclude?: (key: string, value: string) => void;
}

/**
 * Key/value list used for attributes, tags, HTTP and user blocks. Every value offers
 * Filter / Exclude / Copy on hover when handlers are supplied.
 */
export function KeyValueList({ entries, actions, keyPrefix = "", className, mono = true, emptyText = "none" }: { entries: Record<string, unknown> | [string, unknown][] | undefined; actions?: KVAction; keyPrefix?: string; className?: string; mono?: boolean; emptyText?: string }) {
  const list = Array.isArray(entries) ? entries : Object.entries(entries ?? {});
  if (list.length === 0) return <div className="px-1 text-xs text-ink-3">{emptyText}</div>;
  return (
    <dl className={clsx("grid grid-cols-[minmax(8rem,max-content)_1fr] gap-x-3 text-xs", className)}>
      {list
        .filter(([, v]) => v !== undefined && v !== null && v !== "")
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([k, v]) => {
          const sv = typeof v === "string" ? v : typeof v === "object" ? JSON.stringify(v) : String(v);
          return (
            <Row key={k} k={k} v={sv} mono={mono}>
              {actions?.onFilter && (
                <button type="button" title={`Filter ${keyPrefix}${k}:${sv}`} className="rounded p-0.5 text-ink-3 hover:bg-surface-3 hover:text-accent" onClick={() => actions.onFilter!(keyPrefix + k, sv)}>
                  <Filter size={11} />
                </button>
              )}
              {actions?.onExclude && (
                <button type="button" title={`Exclude ${keyPrefix}${k}:${sv}`} className="rounded p-0.5 text-ink-3 hover:bg-surface-3 hover:text-critical" onClick={() => actions.onExclude!(keyPrefix + k, sv)}>
                  <FilterX size={11} />
                </button>
              )}
              <CopyButton text={sv} className="h-4 px-0.5" />
            </Row>
          );
        })}
    </dl>
  );
}

function Row({ k, v, mono, children }: { k: string; v: string; mono: boolean; children: ReactNode }) {
  return (
    <>
      <dt className="group/kv truncate py-0.5 font-mono text-ink-3" title={k}>
        {k}
      </dt>
      <dd className="group/kv flex min-w-0 items-start gap-1 py-0.5">
        <span className={clsx("min-w-0 flex-1 whitespace-pre-wrap break-all text-ink", mono && "font-mono")}>{v}</span>
        <span className="invisible flex flex-none items-center group-hover/kv:visible">{children}</span>
      </dd>
    </>
  );
}
