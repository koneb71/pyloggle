import type { ReactNode } from "react";
import clsx from "clsx";
import { AlertCircle, RotateCw } from "lucide-react";
import { ApiRequestError } from "@/api/client";
import { Button } from "./button";
import { Skeleton } from "./misc";

/** A failure in words a person can act on, with the technical message kept underneath. */
export function friendlyError(error: unknown): { title: string; detail: string } {
  const detail = error instanceof Error ? error.message : String(error ?? "");
  if (error instanceof ApiRequestError) {
    if (error.status === 400) return { title: "That search or setting isn't valid", detail };
    if (error.status === 403) return { title: "You don't have access to this", detail };
    if (error.status === 404) return { title: "This doesn't exist anymore", detail };
    if (error.status === 429) return { title: "Too many requests right now", detail: "Wait a moment and try again." };
    if (error.status >= 500) return { title: "The server had a problem loading this", detail };
  }
  if (error instanceof TypeError) return { title: "Can't reach the server", detail: "Check your connection, then try again." };
  return { title: "Couldn't load this", detail };
}

export function ErrorState({ error, retry: retryProp, title, compact, className }: { error: unknown; retry?: () => void; title?: string; compact?: boolean; className?: string }) {
  const f = friendlyError(error);
  // Retrying can't fix a missing page or missing access.
  const retry = error instanceof ApiRequestError && (error.status === 404 || error.status === 403) ? undefined : retryProp;
  if (compact) {
    return (
      <div role="alert" className={clsx("flex items-center gap-2 px-4 py-3 text-[13px]", className)}>
        <AlertCircle size={15} className="flex-none text-critical-text" />
        <span className="min-w-0 flex-1 truncate text-ink" title={f.detail}>
          {title ?? f.title}
        </span>
        {retry && (
          <Button size="xs" variant="ghost" icon={<RotateCw size={12} />} onClick={retry}>
            Try again
          </Button>
        )}
      </div>
    );
  }
  return (
    <div role="alert" className={clsx("flex flex-col items-center justify-center gap-1 px-4 py-10 text-center", className)}>
      <div className="mb-2 flex h-10 w-10 items-center justify-center rounded-full bg-critical/10 text-critical-text">
        <AlertCircle size={18} />
      </div>
      <div className="text-sm font-medium text-ink">{title ?? f.title}</div>
      {f.detail && <div className="max-w-md break-words text-xs text-ink-3">{f.detail}</div>}
      {retry && (
        <Button className="mt-3" icon={<RotateCw size={13} />} onClick={retry}>
          Try again
        </Button>
      )}
    </div>
  );
}

/** The parts of a TanStack query QueryState reads. */
export interface QueryLike<T> {
  data: T | undefined;
  isPending: boolean;
  isError: boolean;
  error: unknown;
  fetchStatus: "fetching" | "paused" | "idle";
  refetch: () => unknown;
}

/**
 * Renders exactly one of: error (with Try again), loading, empty, or the content, in that
 * priority. A query that is switched off (no id yet) counts as empty, never as loading forever.
 * When a background refresh fails the content stays, with a one-line notice above it.
 */
export function QueryState<T>({ query, isEmpty, empty, skeleton, children, errorTitle, compact }: { query: QueryLike<T>; isEmpty?: (data: T) => boolean; empty?: ReactNode; skeleton?: ReactNode; children: (data: T) => ReactNode; errorTitle?: string; compact?: boolean }) {
  const { data } = query;
  if (data === undefined) {
    if (query.isError) return <ErrorState error={query.error} retry={() => query.refetch()} title={errorTitle} compact={compact} />;
    if (query.isPending && query.fetchStatus !== "idle") return <>{skeleton ?? <Skeleton rows={4} className="p-4" />}</>;
    return <>{empty ?? null}</>;
  }
  const content = isEmpty?.(data) ? (empty ?? null) : children(data);
  if (query.isError) {
    return (
      <>
        <ErrorState compact error={query.error} retry={() => query.refetch()} title="Couldn't refresh. Showing earlier data." className="border-b border-border" />
        {content}
      </>
    );
  }
  return <>{content}</>;
}
