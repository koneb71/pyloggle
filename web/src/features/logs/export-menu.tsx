import { useState } from "react";
import { Download } from "lucide-react";
import clsx from "clsx";
import { Button, Popover, Select } from "@/components/ui";
import { EXPORT_LIMITS, exportUrl, type ExportFormat } from "./export-url";

/** Download the current selection as CSV or NDJSON (streamed by the server, newest first). */
export function ExportMenu({ q, range, disabled }: { q: string; range: { from: string; to: string }; disabled?: boolean }) {
  const [format, setFormat] = useState<ExportFormat>("csv");
  const [limit, setLimit] = useState(10000);
  const href = exportUrl({ q, from: range.from, to: range.to, format, limit });
  return (
    <Popover
      align="end"
      width={260}
      trigger={({ toggle, open }) => (
        <Button onClick={toggle} active={open} icon={<Download size={13} />} disabled={disabled} title="Export the current selection">
          Export
        </Button>
      )}
    >
      {(close) => (
        <div className="p-1.5">
          <div className="px-1 pb-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Export logs</div>
          <div className="flex gap-1 px-1 pb-2">
            {(["csv", "ndjson"] as ExportFormat[]).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setFormat(f)}
                className={clsx("flex-1 rounded-md border px-2 py-1 text-xs font-medium", format === f ? "border-accent bg-accent-soft text-accent" : "border-border text-ink-2 hover:bg-surface-2")}
              >
                {f.toUpperCase()}
              </button>
            ))}
          </div>
          <label className="block px-1 pb-2 text-xs text-ink-2">
            <span className="mb-1 block">Rows (newest first)</span>
            <Select value={limit} onChange={(e) => setLimit(Number(e.target.value))} className="h-7 text-xs">
              {EXPORT_LIMITS.map((n) => (
                <option key={n} value={n}>
                  up to {n.toLocaleString()}
                </option>
              ))}
            </Select>
          </label>
          <div className="px-1 pb-1 text-[11px] text-ink-3">{format === "csv" ? "Spreadsheet-safe CSV: attrs and tags as JSON columns." : "One JSON event per line, same shape as the API."}</div>
          <a
            href={href}
            download
            onClick={() => close()}
            className="mx-1 mb-0.5 flex h-7 items-center justify-center gap-1.5 rounded-md bg-accent px-3 text-xs font-medium text-accent-ink hover:brightness-110"
          >
            <Download size={12} /> Download {format.toUpperCase()}
          </a>
        </div>
      )}
    </Popover>
  );
}
