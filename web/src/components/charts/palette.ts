import { LEVELS, levelVar } from "@/lib/levels";

export const SERIES_VARS = Array.from({ length: 8 }, (_, i) => `var(--series-${i + 1})`);
export const MAX_SERIES = 8;

/** Color for a named series. Levels use the level tokens; everything else takes categorical slots in fixed order. */
export function seriesColor(groupBy: string, name: string, index: number): string {
  if (groupBy === "level" || groupBy === "") return levelVar(name);
  if (name === "Other") return "var(--lvl-other)";
  return SERIES_VARS[index % SERIES_VARS.length];
}

/** Keep the fixed level order, otherwise the server order; fold extra series into "Other". */
export function orderSeries(groupBy: string, series: string[]): { kept: string[]; folded: string[] } {
  if (groupBy === "level" || groupBy === "") {
    const known = LEVELS.filter((l) => series.includes(l));
    const rest = series.filter((s) => !LEVELS.includes(s as (typeof LEVELS)[number]));
    return { kept: [...known, ...rest], folded: [] };
  }
  if (series.length <= MAX_SERIES) return { kept: series, folded: [] };
  return { kept: [...series.slice(0, MAX_SERIES - 1), "Other"], folded: series.slice(MAX_SERIES - 1) };
}
