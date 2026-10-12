import { useMemo, useState } from "react";
import clsx from "clsx";
import { ChevronDown, ChevronRight, Plus, X } from "lucide-react";
import { useLogFacets } from "@/api/hooks";
import { ApiRequestError } from "@/api/client";
import type { FacetCount, LogFacets } from "@/api/types";
import { ErrorState, Input, Skeleton, UpdatingDot } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { addTerm, hasTerm, removeTerm, term } from "@/lib/query";
import { levelLabel, levelVar } from "@/lib/levels";
import { fieldLabel, isCodeLike } from "@/lib/fields";
import { FACET_GROUPS, FACET_TO_FIELD, OPEN_FACETS } from "./log-fields";

export interface FacetsPanelProps {
  q: string;
  range: { from: string; to: string };
  onChange: (q: string) => void;
  extraFacets: string[]; // attrs.x / tags.y pinned by the user
  onExtraFacets: (f: string[]) => void;
}

/** The Filters sidebar: the most common values of each field, one click to narrow the list. */
export function FacetsPanel({ q, range, onChange, extraFacets, onExtraFacets }: FacetsPanelProps) {
  const facets = useLogFacets({ q, from: range.from, to: range.to, attr_keys: 40 });
  const data = facets.data;

  const toggle = (field: string, value: string, negate = false) => {
    const t = term(field, value, negate);
    if (!negate && hasTerm(q, field, value)) onChange(removeTerm(q, t));
    else onChange(addTerm(q, field, value, negate));
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-10 flex-none items-center justify-between border-b border-border px-3">
        <span className="text-[13px] font-semibold text-ink">Filters</span>
        <UpdatingDot active={facets.isFetching && !!data} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {!data && facets.isError ? (
          facets.error instanceof ApiRequestError && facets.error.status === 400 ? (
            <p className="px-3 py-3 text-[13px] text-ink-3">Fix the search to see filters.</p>
          ) : (
            <ErrorState compact error={facets.error} retry={() => facets.refetch()} title="Couldn't load filters" />
          )
        ) : !data ? (
          <Skeleton rows={10} className="p-3" />
        ) : (
          <>
            {FACET_GROUPS.map((g) => (
              <Group key={g.title} title={g.title}>
                {g.facets.map((key) => (
                  <Facet key={key} field={FACET_TO_FIELD[key]} items={data.fields?.[key] ?? []} q={q} onToggle={toggle} defaultOpen={OPEN_FACETS.has(key)} />
                ))}
              </Group>
            ))}
            <Group title="Extra details" hint="Fields your app sent with messages">
              {extraFacets
                .filter((k) => k.startsWith("attrs."))
                .map((k) => (
                  <DetailFacet key={k} fullKey={k} q={q} range={range} onToggle={toggle} onRemove={() => onExtraFacets(extraFacets.filter((x) => x !== k))} />
                ))}
              <KeyPicker items={data.attr_keys ?? []} prefix="attrs." pinned={extraFacets} onPin={(k) => onExtraFacets([...extraFacets, k])} />
            </Group>
            {((data.tag_keys?.length ?? 0) > 0 || extraFacets.some((k) => k.startsWith("tags."))) && (
              <Group title="Labels" hint="Tags your code set">
                {extraFacets
                  .filter((k) => k.startsWith("tags."))
                  .map((k) => (
                    <DetailFacet key={k} fullKey={k} q={q} range={range} onToggle={toggle} onRemove={() => onExtraFacets(extraFacets.filter((x) => x !== k))} />
                  ))}
                <KeyPicker items={data.tag_keys ?? []} prefix="tags." pinned={extraFacets} onPin={(k) => onExtraFacets([...extraFacets, k])} />
              </Group>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function Group({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="border-b border-border pb-1.5">
      <div className="px-3 pb-1 pt-3 text-xs font-medium text-ink-3" title={hint}>
        {title}
      </div>
      {children}
    </div>
  );
}

function Facet({ field, items, q, onToggle, defaultOpen = false, onRemove }: { field: string; items: FacetCount[]; q: string; onToggle: (field: string, value: string, negate?: boolean) => void; defaultOpen?: boolean; onRemove?: () => void }) {
  const active = items.some((it) => hasTerm(q, field, it.value));
  const [open, setOpen] = useState(defaultOpen || active);
  const [showAll, setShowAll] = useState(false);
  const total = useMemo(() => items.reduce((n, i) => n + i.count, 0), [items]);
  const shown = showAll ? items : items.slice(0, 6);
  const isLevel = field === "level";
  const label = fieldLabel(field);
  return (
    <div className="px-1.5">
      <div className="group/f flex items-center">
        <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} title={`Field: ${field}`} className="flex flex-1 items-center gap-1.5 rounded px-1.5 py-1.5 text-left text-[13px] font-medium text-ink hover:bg-surface-2">
          {open ? <ChevronDown size={13} className="text-ink-3" /> : <ChevronRight size={13} className="text-ink-3" />}
          <span className="truncate">{label}</span>
        </button>
        {onRemove && (
          <button type="button" className="invisible rounded p-1 text-ink-3 hover:text-ink group-hover/f:visible" onClick={onRemove} aria-label={`Remove the ${label} filter`} title="Remove from the sidebar">
            <X size={13} />
          </button>
        )}
      </div>
      {open && (
        <ul className="pb-1">
          {items.length === 0 && <li className="px-3 py-1 text-xs text-ink-3">Nothing in this time range</li>}
          {shown.map((it) => {
            const on = hasTerm(q, field, it.value);
            const off = q.split(/\s+/).includes(term(field, it.value, true));
            return (
              <li key={it.value} className={clsx("group flex items-center gap-2 rounded px-2 py-1 text-[13px] hover:bg-surface-2", on && "bg-accent-soft hover:bg-accent-soft")}>
                <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => onToggle(field, it.value)} aria-pressed={on} title={on ? "Click to remove this filter" : `Show only ${label}: ${it.value || "empty"}`}>
                  {isLevel ? (
                    <span className="inline-block h-2 w-2 flex-none rounded-full" style={{ background: levelVar(it.value) }} />
                  ) : (
                    <span className={clsx("inline-flex h-3.5 w-3.5 flex-none items-center justify-center rounded-[4px] border", on ? "border-accent bg-accent" : "border-border-strong bg-surface")} aria-hidden>
                      {on && <svg viewBox="0 0 10 8" className="h-2 w-2.5 text-accent-ink"><path d="M1 4l3 3 5-6" fill="none" stroke="currentColor" strokeWidth="1.6" /></svg>}
                    </span>
                  )}
                  <span className={clsx("min-w-0 flex-1 truncate", off ? "text-ink-3 line-through" : "text-ink", isCodeLike(field, it.value) && "font-mono text-[12px]")}>
                    {it.value === "" ? <em className="text-ink-3">empty</em> : isLevel ? levelLabel(it.value) : it.value}
                  </span>
                </button>
                <button type="button" title={`Hide ${label}: ${it.value || "empty"}`} aria-label={`Hide ${it.value || "empty"}`} className="invisible flex-none rounded px-1 text-xs text-ink-3 hover:text-critical-text group-hover:visible" onClick={() => onToggle(field, it.value, true)}>
                  Hide
                </button>
                <span className="w-10 flex-none text-right text-xs tabular text-ink-3" title={total ? `${Math.round((it.count / total) * 100)}% of messages` : undefined}>
                  {fmtNumber(it.count)}
                </span>
              </li>
            );
          })}
          {items.length > 6 && (
            <li>
              <button type="button" className="px-3 py-1 text-xs text-ink-3 hover:text-ink" onClick={() => setShowAll(!showAll)}>
                {showAll ? "Show fewer" : `Show ${items.length - 6} more`}
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}

function DetailFacet({ fullKey, q, range, onToggle, onRemove }: { fullKey: string; q: string; range: { from: string; to: string }; onToggle: (field: string, value: string, negate?: boolean) => void; onRemove: () => void }) {
  const vals = useLogFacets({ q, from: range.from, to: range.to, key: fullKey, fields: ["service"], attr_keys: 0 });
  return <Facet field={fullKey} items={(vals.data as LogFacets | undefined)?.values ?? []} q={q} onToggle={onToggle} onRemove={onRemove} defaultOpen />;
}

function KeyPicker({ items, prefix, pinned, onPin }: { items: FacetCount[]; prefix: string; pinned: string[]; onPin: (k: string) => void }) {
  const [open, setOpen] = useState(false);
  const [find, setFind] = useState("");
  const s = find.toLowerCase();
  const list = items.filter((k) => !pinned.includes(prefix + k.value) && (!s || k.value.toLowerCase().includes(s) || fieldLabel(k.value).toLowerCase().includes(s)));
  if (items.length === 0) return <div className="px-3 py-1 text-xs text-ink-3">None in this time range</div>;
  return (
    <div className="px-1.5">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)} className="flex w-full items-center gap-1.5 rounded px-1.5 py-1.5 text-left text-[13px] text-ink-2 hover:bg-surface-2 hover:text-ink">
        <Plus size={13} /> Add a filter
      </button>
      {open && (
        <div className="pb-1">
          <div className="px-1 pb-1">
            <Input autoFocus value={find} onChange={(e) => setFind(e.target.value)} placeholder="Find a field…" aria-label="Find a field" />
          </div>
          <ul className="max-h-56 overflow-y-auto">
            {list.slice(0, 60).map((k) => (
              <li key={k.value}>
                <button type="button" onClick={() => onPin(prefix + k.value)} className="flex w-full items-baseline gap-2 rounded px-2 py-1 text-left text-[13px] hover:bg-surface-2" title={`Field: ${prefix}${k.value}`}>
                  <span className="min-w-0 flex-1 truncate text-ink">{fieldLabel(k.value)}</span>
                  <span className="text-xs tabular text-ink-3">{fmtNumber(k.count)}</span>
                </button>
              </li>
            ))}
            {list.length === 0 && <li className="px-2 py-1 text-xs text-ink-3">No field matches.</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
