import type { KeyboardEvent, MouseEvent, ReactNode } from "react";
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
}

/** Column gap shared by the header and every row, so they can never drift apart. */
export const TABLE_GAP = "gap-x-4";

export function gridTemplate(columns: { width?: string }[]): string {
  return columns.map((c) => c.width ?? "minmax(0,1fr)").join(" ");
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
}) {
  const template = gridTemplate(columns);
  const hasRows = !!rows && rows.length > 0;
  let body: ReactNode;
  if (error && !hasRows) body = <ErrorState error={error} retry={onRetry} />;
  else if (loading && !hasRows) body = <Skeleton rows={skeletonRows} className="px-4 py-4" />;
  else if (!hasRows) body = typeof empty === "string" || empty === undefined ? <EmptyState title={empty ?? "Nothing to show"} /> : empty;
  else
    body = rows!.map((row, i) => (
      <Row key={rowKey(row)} row={row} index={i} columns={columns} template={template} href={rowHref?.(row)} onClick={onRowClick} selected={selected?.(row)} className={rowClassName?.(row)} />
    ));
  return (
    <div role="table" aria-label={label} className={clsx("min-w-0", className)}>
      <div role="rowgroup" className={clsx(stickyHeader && "sticky top-0 z-10", "bg-surface")}>
        <div role="row" className={clsx("grid items-center border-b border-border px-4 py-2 text-xs font-medium text-ink-3", TABLE_GAP)} style={{ gridTemplateColumns: template }}>
          {columns.map((c) => (
            <div key={c.id} role="columnheader" className={clsx("flex min-w-0 items-center truncate", alignClass(c.align))}>
              {c.header}
            </div>
          ))}
        </div>
      </div>
      <div role="rowgroup">{body}</div>
      {footer}
    </div>
  );
}

const INTERACTIVE = "a,button,input,select,textarea,label,[role=menuitem],[data-row-ignore]";

function Row<T>({ row, index, columns, template, href, onClick, selected, className }: { row: T; index: number; columns: Column<T>[]; template: string; href?: string; onClick?: (row: T) => void; selected?: boolean; className?: string }) {
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
        className,
      )}
      style={{ gridTemplateColumns: template }}
    >
      {columns.map((c) => (
        <div key={c.id} role="cell" className={clsx("flex min-w-0 items-center", alignClass(c.align), c.align === "right" && "tabular", c.className)}>
          {c.cell(row, index)}
        </div>
      ))}
    </div>
  );
}
