import clsx from "clsx";
import { forwardRef, type InputHTMLAttributes, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";

const field =
  "rounded-md border border-border bg-surface px-2.5 text-[13px] text-ink placeholder:text-ink-3 focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/30 disabled:opacity-60";

/** Fields fill their container unless the caller sets an explicit width utility. */
const widthOf = (className?: string) => (/(^|\s)(w-|min-w-|max-w-)/.test(className ?? "") ? "" : "w-full");

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input({ className, ...rest }, ref) {
  return <input ref={ref} className={clsx(field, widthOf(className), "h-8", className)} {...rest} />;
});

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={clsx(field, widthOf(className), "py-1.5", className)} {...rest} />;
});

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select({ className, children, ...rest }, ref) {
  return (
    <select ref={ref} className={clsx(field, widthOf(className), "h-8 appearance-none bg-no-repeat pr-7", className)} style={{ backgroundImage: chevron, backgroundPosition: "right 8px center", backgroundSize: "10px" }} {...rest}>
      {children}
    </select>
  );
});

const chevron =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' fill='none' stroke='%23898781' stroke-width='1.5'/%3E%3C/svg%3E\")";

export function Field({ label, hint, children, className }: { label: string; hint?: string; children: React.ReactNode; className?: string }) {
  return (
    <label className={clsx("block", className)}>
      <span className="mb-1 block text-xs font-medium text-ink-2">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-[11px] text-ink-3">{hint}</span>}
    </label>
  );
}

export function Checkbox({ label, className, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label: React.ReactNode }) {
  return (
    <label className={clsx("inline-flex cursor-pointer items-center gap-2 text-[13px] text-ink", className)}>
      <input type="checkbox" className="h-3.5 w-3.5 accent-accent" {...rest} />
      {label}
    </label>
  );
}
