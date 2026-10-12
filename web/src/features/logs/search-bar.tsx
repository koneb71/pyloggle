import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import clsx from "clsx";
import { HelpCircle, Plus, Search, X } from "lucide-react";
import { useLogFacets, useLogFields } from "@/api/hooks";
import { Button, IconButton, Input, Kbd, Popover, SegmentedControl, Skeleton } from "@/components/ui";
import { addTerm, chips, removeTerm, tokenize } from "@/lib/query";
import { describeChip } from "@/lib/query-words";
import { fieldLabel, isCodeLike } from "@/lib/fields";
import { levelLabel } from "@/lib/levels";
import { fmtNumber } from "@/lib/time";
import { FACET_TO_FIELD } from "./log-fields";

export interface SearchExample {
  query: string;
  says: string;
}

/** Plain-language search help for the logs page; other pages pass their own. */
export const LOG_SEARCH_EXAMPLES: SearchExample[] = [
  { query: "timed out", says: "Messages that contain these words" },
  { query: '"payment failed"', says: "An exact phrase" },
  { query: "service:api", says: "Only one app" },
  { query: "-env:dev", says: "Leave something out (a minus in front)" },
  { query: "level:(error OR fatal)", says: "Any of several values" },
  { query: "http.status:>=500", says: "Compare numbers: failed requests" },
  { query: "duration_ms:>250", says: "Requests slower than 250 ms" },
  { query: "attrs.order_id:42", says: "A detail your app sent with the message" },
  { query: "msg:connect*", says: "Messages that start with a word" },
];

export interface SearchBarProps {
  value: string;
  onChange: (q: string) => void;
  error?: { message: string; pos?: number } | null;
  range: { from: string; to: string };
  autoFocus?: boolean;
  /** Field names to suggest instead of the log fields (traces page); value suggestions and the filter picker are skipped. */
  fields?: string[];
  /** The "How to search" examples; defaults to the logs examples. */
  examples?: SearchExample[];
  placeholder?: string;
  ariaLabel?: string;
}

const FIELD_TO_FACET: Record<string, string> = Object.fromEntries(Object.entries(FACET_TO_FIELD).map(([k, v]) => [v, k]));

