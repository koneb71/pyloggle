import clsx from "clsx";
import type { ReactNode } from "react";
import { AlertTriangle, Ban, CheckCircle2, CircleDot, RotateCcw } from "lucide-react";
import { levelVar } from "@/lib/levels";

export function Badge({ children, className, tone = "neutral" }: { children: ReactNode; className?: string; tone?: "neutral" | "accent" | "outline" }) {
  return (
    <span
      className={clsx(
        "inline-flex items-center gap-1 rounded px-1.5 py-px text-[11px] font-medium leading-4",
        tone === "neutral" && "bg-surface-3 text-ink-2",
        tone === "accent" && "bg-accent-soft text-accent",
        tone === "outline" && "border border-border text-ink-2",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** Level identity: a colored dot (series color) + the level name in text ink. */
export function LevelBadge({ level, className, compact }: { level: string; className?: string; compact?: boolean }) {
  const l = (level || "info").toLowerCase();
  return (
    <span className={clsx("inline-flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-wide text-ink-2", className)} title={l}>
      <span className="inline-block h-2 w-2 rounded-sm" style={{ background: levelVar(l) }} aria-hidden />
      {!compact && <span className={clsx(l === "error" || l === "fatal" ? "text-ink" : "")}>{l}</span>}
    </span>
  );
}

const STATUS: Record<string, { icon: ReactNode; label: string; cls: string }> = {
  open: { icon: <CircleDot size={12} />, label: "Open", cls: "text-accent bg-accent-soft" },
  resolved: { icon: <CheckCircle2 size={12} />, label: "Resolved", cls: "text-good-text bg-surface-3" },
  ignored: { icon: <Ban size={12} />, label: "Ignored", cls: "text-ink-3 bg-surface-3" },
  regressed: { icon: <RotateCcw size={12} />, label: "Regressed", cls: "text-critical bg-surface-3" },
  unhandled: { icon: <AlertTriangle size={12} />, label: "Unhandled", cls: "text-serious bg-surface-3" },
};

export function StatusBadge({ status, regressed, className }: { status: string; regressed?: boolean; className?: string }) {
  const key = regressed && status === "open" ? "regressed" : status;
  const s = STATUS[key] ?? { icon: <CircleDot size={12} />, label: status, cls: "text-ink-2 bg-surface-3" };
  return (
    <span className={clsx("inline-flex items-center gap-1 rounded px-1.5 py-px text-[11px] font-medium leading-4", s.cls, className)}>
      {s.icon}
      {s.label}
    </span>
  );
}

export function UnhandledBadge() {
  const s = STATUS.unhandled;
  return (
    <span className={clsx("inline-flex items-center gap-1 rounded px-1.5 py-px text-[11px] font-medium leading-4", s.cls)}>
      {s.icon}
      {s.label}
    </span>
  );
}
