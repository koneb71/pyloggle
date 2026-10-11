import { useCallback } from "react";
import { useSearchParams } from "react-router";

/** Read/write page state in the URL so every view is shareable. */
export function useUrlState() {
  const [sp, setSp] = useSearchParams();
  const get = useCallback((k: string, def = "") => sp.get(k) ?? def, [sp]);
  const set = useCallback(
    (patch: Record<string, string | number | undefined | null>, replace = false) => {
      setSp(
        (prev) => {
          const next = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(patch)) {
            if (v === undefined || v === null || v === "") next.delete(k);
            else next.set(k, String(v));
          }
          return next;
        },
        { replace },
      );
    },
    [setSp],
  );
  return { get, set, params: sp };
}
