import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiRequestError, del, get, post } from "./client";
import type { APIKey, AlertFiring, AlertRule, AttentionItem, AttentionReason, AuditEntry, ChartFields, ChartSource, ErrorAnalytics, Histogram, IssueActivity, IssueDetail, IssueListResponse, LogAggregate, LogContext, LogEvent, LogFacets, LogPattern, LogsResponse, Me, MetricInfo, MetricSeries, Occurrence, Overview, Project, ProjectUsage, Release, RequestStatsResponse, SavedView, ServiceHealth, TimeSeriesPoint, TraceDetail, TracesResponse, UsageAnalytics } from "./types";

export const useMe = () =>
  useQuery({
    queryKey: ["me"],
    queryFn: () => get<Me>("/me"),
    staleTime: 60_000,
    retry: false,
    // While the server or its database is unreachable, keep checking so the app comes back on its own.
    refetchInterval: (q) => {
      const e = q.state.error;
      return e && !(e instanceof ApiRequestError && e.status < 500) ? 10_000 : false;
    },
  });

export interface IssuesParams {
  status?: string;
  assignee?: string;
  q?: string;
  service?: string;
  env?: string;
  level?: string;
  release?: string;
  sort?: string;
  from?: string;
  to?: string;
  limit?: number;
  offset?: number;
}

export const useIssues = (p: IssuesParams, refetchInterval?: number | false) =>
  useQuery({
    queryKey: ["issues", p],
    queryFn: () => get<IssueListResponse>("/issues", { ...p, sparklines: 1 }),
    placeholderData: keepPreviousData,
    refetchInterval: refetchInterval ?? false,
  });

export const useIssue = (id: string | undefined, range: { from?: string; to?: string }) =>
  useQuery({ queryKey: ["issue", id, range], queryFn: () => get<IssueDetail>(`/issues/${id}`, range), enabled: !!id });

export const useIssueTrend = (id: string | undefined, range: { from?: string; to?: string }) =>
  useQuery({
    queryKey: ["issue-trend", id, range],
    queryFn: () => get<{ bucket_seconds: number; points: TimeSeriesPoint[] }>(`/issues/${id}/trend`, range),
    enabled: !!id,
  });

export const useOccurrence = (eventId: string | undefined) =>
  useQuery({ queryKey: ["occurrence", eventId], queryFn: () => get<Occurrence>(`/events/${eventId}`), enabled: !!eventId });

export const useOccurrences = (id: string | undefined, limit = 50, range: { from?: string; to?: string } = {}) =>
  useQuery({
    queryKey: ["occurrences", id, limit, range],
    queryFn: () => get<{ items: Occurrence[]; next_before?: string }>(`/issues/${id}/occurrences`, { limit, ...range }),
    enabled: !!id,
  });

export interface StateChange {
  status: string;
  comment?: string;
  snooze_for_seconds?: number;
  snooze_until?: string;
  snooze_count?: number;
  resolved_in_release?: string; // release name or "next"
}

function invalidateIssue(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["issues"] });
  qc.invalidateQueries({ queryKey: ["issue"] });
  qc.invalidateQueries({ queryKey: ["issue-activity"] });
}

export function useIssueState() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "update the issue" },
    mutationFn: (v: { id: string } & StateChange) => {
      const { id, ...body } = v;
      return post(`/issues/${id}/state`, body);
    },
    onSuccess: () => invalidateIssue(qc),
  });
}

export function useBulkIssueState() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "update the selected issues" },
    mutationFn: (v: { issue_ids: string[] } & StateChange) => post(`/issues/state`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });
}

export function useAssignIssue() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "change who it is assigned to" },
    mutationFn: (v: { id: string; assignee: string }) => post<{ ok: boolean; assignee: string }>(`/issues/${v.id}/assign`, { assignee: v.assignee }),
    onSuccess: () => invalidateIssue(qc),
  });
}

export function useAddComment() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "add the comment" },
    mutationFn: (v: { id: string; body: string }) => post(`/issues/${v.id}/comments`, { body: v.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue-activity"] }),
  });
}

export const useIssueActivity = (id: string | undefined) =>
  useQuery({ queryKey: ["issue-activity", id], queryFn: () => get<{ items: IssueActivity[] }>(`/issues/${id}/activity`, { limit: 200 }), enabled: !!id });

export const useAssignees = () => useQuery({ queryKey: ["assignees"], queryFn: () => get<{ items: string[]; me: string }>("/issues/assignees"), staleTime: 60_000 });

