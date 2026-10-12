import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";

/**
 * A minimal click-to-open popover anchored to its trigger. Closes on an outside tap or click,
 * and on Escape. It never leaves the window: on a narrow screen it slides back into view.
 */
export function Popover({
  trigger,
  children,
  open,
  onOpenChange,
  align = "start",
  className,
  anchorClassName,
  width,
}: {
  trigger: (props: { open: boolean; toggle: () => void }) => ReactNode;
  children: ReactNode | ((close: () => void) => ReactNode);
  open?: boolean;
  onOpenChange?: (o: boolean) => void;
  align?: "start" | "end";
  className?: string;
  /** Classes for the wrapper around the trigger, e.g. "flex w-full" for a full-width trigger. */
  anchorClassName?: string;
  width?: number | string;
}) {
  const [internal, setInternal] = useState(false);
  const isOpen = open ?? internal;
  const setOpen = (o: boolean) => {
    setInternal(o);
    onOpenChange?.(o);
  };
  const anchor = useRef<HTMLSpanElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; right?: number } | null>(null);

  useLayoutEffect(() => {
    if (!isOpen || !anchor.current) return;
    const r = anchor.current.getBoundingClientRect();
    const top = r.bottom + 4 + window.scrollY;
    if (align === "end") setPos({ top, left: 0, right: Math.max(8, window.innerWidth - r.right - window.scrollX) });
    else setPos({ top, left: r.left + window.scrollX });
  }, [isOpen, align]);

  // Once its size is known: a panel that would cross a side of the window moves inside it, and
  // one that would run off the bottom opens above its trigger when there is room there.
  useLayoutEffect(() => {
    const el = panel.current;
    const a = anchor.current?.getBoundingClientRect();
    if (!isOpen || !pos || !el || !a) return;
    const { offsetWidth: w, offsetHeight: h } = el;
    const vw = document.documentElement.clientWidth;
    const left = pos.right === undefined ? pos.left - window.scrollX : vw - pos.right - w;
    const fitted = Math.min(Math.max(8, left), Math.max(8, vw - w - 8));
    const above = a.bottom + 4 + h > window.innerHeight - 8 && a.top - 4 - h >= 8;
    const top = (above ? a.top - 4 - h : a.bottom + 4) + window.scrollY;
    const moveX = Math.abs(fitted - left) > 0.5;
    if (moveX || Math.abs(top - pos.top) > 0.5) setPos(moveX ? { top, left: fitted + window.scrollX } : { ...pos, top });
  }, [isOpen, pos]);

  useEffect(() => {
    if (!isOpen) return;
    // Pointer events, not mouse events: iOS sends no mousedown for a tap on a plain area.
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (panel.current?.contains(t) || anchor.current?.contains(t)) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  const close = () => setOpen(false);
  return (
    <>
      <span ref={anchor} className={anchorClassName ?? "inline-flex"}>
        {trigger({ open: isOpen, toggle: () => setOpen(!isOpen) })}
      </span>
      {isOpen &&
        pos &&
        createPortal(
          <div
            ref={panel}
            role="dialog"
            className={clsx("absolute z-[70] rounded-lg border border-border bg-surface p-1 text-ink shadow-[var(--shadow)]", className)}
            style={{ top: pos.top, left: pos.right === undefined ? pos.left : undefined, right: pos.right, width, maxWidth: "calc(100vw - 16px)" }}
          >
            {typeof children === "function" ? children(close) : children}
          </div>,
          document.body,
        )}
    </>
  );
}

export function MenuItem({ children, onClick, danger, icon, disabled }: { children: ReactNode; onClick?: () => void; danger?: boolean; icon?: ReactNode; disabled?: boolean }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={clsx(
        "flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] hover:bg-surface-2 disabled:opacity-50 pointer-coarse:py-2.5 pointer-coarse:text-sm",
        danger ? "text-critical" : "text-ink",
      )}
    >
      {icon && <span className="text-ink-3">{icon}</span>}
      {children}
    </button>
  );
}
