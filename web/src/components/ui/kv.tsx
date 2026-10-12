import type { ReactNode } from "react";
import clsx from "clsx";
import { Filter, FilterX } from "lucide-react";
import { fieldLabel, isCodeLike } from "@/lib/fields";
import { CopyButton } from "./misc";

export interface KVAction {
  onFilter?: (key: string, value: string) => void;
  onExclude?: (key: string, value: string) => void;
}

/**
 * Key/value list for details, attributes and labels. Keys read as plain labels ("Response
 * code"), with the technical name on hover; values are monospace only when they are code or
 * identifiers. Every value offers Show only / Hide / Copy on hover when handlers are supplied.
 */
export function KeyValueList({
  entries,
  actions,
  keyPrefix = "",
  className,
  mono = "auto",
  humanize = true,
  emptyText = "Nothing recorded",
}: {
  entries: Record<string, unknown> | [string, unknown][] | undefined;
  actions?: KVAction;
  keyPrefix?: string;
  className?: string;
  mono?: boolean | "auto";
  humanize?: boolean;
  emptyText?: string;
}) {
  const list = (Array.isArray(entries) ? entries : Object.entries(entries ?? {}))
    .filter(([, v]) => v !== undefined && v !== null && v !== "")
    .map(([k, v]) => {
      const sv = typeof v === "string" ? v : typeof v === "object" ? JSON.stringify(v) : String(v);
      return { k, v: sv, label: humanize ? fieldLabel(k) : k };
    })
    .sort((a, b) => a.label.localeCompare(b.label));
  if (list.length === 0) return <div className="text-[13px] text-ink-3">{emptyText}</div>;
  return (
    <dl className={clsx("grid grid-cols-[minmax(7rem,max-content)_minmax(0,1fr)] gap-x-4 text-[13px]", className)}>
      {list.map(({ k, v, label }) => (
        <Row key={k} label={label} technical={keyPrefix + k} v={v} mono={mono === "auto" ? isCodeLike(k, v) : mono}>
          {actions?.onFilter && (
            <button type="button" title={`Show only ${label}: ${v}`} aria-label={`Show only ${label}: ${v}`} className="rounded p-1 text-ink-3 hover:bg-surface-3 hover:text-accent" onClick={() => actions.onFilter!(keyPrefix + k, v)}>
              <Filter size={12} />
            </button>
          )}
          {actions?.onExclude && (
            <button type="button" title={`Hide ${label}: ${v}`} aria-label={`Hide ${label}: ${v}`} className="rounded p-1 text-ink-3 hover:bg-surface-3 hover:text-critical-text" onClick={() => actions.onExclude!(keyPrefix + k, v)}>
              <FilterX size={12} />
            </button>
          )}
          <CopyButton text={v} label={`Copy ${label}`} className="h-6" />
        </Row>
      ))}
    </dl>
  );
}

function Row({ label, technical, v, mono, children }: { label: string; technical: string; v: string; mono: boolean; children: ReactNode }) {
  return (
    <div className="group/kv contents">
      <dt className="truncate py-1 text-ink-3" title={label === technical ? undefined : `Field: ${technical}`}>
        {label}
      </dt>
      <dd className="flex min-w-0 items-start gap-1 py-1">
        <span className={clsx("min-w-0 flex-1 whitespace-pre-wrap break-words text-ink", mono && "break-all font-mono text-[12px] leading-5")}>{v}</span>
        <span className="invisible -my-0.5 flex flex-none items-center group-hover/kv:visible group-focus-within/kv:visible">{children}</span>
      </dd>
    </div>
  );
}
