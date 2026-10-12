import type { ReactNode } from "react";
import { Link } from "react-router";
import clsx from "clsx";
import { ChevronLeft } from "lucide-react";

/**
 * Every page starts the same way: a title, one line saying what the page is for, and the
 * page's actions on the right.
 *
 * - "reading" pages (Overview, Issues, Dashboards, settings) scroll as a whole and center their
 *   content at a comfortable width.
 * - "full" pages (Logs, Requests) are workspaces: the header stays put and the content fills
 *   the rest of the window and scrolls inside itself.
 */
export function Page({
  title,
  description,
  actions,
  back,
  width = "reading",
  toolbar,
  children,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  back?: { to: string; label: string };
  width?: "reading" | "full";
  toolbar?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  const header = (
    <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="min-w-0 flex-1">
        {back && (
          <Link to={back.to} className="mb-1 inline-flex items-center gap-0.5 text-[13px] text-ink-3 hover:text-ink">
            <ChevronLeft size={14} />
            {back.label}
          </Link>
        )}
        <h1 className={clsx("font-semibold tracking-tight text-ink", width === "full" ? "text-lg leading-7" : "text-xl leading-7")}>{title}</h1>
        {description && <div className="mt-0.5 text-[13px] text-ink-3">{description}</div>}
      </div>
      {actions && <div className="flex flex-none flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );

  if (width === "full") {
    return (
      <div className={clsx("flex min-h-0 flex-1 flex-col", className)}>
        <header className="flex-none border-b border-border bg-surface px-5 py-3">{header}</header>
        {toolbar && <div className="flex-none border-b border-border bg-surface px-5 py-2.5">{toolbar}</div>}
        <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      </div>
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className={clsx("mx-auto w-full max-w-[1280px] px-6 pb-12 pt-6", className)}>
        {header}
        {toolbar && <div className="mt-4">{toolbar}</div>}
        <div className="mt-5 flex flex-col gap-5">{children}</div>
      </div>
    </div>
  );
}
