import clsx from "clsx";
import type { HTMLAttributes, ReactNode } from "react";
import { AlertTriangle, ArrowDownRight, ArrowUpRight, CheckCircle2, Minus, OctagonAlert } from "lucide-react";
import { fmtDelta } from "@/lib/format";

export function Card({ className, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return <div className={clsx("rounded-lg border border-border bg-surface", className)} {...rest} />;
}

export function CardHeader({ title, actions, className, sub }: { title: ReactNode; actions?: ReactNode; className?: string; sub?: ReactNode }) {
  return (
    <div className={clsx("flex items-start justify-between gap-3 border-b border-border px-4 py-3", className)}>
      <div className="min-w-0">
        <h3 className="truncate text-[14px] font-semibold leading-5 text-ink">{title}</h3>
        {sub && <div className="mt-0.5 text-xs text-ink-3">{sub}</div>}
      </div>
      {actions && <div className="flex flex-none items-center gap-1.5">{actions}</div>}
    </div>
  );
}

/**
 * A titled card: the building block of reading pages. `flush` drops the body padding for
 * tables and lists that run edge to edge.
 */
export function Section({ title, description, actions, children, className, bodyClassName, flush, id }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string; bodyClassName?: string; flush?: boolean; id?: string }) {
  return (
    <section id={id} className={clsx("rounded-lg border border-border bg-surface", className)}>
      <CardHeader title={title} sub={description} actions={actions} />
      <div className={clsx(!flush && "p-4", bodyClassName)}>{children}</div>
    </section>
  );
}

export type Tone = "good" | "warning" | "critical";

const TONE_TEXT: Record<Tone, string> = { good: "text-good-text", warning: "text-warning-text", critical: "text-critical-text" };
const TONE_ICON: Record<Tone, ReactNode> = {
  good: <CheckCircle2 size={16} aria-label="Healthy" />,
  warning: <AlertTriangle size={16} aria-label="Warning" />,
  critical: <OctagonAlert size={16} aria-label="Problem" />,
};

export interface StatDelta {
  current: number;
  previous: number;
  /** Which way is good news: errors going "down", requests going "up", or "neither". */
  goodWhen: "up" | "down" | "neither";
  /** "vs previous 24 hours". */
  label?: string;
}

/**
 * A headline number. The status tone colors the value and adds an icon, so state is never
 * shown by color alone; the delta says how it moved against the previous period.
 */
export function StatTile({ label, value, hint, tone, delta, loading, error, chart, className }: { label: ReactNode; value: ReactNode; hint?: ReactNode; tone?: Tone; delta?: StatDelta; loading?: boolean; error?: boolean; chart?: ReactNode; className?: string }) {
  return (
    <Card className={clsx("flex min-w-0 flex-col px-4 py-3.5", className)}>
      <div className="truncate text-[13px] font-medium text-ink-2">{label}</div>
      {loading ? (
        <div className="skeleton mt-2 h-7 w-20" aria-label="Loading" />
      ) : error ? (
        <div className="mt-1.5 text-[13px] text-ink-3">Couldn't load</div>
      ) : (
        <div className={clsx("mt-1 flex items-center gap-1.5 text-[26px] font-semibold leading-8 tracking-tight tabular", tone ? TONE_TEXT[tone] : "text-ink")}>
          {tone && tone !== "good" && <span className="flex-none">{TONE_ICON[tone]}</span>}
          <span className="min-w-0 truncate">{value}</span>
        </div>
      )}
      {!loading && !error && (delta || hint) && (
        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-ink-3">
          {delta && <DeltaText {...delta} />}
          {hint && <span className="min-w-0 truncate">{hint}</span>}
        </div>
      )}
      {chart && !loading && !error && <div className="mt-2">{chart}</div>}
    </Card>
  );
}

export function DeltaText({ current, previous, goodWhen, label }: StatDelta) {
  const d = fmtDelta(current, previous);
  if (!d) return null;
  const good = d.direction === "flat" || goodWhen === "neither" ? null : (d.direction === "up") === (goodWhen === "up");
  const Icon = d.direction === "up" ? ArrowUpRight : d.direction === "down" ? ArrowDownRight : Minus;
  return (
    <span className={clsx("inline-flex items-center gap-0.5 font-medium tabular", good === true && "text-good-text", good === false && "text-critical-text", good === null && "text-ink-2")}>
      <Icon size={13} aria-hidden />
      {d.text}
      {label && <span className="ml-1 font-normal text-ink-3">{label}</span>}
    </span>
  );
}
