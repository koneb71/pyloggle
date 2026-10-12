import { useState } from "react";
import { Download } from "lucide-react";
import clsx from "clsx";
import { Button, Field, Popover, Select, buttonClass } from "@/components/ui";
import { EXPORT_LIMITS, exportUrl, type ExportFormat } from "./export-url";

/** Download the current selection as a spreadsheet (CSV) or JSON lines, streamed by the server. */
export function ExportMenu({ q, range, disabled }: { q: string; range: { from: string; to: string }; disabled?: boolean }) {
  const [format, setFormat] = useState<ExportFormat>("csv");
  const [limit, setLimit] = useState(10000);
  const href = exportUrl({ q, from: range.from, to: range.to, format, limit });
  return (
    <Popover
      align="end"
      width={300}
      trigger={({ toggle, open }) => (
        <Button onClick={toggle} active={open} icon={<Download size={14} />} disabled={disabled} title="Download the messages you're looking at">
          Export
        </Button>
      )}
    >
      {(close) => (
        <div className="p-2">
          <div className="text-[13px] font-semibold text-ink">Download messages</div>
          <p className="mb-3 mt-0.5 text-xs text-ink-3">The messages matching this search and time range, newest first.</p>
          <div className="mb-3 grid grid-cols-2 gap-1.5">
            {(
              [
                ["csv", "Spreadsheet", "CSV, opens in Excel or Sheets"],
                ["ndjson", "JSON lines", "One message per line, for scripts"],
              ] as [ExportFormat, string, string][]
            ).map(([f, label, hint]) => (
              <button
                key={f}
                type="button"
                onClick={() => setFormat(f)}
                aria-pressed={format === f}
                className={clsx("rounded-md border px-2.5 py-2 text-left", format === f ? "border-accent bg-accent-soft" : "border-border-strong hover:bg-surface-2")}
              >
                <span className={clsx("block text-[13px] font-medium", format === f ? "text-accent" : "text-ink")}>{label}</span>
                <span className="block text-[11px] leading-4 text-ink-3">{hint}</span>
              </button>
            ))}
          </div>
          <Field label="How many">
            <Select value={limit} onChange={(e) => setLimit(Number(e.target.value))}>
              {EXPORT_LIMITS.map((n) => (
                <option key={n} value={n}>
                  Up to {n.toLocaleString()} messages
                </option>
              ))}
            </Select>
          </Field>
          <a href={href} download onClick={() => close()} className={buttonClass({ variant: "primary", className: "mt-3 w-full" })}>
            <Download size={14} /> Download
          </a>
        </div>
      )}
    </Popover>
  );
}
