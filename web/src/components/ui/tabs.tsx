import clsx from "clsx";
import type { ReactNode } from "react";

export interface TabItem {
  id: string;
  label: ReactNode;
  count?: number;
  title?: string;
}

export function Tabs({ items, value, onChange, className, size = "md" }: { items: TabItem[]; value: string; onChange: (id: string) => void; className?: string; size?: "sm" | "md" }) {
  return (
    <div role="tablist" className={clsx("flex items-end gap-4 overflow-x-auto border-b border-border", className)}>
      {items.map((t) => (
        <button
          key={t.id}
          role="tab"
          type="button"
          title={t.title}
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={clsx(
            "-mb-px flex items-center gap-1.5 whitespace-nowrap border-b-2 font-medium transition-colors",
            size === "md" ? "pb-2.5 pt-1 text-[13px]" : "pb-2 text-xs",
            value === t.id ? "border-accent text-ink" : "border-transparent text-ink-3 hover:text-ink",
          )}
        >
          {t.label}
          {t.count !== undefined && <span className={clsx("rounded-full px-1.5 text-[11px] tabular", value === t.id ? "bg-accent-soft text-accent" : "bg-surface-3 text-ink-2")}>{t.count.toLocaleString()}</span>}
        </button>
      ))}
    </div>
  );
}

export function SegmentedControl<T extends string>({ options, value, onChange, className, label }: { options: { value: T; label: ReactNode; title?: string }[]; value: T; onChange: (v: T) => void; className?: string; label?: string }) {
  return (
    <div className={clsx("inline-flex h-8 items-center rounded-md bg-surface-3/70 p-0.5", className)} role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          title={o.title}
          onClick={() => onChange(o.value)}
          className={clsx(
            "inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-[6px] px-2.5 text-[13px] font-medium transition-colors",
            value === o.value ? "bg-surface text-ink shadow-[0_1px_2px_rgba(0,0,0,0.08)]" : "text-ink-2 hover:text-ink",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
