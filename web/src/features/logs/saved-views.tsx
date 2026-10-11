import { useState } from "react";
import { Bookmark, BookmarkPlus, Check, Trash2 } from "lucide-react";
import clsx from "clsx";
import { useDeleteView, useSaveView, useSavedViews } from "@/api/hooks";
import type { SavedView } from "@/api/types";
import { Button, Checkbox, Dialog, Field, Input, MenuItem, Popover } from "@/components/ui";

export interface ViewState {
  q: string;
  from: string;
  to: string;
  group_by?: string;
  cols?: string;
  facets?: string;
  /** Explorer mode: list (default) | patterns | analytics. */
  view?: string;
  /** Analytics mode settings. */
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
  const items = views.data?.items ?? [];
  return (
    <>
      <Popover
        width={300}
        trigger={({ toggle, open }) => (
          <Button onClick={toggle} active={open} icon={<Bookmark size={13} />}>
            {activeName ? <span className="max-w-40 truncate">{activeName}</span> : "Views"}
          </Button>
        )}
      >
        {(close) => (
          <div>
            <div className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-ink-3">Saved views</div>
            {items.length === 0 && <div className="px-2 py-1.5 text-xs text-ink-3">No saved views yet.</div>}
            <div className="max-h-72 overflow-y-auto">
              {items.map((v) => {
                const st = parseView(v);
                return (
                  <div key={v.id} className={clsx("group flex items-center gap-1 rounded hover:bg-surface-2", activeName === v.name && "bg-accent-soft")}>
                    <MenuItem
                      icon={activeName === v.name ? <Check size={13} className="text-accent" /> : <span className="inline-block w-[13px]" />}
                      onClick={() => {
                        if (st) onApply(st, v.name);
                        close();
                      }}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{v.name}</span>
                        {st?.q && <span className="block truncate font-mono text-[10px] text-ink-3">{st.q}</span>}
                      </span>
                      {v.is_public && <span className="text-[10px] text-ink-3">shared</span>}
                    </MenuItem>
                    <button type="button" aria-label={`Delete view ${v.name}`} className="invisible mr-1 rounded p-1 text-ink-3 hover:text-critical group-hover:visible" onClick={() => del.mutate(v.id)}>
                      <Trash2 size={12} />
                    </button>
                  </div>
                );
              })}
            </div>
            <div className="my-1 h-px bg-border" />
            <MenuItem
              icon={<BookmarkPlus size={13} />}
              onClick={() => {
                close();
                setSaving(true);
              }}
            >
              Save current view…
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
    <Dialog open onClose={onClose} title="Save view">
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!name.trim()) return;
          save.mutate({ name: name.trim(), description, is_public: isPublic, query: JSON.stringify(current) }, { onSuccess: onClose });
        }}
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. API 5xx last hour" />
        </Field>
        <Field label="Description">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="rounded-md bg-surface-2 p-2 font-mono text-[11px] text-ink-2">
          <div>q: {current.q || <em className="font-sans text-ink-3">(all)</em>}</div>
          <div>
            range: {current.from} → {current.to || "now"}
          </div>
          {current.group_by && <div>group by: {current.group_by}</div>}
        </div>
        <Checkbox label="Share with everyone in this project" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} />
        {save.isError && <div className="text-xs text-critical">{(save.error as Error).message}</div>}
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
