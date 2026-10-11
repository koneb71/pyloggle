import { useState } from "react";
import { CalendarClock, ChevronDown, Radio, RefreshCw } from "lucide-react";
import clsx from "clsx";
import { Button, Popover, Input, Field } from "@/components/ui";
import { QUICK_RANGES, rangeLabel, resolveRange, type Range } from "@/lib/time";

function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function TimeRangePicker({ value, onChange, onRefresh, refreshing, className }: { value: Range; onChange: (r: Range) => void; onRefresh?: () => void; refreshing?: boolean; className?: string }) {
  const live = !value.to;
  return (
    <div className={clsx("inline-flex items-center gap-1", className)}>
      <Popover
        align="end"
        width={320}
        trigger={({ toggle, open }) => (
          <Button onClick={toggle} active={open} icon={live ? <Radio size={13} className="text-good" /> : <CalendarClock size={13} />} title="Time range">
            <span className="tabular">{rangeLabel(value)}</span>
            <ChevronDown size={12} className="text-ink-3" />
          </Button>
        )}
      >
        {(close) => <RangeMenu value={value} onChange={(r) => (onChange(r), close())} />}
      </Popover>
      {onRefresh && <Button variant="ghost" className="px-1.5" title="Refresh" aria-label="Refresh" onClick={onRefresh} icon={<RefreshCw size={13} className={refreshing ? "animate-spin" : ""} />} />}
    </div>
  );
}

function RangeMenu({ value, onChange }: { value: Range; onChange: (r: Range) => void }) {
  const r = resolveRange(value);
  const [from, setFrom] = useState(toLocalInput(r.from));
  const [to, setTo] = useState(toLocalInput(r.to));
  return (
    <div className="p-1">
      <div className="mb-1 px-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Quick ranges</div>
      <div className="grid grid-cols-4 gap-1">
        {QUICK_RANGES.map((q) => (
          <button
            key={q.from}
            type="button"
            className={clsx("rounded px-2 py-1 text-xs hover:bg-surface-2", value.from === q.from && !value.to ? "bg-accent-soft font-medium text-accent" : "text-ink")}
            onClick={() => onChange({ from: q.from, to: "" })}
          >
            Last {q.label}
          </button>
        ))}
      </div>
      <div className="my-2 h-px bg-border" />
      <div className="mb-1 px-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Custom (local time)</div>
      <div className="grid grid-cols-2 gap-2 px-1">
        <Field label="From">
          <Input type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label="To">
          <Input type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
      </div>
      <div className="mt-2 flex justify-end gap-1 px-1">
        <Button
          variant="primary"
          onClick={() => {
            const f = new Date(from);
            const t = new Date(to);
            if (isNaN(f.getTime()) || isNaN(t.getTime()) || f >= t) return;
            onChange({ from: f.toISOString(), to: t.toISOString() });
          }}
        >
          Apply
        </Button>
      </div>
    </div>
  );
}
