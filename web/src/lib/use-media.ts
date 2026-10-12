import { useEffect, useLayoutEffect, useState, type RefObject } from "react";

/** True while the CSS media query matches; follows window resizes. */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const m = window.matchMedia(query);
    const on = () => setMatches(m.matches);
    on();
    m.addEventListener("change", on);
    return () => m.removeEventListener("change", on);
  }, [query]);
  return matches;
}

/**
 * Phones and small tablets: below Tailwind's `md` (768px), the same line the `max-md:` classes
 * use. The navigation moves into a menu, side panels cover the screen, and lists that are
 * tables on a desk become two-line rows.
 */
export const SMALL_SCREEN = "(max-width: 767px)";

export function useSmallScreen(): boolean {
  return useMediaQuery(SMALL_SCREEN);
}

/**
 * The element's width, measured before the first paint and followed as it resizes; 0 until it
 * is known (and always in tests, which have no layout). For lists that change shape with the
 * room they actually get: a filters panel beside them narrows them as much as a phone does.
 */
export function useElementWidth(ref: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setWidth(el.getBoundingClientRect().width);
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return width;
}
