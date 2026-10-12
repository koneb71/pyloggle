import { cloneElement, isValidElement, useId, useLayoutEffect, useRef, useState, type ReactElement, type ReactNode } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { term, type TermKey } from "@/lib/glossary";

/**
 * Accessible tooltip: opens on hover and on keyboard focus, closes on leave, blur and Escape,
 * and is linked to its trigger with aria-describedby. The trigger must be a single element.
 */
export function Tooltip({ content, children, delay = 250, className }: { content: ReactNode; children: ReactElement<Record<string, unknown>>; delay?: number; className?: string }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const tip = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; below: boolean } | null>(null);

  const show = (el: Element) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      setAnchor(el.getBoundingClientRect());
      setOpen(true);
    }, delay);
  };
  const hide = () => {
    window.clearTimeout(timer.current);
    setOpen(false);
    setPos(null);
  };

  useLayoutEffect(() => {
    if (!open || !anchor || !tip.current) return;
    const w = tip.current.offsetWidth;
    const h = tip.current.offsetHeight;
    const below = anchor.top - h - 8 < 4;
    const top = below ? anchor.bottom + 6 : anchor.top - h - 6;
    const left = Math.min(Math.max(8, anchor.left + anchor.width / 2 - w / 2), window.innerWidth - w - 8);
    setPos({ top, left, below });
  }, [open, anchor]);

  if (!content || !isValidElement(children)) return children;
  const p = children.props as Record<string, (e: unknown) => void>;
  const trigger = cloneElement(children, {
    "aria-describedby": open ? id : undefined,
    onMouseEnter: (e: React.MouseEvent) => (p.onMouseEnter?.(e), show(e.currentTarget)),
    onMouseLeave: (e: React.MouseEvent) => (p.onMouseLeave?.(e), hide()),
    onFocus: (e: React.FocusEvent) => (p.onFocus?.(e), show(e.currentTarget)),
    onBlur: (e: React.FocusEvent) => (p.onBlur?.(e), hide()),
    onKeyDown: (e: React.KeyboardEvent) => (p.onKeyDown?.(e), e.key === "Escape" && hide()),
  });
  return (
    <>
      {trigger}
      {open &&
        createPortal(
          <div
            ref={tip}
            id={id}
            role="tooltip"
            className={clsx("pointer-events-none fixed z-[80] max-w-[18rem] rounded-md bg-[var(--text)] px-2.5 py-1.5 text-xs leading-snug text-[var(--surface)] shadow-[var(--shadow)]", className)}
            style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999 }}
          >
            {content}
          </div>,
          document.body,
        )}
    </>
  );
}

/**
 * A plain-language term with a dotted underline. Hover or focus shows what it means and its
 * technical name, so the UI can say "Came back" while engineers still find "regressed".
 */
export function Term({ k, children, className }: { k: TermKey; children?: ReactNode; className?: string }) {
  const e = term(k);
  return (
    <Tooltip
      content={
        <>
          <span className="block">{e.explain}</span>
          <span className="mt-1 block opacity-70">Technical name: {e.technical}</span>
        </>
      }
    >
      <span tabIndex={0} className={clsx("term", className)}>
        {children ?? e.label}
      </span>
    </Tooltip>
  );
}

/** A small "i" style hint: muted text that explains itself on hover. */
export function Hint({ text, children, className }: { text: ReactNode; children: ReactNode; className?: string }) {
  return (
    <Tooltip content={text}>
      <span tabIndex={0} className={clsx("term", className)}>
        {children}
      </span>
    </Tooltip>
  );
}
