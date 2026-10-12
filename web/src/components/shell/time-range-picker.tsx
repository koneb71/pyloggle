import { useState } from "react";
import { CalendarClock, ChevronDown, RefreshCw } from "lucide-react";
import clsx from "clsx";
import { Button, Popover, Input, Field, IconButton } from "@/components/ui";
import { QUICK_RANGES, rangeLabel, resolveRange, type Range } from "@/lib/time";

function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * Time range control. A relative range ("Last hour") follows the clock and is marked Live;
 * a custom range is fixed.
 */
export function TimeRangePicker({ value, onChange, onRefresh, refreshing, className }: { value: Range; onChange: (r: Range) => void; onRefresh?: () => void; refreshing?: boolean; className?: string }) {
  const live = !value.to;
  return (
    <div className={clsx("inline-flex items-center gap-1", className)}>
      <Popover
        align="end"
        width={340}
        trigger={({ toggle, open }) => (
          <Button onClick={toggle} active={open} icon={live ? <span className="live-dot" aria-hidden /> : <CalendarClock size={14} />} title={live ? "Follows the clock: new data appears as it arrives" : "A fixed time range"}>
            <span className="tabular">{rangeLabel(value)}</span>
            {live && <span className="text-xs font-normal text-good-text">Live</span>}
            <ChevronDown size={13} className="text-ink-3" />
          </Button>
        )}
      >
        {(close) => <RangeMenu value={value} onChange={(r) => (onChange(r), close())} />}
      </Popover>
      {onRefresh && <IconButton label="Refresh now" onClick={onRefresh} icon={<RefreshCw size={14} className={refreshing ? "animate-spin" : ""} />} />}
    </div>
  );
}

function RangeMenu({ value, onChange }: { value: Range; onChange: (r: Range) => void }) {
  const r = resolveRange(value);
  const [from, setFrom] = useState(toLocalInput(r.from));
  const [to, setTo] = useState(toLocalInput(r.to));
  const [error, setError] = useState("");
  const apply = () => {
    const f = new Date(from);
    const t = new Date(to);
    if (isNaN(f.getTime()) || isNaN(t.getTime())) return setError("Enter both a start and an end.");
    if (f >= t) return setError("The start must be before the end.");
    setError("");
    onChange({ from: f.toISOString(), to: t.toISOString() });
  };
  return (
    <div className="p-1.5">
      <div className="px-1 pb-1.5 text-xs font-medium text-ink-3">Recent</div>
      <div className="grid grid-cols-2 gap-0.5">
        {QUICK_RANGES.map((q) => (
          <button
            key={q.from}
            type="button"
            className={clsx("rounded-md px-2 py-1.5 text-left text-[13px] hover:bg-surface-2", value.from === q.from && !value.to ? "bg-accent-soft font-medium text-accent" : "text-ink")}
            onClick={() => onChange({ from: q.from, to: "" })}
          >
            Last {q.label}
          </button>
        ))}
      </div>
      <div className="my-2 h-px bg-border" />
      <div className="px-1 pb-1.5 text-xs font-medium text-ink-3">Specific dates (your local time)</div>
      <div className="grid grid-cols-2 gap-2 px-1">
        <Field label="From">
          <Input type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} aria-invalid={!!error || undefined} />
        </Field>
        <Field label="To">
          <Input type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} aria-invalid={!!error || undefined} />
        </Field>
      </div>
      {error && (
        <div role="alert" className="px-1 pt-2 text-xs text-critical-text">
          {error}
        </div>
      )}
      <div className="mt-2.5 flex justify-end px-1 pb-0.5">
        <Button variant="primary" onClick={apply}>
          Apply
        </Button>
      </div>
    </div>
  );
}
