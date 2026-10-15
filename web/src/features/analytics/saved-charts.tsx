import { useMemo, useState } from "react";
import { BookmarkPlus, Check, LineChart, Pin, Trash2 } from "lucide-react";
import clsx from "clsx";
import { useDeleteView, usePinView, useSaveView, useSavedViews } from "@/api/hooks";
import type { SavedView } from "@/api/types";
import { Button, Checkbox, Dialog, Field, Input, MenuItem, Popover } from "@/components/ui";
import { rangeLabel } from "@/lib/time";
import { metricLabel, parseSavedChart, savedChart, SOURCE_WORDS, type ChartState, type SavedChart } from "./chart-model";

/** The saved views that are charts, each with its parsed chart. */
export function useSavedCharts(pinned = false) {
  const views = useSavedViews(pinned ? { pinned: true } : {});
  const charts = useMemo(
    () =>
      (views.data?.items ?? []).flatMap((v) => {
        const chart = parseSavedChart(v.query);
        return chart ? [{ view: v, chart }] : [];
      }),
    [views.data],
  );
  return { ...views, charts };
}

/** "Saved charts ▾": open, pin to Dashboards or delete a saved chart, or save this one. */
export function SavedChartsMenu({ current, range, activeId, onOpen }: { current: ChartState; range: { from: string; to: string }; activeId?: string; onOpen: (chart: SavedChart, view: SavedView) => void }) {
  const { charts, isError, data } = useSavedCharts();
  const del = useDeleteView();
  const pin = usePinView();
  const [saving, setSaving] = useState(false);
  const active = charts.find((c) => c.view.id === activeId);
  return (
    <>
      <Popover
        width={320}
        align="end"
        trigger={({ toggle, open }) => (
          <Button onClick={toggle} active={open} icon={<LineChart size={14} />} title="Saved charts">
            {active ? <span className="max-w-40 truncate">{active.view.name}</span> : "Saved charts"}
          </Button>
        )}
      >
        {(close) => (
          <div>
            <div className="px-2 pb-1 pt-1.5 text-xs font-medium text-ink-3">Saved charts</div>
            {isError && <div className="px-2 py-1.5 text-[13px] text-critical-text">Couldn't load saved charts.</div>}
            {data && charts.length === 0 && <div className="px-2 py-1.5 text-[13px] text-ink-3">Nothing saved yet. Save a chart to come back to it, share it with your team or pin it to Dashboards.</div>}
            <div className="max-h-72 overflow-y-auto">
              {charts.map(({ view, chart }) => (
                <div key={view.id} className={clsx("group flex items-center gap-1 rounded hover:bg-surface-2", activeId === view.id && "bg-accent-soft")}>
                  <MenuItem
                    icon={activeId === view.id ? <Check size={14} className="text-accent" /> : <span className="inline-block w-[14px]" />}
                    onClick={() => {
                      onOpen(chart, view);
                      close();
                    }}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{view.name}</span>
                      <span className="block truncate text-[11px] text-ink-3">
                        {metricLabel(chart.source, chart.metric, chart.of)}
                        {chart.source !== "requests" || chart.metric !== "count" ? ` · ${SOURCE_WORDS[chart.source].many}` : ""}
                        {view.is_public ? " · shared" : ""}
                      </span>
                    </span>
                  </MenuItem>
                  <button
                    type="button"
                    aria-label={view.is_pinned ? `Unpin ${view.name} from Dashboards` : `Pin ${view.name} to Dashboards`}
                    aria-pressed={view.is_pinned}
                    title={view.is_pinned ? "Pinned to Dashboards: click to unpin" : "Pin to Dashboards"}
                    className={clsx("rounded p-1 hover:text-ink", view.is_pinned ? "text-accent" : "invisible text-ink-3 pointer-coarse:visible group-hover:visible")}
                    onClick={() => pin.mutate({ id: view.id, pinned: !view.is_pinned })}
                  >
                    <Pin size={13} fill={view.is_pinned ? "currentColor" : "none"} />
                  </button>
                  <button type="button" aria-label={`Delete ${view.name}`} title="Delete" className="invisible pointer-coarse:visible mr-1 rounded p-1 text-ink-3 hover:text-critical-text group-hover:visible" onClick={() => del.mutate(view.id)}>
                    <Trash2 size={13} />
                  </button>
                </div>
              ))}
            </div>
            {pin.isError && <div className="px-2 py-1 text-[12px] text-critical-text">{(pin.error as Error).message}</div>}
            <div className="my-1 h-px bg-border" />
            <MenuItem
              icon={<BookmarkPlus size={14} />}
              onClick={() => {
                close();
                setSaving(true);
              }}
            >
              Save this chart…
            </MenuItem>
          </div>
        )}
      </Popover>
      {saving && <SaveChartDialog current={current} range={range} onClose={() => setSaving(false)} />}
    </>
  );
}

export function SaveChartDialog({ current, range, onClose }: { current: ChartState; range: { from: string; to: string }; onClose: () => void }) {
  const save = useSaveView();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isPublic, setPublic] = useState(true);
  const [pinned, setPinned] = useState(true);
  const chart = savedChart(current, range);
  return (
    <Dialog open onClose={onClose} title="Save this chart">
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (!name.trim()) return;
          save.mutate({ name: name.trim(), description, is_public: isPublic, is_pinned: pinned, query: JSON.stringify(chart) }, { onSuccess: onClose });
        }}
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Checkout response time" />
        </Field>
        <Field label="Description" hint="Optional: what this chart is for.">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 rounded-md bg-surface-2 px-3 py-2 text-[13px]">
          <dt className="text-ink-3">Chart</dt>
          <dd className="text-ink">
            {metricLabel(chart.source, chart.metric, chart.of)} of {SOURCE_WORDS[chart.source].many}
          </dd>
          <dt className="text-ink-3">Search</dt>
          <dd className="min-w-0 break-words font-mono text-[12px] text-ink">{chart.q || <span className="font-sans text-ink-3">Everything</span>}</dd>
          <dt className="text-ink-3">Time</dt>
          <dd className="text-ink">{rangeLabel({ from: chart.from, to: chart.to ?? "" })}</dd>
        </dl>
        <Checkbox label="Share with everyone in this project" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} />
        <Checkbox label={isPublic ? "Pin to Dashboards, for everyone in this project" : "Pin to my Dashboards"} checked={pinned} onChange={(e) => setPinned(e.target.checked)} />
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
