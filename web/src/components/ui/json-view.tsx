import { useState } from "react";
import clsx from "clsx";
import { ChevronDown, ChevronRight } from "lucide-react";
import { CopyButton } from "./misc";

type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

function isObj(v: Json): v is { [k: string]: Json } {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Collapsible JSON tree. Keys and punctuation stay in text ink; values get subtle type tints. */
export function JsonView({ value, depth = 0, defaultOpen = 2, className }: { value: unknown; depth?: number; defaultOpen?: number; className?: string }) {
  return (
    <div className={clsx("font-mono text-xs leading-5 text-ink", className)}>
      <Node value={value as Json} depth={depth} defaultOpen={defaultOpen} />
    </div>
  );
}

function Node({ value, depth, defaultOpen, k }: { value: Json; depth: number; defaultOpen: number; k?: string }) {
  const [open, setOpen] = useState(depth < defaultOpen);
  const key = k !== undefined ? <span className="text-ink-2">{k}: </span> : null;
  if (isObj(value) || Array.isArray(value)) {
    const entries = Array.isArray(value) ? value.map((v, i) => [String(i), v] as const) : Object.entries(value);
    const [o, c] = Array.isArray(value) ? ["[", "]"] : ["{", "}"];
    if (entries.length === 0) return <div style={{ paddingLeft: depth ? 12 : 0 }}>{key}{o}{c}</div>;
    return (
      <div style={{ paddingLeft: depth ? 12 : 0 }}>
        <button type="button" className="inline-flex items-center gap-0.5 text-left hover:text-accent" onClick={() => setOpen(!open)}>
          <span className="text-ink-3">{open ? <ChevronDown size={11} /> : <ChevronRight size={11} />}</span>
          {key}
          <span className="text-ink-3">{o}{!open && ` ${entries.length} ${Array.isArray(value) ? "items" : "keys"} ${c}`}</span>
        </button>
        {open && (
          <>
            {entries.map(([ek, ev]) => (
              <Node key={ek} k={ek} value={ev} depth={depth + 1} defaultOpen={defaultOpen} />
            ))}
            <div className="text-ink-3">{c}</div>
          </>
        )}
      </div>
    );
  }
  return (
    <div className="group flex items-start gap-1 whitespace-pre-wrap break-all" style={{ paddingLeft: depth ? 12 : 0 }}>
      <span>
        {key}
        <Scalar v={value} />
      </span>
      {typeof value === "string" && value.length > 0 && <CopyButton text={value} className="invisible h-4 group-hover:visible" />}
    </div>
  );
}

function Scalar({ v }: { v: Json }) {
  if (v === null) return <span className="text-ink-3">null</span>;
  if (typeof v === "boolean") return <span className="text-serious">{String(v)}</span>;
  if (typeof v === "number") return <span className="text-accent">{String(v)}</span>;
  return <span className="text-ink">"{String(v)}"</span>;
}
