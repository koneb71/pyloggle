// Mirrors internal/api/types.go and internal/model (JSON field names).

export type Level = "debug" | "info" | "warn" | "error" | "fatal" | string;
export type Status = "open" | "resolved" | "ignored";

export interface Me {
  auth_mode: "disabled" | "azure" | string;
  user?: { sub: string; name?: string; email?: string };
  is_admin: boolean;
  project?: { id: string; name: string };
  projects: { id: string; name: string }[];
  features: { alerting: boolean; public_url?: string };
}

export interface IssueSummary {
  issue_id: string;
  service: string;
  env: string;
  project_id: string;
  title: string;
  culprit: string;
  last_message: string;
  level: Level;
  status: Status;
  is_regressed: boolean;
  assignee?: string;
  status_updated_at: string;
  first_seen: string;
  last_seen: string;
  occurrence_count: number;
  affected_users: number;
  exception_type: string;
  exception_module?: string;
  last_release?: string;
  stack_fingerprint: string;
  sparkline?: number[];
}

export interface IssueListResponse {
  items: IssueSummary[];
  counts: Record<string, number>;
}

export interface Frame {
  filename: string;
  abs_path?: string;
  module?: string;
  function: string;
  lineno: number;
  colno?: number;
  context: string;
  pre_context?: string[];
  post_context?: string[];
  vars?: Record<string, string>;
  in_app: boolean | null;
}

export interface ChainedException {
  type: string;
  module?: string;
  message: string;
  frames: Frame[];
  is_cause: boolean;
  relation?: "cause" | "context" | "group" | string;
  notes?: string[];
}

export interface Breadcrumb {
  ts: string;
  type?: string;
  category?: string;
  level?: string;
  message: string;
  data?: Record<string, string>;
}

export interface HTTPInfo {
  method: string;
  route: string;
  status: number;
  url?: string;
  query?: string;
  headers?: Record<string, string>;
  body?: string;
  user_agent: string;
  remote_addr: string;
  server_addr: string;
  client_ip?: string;
}

export interface Occurrence {
  ts: string;
  event_id: string;
  issue_id: string;
  service: string;
  env: string;
  project_id: string;
  level: Level;
  logger?: string;
  message: string;
  title: string;
  culprit: string;
  exception_type: string;
  exception_module?: string;
  exception_message: string;
  stack_fingerprint: string;
  fingerprint_version: number;
  frames: Frame[];
  chain?: ChainedException[];
  notes?: string[];
  breadcrumbs?: Breadcrumb[];
  trace_id: string;
  span_id?: string;
  request_id: string;
  http_method: string;
  http_route: string;
  http_status: number;
  http?: HTTPInfo;
  user_id_hash: string;
  user?: { id_hash: string; id?: string; email?: string; username?: string; ip?: string };
  runtime?: { name?: string; version?: string; platform?: string; pid?: number; thread?: string };
  sdk?: { name: string; version: string };
  release?: string;
  hostname?: string;
  source?: string;
  mechanism?: { type: string; handled: boolean | null };
  tags?: Record<string, string>;
  attrs: Record<string, string>;
  extra?: unknown;
  prev_event_id?: string;
  next_event_id?: string;
}

export interface FacetCount {
  value: string;
  count: number;
  users?: number;
}

export interface TimeSeriesPoint {
  ts: string;
  count: number;
}

export interface IssueDetail {
  issue: IssueSummary;
  breakdowns: Record<string, FacetCount[]>;
  trend: TimeSeriesPoint[];
  latest?: Occurrence;
}

export interface LogEvent {
  ts: string;
  event_id: string;
  service: string;
  env: string;
  level: Level;
  logger: string;
  message: string;
  trace_id: string;
  span_id: string;
  request_id: string;
  project_id: string;
  issue_id?: string;
  release?: string;
  hostname?: string;
  source?: string;
  http_method?: string;
  http_route?: string;
  http_status?: number;
  http_url?: string;
  duration_ms?: number;
  user_id_hash?: string;
  exception_type?: string;
  attrs: Record<string, string>;
  tags?: Record<string, string>;
}

export interface LogsResponse {
  items: LogEvent[];
  next_cursor?: string;
  newest_ts?: string;
  from: string;
  to: string;
}

export interface HistogramBucket {
  ts: string;
  total: number;
  counts: Record<string, number>;
}

export interface Histogram {
  bucket_seconds: number;
  group_by: string;
  series: string[];
  buckets: HistogramBucket[];
}

export interface LogFacets {
  fields: Record<string, FacetCount[]>;
  attr_keys: FacetCount[];
  tag_keys: FacetCount[];
  values?: FacetCount[];
}

export interface LogPattern {
  pattern: string;
  count: number;
  sample: string;
  level: string;
  services: string[];
}

export interface LogContext {
  anchor: LogEvent;
  before: LogEvent[];
  after: LogEvent[];
}

export interface Overview {
  events: number;
  errors: number;
  open_issues: number;
  new_issues: number;
  affected_users: number;
  services: number;
}

export interface SavedView {
  id: string;
  project_id: string;
  user_id: string;
  name: string;
  description: string;
  query: string; // JSON of {q, range, group_by}
  is_public: boolean;
  is_pinned: boolean;
  created_at: string;
  updated_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  max_events_per_second: number;
  max_storage_gb: number;
  api_key_hash: string;
  api_key_created_at: string;
  created_at: string;
  updated_at: string;
}

export interface AlertRule {
  id: string;
  project_id: string;
  name: string;
  description: string;
  service: string;
  env: string;
  http_route: string;
  http_method: string;
  condition_type: "error_rate" | "issue_spike" | "new_issue" | "regression" | string;
  threshold: number;
  time_window_seconds: number;
  cooldown_seconds: number;
  enabled: boolean;
  notification_channels: string[];
  notification_emails: string[];
  webhook_urls: string[];
  telegram_chat_ids: string[];
  created_at: string;
  updated_at: string;
}

export interface AlertFiring {
  id: string;
  rule_id: string;
  project_id: string;
  service: string;
  env: string;
  condition_type: string;
  fired_at: string;
  window_start: string;
  window_end: string;
  payload: unknown;
}

export interface ApiError {
  error: string;
  message?: string;
  pos?: number;
  login_url?: string;
}