/** Query input with plain-language chips, an "Add filter" picker, inline errors, help and autocomplete. */
export function SearchBar({ value, onChange, error, range, fields: fieldsOverride, examples = LOG_SEARCH_EXAMPLES, placeholder, ariaLabel }: SearchBarProps) {
  const [draft, setDraft] = useState(value);
  const [focused, setFocused] = useState(false);
  const [cursor, setCursor] = useState(0);
  const [hi, setHi] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => setDraft(value), [value]);

  // "/" focuses the search from anywhere
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key === "/" && t.tagName !== "INPUT" && t.tagName !== "TEXTAREA" && !t.isContentEditable) {
        e.preventDefault();
        input.current?.focus();
        input.current?.select();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const logFields = useLogFields();
  const isLogs = !fieldsOverride;
  // current token under the caret
  const token = useMemo(() => currentToken(draft, cursor), [draft, cursor]);
  const colon = token.text.indexOf(":");
  const fieldPart = colon >= 0 ? token.text.slice(0, colon).replace(/^-/, "") : token.text.replace(/^-/, "");
  const valuePart = colon >= 0 ? token.text.slice(colon + 1) : "";
  const wantValues = colon >= 0 && focused && isLogs;
  const facetKey = wantValues ? facetKeyFor(fieldPart) : "";
  const facets = useLogFacets(
    { q: value, from: range.from, to: range.to, fields: facetKey && !facetKey.includes(".") ? [facetKey] : [], key: facetKey.includes(".") ? facetKey : undefined, attr_keys: 100 },
    wantValues && !!facetKey,
  );

  const suggestions = useMemo<string[]>(() => {
    if (!focused || !token.text || token.text.startsWith('"')) return [];
    if (colon < 0) {
      const names = [...(fieldsOverride ?? logFields.data?.fields ?? []).filter((f) => !f.includes("<")), ...(fieldsOverride ? [] : (facets.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`))];
      const p = fieldPart.toLowerCase();
      return Array.from(new Set(names))
        .filter((n) => n.toLowerCase().startsWith(p) && n !== p)
        .slice(0, 8)
        .map((n) => `${token.text.startsWith("-") ? "-" : ""}${n}:`);
    }
    const vals = facetKey.includes(".") ? (facets.data?.values ?? []) : (facets.data?.fields?.[facetKey] ?? []);
    const vp = valuePart.toLowerCase().replace(/^"/, "");
    return vals
      .filter((v) => v.value.toLowerCase().includes(vp))
      .slice(0, 10)
      .map((v) => `${token.text.slice(0, colon + 1)}${quote(v.value)}`);
  }, [focused, token.text, colon, logFields.data, facets.data, fieldPart, valuePart, facetKey, fieldsOverride]);

  useEffect(() => setHi(0), [suggestions.length, token.text]);

  const apply = (s: string) => {
    const next = draft.slice(0, token.start) + s + (s.endsWith(":") ? "" : " ") + draft.slice(token.end);
    setDraft(next);
    requestAnimationFrame(() => {
      const pos = token.start + s.length + (s.endsWith(":") ? 0 : 1);
      input.current?.setSelectionRange(pos, pos);
      setCursor(pos);
    });
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (suggestions.length && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
      e.preventDefault();
      setHi((h) => (e.key === "ArrowDown" ? (h + 1) % suggestions.length : (h - 1 + suggestions.length) % suggestions.length));
      return;
    }
    if (suggestions.length && e.key === "Tab") {
      e.preventDefault();
      apply(suggestions[hi]);
      return;
    }
    if (e.key === "Enter") {
      if (suggestions.length && suggestions[hi] && !suggestions[hi].endsWith(":") && valuePart === "") {
        apply(suggestions[hi]);
        return;
      }
      onChange(draft.trim());
      setFocused(false);
      input.current?.blur();
      return;
    }
    if (e.key === "Escape") {
      setDraft(value);
      input.current?.blur();
    }
  };

  const parsed = useMemo(() => chips(value), [value]);
  const insertExample = (q: string) => onChange(tokenize(value).includes(q) ? value : [value, q].filter(Boolean).join(" "));
  return (
    <div className="flex flex-col gap-2">
      <div className="relative">
        <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
        <input
          ref={input}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            setCursor(e.target.selectionStart ?? e.target.value.length);
          }}
          onKeyDown={onKey}
          onFocus={() => setFocused(true)}
          onBlur={() => setTimeout(() => setFocused(false), 120)}
          onSelect={(e) => setCursor((e.target as HTMLInputElement).selectionStart ?? 0)}
          placeholder={placeholder ?? "Search messages, e.g. timed out — or filter with service:api level:error"}
          spellCheck={false}
          autoComplete="off"
          aria-label={ariaLabel ?? "Search messages"}
          aria-invalid={!!error || undefined}
          className={clsx(
            "h-10 w-full rounded-md border bg-surface pl-9 pr-28 font-mono text-[13px] text-ink placeholder:font-sans placeholder:text-ink-3 focus:outline-none focus:ring-2 focus:ring-accent/25",
            error ? "border-critical" : "border-border-strong focus:border-accent",
          )}
        />
        <div className="absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center gap-0.5">
          {draft && (
            <IconButton
              size="xs"
              label="Clear search"
              icon={<X size={13} />}
              onClick={() => {
                setDraft("");
                onChange("");
              }}
            />
          )}
          <SearchHelp examples={examples} onPick={insertExample} />
          <span title="Press / to search from anywhere on the page">
            <Kbd>/</Kbd>
          </span>
        </div>
        {focused && suggestions.length > 0 && (
          <ul className="absolute left-9 top-full z-30 mt-1 w-96 overflow-hidden rounded-lg border border-border bg-surface py-1 text-[13px] shadow-[var(--shadow)]" role="listbox">
            {suggestions.map((s, i) => (
              <li key={s}>
                <button
                  type="button"
                  role="option"
                  aria-selected={i === hi}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    apply(s);
                  }}
                  className={clsx("flex w-full items-baseline gap-3 px-3 py-1.5 text-left", i === hi ? "bg-accent-soft text-ink" : "text-ink-2 hover:bg-surface-2")}
                >
                  <span className="min-w-0 truncate font-mono text-[12px]">{s}</span>
                  {s.endsWith(":") && <span className="ml-auto flex-none text-xs text-ink-3">{fieldLabel(s.replace(/^-/, "").slice(0, -1))}</span>}
                </button>
              </li>
            ))}
            <li className="border-t border-border px-3 pb-0.5 pt-1.5 text-xs text-ink-3">↑ ↓ to choose · Tab to insert · Enter to search</li>
          </ul>
        )}
      </div>
      {error && (
        <div role="alert" className="text-[13px] text-critical-text">
          This search isn't valid: {error.message}
          {error.pos !== undefined && error.pos > 0 && <span className="text-ink-3"> (near character {error.pos})</span>}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        {parsed.map((c, i) => (
          <span key={`${c.raw}-${i}`} className={clsx("q-chip", c.negate && "neg-chip")} title={`Search term: ${c.raw}`}>
            <span className="truncate text-ink">{describeChip(c)}</span>
            <button type="button" aria-label={`Remove: ${describeChip(c)}`} className="-mr-0.5 rounded-full p-0.5 text-ink-3 hover:bg-surface-3 hover:text-ink" onClick={() => onChange(removeTerm(value, c.raw))}>
              <X size={12} />
            </button>
          </span>
        ))}
        {isLogs && <AddFilter q={value} range={range} onChange={onChange} />}
        {tokenize(value).length > 1 && (
          <button type="button" className="px-1 text-xs text-ink-3 hover:text-ink" onClick={() => onChange("")}>
            Clear all
          </button>
        )}
      </div>
    </div>
  );
}

/** Query field → the facet that lists its values ("" when the field has none). */
function facetKeyFor(field: string): string {
  if (field.startsWith("attrs.") || field.startsWith("tags.")) return field;
  if (field.startsWith("@")) return `attrs.${field.slice(1)}`;
  return FIELD_TO_FACET[field] || "";
}

/** Fields offered first in the filter picker, in the order people reach for them. */
const PICK_FIELDS = ["level", "service", "env", "host", "logger", "release", "http.method", "http.status", "http.route", "exception", "source"];

/** "Add filter": pick a field by its plain name, then a value from what is in range, then is / is not. */
function AddFilter({ q, range, onChange }: { q: string; range: { from: string; to: string }; onChange: (q: string) => void }) {
  return (
    <Popover
      width={320}
      trigger={({ toggle, open }) => (
        <Button size="xs" variant="ghost" active={open} icon={<Plus size={13} />} onClick={toggle} className="border-dashed border-border-strong">
          Add filter
        </Button>
      )}
    >
      {(close) => <FilterPicker q={q} range={range} onPick={(next) => (onChange(next), close())} />}
    </Popover>
  );
}

/** The picker's body: mounted only while the popover is open, so its lookups run on demand. */
function FilterPicker({ q, range, onPick }: { q: string; range: { from: string; to: string }; onPick: (q: string) => void }) {
  const [field, setField] = useState<string | null>(null);
  const [negate, setNegate] = useState(false);
  const [custom, setCustom] = useState("");
  const [find, setFind] = useState("");
  const keys = useLogFacets({ q, from: range.from, to: range.to, fields: [], attr_keys: 60 });
  const facetKey = field ? facetKeyFor(field) : "";
  const vals = useLogFacets({ q, from: range.from, to: range.to, fields: facetKey && !facetKey.includes(".") ? [facetKey] : [], key: facetKey.includes(".") ? facetKey : undefined, attr_keys: 0 }, !!facetKey);
  const values = facetKey ? ((facetKey.includes(".") ? vals.data?.values : vals.data?.fields?.[facetKey]) ?? []) : [];
  const matches = (f: string) => !find || fieldLabel(f).toLowerCase().includes(find.toLowerCase()) || f.includes(find.toLowerCase());
  const common = PICK_FIELDS.filter(matches);
  const details = (keys.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`).filter(matches);

  if (!field) {
    const row = (f: string) => (
      <li key={f}>
        <button type="button" onClick={() => setField(f)} className="flex w-full items-baseline gap-3 rounded px-2 py-1.5 text-left text-[13px] text-ink hover:bg-surface-2">
          <span className="min-w-0 truncate">{fieldLabel(f)}</span>
          <span className="ml-auto flex-none font-mono text-[11px] text-ink-3">{f}</span>
        </button>
      </li>
    );
    return (
      <div>
        <div className="px-1 pb-1.5 pt-1">
          <Input autoFocus value={find} onChange={(e) => setFind(e.target.value)} placeholder="Filter by…" aria-label="Find a field" />
        </div>
        <ul className="max-h-72 overflow-y-auto">
          {common.map(row)}
          {details.length > 0 && <li className="px-2 pb-1 pt-2 text-xs font-medium text-ink-3">Details sent with messages</li>}
          {details.map(row)}
          {common.length + details.length === 0 && <li className="px-2 py-2 text-[13px] text-ink-3">No field matches.</li>}
        </ul>
      </div>
    );
  }
  return (
    <div className="p-1">
      <div className="flex items-center justify-between gap-2 px-1 pb-2">
        <button type="button" className="text-xs text-ink-3 hover:text-ink" onClick={() => setField(null)}>
          ← {fieldLabel(field)}
        </button>
        <SegmentedControl
          label="Match"
          value={negate ? "not" : "is"}
          onChange={(v) => setNegate(v === "not")}
          options={[
            { value: "is", label: "is" },
            { value: "not", label: "is not" },
          ]}
        />
      </div>
      {facetKey &&
        (vals.isPending ? (
          <Skeleton rows={4} className="px-2 py-1" />
        ) : (
          <ul className="max-h-64 overflow-y-auto">
            {values.map((v) => (
              <li key={v.value}>
                <button type="button" onClick={() => onPick(addTerm(q, field, v.value, negate))} className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-left text-[13px] hover:bg-surface-2">
                  <span className={clsx("min-w-0 flex-1 truncate text-ink", isCodeLike(field, v.value) && "font-mono text-[12px]")}>{v.value === "" ? <em className="text-ink-3">empty</em> : field === "level" ? levelLabel(v.value) : v.value}</span>
                  <span className="flex-none text-xs tabular text-ink-3">{fmtNumber(v.count)}</span>
                </button>
              </li>
            ))}
            {values.length === 0 && <li className="px-2 py-1.5 text-[13px] text-ink-3">No values in this time range. Type one below.</li>}
          </ul>
        ))}
      <form
        className="mt-1 flex gap-1.5 border-t border-border px-1 pt-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (custom.trim()) onPick(addTerm(q, field, custom.trim(), negate));
        }}
      >
        <Input autoFocus={!facetKey} value={custom} onChange={(e) => setCustom(e.target.value)} placeholder="Or type a value" aria-label="Value" />
        <Button type="submit" variant="primary" disabled={!custom.trim()}>
          Add
        </Button>
      </form>
    </div>
  );
}

function SearchHelp({ examples, onPick }: { examples: SearchExample[]; onPick: (q: string) => void }) {
  return (
    <Popover align="end" width={440} trigger={({ toggle }) => <IconButton size="xs" label="How to search" icon={<HelpCircle size={14} />} onClick={toggle} />}>
      {(close) => (
        <div className="p-2">
          <div className="text-[13px] font-semibold text-ink">How to search</div>
          <p className="mb-2 mt-0.5 text-xs text-ink-3">Combine any of these; every part must match. Click an example to add it.</p>
          <ul className="divide-y divide-border">
            {examples.map((x) => (
              <li key={x.query}>
                <button
                  type="button"
                  className="flex w-full items-baseline gap-3 rounded px-1 py-1.5 text-left hover:bg-surface-2"
                  onClick={() => {
                    onPick(x.query);
                    close();
                  }}
                >
                  <code className="w-44 flex-none truncate font-mono text-[12px] text-accent">{x.query}</code>
                  <span className="text-[13px] text-ink-2">{x.says}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Popover>
  );
}

function currentToken(s: string, pos: number): { text: string; start: number; end: number } {
  let start = pos;
  let inQuote = false;
  for (let i = 0; i < pos; i++) if (s[i] === '"') inQuote = !inQuote;
  if (inQuote) {
    start = s.lastIndexOf('"', pos - 1);
  } else {
    while (start > 0 && !/\s/.test(s[start - 1])) start--;
  }
  let end = pos;
  while (end < s.length && !/\s/.test(s[end])) end++;
  return { text: s.slice(start, end), start, end };
}

function quote(v: string): string {
  return /[\s"'()]/.test(v) || v === "" ? `"${v.replace(/"/g, '\\"')}"` : v;
}
