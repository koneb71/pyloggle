import { useMemo, useState } from "react";
import clsx from "clsx";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { ChainedException, Frame, Occurrence } from "@/api/types";
import { Badge, Button, CopyButton, SegmentedControl } from "@/components/ui";
import { lastSegments } from "@/lib/format";

interface Section {
  key: string;
  heading?: string; // chain relation text
  type: string;
  module?: string;
  message: string;
  frames: Frame[];
  notes?: string[];
}

/** Builds the display order: root cause first (deepest in the chain), the reported exception last — like Python prints it. */
function sections(o: Occurrence): Section[] {
  // chain[0] is the direct cause/context of the main exception, chain[1] the cause of chain[0], ...
  // Display deepest cause first (as Python prints it); the heading before a section explains how
  // the previous section relates to it.
  const chain = [...(o.chain ?? [])].reverse();
  const out: Section[] = [];
  let heading: string | undefined;
  chain.forEach((c: ChainedException, i) => {
    out.push({ key: `chain-${i}`, type: c.type, module: c.module, message: c.message, frames: c.frames ?? [], notes: c.notes, heading });
    heading = relationText(c);
  });
  out.push({ key: "main", type: o.exception_type, module: o.exception_module, message: o.exception_message, frames: o.frames ?? [], notes: o.notes, heading });
  return out;
}

function relationText(c: ChainedException): string {
  const rel = c.relation || (c.is_cause ? "cause" : "context");
  if (rel === "cause") return "The above exception was the direct cause of the following exception:";
  if (rel === "group") return "Part of an exception group raised below:";
  return "During handling of the above exception, another exception occurred:";
}

