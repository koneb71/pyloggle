import { useLogFacets } from "@/api/hooks";
import { Select } from "@/components/ui";

/** "For [All apps] in [All environments]": the app and environment a page looks at. */
export function ScopeFilter({ range, service, env, onChange }: { range: { from: string; to: string }; service: string; env: string; onChange: (patch: { service?: string; env?: string }) => void }) {
  const facets = useLogFacets({ from: range.from, to: range.to || undefined, fields: ["service", "env"], attr_keys: 0 });
  const options = (field: "service" | "env", current: string) => {
    const values = (facets.data?.fields[field] ?? []).map((f) => f.value).filter(Boolean);
    if (current && !values.includes(current)) values.push(current);
    return values.map((v) => (
      <option key={v} value={v}>
        {v}
      </option>
    ));
  };
  return (
    <div className="flex flex-wrap items-center gap-2 text-[13px] text-ink-3">
      <span>For</span>
      <Select value={service} onChange={(e) => onChange({ service: e.target.value || undefined })} className="w-auto min-w-32" aria-label="App">
        <option value="">All apps</option>
        {options("service", service)}
      </Select>
      <span>in</span>
      <Select value={env} onChange={(e) => onChange({ env: e.target.value || undefined })} className="w-auto min-w-36" aria-label="Environment">
        <option value="">All environments</option>
        {options("env", env)}
      </Select>
    </div>
  );
}
