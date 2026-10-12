import { useEffect, type ReactNode } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { X } from "lucide-react";
import { IconButton } from "./button";

export function Dialog({ open, onClose, title, children, className, wide, maxWidth }: { open: boolean; onClose: () => void; title?: ReactNode; children: ReactNode; className?: string; wide?: boolean; maxWidth?: number }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-start justify-center overflow-y-auto bg-black/40 p-2 sm:p-4 sm:pt-[8vh]" onPointerDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal className={clsx("w-full rounded-lg border border-border bg-surface shadow-[var(--shadow)]", !maxWidth && (wide ? "max-w-5xl" : "max-w-lg"), className)} style={maxWidth ? { maxWidth } : undefined}>
        {title !== undefined && (
          <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
            <h2 className="text-[15px] font-semibold text-ink">{title}</h2>
            <IconButton label="Close" icon={<X size={14} />} onClick={onClose} />
          </div>
        )}
        <div className="p-4">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
