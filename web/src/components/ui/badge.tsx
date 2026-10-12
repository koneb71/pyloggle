import clsx from "clsx";
import type { ReactNode } from "react";
import { AlertTriangle, Ban, CheckCircle2, CircleDot, RotateCcw } from "lucide-react";
import { levelLabel, levelVar } from "@/lib/levels";
import { term, type TermKey } from "@/lib/glossary";

export type BadgeTone = "neutral" | "accent" | "outline" | "good" | "warning" | "critical";

const TONES: Record<BadgeTone, string> = {
  neutral: "bg-surface-3 text-ink-2",
  accent: "bg-accent-soft text-accent",
  outline: "border border-border-strong text-ink-2",
  good: "bg-good/12 text-good-text",
  warning: "bg-warning/18 text-warning-text",
  critical: "bg-critical/10 text-critical-text",
};

export function Badge({ children, className, tone = "neutral", title, icon }: { children: ReactNode; className?: string; tone?: BadgeTone; title?: string; icon?: ReactNode }) {
  return (
    <span title={title} className={clsx("inline-flex items-center gap-1 whitespace-nowrap rounded-md px-1.5 py-0.5 text-xs font-medium leading-4", TONES[tone], className)}>
      {icon}
      {children}
    </span>
  );
}

/** Severity: a colored dot (the level's series color) and its name in text ink, never color alone. */
export function LevelBadge({ level, className, compact }: { level: string; className?: string; compact?: boolean }) {
  const l = (level || "info").toLowerCase();
  return (
    <span className={clsx("inline-flex items-center gap-1.5 whitespace-nowrap text-[13px] text-ink-2", className)} title={compact ? levelLabel(l) : undefined}>
      <span className="inline-block h-2 w-2 flex-none rounded-full" style={{ background: levelVar(l) }} aria-hidden />
      {compact ? <span className="sr-only">{levelLabel(l)}</span> : <span className={clsx((l === "error" || l === "fatal") && "font-medium text-ink")}>{levelLabel(l)}</span>}
    </span>
  );
}

const STATUS: Record<string, { icon: ReactNode; term: TermKey; tone: BadgeTone }> = {
  open: { icon: <CircleDot size={12} />, term: "unresolved", tone: "accent" },
  resolved: { icon: <CheckCircle2 size={12} />, term: "resolved", tone: "good" },
  ignored: { icon: <Ban size={12} />, term: "ignored", tone: "neutral" },
  regressed: { icon: <RotateCcw size={12} />, term: "regressed", tone: "critical" },
  unhandled: { icon: <AlertTriangle size={12} />, term: "unhandled", tone: "warning" },
};

/** Issue state in plain words: Unresolved, Came back, Resolved, Ignored. */
export function StatusBadge({ status, regressed, className }: { status: string; regressed?: boolean; className?: string }) {
  const key = regressed && status === "open" ? "regressed" : status;
  const s = STATUS[key];
  if (!s) return <Badge className={className}>{status}</Badge>;
  const t = term(s.term);
  return (
    <Badge tone={s.tone} icon={s.icon} className={className} title={`${t.explain} (${t.technical})`}>
      {t.label}
    </Badge>
  );
}

/** "Crashed": nothing in the app caught the error. */
export function UnhandledBadge({ className }: { className?: string }) {
  return <StatusBadge status="unhandled" className={className} />;
}
