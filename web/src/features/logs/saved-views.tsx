import { useState } from "react";
import { Bookmark, BookmarkPlus, Check, Trash2 } from "lucide-react";
import clsx from "clsx";
import { useDeleteView, useSaveView, useSavedViews } from "@/api/hooks";
import type { SavedView } from "@/api/types";
import { Button, Checkbox, Dialog, Field, Input, MenuItem, Popover } from "@/components/ui";
import { rangeLabel } from "@/lib/time";
import { parseSavedChart } from "@/features/analytics/chart-model";

export interface ViewState {
  q: string;
  from: string;
  to: string;
  group_by?: string;
  cols?: string;
  facets?: string;
  /** Explorer mode: list (default) | patterns. Searches saved with "analytics" open in Analytics → Charts. */
  view?: string;
  /** The Charts settings of searches saved before charts moved to Analytics. */
  group_by_a?: string;
  metric?: string;
  of?: string;
}

export function parseView(v: SavedView): ViewState | null {
  try {
    const o = JSON.parse(v.query) as Partial<ViewState>;
    return { q: o.q ?? "", from: o.from ?? "-1h", to: o.to ?? "", group_by: o.group_by, cols: o.cols, facets: o.facets, view: o.view, group_by_a: o.group_by_a, metric: o.metric, of: o.of };
  } catch {
    return { q: v.query, from: "-1h", to: "" };
  }
}

export function SavedViewsMenu({ current, onApply, activeName }: { current: ViewState; onApply: (v: ViewState, name: string) => void; activeName?: string }) {
  const views = useSavedViews();
  const del = useDeleteView();
  const [saving, setSaving] = useState(false);
  // Saved charts live in Analytics → Charts.
  const items = (views.data?.items ?? []).filter((v) => parseSavedChart(v.query) === null);
  return (
    <>
      <Popover
        width={300}
        trigger={({ toggle, open }) => (
          <Button onClick={toggle} active={open} icon={<Bookmark size={14} />} title="Saved searches">
            {activeName ? <span className="max-w-40 truncate">{activeName}</span> : "Saved"}
          </Button>
        )}
      >
        {(close) => (
          <div>
            <div className="px-2 pb-1 pt-1.5 text-xs font-medium text-ink-3">Saved searches</div>
            {views.isError && <div className="px-2 py-1.5 text-[13px] text-critical-text">Couldn't load saved searches.</div>}
            {views.data && items.length === 0 && <div className="px-2 py-1.5 text-[13px] text-ink-3">Nothing saved yet. Save a search to come back to it, or to share it with your team.</div>}
            <div className="max-h-72 overflow-y-auto">
              {items.map((v) => {
                const st = parseView(v);
                return (
                  <div key={v.id} className={clsx("group flex items-center gap-1 rounded hover:bg-surface-2", activeName === v.name && "bg-accent-soft")}>
                    <MenuItem
                      icon={activeName === v.name ? <Check size={14} className="text-accent" /> : <span className="inline-block w-[14px]" />}
                      onClick={() => {
                        if (st) onApply(st, v.name);
                        close();
                      }}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{v.name}</span>
                        {st?.q && <span className="block truncate font-mono text-[11px] text-ink-3">{st.q}</span>}
                      </span>
                      {v.is_public && <span className="text-xs text-ink-3">Shared</span>}
                    </MenuItem>
                    <button type="button" aria-label={`Delete ${v.name}`} title="Delete" className="invisible pointer-coarse:visible mr-1 rounded p-1 text-ink-3 hover:text-critical-text group-hover:visible" onClick={() => del.mutate(v.id)}>
                      <Trash2 size={13} />
                    </button>
                  </div>
                );
              })}
            </div>
            <div className="my-1 h-px bg-border" />
            <MenuItem
              icon={<BookmarkPlus size={14} />}
              onClick={() => {
                close();
                setSaving(true);
              }}
            >
              Save this search…
            </MenuItem>
          </div>
        )}
      </Popover>
      {saving && <SaveViewDialog current={current} onClose={() => setSaving(false)} />}
    </>
  );
}

function SaveViewDialog({ current, onClose }: { current: ViewState; onClose: () => void }) {
  const save = useSaveView();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isPublic, setPublic] = useState(true);
  return (
    <Dialog open onClose={onClose} title="Save this search">
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!name.trim()) return;
          save.mutate({ name: name.trim(), description, is_public: isPublic, query: JSON.stringify(current) }, { onSuccess: onClose });
        }}
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Failed API requests" />
        </Field>
        <Field label="Description" hint="Optional: what this search is for.">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 rounded-md bg-surface-2 px-3 py-2 text-[13px]">
          <dt className="text-ink-3">Search</dt>
          <dd className="min-w-0 break-words font-mono text-[12px] text-ink">{current.q || <span className="font-sans text-ink-3">Everything</span>}</dd>
          <dt className="text-ink-3">Time</dt>
          <dd className="text-ink">{rangeLabel({ from: current.from, to: current.to })}</dd>
          {current.view && (
            <>
              <dt className="text-ink-3">Showing</dt>
              <dd className="text-ink">{current.view === "patterns" ? "Similar messages" : "Messages"}</dd>
            </>
          )}
        </dl>
        <Checkbox label="Share with everyone in this project" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} />
        {save.isError && <div role="alert" className="text-[13px] text-critical-text">{(save.error as Error).message}</div>}
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" loading={save.isPending}>
            Save
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
