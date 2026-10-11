import { useEffect, type ReactNode } from "react";
import clsx from "clsx";
import { X } from "lucide-react";
import { IconButton } from "./button";

/** Right-hand detail panel. Rendered inline (not a portal) so it participates in page layout. */
export function SidePanel({ open, onClose, title, children, width = 520, actions, className }: { open: boolean; onClose: () => void; title?: ReactNode; children: ReactNode; width?: number; actions?: ReactNode; className?: string }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement)) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <aside className={clsx("flex h-full min-h-0 flex-none flex-col border-l border-border bg-surface", className)} style={{ width }} aria-label={typeof title === "string" ? title : "Details"}>
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
        <div className="min-w-0 flex-1 truncate text-[13px] font-semibold text-ink">{title}</div>
        <div className="flex items-center gap-1">
          {actions}
          <IconButton label="Close" icon={<X size={14} />} onClick={onClose} />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </aside>
  );
}
