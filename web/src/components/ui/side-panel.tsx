import { useEffect, type CSSProperties, type ReactNode } from "react";
import clsx from "clsx";
import { X } from "lucide-react";
import { createPortal } from "react-dom";
import { IconButton } from "./button";

/**
 * Right-hand detail panel. Rendered inline (not a portal) so it participates in page layout;
 * on small screens there is no room beside the list, so it covers the screen instead.
 */
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
    <aside
      className={clsx(
        "flex h-full min-h-0 flex-none flex-col border-l border-border bg-surface md:w-[var(--panel-w)]",
        "max-md:fixed max-md:inset-0 max-md:z-[54] max-md:w-full max-md:border-l-0",
        className,
      )}
      style={{ "--panel-w": `min(${width}px, 45%)` } as CSSProperties}
      aria-label={typeof title === "string" ? title : "Details"}
    >
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2 max-md:min-h-12">
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

/** A panel that slides over the page from the right, for details and settings on reading pages. */
export function Drawer({ open, onClose, title, description, actions, children, width = 640 }: { open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode; width?: number }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement) && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-[55] flex justify-end bg-black/30" onPointerDown={(e) => e.target === e.currentTarget && onClose()}>
      <aside role="dialog" aria-modal aria-label={typeof title === "string" ? title : "Details"} className="toast-in flex h-full w-full flex-col border-l border-border bg-surface shadow-[var(--shadow)]" style={{ maxWidth: width }}>
        <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-4">
          <div className="min-w-0">
            <h2 className="truncate text-[17px] font-semibold text-ink">{title}</h2>
            {description && <div className="mt-0.5 text-[13px] text-ink-3">{description}</div>}
          </div>
          <div className="flex flex-none items-center gap-1.5">
            {actions}
            <IconButton label="Close" icon={<X size={15} />} onClick={onClose} />
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </aside>
    </div>,
    document.body,
  );
}

/**
 * Phones only: a panel that covers the screen, for what sits beside the content on a desk
 * (the Logs filters). Escape or the close button in `children` dismisses it; `footer` stays at
 * the bottom, within reach of a thumb.
 */
export function Sheet({ open, onClose, label, children, footer }: { open: boolean; onClose: () => void; label: string; children: ReactNode; footer?: ReactNode }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div role="dialog" aria-modal aria-label={label} className="toast-in fixed inset-0 z-[54] flex flex-col bg-surface">
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      {footer && <div className="flex-none border-t border-border p-3">{footer}</div>}
    </div>,
    document.body,
  );
}
