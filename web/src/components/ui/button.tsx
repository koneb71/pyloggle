import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Link, type LinkProps } from "react-router";
import clsx from "clsx";

type Variant = "primary" | "default" | "ghost" | "danger" | "subtle";
type Size = "xs" | "sm" | "md";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  icon?: ReactNode;
  loading?: boolean;
  active?: boolean;
  /** An icon-only button: as wide as it is tall. */
  square?: boolean;
}

const base =
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md border font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none select-none";
const variants: Record<Variant, string> = {
  primary: "bg-accent text-accent-ink border-transparent hover:bg-accent-hover shadow-[0_1px_0_rgba(0,0,0,0.06)]",
  default: "bg-surface text-ink border-border-strong hover:bg-surface-2 shadow-[0_1px_0_rgba(0,0,0,0.03)]",
  subtle: "bg-surface-2 text-ink border-transparent hover:bg-surface-3",
  ghost: "bg-transparent text-ink-2 border-transparent hover:bg-surface-2 hover:text-ink",
  danger: "bg-surface text-critical-text border-border-strong hover:bg-critical/10",
};
const sizes: Record<Size, string> = {
  xs: "h-6 text-xs",
  sm: "h-8 text-[13px]",
  md: "h-9 text-sm",
};
// Padding and width are separate from height so a square button never fights a padding class.
const pads: Record<Size, string> = { xs: "px-2", sm: "px-3", md: "px-3.5" };
const squares: Record<Size, string> = { xs: "w-6", sm: "w-8", md: "w-9" };

export function buttonClass({ variant = "default", size = "sm", active, square, className }: { variant?: Variant; size?: Size; active?: boolean; square?: boolean; className?: string } = {}): string {
  return clsx(base, variants[variant], sizes[size], square ? squares[size] : pads[size], active && "border-accent ring-2 ring-accent/25", className);
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "default", size = "sm", icon, loading, active, square, className, children, type = "button", disabled, ...rest },
  ref,
) {
  return (
    <button ref={ref} type={type} className={buttonClass({ variant, size, active, square, className })} aria-busy={loading || undefined} disabled={disabled || loading} {...rest}>
      {loading ? <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" aria-hidden /> : icon}
      {children}
    </button>
  );
});

export function IconButton({ label, variant = "ghost", ...rest }: ButtonProps & { label: string }) {
  return <Button aria-label={label} title={label} variant={variant} square {...rest} />;
}

/** A router link that looks like a button. */
export function LinkButton({ variant = "default", size = "sm", icon, className, children, ...rest }: LinkProps & { variant?: Variant; size?: Size; icon?: ReactNode }) {
  return (
    <Link className={buttonClass({ variant, size, className })} {...rest}>
      {icon}
      {children}
    </Link>
  );
}
