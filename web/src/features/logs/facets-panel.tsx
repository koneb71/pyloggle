import { useMemo, useState } from "react";
import clsx from "clsx";
import { ChevronDown, ChevronRight, Filter, FilterX, Plus, Search } from "lucide-react";
import { useLogFacets } from "@/api/hooks";
import type { FacetCount, LogFacets } from "@/api/types";
import { Input, Skeleton } from "@/components/ui";
import { fmtNumber } from "@/lib/time";
import { addTerm, hasTerm, removeTerm, term } from "@/lib/query";
import { levelVar } from "@/lib/levels";
import { FACET_GROUPS, FACET_TO_FIELD } from "./log-fields";

export interface FacetsPanelProps {
  q: string;
  range: { from: string; to: string };
  onChange: (q: string) => void;
  extraFacets: string[]; // attrs.x / tags.y pinned by the user
  onExtraFacets: (f: string[]) => void;
}

export function FacetsPanel({ q, range, onChange, extraFacets, onExtraFacets }: FacetsPanelProps) {
  const facets = useLogFacets({ q, from: range.from, to: range.to, attr_keys: 40 });
  const [keySearch, setKeySearch] = useState("");
  const data = facets.data;

  const toggle = (field: string, value: string, negate = false) => {
    const t = term(field, value, negate);
    if (hasTerm(q, field, value) && !negate) onChange(removeTerm(q, t));
    else onChange(addTerm(q, field, value, negate));
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex-none border-b border-border px-2 py-1.5 text-[11px] font-medium uppercase tracking-wide text-ink-3">Facets {facets.isFetching && <span className="ml-1 normal-case text-ink-3">· updating</span>}</div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {!data && facets.isPending ? (
          <Skeleton rows={10} className="p-2" />
        ) : (
          <>
            {FACET_GROUPS.map((g) => (
              <Group key={g.title} title={g.title}>
                {g.facets.map((f) => (
                  <Facet key={f.key} label={f.label} field={FACET_TO_FIELD[f.key]} items={data?.fields?.[f.key] ?? []} q={q} onToggle={toggle} colorFn={f.key === "level" ? levelVar : undefined} />
                ))}
              </Group>
            ))}
            <Group title="Attributes" count={data?.attr_keys?.length}>
              {extraFacets
                .filter((k) => k.startsWith("attrs."))
                .map((k) => (
                  <AttrFacet key={k} fullKey={k} q={q} range={range} onToggle={toggle} onRemove={() => onExtraFacets(extraFacets.filter((x) => x !== k))} />
                ))}
              <KeyPicker items={data?.attr_keys ?? []} prefix="attrs." pinned={extraFacets} onPin={(k) => onExtraFacets([...extraFacets, k])} search={keySearch} onSearch={setKeySearch} />
            </Group>
            {(data?.tag_keys?.length ?? 0) > 0 || extraFacets.some((k) => k.startsWith("tags.")) ? (
              <Group title="Tags" count={data?.tag_keys?.length}>
                {extraFacets
                  .filter((k) => k.startsWith("tags."))
                  .map((k) => (
                    <AttrFacet key={k} fullKey={k} q={q} range={range} onToggle={toggle} onRemove={() => onExtraFacets(extraFacets.filter((x) => x !== k))} />
                  ))}
                <KeyPicker items={data?.tag_keys ?? []} prefix="tags." pinned={extraFacets} onPin={(k) => onExtraFacets([...extraFacets, k])} />
              </Group>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}

function Group({ title, children, count }: { title: string; children: React.ReactNode; count?: number }) {
  const [open, setOpen] = useState(true);
  return (
    <div className="border-b border-border">
      <button type="button" onClick={() => setOpen(!open)} className="flex w-full items-center gap-1 px-2 py-1.5 text-left text-[11px] font-semibold uppercase tracking-wide text-ink-2 hover:bg-surface-2">
        {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        {title}
        {count !== undefined && <span className="ml-auto font-normal normal-case text-ink-3">{count}</span>}
      </button>
      {open && <div className="pb-1">{children}</div>}
    </div>
  );
}

function Facet({ label, field, items, q, onToggle, colorFn, onRemove }: { label: string; field: string; items: FacetCount[]; q: string; onToggle: (field: string, value: string, negate?: boolean) => void; colorFn?: (v: string) => string; onRemove?: () => void }) {
  const [open, setOpen] = useState(true);
  const [showAll, setShowAll] = useState(false);
  const total = useMemo(() => items.reduce((n, i) => n + i.count, 0), [items]);
  const shown = showAll ? items : items.slice(0, 6);
  return (
    <div className="px-1">
      <div className="flex items-center">
        <button type="button" onClick={() => setOpen(!open)} className="flex flex-1 items-center gap-1 px-1 py-1 text-left text-xs font-medium text-ink hover:text-accent">
          {open ? <ChevronDown size={11} className="text-ink-3" /> : <ChevronRight size={11} className="text-ink-3" />}
          {label}
          <span className="ml-1 font-mono text-[10px] font-normal text-ink-3">{field}</span>
        </button>
        {onRemove && (
          <button type="button" className="px-1 text-[10px] text-ink-3 hover:text-critical" onClick={onRemove} title="Unpin facet">
            unpin
          </button>
        )}
      </div>
      {open && (
        <ul>
          {items.length === 0 && <li className="px-2 py-0.5 text-[11px] text-ink-3">no values</li>}
          {shown.map((it) => {
            const on = hasTerm(q, field, it.value);
            const off = hasTerm(q, `-${field}`, it.value) || q.split(/\s+/).includes(term(field, it.value, true));
            return (
              <li key={it.value} className={clsx("group flex items-center gap-1.5 rounded px-1.5 py-[3px] text-xs hover:bg-surface-2", on && "bg-accent-soft")}>
                <button type="button" className="flex min-w-0 flex-1 items-center gap-1.5 text-left" onClick={() => onToggle(field, it.value)} title={`${field}:${it.value}`}>
                  {colorFn ? <span className="inline-block h-2 w-2 flex-none rounded-sm" style={{ background: colorFn(it.value) }} /> : <span className={clsx("inline-block h-2.5 w-2.5 flex-none rounded-sm border", on ? "border-accent bg-accent" : "border-border-strong")} />}
                  <span className={clsx("min-w-0 flex-1 truncate font-mono", off ? "line-through text-ink-3" : "text-ink")}>{it.value === "" ? <em className="font-sans text-ink-3">empty</em> : it.value}</span>
                </button>
                <span className="relative flex h-1 w-10 flex-none items-center rounded bg-surface-3">
                  <span className="h-1 rounded bg-ink-3/60" style={{ width: `${Math.max(3, (it.count / Math.max(1, total)) * 100)}%` }} />
                </span>
                <span className="w-10 flex-none text-right tabular text-[11px] text-ink-3">{fmtNumber(it.count)}</span>
                <span className="invisible flex flex-none items-center group-hover:visible">
                  <button type="button" aria-label="Filter to value" title="Only this value" className="rounded p-0.5 text-ink-3 hover:text-accent" onClick={() => onToggle(field, it.value)}>
                    <Filter size={11} />
                  </button>
                  <button type="button" aria-label="Exclude value" title="Exclude this value" className="rounded p-0.5 text-ink-3 hover:text-critical" onClick={() => onToggle(field, it.value, true)}>
                    <FilterX size={11} />
                  </button>
                </span>
              </li>
            );
          })}
          {items.length > 6 && (
            <li>
              <button type="button" className="px-2 py-0.5 text-[11px] text-ink-3 hover:text-ink" onClick={() => setShowAll(!showAll)}>
                {showAll ? "show less" : `show ${items.length - 6} more`}
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}

function AttrFacet({ fullKey, q, range, onToggle, onRemove }: { fullKey: string; q: string; range: { from: string; to: string }; onToggle: (field: string, value: string, negate?: boolean) => void; onRemove: () => void }) {
  const vals = useLogFacets({ q, from: range.from, to: range.to, key: fullKey, fields: ["service"], attr_keys: 0 });
  const label = fullKey.split(".").slice(1).join(".");
  return <Facet label={label} field={fullKey} items={(vals.data as LogFacets | undefined)?.values ?? []} q={q} onToggle={onToggle} onRemove={onRemove} />;
}

function KeyPicker({ items, prefix, pinned, onPin, search, onSearch }: { items: FacetCount[]; prefix: string; pinned: string[]; onPin: (k: string) => void; search?: string; onSearch?: (s: string) => void }) {
  const [open, setOpen] = useState(false);
  const [local, setLocal] = useState("");
  const s = (search ?? local).toLowerCase();
  const setS = onSearch ?? setLocal;
  const list = items.filter((k) => !pinned.includes(prefix + k.value) && (!s || k.value.toLowerCase().includes(s)));
  return (
    <div className="px-1">
      <button type="button" onClick={() => setOpen(!open)} className="flex w-full items-center gap-1 px-1 py-1 text-left text-xs text-ink-2 hover:text-accent">
        <Plus size={11} /> Add facet <span className="ml-auto font-mono text-[10px] text-ink-3">{list.length} keys</span>
      </button>
      {open && (
        <div className="pb-1">
          <div className="relative px-1 pb-1">
            <Search size={11} className="pointer-events-none absolute left-2.5 top-2 text-ink-3" />
            <Input value={search ?? local} onChange={(e) => setS(e.target.value)} placeholder="find key…" className="h-6 pl-6 text-xs" />
          </div>
          <ul className="max-h-48 overflow-y-auto">
            {list.slice(0, 50).map((k) => (
              <li key={k.value}>
                <button type="button" onClick={() => onPin(prefix + k.value)} className="flex w-full items-center gap-2 rounded px-1.5 py-[3px] text-left text-xs hover:bg-surface-2">
                  <span className="min-w-0 flex-1 truncate font-mono text-ink">{k.value}</span>
                  <span className="tabular text-[11px] text-ink-3">{fmtNumber(k.count)}</span>
                </button>
              </li>
            ))}
            {list.length === 0 && <li className="px-2 py-1 text-[11px] text-ink-3">no keys in this range</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
