import { useCallback, useMemo } from "react";
import { useUrlState } from "./url";
import { DEFAULT_RANGE, resolveRange, type Range } from "./time";

/** Time range held in the URL (`from`, `to`). Relative `from` ("-1h") keeps the view live. */
export function useRange(def: Range = DEFAULT_RANGE) {
  const { get, set } = useUrlState();
  const range = useMemo<Range>(() => ({ from: get("from") || def.from, to: get("to") || def.to }), [get, def.from, def.to]);
  const setRange = useCallback((r: Range) => set({ from: r.from, to: r.to || undefined, cursor: undefined }), [set]);
  const resolved = useMemo(() => resolveRange(range), [range]);
  const zoom = useCallback((from: Date, to: Date) => set({ from: from.toISOString(), to: to.toISOString(), cursor: undefined }), [set]);
  return { range, setRange, resolved, zoom, isLive: !range.to };
}
