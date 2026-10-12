import { useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";

type Kind = "error" | "success" | "info";
interface ToastItem {
  id: number;
  kind: Kind;
  title: string;
  detail?: string;
}

let items: ToastItem[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

function push(kind: Kind, title: string, detail?: string) {
  const id = nextId++;
  // Same message twice in a row (a retried action) shows once.
  if (items.some((t) => t.title === title && t.detail === detail)) return;
  items = [...items.slice(-3), { id, kind, title, detail }];
  emit();
  setTimeout(() => dismiss(id), kind === "error" ? 8000 : 4000);
}

function dismiss(id: number) {
  items = items.filter((t) => t.id !== id);
  emit();
}

/** Callable from anywhere, including the query client: toast.error("Couldn't save", err.message). */
export const toast = {
  error: (title: string, detail?: string) => push("error", title, detail),
  success: (title: string, detail?: string) => push("success", title, detail),
  info: (title: string, detail?: string) => push("info", title, detail),
};

const ICON: Record<Kind, React.ReactNode> = {
  error: <AlertCircle size={16} className="text-critical-text" />,
  success: <CheckCircle2 size={16} className="text-good-text" />,
  info: <Info size={16} className="text-accent" />,
};

export function Toaster() {
  const list = useSyncExternalStore(
    (l) => (listeners.add(l), () => listeners.delete(l)),
    () => items,
    () => items,
  );
  return createPortal(
    <div className="pointer-events-none fixed bottom-4 right-4 z-[90] flex w-[22rem] max-w-[calc(100vw-2rem)] flex-col gap-2" aria-live="polite">
      {list.map((t) => (
        <div key={t.id} role={t.kind === "error" ? "alert" : "status"} className={clsx("toast-in pointer-events-auto flex items-start gap-2.5 rounded-lg border border-border bg-surface px-3.5 py-3 shadow-[var(--shadow)]")}>
          <span className="mt-px flex-none">{ICON[t.kind]}</span>
          <div className="min-w-0 flex-1">
            <div className="text-[13px] font-medium text-ink">{t.title}</div>
            {t.detail && <div className="mt-0.5 break-words text-xs text-ink-3">{t.detail}</div>}
          </div>
          <button type="button" aria-label="Dismiss" className="-mr-1 -mt-0.5 rounded p-1 text-ink-3 hover:bg-surface-2 hover:text-ink" onClick={() => dismiss(t.id)}>
            <X size={13} />
          </button>
        </div>
      ))}
    </div>,
    document.body,
  );
}
