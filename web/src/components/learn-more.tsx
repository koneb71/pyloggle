import type { ReactNode } from "react";
import { ExternalLink } from "lucide-react";
import clsx from "clsx";
import { useMe } from "@/api/hooks";
import { docsUrl, type DocKey } from "@/lib/docs";

/** A link to a page of the documentation, opened in a new tab. Renders nothing when the server
 * hides the docs links (PYLOGGLE_DOCS_URL empty). */
export function LearnMore({ doc, anchor, children = "Learn more", className }: { doc: DocKey; anchor?: string; children?: ReactNode; className?: string }) {
  const me = useMe();
  const href = docsUrl(me.data?.features.docs_url, doc, anchor);
  if (!href) return null;
  return (
    <a href={href} target="_blank" rel="noreferrer" className={clsx("inline-flex items-center gap-1 text-accent hover:underline", className)}>
      {children}
      <ExternalLink size={12} aria-hidden className="flex-none" />
    </a>
  );
}
