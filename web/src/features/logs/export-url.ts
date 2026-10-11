import { apiUrl } from "@/api/client";

export type ExportFormat = "csv" | "ndjson";
export const EXPORT_LIMITS = [1000, 10000, 100000];

export interface ExportRequest {
  q: string;
  from: string;
  to: string;
  format: ExportFormat;
  limit: number;
  cols?: string[];
}

/** URL of the streamed export for the current explorer selection. */
export function exportUrl(r: ExportRequest): string {
  return apiUrl("/logs/export", { q: r.q || undefined, from: r.from, to: r.to || undefined, format: r.format, limit: r.limit, cols: r.cols?.length ? r.cols : undefined });
}