export interface LogsParams {
  q?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
  after?: string;
  service?: string;
  env?: string;
  level?: string;
  request_id?: string;
  trace_id?: string;
  issue_id?: string;
}

export const useLogs = (p: LogsParams, enabled = true) =>
  useQuery({ queryKey: ["logs", p], queryFn: () => get<LogsResponse>("/logs", p), enabled, placeholderData: keepPreviousData, retry: false });

export const useLogHistogram = (p: LogsParams & { group_by?: string; buckets?: number }, enabled = true) =>
  useQuery({ queryKey: ["logs-histogram", p], queryFn: () => get<Histogram>("/logs/histogram", p), enabled, placeholderData: keepPreviousData, retry: false });

export interface AggregateParams extends LogsParams {
  group_by?: string;
  metric?: string;
  of?: string;
  buckets?: number;
}

export const useLogAggregate = (p: AggregateParams, enabled = true) =>
  useQuery({ queryKey: ["logs-aggregate", p], queryFn: () => get<LogAggregate>("/logs/aggregate", p), enabled, placeholderData: keepPreviousData, retry: false });

export const useLogFacets = (p: LogsParams & { key?: string; fields?: string[]; attr_keys?: number }, enabled = true) =>
  useQuery({ queryKey: ["logs-facets", p], queryFn: () => get<LogFacets>("/logs/facets", p), enabled, placeholderData: keepPreviousData, retry: false });

export const useLogPatterns = (p: LogsParams, enabled = true) =>
  useQuery({ queryKey: ["logs-patterns", p], queryFn: () => get<{ items: LogPattern[] }>("/logs/patterns", { ...p, limit: 100 }), enabled, retry: false });

/** One message with its request body (the lists leave bodies out). */
export const useLogEvent = (eventId: string | undefined, enabled = true) =>
  useQuery({ queryKey: ["log-event", eventId], queryFn: () => get<LogEvent>(`/logs/${encodeURIComponent(eventId!)}`), enabled: enabled && !!eventId, staleTime: 5 * 60_000 });

export const useLogContext = (eventId: string | undefined) =>
  useQuery({ queryKey: ["logs-context", eventId], queryFn: () => get<LogContext>("/logs/context", { event_id: eventId, before: 40, after: 40 }), enabled: !!eventId });

export const useLogFields = () =>
  useQuery({ queryKey: ["logs-fields"], queryFn: () => get<{ fields: string[]; facets: string[]; histogram_group: string[]; syntax: string[] }>("/logs/fields"), staleTime: Infinity });

export const useSavedViews = (p: { pinned?: boolean } = {}) =>
  useQuery({ queryKey: ["saved-views", p], queryFn: () => get<{ items: SavedView[] }>("/saved-views", p.pinned ? { pinned: 1 } : undefined) });