export function ExceptionView({ occ }: { occ: Occurrence }) {
  const [mode, setMode] = useState<"app" | "all" | "raw">("app");
  const secs = useMemo(() => sections(occ), [occ]);
  const hasInApp = secs.some((s) => s.frames.some((f) => f.in_app === true));
  const effectiveMode = mode === "app" && !hasInApp ? "all" : mode;
  const raw = useMemo(() => renderRaw(secs), [secs]);
  if (!occ.exception_type && (occ.frames ?? []).length === 0) {
    return (
      <div className="rounded-md border border-border bg-surface-2 p-3 text-xs text-ink-2">
        No exception attached. This issue groups <span className="font-medium text-ink">{occ.level}</span>-level log lines from <span className="font-mono">{occ.logger || occ.service}</span>.
      </div>
    );
  }
  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="text-xs text-ink-3">
          {secs.reduce((n, s) => n + s.frames.length, 0)} frames{secs.length > 1 && ` · ${secs.length - 1} chained`}
        </div>
        <div className="flex items-center gap-1">
          <SegmentedControl
            value={effectiveMode}
            onChange={setMode}
            options={[
              { value: "app", label: "In-app", title: "Only frames from your code" },
              { value: "all", label: "All frames" },
              { value: "raw", label: "Raw" },
            ]}
          />
          <CopyButton text={raw} label="Copy traceback" size="sm" />
        </div>
      </div>
      {effectiveMode === "raw" ? (
        <pre className="overflow-x-auto rounded-md border border-border bg-surface-2 p-3 font-mono text-xs leading-5 text-ink">{raw}</pre>
      ) : (
        <div className="space-y-4">
          {secs.map((s, i) => (
            <div key={s.key}>
              {s.heading && <div className="mb-2 text-center text-[11px] italic text-ink-3">{s.heading}</div>}
              <ExceptionHeader s={s} primary={i === secs.length - 1} />
              <FrameList frames={s.frames} appOnly={effectiveMode === "app"} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ExceptionHeader({ s, primary }: { s: Section; primary: boolean }) {
  return (
    <div className={clsx("mb-1.5 rounded-md border px-3 py-2", primary ? "border-critical/30 bg-surface" : "border-border bg-surface")}>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-mono text-[13px] font-semibold text-ink">
          {s.module && s.module !== "builtins" ? `${s.module}.` : ""}
          {s.type || "Error"}
        </span>
        {s.message && <span className="min-w-0 break-words font-mono text-xs text-ink-2">{s.message}</span>}
      </div>
      {s.notes && s.notes.length > 0 && (
        <ul className="mt-1 list-disc pl-5 text-xs text-ink-2">
          {s.notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function FrameList({ frames, appOnly }: { frames: Frame[]; appOnly: boolean }) {
  // Frames arrive oldest-first (crash site last). Show the crash site first.
  const ordered = useMemo(() => [...frames].reverse(), [frames]);
  const [showHidden, setShowHidden] = useState(false);
  if (ordered.length === 0) return <div className="px-1 text-xs text-ink-3">no frames</div>;
  const rows: { f: Frame; idx: number }[] = ordered.map((f, idx) => ({ f, idx }));
  const visible = appOnly && !showHidden ? rows.filter((r) => r.f.in_app !== false) : rows;
  const hidden = rows.length - visible.length;
  return (
    <ol className="divide-y divide-border overflow-hidden rounded-md border border-border">
      {visible.map(({ f, idx }) => (
        <FrameRow key={idx} f={f} defaultOpen={idx === 0 || (f.in_app === true && idx < 4)} />
      ))}
      {hidden > 0 && (
        <li className="bg-surface-2 px-3 py-1.5 text-xs text-ink-3">
          <Button size="xs" variant="ghost" onClick={() => setShowHidden(true)}>
            Show {hidden} library frame{hidden === 1 ? "" : "s"}
          </Button>
        </li>
      )}
    </ol>
  );
}

function FrameRow({ f, defaultOpen }: { f: Frame; defaultOpen: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const hasCtx = !!f.context || (f.pre_context?.length ?? 0) > 0;
  const hasVars = f.vars && Object.keys(f.vars).length > 0;
  const inApp = f.in_app !== false;
  const start = f.lineno - (f.pre_context?.length ?? 0);
  return (
    <li className={clsx("bg-surface", !inApp && "bg-surface-2/60")}>
      <button type="button" className="flex w-full items-center gap-2 px-2 py-1.5 text-left hover:bg-surface-2" onClick={() => setOpen(!open)} disabled={!hasCtx && !hasVars}>
        <span className="text-ink-3">{hasCtx || hasVars ? open ? <ChevronDown size={12} /> : <ChevronRight size={12} /> : <span className="inline-block w-3" />}</span>
        <span className={clsx("min-w-0 flex-1 truncate font-mono text-xs", inApp ? "text-ink" : "text-ink-2")} title={f.abs_path || f.filename}>
          <span className={inApp ? "font-medium" : ""}>{f.module || lastSegments(f.filename, 2)}</span>
          <span className="text-ink-3"> in </span>
          <span className={clsx(inApp ? "text-accent" : "text-ink-2")}>{f.function || "<module>"}</span>
          <span className="text-ink-3"> at line </span>
          <span className="tabular">{f.lineno}</span>
        </span>
        {!inApp && <Badge tone="outline">lib</Badge>}
      </button>
      {open && (hasCtx || hasVars) && (
        <div className="border-t border-border">
          {hasCtx && (
            <div className="frame-context px-2 py-1.5">
              {(f.pre_context ?? []).map((l, i) => (
                <div key={`p${i}`}>
                  <span className="ln">{start + i}</span>
                  {l}
                </div>
              ))}
              <div className="hl">
                <span className="ln">{f.lineno}</span>
                {f.context}
              </div>
              {(f.post_context ?? []).map((l, i) => (
                <div key={`q${i}`}>
                  <span className="ln">{f.lineno + 1 + i}</span>
                  {l}
                </div>
              ))}
            </div>
          )}
          {hasVars && (
            <div className="border-t border-border bg-surface-2/60 px-3 py-1.5">
              <div className="mb-0.5 text-[10px] font-medium uppercase tracking-wide text-ink-3">Local variables</div>
              <dl className="grid grid-cols-[max-content_1fr] gap-x-3 font-mono text-xs">
                {Object.entries(f.vars!).map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt className="text-accent">{k}</dt>
                    <dd className="min-w-0 whitespace-pre-wrap break-all text-ink-2">{v}</dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

function renderRaw(secs: Section[]): string {
  const out: string[] = [];
  secs.forEach((s, i) => {
    if (i > 0) out.push("", s.heading ?? "", "");
    out.push("Traceback (most recent call last):");
    for (const f of s.frames) {
      out.push(`  File "${f.abs_path || f.filename}", line ${f.lineno}, in ${f.function || "<module>"}`);
      if (f.context) out.push(`    ${f.context.trim()}`);
    }
    out.push(`${s.module && s.module !== "builtins" ? s.module + "." : ""}${s.type}: ${s.message}`);
    for (const n of s.notes ?? []) out.push(n);
  });
  return out.join("\n");
}
