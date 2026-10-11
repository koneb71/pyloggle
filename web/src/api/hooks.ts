import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { del, get, post } from "./client";
import type {
  AlertFiring,
  AlertRule,
  Histogram,
  IssueActivity,
  LogAggregate,
  MetricInfo,
  MetricSeries,
  RequestStatsResponse,
  TraceDetail,
  TracesResponse,
  IssueDetail,
  IssueListResponse,
  LogContext,
  LogFacets,
  LogPattern,
  LogsResponse,
  Me,
  Occurrence,
  Overview,
  Project,
  SavedView,
  TimeSeriesPoint,
} from "./types";

export const useMe = () => useQuery({ queryKey: ["me"], queryFn: () => get<Me>("/me"), staleTime: 60_000, retry: false });

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

export const useOccurrences = (id: string | undefined, limit = 50) =>
  useQuery({
    queryKey: ["occurrences", id, limit],
    queryFn: () => get<{ items: Occurrence[]; next_before?: string }>(`/issues/${id}/occurrences`, { limit }),
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
  return useMutation({
    mutationFn: (v: { id: string } & StateChange) => {
      const { id, ...body } = v;
      return post(`/issues/${id}/state`, body);
    },
    onSuccess: () => invalidateIssue(qc),
  });
}

export function useBulkIssueState() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issue_ids: string[] } & StateChange) => post(`/issues/state`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });
}

export function useAssignIssue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; assignee: string }) => post<{ ok: boolean; assignee: string }>(`/issues/${v.id}/assign`, { assignee: v.assignee }),
    onSuccess: () => invalidateIssue(qc),
  });
}

export function useAddComment() {
  const qc = useQueryClient();
  return useMutation({
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

export const useLogContext = (eventId: string | undefined) =>
  useQuery({ queryKey: ["logs-context", eventId], queryFn: () => get<LogContext>("/logs/context", { event_id: eventId, before: 40, after: 40 }), enabled: !!eventId });

export const useLogFields = () =>
  useQuery({ queryKey: ["logs-fields"], queryFn: () => get<{ fields: string[]; facets: string[]; histogram_group: string[]; syntax: string[] }>("/logs/fields"), staleTime: Infinity });

export const useSavedViews = () => useQuery({ queryKey: ["saved-views"], queryFn: () => get<{ items: SavedView[] }>("/saved-views") });

export function useSaveView() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Partial<SavedView>) => post<{ view: SavedView }>("/saved-views", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-views"] }),
  });
}

export function useDeleteView() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id: string) => del(`/saved-views/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["saved-views"] }) });
}

export const useOverview = (p: { from?: string; to?: string; service?: string; env?: string }) =>
  useQuery({ queryKey: ["overview", p], queryFn: () => get<{ overview: Overview }>("/metrics/overview", p), placeholderData: keepPreviousData });

export const useErrorTrend = (p: { from?: string; to?: string; service?: string; env?: string }) =>
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

export const useRequestStats = (p: { from?: string; to?: string; service?: string; env?: string; source?: string }) =>
  useQuery({ queryKey: ["request-stats", p], queryFn: () => get<RequestStatsResponse>("/metrics/requests", { ...p, limit: 10 }), placeholderData: keepPreviousData });

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

export const useProjects = () => useQuery({ queryKey: ["projects"], queryFn: () => get<{ items: Project[] }>("/projects") });

export function useProjectMutations() {
  const qc = useQueryClient();
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["projects"] });
    qc.invalidateQueries({ queryKey: ["me"] });
  };
  return {
    create: useMutation({ mutationFn: (v: Partial<Project>) => post<{ project: Project; api_key: string }>("/projects", v), onSuccess: invalidate }),
    update: useMutation({ mutationFn: (v: Partial<Project> & { id: string }) => post<{ project: Project }>(`/projects/${v.id}`, v), onSuccess: invalidate }),
    rotate: useMutation({ mutationFn: (id: string) => post<{ project: Project; api_key: string }>(`/projects/${id}/rotate-key`), onSuccess: invalidate }),
    select: useMutation({ mutationFn: (project_id: string) => post("/project/select", { project_id }), onSuccess: () => qc.invalidateQueries() }),
  };
}

export const useAlertRules = () => useQuery({ queryKey: ["alert-rules"], queryFn: () => get<{ items: AlertRule[]; conditions: string[] }>("/alerts/rules") });
export const useAlertFirings = () => useQuery({ queryKey: ["alert-firings"], queryFn: () => get<{ items: AlertFiring[] }>("/alerts/firings", { limit: 50 }) });
export const previewAlertRule = (p: { query: string; window_seconds: number; service?: string; env?: string; http_route?: string; http_method?: string }) =>
  get<{ count: number; window_start: string; window_end: string }>("/alerts/rules/preview", p);

export function useSaveAlertRule() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (rule: Partial<AlertRule>) => post<{ rule: AlertRule }>("/alerts/rules", { rule }), onSuccess: () => qc.invalidateQueries({ queryKey: ["alert-rules"] }) });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (api_key: string) => post("/login", { api_key }), onSuccess: () => qc.invalidateQueries() });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => post("/logout"), onSuccess: () => qc.clear() });
}
