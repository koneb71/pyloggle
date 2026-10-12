import { Swatch } from "@/components/ui";
import { durationWords } from "@/lib/time";

/**
 * Chart legend above the plot (never over it): one entry per series in chart order, then the
 * bucket size in words. Identity is never color alone: every swatch has its name.
 */
export function ChartLegend({ items, shape = "square", bucketSeconds, unit = "bar" }: { items: { name: string; color: string }[]; shape?: "square" | "line"; bucketSeconds?: number; unit?: "bar" | "point" }) {
  if (items.length < 2 && !bucketSeconds) return null;
  return (
    <div className="mb-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-2">
      {items.length >= 2 &&
        items.map((s) => (
          <span key={s.name} className="inline-flex min-w-0 items-center gap-1.5">
            <Swatch color={s.color} shape={shape} />
            <span className="max-w-48 truncate">{s.name}</span>
          </span>
        ))}
      {bucketSeconds ? <span className="ml-auto text-ink-3">Each {unit} is {durationWords(bucketSeconds)}</span> : null}
    </div>
  );
}
