import clsx from "clsx";
import type { ReactNode } from "react";

export interface TabItem {
  id: string;
  label: ReactNode;
  count?: number;
}

export function Tabs({ items, value, onChange, className, size = "md" }: { items: TabItem[]; value: string; onChange: (id: string) => void; className?: string; size?: "sm" | "md" }) {
  return (
    <div role="tablist" className={clsx("flex items-end gap-0.5 overflow-x-auto border-b border-border", className)}>
      {items.map((t) => (
        <button
          key={t.id}
          role="tab"
          type="button"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={clsx(
            "-mb-px flex items-center gap-1.5 whitespace-nowrap border-b-2 px-2.5 font-medium transition-colors",
            size === "md" ? "py-2 text-[13px]" : "py-1.5 text-xs",
            value === t.id ? "border-accent text-ink" : "border-transparent text-ink-2 hover:text-ink",
          )}
        >
          {t.label}
          {t.count !== undefined && <span className="rounded bg-surface-3 px-1.5 text-[11px] tabular text-ink-2">{t.count.toLocaleString()}</span>}
        </button>
      ))}
    </div>
  );
}

export function SegmentedControl<T extends string>({ options, value, onChange, className }: { options: { value: T; label: ReactNode; title?: string }[]; value: T; onChange: (v: T) => void; className?: string }) {
  return (
    <div className={clsx("inline-flex rounded-md border border-border bg-surface p-0.5", className)} role="radiogroup">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          title={o.title}
          onClick={() => onChange(o.value)}
          className={clsx("rounded px-2 py-0.5 text-xs font-medium transition-colors", value === o.value ? "bg-surface-3 text-ink shadow-sm" : "text-ink-2 hover:text-ink")}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
