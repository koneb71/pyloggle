import type { CSSProperties, KeyboardEvent, MouseEvent, ReactNode } from "react";
import { useHref, useNavigate } from "react-router";
import clsx from "clsx";
import { ErrorState } from "./query-state";
import { EmptyState, Skeleton } from "./misc";

export interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T, index: number) => ReactNode;
  /** A CSS grid track; defaults to a flexible column. */
  width?: string;
  align?: "left" | "right" | "center";
  className?: string;
  /**
   * Leave the column out on screens narrower than this Tailwind breakpoint (sm 640px, md 768,
   * lg 1024, xl 1280), so the columns that matter keep their room on a phone.
   */
  hideBelow?: Breakpoint;
  /** The reverse: only on screens narrower than this, e.g. a compact version of another column. */
  showBelow?: Breakpoint;
}

export type Breakpoint = "sm" | "md" | "lg" | "xl";
const BREAKPOINTS: Breakpoint[] = ["sm", "md", "lg", "xl"];
// Literal class names, so Tailwind sees each one.
const HIDE_BELOW: Record<Breakpoint, string> = { sm: "max-sm:hidden", md: "max-md:hidden", lg: "max-lg:hidden", xl: "max-xl:hidden" };
const SHOW_BELOW: Record<Breakpoint, string> = { sm: "sm:hidden", md: "md:hidden", lg: "lg:hidden", xl: "xl:hidden" };
const visibility = (c: { hideBelow?: Breakpoint; showBelow?: Breakpoint }) => clsx(c.hideBelow && HIDE_BELOW[c.hideBelow], c.showBelow && SHOW_BELOW[c.showBelow]);
const TRACKS =
  "grid-cols-(--cols-base) sm:grid-cols-(--cols-sm) md:grid-cols-(--cols-md) lg:grid-cols-(--cols-lg) xl:grid-cols-(--cols-xl)";

/** Column gap shared by the header and every row, so they can never drift apart. */
export const TABLE_GAP = "gap-x-4";

export function gridTemplate(columns: { width?: string }[]): string {
  return columns.map((c) => c.width ?? "minmax(0,1fr)").join(" ");
}

/**
 * The grid tracks shared by the header and the rows. Without hidden columns it is one inline
 * template; otherwise one template per breakpoint, picked by CSS, so nothing reflows after load.
 */
function gridLayout(columns: { width?: string; hideBelow?: Breakpoint; showBelow?: Breakpoint }[]): { style: CSSProperties; className?: string } {
  if (!columns.some((c) => c.hideBelow || c.showBelow)) return { style: { gridTemplateColumns: gridTemplate(columns) } };
  // Rank 0 is below sm; rank n is from the n-th breakpoint up.
  const shownAt = (rank: number) =>
    gridTemplate(columns.filter((c) => (!c.hideBelow || BREAKPOINTS.indexOf(c.hideBelow) < rank) && (!c.showBelow || rank <= BREAKPOINTS.indexOf(c.showBelow))));
  const style: Record<string, string> = { "--cols-base": shownAt(0) };
  BREAKPOINTS.forEach((bp, i) => (style[`--cols-${bp}`] = shownAt(i + 1)));
  return { style: style as CSSProperties, className: TRACKS };
}

const alignClass = (a?: "left" | "right" | "center") => (a === "right" ? "justify-end text-right" : a === "center" ? "justify-center text-center" : "");