export function useSaveView() {
  const qc = useQueryClient();
  return useMutation({ meta: { inlineError: true },
    mutationFn: (v: Partial<SavedView>) => post<{ view: SavedView }>("/saved-views", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-views"] }),
  });
}

export function useDeleteView() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "delete the saved search" }, mutationFn: (id: string) => del(`/saved-views/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-views"] }) });
}

/** Pin a saved chart to Dashboards, or unpin it. */
export function usePinView() {
  const qc = useQueryClient();
  return useMutation({
    meta: { action: "pin the chart" },
    mutationFn: ({ id, pinned }: { id: string; pinned: boolean }) => post<{ view: SavedView }>(`/saved-views/${id}/pin`, { pinned }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-views"] }),
  });
}

export interface AnalyticsParams {
  from?: string;
  to?: string;
  service?: string;
  env?: string;
}

export const useErrorAnalytics = (p: AnalyticsParams, enabled = true) =>
  useQuery({ queryKey: ["analytics-errors", p], queryFn: () => get<ErrorAnalytics>("/analytics/errors", p), enabled, placeholderData: keepPreviousData });

export const useUsageAnalytics = (p: AnalyticsParams & { sort?: "requests" | "people" }, enabled = true) =>
  useQuery({ queryKey: ["analytics-usage", p], queryFn: () => get<UsageAnalytics>("/analytics/usage", p), enabled, placeholderData: keepPreviousData });

export interface ChartParams extends AnalyticsParams {
  source: ChartSource;
  q?: string;
  group_by?: string;
  metric?: string;
  of?: string;
  limit?: number;
  buckets?: number;
}

export const useChartAggregate = (p: ChartParams, enabled = true) =>
  useQuery({ queryKey: ["analytics-aggregate", p], queryFn: () => get<LogAggregate>("/analytics/aggregate", p), enabled, placeholderData: keepPreviousData, retry: false });

export const useChartFields = (source: ChartSource) =>
  useQuery({ queryKey: ["analytics-fields", source], queryFn: () => get<ChartFields>("/analytics/fields", { source }), staleTime: Infinity });

export const useOverview = (p: { from?: string; to?: string; service?: string; env?: string; compare?: "previous" }, refetchInterval?: number | false) =>
  useQuery({ queryKey: ["overview", p], queryFn: () => get<{ overview: Overview; previous?: Overview }>("/metrics/overview", p), placeholderData: keepPreviousData, refetchInterval: refetchInterval ?? false });

export const useServiceHealth = (p: { from?: string; to?: string; env?: string }, refetchInterval?: number | false) =>
  useQuery({ queryKey: ["service-health", p], queryFn: () => get<{ items: ServiceHealth[] }>("/metrics/services", p), placeholderData: keepPreviousData, refetchInterval: refetchInterval ?? false });

export const useAttention = (p: { from?: string; to?: string; service?: string; env?: string; limit?: number }, refetchInterval?: number | false) =>
  useQuery({
    queryKey: ["attention", p],
    queryFn: () => get<{ items: AttentionItem[]; counts: Record<AttentionReason, number> }>("/metrics/attention", p),
    placeholderData: keepPreviousData,
    refetchInterval: refetchInterval ?? false,
  });

export const useReleases = (p: { from?: string; to?: string; limit?: number }) =>
  useQuery({ queryKey: ["releases", p], queryFn: () => get<{ items: Release[] }>("/releases", p), placeholderData: keepPreviousData });

export const useErrorTrend = (p: { from?: string; to?: string; service?: string; env?: string; buckets?: number }) =>
  useQuery({ queryKey: ["error-trend", p], queryFn: () => get<{ bucket_seconds: number; points: TimeSeriesPoint[] }>("/metrics/errors", p), placeholderData: keepPreviousData });

export const useLogVolume = (p: { from?: string; to?: string; service?: string; env?: string }) =>
  useQuery({ queryKey: ["log-volume", p], queryFn: () => get<Histogram>("/metrics/logs", p), placeholderData: keepPreviousData });

export const useTopIssues = (p: { from?: string; to?: string; service?: string; env?: string }) =>
  useQuery({ queryKey: ["top-issues", p], queryFn: () => get<{ items: IssueListResponse["items"] }>("/metrics/top-issues", { ...p, limit: 8 }), placeholderData: keepPreviousData });

export interface TracesParams {
  q?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
  include?: string;
}

export const useTraces = (p: TracesParams, enabled = true) =>
  useQuery({ queryKey: ["traces", p], queryFn: () => get<TracesResponse>("/traces", p), enabled, placeholderData: keepPreviousData, retry: false });

export const useTrace = (id: string | undefined, at?: string) =>
  useQuery({ queryKey: ["trace", id, at], queryFn: () => get<TraceDetail>(`/traces/${id}`, { at }), enabled: !!id, retry: false });

export const useTraceFields = () =>
  useQuery({ queryKey: ["trace-fields"], queryFn: () => get<{ fields: string[]; syntax: string[] }>("/traces/fields"), staleTime: Infinity });

export const useRequestStats = (p: { from?: string; to?: string; service?: string; env?: string; source?: string; compare?: "previous"; buckets?: number }, enabled = true) =>
  useQuery({ queryKey: ["request-stats", p], queryFn: () => get<RequestStatsResponse>("/metrics/requests", { ...p, limit: 10 }), placeholderData: keepPreviousData, enabled });

export const useMetricNames = () =>
  useQuery({ queryKey: ["metric-names"], queryFn: () => get<{ items: MetricInfo[]; aggs: Record<string, string[]> }>("/metrics/names"), staleTime: 60_000 });

export interface MetricSeriesParams {
  name: string;
  agg?: string;
  group_by?: string;
  from?: string;
  to?: string;
  service?: string;
  env?: string;
  limit?: number;
  buckets?: number;
}

export const useMetricSeries = (p: MetricSeriesParams, enabled = true) =>
  useQuery({ queryKey: ["metric-series", p], queryFn: () => get<MetricSeries>("/metrics/series", p), enabled: enabled && !!p.name, placeholderData: keepPreviousData, retry: false });

export const useProjectUsage = (id: string | undefined, enabled = true) =>
  useQuery({ queryKey: ["project-usage", id], queryFn: () => get<ProjectUsage>(`/projects/${id}/usage`, { days: 30 }), enabled: enabled && !!id, staleTime: 30_000 });

export const useProjectKeys = (id: string | undefined, enabled = true) =>
  useQuery({ queryKey: ["project-keys", id], queryFn: () => get<{ items: APIKey[]; scopes: string[] }>(`/projects/${id}/keys`), enabled: enabled && !!id, staleTime: 15_000 });

export const useAudit = (enabled: boolean) => useQuery({ queryKey: ["audit"], queryFn: () => get<{ items: AuditEntry[] }>("/admin/audit", { limit: 50 }), enabled });

export const useProjects = () => useQuery({ queryKey: ["projects"], queryFn: () => get<{ items: Project[] }>("/projects") });

export function useProjectMutations() {
  const qc = useQueryClient();
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["projects"] });
    qc.invalidateQueries({ queryKey: ["me"] });
  };
  return {
    create: useMutation({ meta: { inlineError: true }, mutationFn: (v: Partial<Project>) => post<{ project: Project; api_key: string }>("/projects", v), onSuccess: invalidate }),
    update: useMutation({ meta: { inlineError: true }, mutationFn: (v: Partial<Project> & { id: string }) => post<{ project: Project }>(`/projects/${v.id}`, v), onSuccess: invalidate }),
    createKey: useMutation({
      meta: { inlineError: true },
      mutationFn: (v: { id: string; name: string; scopes: string[] }) => post<{ key: APIKey; api_key: string }>(`/projects/${v.id}/keys`, { name: v.name, scopes: v.scopes }),
      onSuccess: (_r, v) => qc.invalidateQueries({ queryKey: ["project-keys", v.id] }),
    }),
    revokeKey: useMutation({
      meta: { action: "disable the key" },
      mutationFn: (v: { id: string; key_id: string }) => post<{ ok: boolean }>(`/projects/${v.id}/keys/${v.key_id}/revoke`),
      onSuccess: (_r, v) => qc.invalidateQueries({ queryKey: ["project-keys", v.id] }),
    }),
    remove: useMutation({ meta: { inlineError: true }, mutationFn: (id: string) => del<{ status: string }>(`/projects/${id}`), onSuccess: invalidate }),
    purgeUser: useMutation({ meta: { inlineError: true }, mutationFn: (v: { id: string; user_id_hash?: string; user_email?: string }) => post<{ status: string }>(`/projects/${v.id}/purge-user`, { user_id_hash: v.user_id_hash, user_email: v.user_email }) }),
    select: useMutation({ meta: { action: "switch project" }, mutationFn: (project_id: string) => post("/project/select", { project_id }), onSuccess: () => qc.invalidateQueries() }),
  };
}

/** Delete every message of a project. Everything on screen may have changed, so all of it reloads. */
export function useClearLogs() {
  const qc = useQueryClient();
  return useMutation({
    meta: { inlineError: true },
    mutationFn: (projectId: string) => del<{ status: string }>(`/logs?project_id=${encodeURIComponent(projectId)}`, { confirm: projectId }),
    onSuccess: () => qc.invalidateQueries(),
  });
}

export const useAlertRules = () => useQuery({ queryKey: ["alert-rules"], queryFn: () => get<{ items: AlertRule[]; conditions: string[] }>("/alerts/rules") });
export const useAlertFirings = (limit = 50, enabled = true) => useQuery({ queryKey: ["alert-firings", limit], queryFn: () => get<{ items: AlertFiring[] }>("/alerts/firings", { limit }), enabled });
export const previewAlertRule = (p: { query: string; window_seconds: number; service?: string; env?: string; http_route?: string; http_method?: string }) =>
  get<{ count: number; window_start: string; window_end: string }>("/alerts/rules/preview", p);

/** The recap a daily_recap rule would send now, for yesterday. */
export const previewRecap = (p: { name?: string; service?: string; env?: string; timezone?: string }) => get<{ text: string }>("/alerts/rules/recap-preview", p);

export function useSaveAlertRule() {
  const qc = useQueryClient();
  return useMutation({ meta: { inlineError: true }, mutationFn: (rule: Partial<AlertRule>) => post<{ rule: AlertRule }>("/alerts/rules", { rule }), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-rules"] }) });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({ meta: { inlineError: true }, mutationFn: (api_key: string) => post("/login", { api_key }), onSuccess: () => qc.invalidateQueries() });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({ meta: { action: "sign out" }, mutationFn: () => post("/logout"), onSuccess: () => qc.clear() });
}
