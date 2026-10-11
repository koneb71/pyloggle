import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import clsx from "clsx";
import { HelpCircle, Search, X } from "lucide-react";
import { useLogFacets, useLogFields } from "@/api/hooks";
import { Button, Kbd, Popover } from "@/components/ui";
import { chips, removeTerm, tokenize } from "@/lib/query";
import { FACET_TO_FIELD } from "./log-fields";

export interface SearchBarProps {
  value: string;
  onChange: (q: string) => void;
  error?: { message: string; pos?: number } | null;
  range: { from: string; to: string };
  autoFocus?: boolean;
}

const FIELD_TO_FACET: Record<string, string> = Object.fromEntries(Object.entries(FACET_TO_FIELD).map(([k, v]) => [v, k]));

/** Query input with parsed-term chips, inline error, syntax help and autocomplete. */
export function SearchBar({ value, onChange, error, range }: SearchBarProps) {
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

  const fields = useLogFields();
  // current token under the caret
  const token = useMemo(() => currentToken(draft, cursor), [draft, cursor]);
  const colon = token.text.indexOf(":");
  const fieldPart = colon >= 0 ? token.text.slice(0, colon).replace(/^-/, "") : token.text.replace(/^-/, "");
  const valuePart = colon >= 0 ? token.text.slice(colon + 1) : "";
  const wantValues = colon >= 0 && focused;
  const facetKey = wantValues ? (fieldPart.startsWith("attrs.") || fieldPart.startsWith("tags.") ? fieldPart : fieldPart.startsWith("@") ? `attrs.${fieldPart.slice(1)}` : FIELD_TO_FACET[fieldPart] || "") : "";
  const facets = useLogFacets(
    { q: value, from: range.from, to: range.to, fields: facetKey && !facetKey.includes(".") ? [facetKey] : [], key: facetKey.includes(".") ? facetKey : undefined, attr_keys: 100 },
    wantValues && !!facetKey,
  );

  const suggestions = useMemo<string[]>(() => {
    if (!focused || !token.text || token.text.startsWith('"')) return [];
    if (colon < 0) {
      const names = [...(fields.data?.fields ?? []).filter((f) => !f.includes("<")), ...(facets.data?.attr_keys ?? []).map((k) => `attrs.${k.value}`)];
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
  }, [focused, token.text, colon, fields.data, facets.data, fieldPart, valuePart, facetKey]);

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
  return (
    <div className="flex flex-col gap-1.5">
      <div className="relative">
        <Search size={14} className="pointer-events-none absolute left-2.5 top-2.5 text-ink-3" />
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
          placeholder='Search logs… e.g. service:api level:error "timed out" -attrs.path:/health'
          spellCheck={false}
          autoComplete="off"
          aria-label="Log query"
          className={clsx(
            "h-9 w-full rounded-md border bg-surface pl-8 pr-24 font-mono text-[13px] text-ink placeholder:font-sans placeholder:text-ink-3 focus:outline-none focus:ring-2 focus:ring-accent/30",
            error ? "border-critical" : "border-border focus:border-accent",
          )}
        />
        <div className="absolute right-1.5 top-1.5 flex items-center gap-1">
          {draft && (
            <Button
              size="xs"
              variant="ghost"
              aria-label="Clear query"
              icon={<X size={12} />}
              onClick={() => {
                setDraft("");
                onChange("");
              }}
            />
          )}
          <SyntaxHelp syntax={fields.data?.syntax} />
          <Kbd>/</Kbd>
        </div>
        {focused && suggestions.length > 0 && (
          <ul className="absolute left-8 top-full z-30 mt-1 w-80 rounded-md border border-border bg-surface py-1 font-mono text-xs shadow-[var(--shadow)]" role="listbox">
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
                  className={clsx("block w-full truncate px-2.5 py-1 text-left", i === hi ? "bg-accent-soft text-ink" : "text-ink-2 hover:bg-surface-2")}
                >
                  {s}
                </button>
              </li>
            ))}
            <li className="px-2.5 pt-1 font-sans text-[10px] text-ink-3">↑↓ choose · Tab insert · Enter search</li>
          </ul>
        )}
      </div>
      {error && (
        <div className="text-xs text-critical">
          {error.message}
          {error.pos !== undefined && error.pos > 0 && <span className="text-ink-3"> (at position {error.pos})</span>}
        </div>
      )}
      {parsed.length > 0 && (
        <div className="flex flex-wrap items-center gap-1">
          {parsed.map((c, i) => (
            <span key={`${c.raw}-${i}`} className={clsx("q-chip", c.negate && "neg")} title={c.raw}>
              {c.field ? (
                <>
                  <span className="text-ink-3">{c.negate ? "-" : ""}{c.field}:</span>
                  <span className="truncate text-ink">{c.value}</span>
                </>
              ) : (
                <span className="truncate text-ink">{c.raw}</span>
              )}
              <button type="button" aria-label={`Remove ${c.raw}`} className="rounded text-ink-3 hover:text-critical" onClick={() => onChange(removeTerm(value, c.raw))}>
                <X size={11} />
              </button>
            </span>
          ))}
          {tokenize(value).length > 1 && (
            <button type="button" className="text-[11px] text-ink-3 hover:text-ink" onClick={() => onChange("")}>
              clear all
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function SyntaxHelp({ syntax }: { syntax?: string[] }) {
  const rows = syntax ?? [
    "service:api level:error — field filters (AND)",
    '"timed out" — phrase in message',
    "-level:debug — exclude",
    "level:(error OR fatal) — any of",
    "http.status:>=500 duration_ms:>250 — comparisons",
    "attrs.user_id:42 or @user_id:42 — attributes; tags.team:core — tags",
    "msg:connect* — prefix wildcard",
  ];
  return (
    <Popover
      align="end"
      width={420}
      trigger={({ toggle }) => <Button size="xs" variant="ghost" aria-label="Query syntax help" icon={<HelpCircle size={13} />} onClick={toggle} />}
    >
      <div className="p-2">
        <div className="mb-1.5 text-xs font-semibold text-ink">Query syntax</div>
        <ul className="space-y-1 font-mono text-[11px] text-ink-2">
          {rows.map((r) => (
            <li key={r} className="whitespace-pre-wrap">
              {r}
            </li>
          ))}
        </ul>
        <div className="mt-2 font-sans text-[11px] text-ink-3">Terms are AND-ed. Fields: service, env, level, logger, host, request_id, trace_id, release, issue, http.method, http.route, http.status, http.url, duration_ms, user, exception, source, attrs.&lt;key&gt;, tags.&lt;key&gt;.</div>
      </div>
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