/**
 * A table built on one shared grid template: the header and the rows use the same tracks and
 * the same gap, numbers align right, and the body shows exactly one of error, loading, empty
 * or rows. Rows with `rowHref` open on click (Ctrl/⌘-click opens a new tab) and on Enter.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  rowHref,
  onRowClick,
  selected,
  loading,
  error,
  onRetry,
  empty,
  skeletonRows = 6,
  footer,
  className,
  label,
  stickyHeader = true,
  rowClassName,
  minWidth,
}: {
  columns: Column<T>[];
  rows: T[] | undefined;
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string;
  onRowClick?: (row: T) => void;
  selected?: (row: T) => boolean;
  loading?: boolean;
  error?: unknown;
  onRetry?: () => void;
  empty?: ReactNode;
  skeletonRows?: number;
  footer?: ReactNode;
  className?: string;
  label?: string;
  stickyHeader?: boolean;
  rowClassName?: (row: T) => string | undefined;
  /** Below this width (px) the table scrolls sideways instead of squeezing. Turns off the sticky header. */
  minWidth?: number;
}) {
  const layout = gridLayout(columns);
  const hasRows = !!rows && rows.length > 0;
  let body: ReactNode;
  if (error && !hasRows) body = <ErrorState error={error} retry={onRetry} />;
  else if (loading && !hasRows) body = <Skeleton rows={skeletonRows} className="px-4 py-4" />;
  else if (!hasRows) body = typeof empty === "string" || empty === undefined ? <EmptyState title={empty ?? "Nothing to show"} /> : empty;
  else
    body = rows!.map((row, i) => (
      <Row key={rowKey(row)} row={row} index={i} columns={columns} layout={layout} href={rowHref?.(row)} onClick={onRowClick} selected={selected?.(row)} className={rowClassName?.(row)} />
    ));
  const table = (
    <div role="table" aria-label={label} className={clsx("min-w-0", !minWidth && className)} style={minWidth ? { minWidth } : undefined}>
      <div role="rowgroup" className={clsx(stickyHeader && !minWidth && "sticky top-0 z-10", "bg-surface")}>
        <div role="row" className={clsx("grid items-center border-b border-border px-4 py-2 text-xs font-medium text-ink-3", TABLE_GAP, layout.className)} style={layout.style}>
          {columns.map((c) => (
            <div key={c.id} role="columnheader" className={clsx("flex min-w-0 items-center truncate", alignClass(c.align), visibility(c))}>
              {c.header}
            </div>
          ))}
        </div>
      </div>
      <div role="rowgroup">{body}</div>
      {footer}
    </div>
  );
  return minWidth ? <div className={clsx("min-w-0 overflow-x-auto", className)}>{table}</div> : table;
}

const INTERACTIVE = "a,button,input,select,textarea,label,[role=menuitem],[data-row-ignore]";

function Row<T>({ row, index, columns, layout, href, onClick, selected, className }: { row: T; index: number; columns: Column<T>[]; layout: { style: CSSProperties; className?: string }; href?: string; onClick?: (row: T) => void; selected?: boolean; className?: string }) {
  const nav = useNavigate();
  const fullHref = useHref(href ?? ".");
  const clickable = !!href || !!onClick;
  const activate = (e: MouseEvent | KeyboardEvent) => {
    if ((e.target as HTMLElement).closest(INTERACTIVE) && e.target !== e.currentTarget) return;
    if (href) {
      if ("metaKey" in e && (e.metaKey || e.ctrlKey)) window.open(fullHref, "_blank", "noopener");
      else nav(href);
    } else onClick?.(row);
  };
  return (
    <div
      role="row"
      tabIndex={clickable ? 0 : undefined}
      aria-selected={selected || undefined}
      onClick={clickable ? activate : undefined}
      onKeyDown={clickable ? (e) => e.key === "Enter" && activate(e) : undefined}
      className={clsx(
        "grid items-center border-b border-border px-4 py-2.5 text-[13px] last:border-b-0",
        TABLE_GAP,
        clickable && "cursor-pointer hover:bg-surface-2 focus-visible:bg-surface-2 focus-visible:outline-none",
        selected && "bg-accent-soft hover:bg-accent-soft",
        layout.className,
        className,
      )}
      style={layout.style}
    >
      {columns.map((c) => (
        <div key={c.id} role="cell" className={clsx("flex min-w-0 items-center", alignClass(c.align), c.align === "right" && "tabular", visibility(c), c.className)}>
          {c.cell(row, index)}
        </div>
      ))}
    </div>
  );
}
