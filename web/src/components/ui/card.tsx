import clsx from "clsx";
import type { HTMLAttributes, ReactNode } from "react";

export function Card({ className, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return <div className={clsx("rounded-lg border border-border bg-surface", className)} {...rest} />;
}

export function CardHeader({ title, actions, className, sub }: { title: ReactNode; actions?: ReactNode; className?: string; sub?: ReactNode }) {
  return (
    <div className={clsx("flex items-center justify-between gap-3 border-b border-border px-3 py-2", className)}>
      <div className="min-w-0">
        <h3 className="truncate text-[13px] font-semibold text-ink">{title}</h3>
        {sub && <div className="text-xs text-ink-3">{sub}</div>}
      </div>
      {actions && <div className="flex items-center gap-1">{actions}</div>}
    </div>
  );
}

export function StatTile({ label, value, hint, tone }: { label: string; value: ReactNode; hint?: ReactNode; tone?: "good" | "critical" | "warning" }) {
  return (
    <Card className="px-3 py-2.5">
      <div className="text-[11px] font-medium uppercase tracking-wide text-ink-3">{label}</div>
      <div className={clsx("mt-0.5 text-2xl font-semibold leading-tight text-ink", tone === "critical" && "text-critical", tone === "good" && "text-good-text")}>{value}</div>
      {hint && <div className="mt-0.5 text-xs text-ink-3">{hint}</div>}
    </Card>
  );
}
