import clsx from "clsx";
import { forwardRef, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { Search, X } from "lucide-react";

const field =
  "rounded-md border border-border-strong bg-surface px-2.5 text-[13px] text-ink placeholder:text-ink-3 focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/25 disabled:opacity-60 aria-[invalid=true]:border-critical";

/** Fields fill their container unless the caller sets an explicit width utility. */
const widthOf = (className?: string) => (/(^|\s)(w-|min-w-|max-w-|flex-1)/.test(className ?? "") ? "" : "w-full");

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(function Input({ className, ...rest }, ref) {
  return <input ref={ref} className={clsx(field, widthOf(className), "h-8", className)} {...rest} />;
});

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(function Textarea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={clsx(field, widthOf(className), "py-1.5 leading-relaxed", className)} {...rest} />;
});

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(function Select({ className, children, ...rest }, ref) {
  return (
    <select ref={ref} className={clsx(field, widthOf(className), "h-8 cursor-pointer appearance-none bg-no-repeat pr-7", className)} style={{ backgroundImage: chevron, backgroundPosition: "right 9px center", backgroundSize: "10px" }} {...rest}>
      {children}
    </select>
  );
});

const chevron =
  "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' fill='none' stroke='%23898781' stroke-width='1.5'/%3E%3C/svg%3E\")";

/** A labeled form field: label, the control, then a hint or an error underneath. */
export function Field({ label, hint, error, children, className }: { label: ReactNode; hint?: ReactNode; error?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <label className={clsx("block", className)}>
      <span className="mb-1.5 block text-[13px] font-medium text-ink">{label}</span>
      {children}
      {error ? <span className="mt-1 block text-xs text-critical-text">{error}</span> : hint ? <span className="mt-1 block text-xs text-ink-3">{hint}</span> : null}
    </label>
  );
}

export function Checkbox({ label, className, ...rest }: InputHTMLAttributes<HTMLInputElement> & { label?: ReactNode }) {
  const box = <input type="checkbox" className="h-4 w-4 flex-none cursor-pointer rounded accent-accent" {...rest} />;
  if (label === undefined) return box;
  return (
    <label className={clsx("inline-flex cursor-pointer items-center gap-2 text-[13px] text-ink", className)}>
      {box}
      {label}
    </label>
  );
}

/** Text input with a search icon inside and an optional clear button. */
export const SearchInput = forwardRef<HTMLInputElement, Omit<InputHTMLAttributes<HTMLInputElement>, "onChange" | "value"> & { value: string; onChange: (v: string) => void }>(function SearchInput(
  { className, value, onChange, ...rest },
  ref,
) {
  return (
    <div className={clsx("relative", widthOf(className), className)}>
      <Search size={14} className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-3" aria-hidden />
      <input ref={ref} type="search" value={value} onChange={(e) => onChange(e.target.value)} className={clsx(field, "h-8 w-full pl-8 pr-7 [&::-webkit-search-cancel-button]:hidden")} {...rest} />
      {value && (
        <button type="button" aria-label="Clear search" className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-ink-3 hover:bg-surface-2 hover:text-ink" onClick={() => onChange("")}>
          <X size={13} />
        </button>
      )}
    </div>
  );
});
