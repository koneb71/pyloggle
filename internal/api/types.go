// Package api holds the JSON types returned by the HTTP API and the UI API.
package api

import (
	"encoding/json"
	"time"

	"github.com/koneb71/pyloggle/internal/model"
)

type LogEvent struct {
	TS        time.Time `json:"ts"`
	EventID   string    `json:"event_id"`
	Service   string    `json:"service"`
	Env       string    `json:"env"`
	Level     string    `json:"level"`
	Logger    string    `json:"logger"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id"`
	SpanID    string    `json:"span_id"`
	RequestID string    `json:"request_id"`
	ProjectID string    `json:"project_id"`
	IssueID   string    `json:"issue_id,omitempty"`
	Release   string    `json:"release,omitempty"`
	Hostname  string    `json:"hostname,omitempty"`
	Source    string    `json:"source,omitempty"`

	HTTPMethod string  `json:"http_method,omitempty"`
	HTTPRoute  string  `json:"http_route,omitempty"`
	HTTPStatus uint16  `json:"http_status,omitempty"`
	HTTPURL    string  `json:"http_url,omitempty"`
	DurationMS float32 `json:"duration_ms,omitempty"`
	// HTTPBody is the request body, only on GET /logs/{event_id}: lists leave it out.
	HTTPBody string `json:"http_body,omitempty"`

	UserIDHash    string `json:"user_id_hash,omitempty"`
	ExceptionType string `json:"exception_type,omitempty"`

	Attrs map[string]string `json:"attrs"`
	Tags  map[string]string `json:"tags,omitempty"`
}

type SavedSearch struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Query       string    `json:"query"`
	IsPublic    bool      `json:"is_public"`
	IsPinned    bool      `json:"is_pinned"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
}

type SavedSearchUpsertRequest struct {
	Search SavedSearch `json:"search"`
}

type TimeSeriesPoint struct {
	TS    time.Time `json:"ts"`
	Count uint64    `json:"count"`
}

// IssueSummary is one row of the issues list.
type IssueSummary struct {
	IssueID     string `json:"issue_id"`
	Service     string `json:"service"`
	Env         string `json:"env"`
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Culprit     string `json:"culprit"`
	LastMessage string `json:"last_message"`
	Level       string `json:"level"`
	Status      string `json:"status"`
	IsRegressed bool   `json:"is_regressed"`
	Assignee    string `json:"assignee,omitempty"`
	// Snooze / release-resolution details (ignored and resolved issues).
	SnoozeUntil       *time.Time `json:"snooze_until,omitempty"`
	SnoozeCount       uint64     `json:"snooze_count,omitempty"`
	ResolvedInRelease string     `json:"resolved_in_release,omitempty"`
	StatusUpdatedAt   time.Time  `json:"status_updated_at"`
	FirstSeen         time.Time  `json:"first_seen"`
	LastSeen          time.Time  `json:"last_seen"`
	OccurrenceCount   uint64     `json:"occurrence_count"`
	AffectedUsers     uint64     `json:"affected_users"`
	ExceptionType     string     `json:"exception_type"`
	ExceptionModule   string     `json:"exception_module,omitempty"`
	LastRelease       string     `json:"last_release,omitempty"`
	// Legacy fields kept for API compatibility (derived from the culprit where possible).
	TopFilename      string `json:"top_filename"`
	TopLineno        uint32 `json:"top_lineno"`
	TopFunction      string `json:"top_function"`
	StackFingerprint string `json:"stack_fingerprint"`
	// Sparkline holds hourly counts for the last 24h when requested.
	Sparkline []uint64 `json:"sparkline,omitempty"`
}

// IssueListResponse is the issues list with per-status counts.
type IssueListResponse struct {
	Items  []IssueSummary    `json:"items"`
	Counts map[string]uint64 `json:"counts"`
}

// IssueDetail bundles everything the issue page needs beside the occurrences.
type IssueDetail struct {
	Issue      IssueSummary            `json:"issue"`
	Breakdowns map[string][]FacetCount `json:"breakdowns"`
	Trend      []TimeSeriesPoint       `json:"trend"`
	Latest     *ErrorOccurrence        `json:"latest,omitempty"`
}

type IssueStateUpdate struct {
	ProjectID string `json:"project_id"`
	IssueID   string `json:"issue_id"`
	Service   string `json:"service"`
	Env       string `json:"env"`
	Status    string `json:"status"` // open|resolved|ignored
	UpdatedBy string `json:"updated_by"`
	Comment   string `json:"comment"`
}

// IssueStateChange is the storage-level state write (regression fields included).
type IssueStateChange struct {
	ProjectID string
	Service   string
	Env       string
	IssueID   string
	// Status "" keeps the current status (assign-only or comment-only changes).
	Status        string
	Regressed     bool
	RegressedFrom string
	UpdatedBy     string
	Comment       string
	// Kind overrides the derived activity kind (status | assign | comment | snooze | unsnooze |
	// regression | resolve_in_release).
	Kind    string
	Release string // release involved (regressions, resolve-in-release)
	// Optional fields: nil leaves the stored value untouched.
	Assignee          *string
	SnoozeUntil       *time.Time
	SnoozeCount       *uint64
	ResolvedInRelease *string
}

// ErrorOccurrence is one stored error event. The legacy flat fields stay for API compatibility;
// the structured fields carry the v2 report (chain, context lines, locals, breadcrumbs, ...).
type ErrorOccurrence struct {
	TS               time.Time                `json:"ts"`
	EventID          string                   `json:"event_id"`
	IssueID          string                   `json:"issue_id"`
	Service          string                   `json:"service"`
	Env              string                   `json:"env"`
	ProjectID        string                   `json:"project_id"`
	Level            string                   `json:"level"`
	Logger           string                   `json:"logger,omitempty"`
	Message          string                   `json:"message"`
	Title            string                   `json:"title"`
	Culprit          string                   `json:"culprit"`
	ExceptionType    string                   `json:"exception_type"`
	ExceptionModule  string                   `json:"exception_module,omitempty"`
	ExceptionMessage string                   `json:"exception_message"`
	StackFingerprint string                   `json:"stack_fingerprint"`
	FingerprintVer   uint8                    `json:"fingerprint_version"`
	StackFrames      []map[string]any         `json:"stack_frames"` // legacy shape
	Frames           []model.Frame            `json:"frames"`
	Chain            []model.ChainedException `json:"chain,omitempty"`
	Notes            []string                 `json:"notes,omitempty"`
	Breadcrumbs      []model.Breadcrumb       `json:"breadcrumbs,omitempty"`
	TraceID          string                   `json:"trace_id"`
	SpanID           string                   `json:"span_id,omitempty"`
	RequestID        string                   `json:"request_id"`
	HTTPMethod       string                   `json:"http_method"`
	HTTPRoute        string                   `json:"http_route"`
	HTTPStatus       uint16                   `json:"http_status"`
	HTTP             *model.HTTPInfo          `json:"http,omitempty"`
	UserIDHash       string                   `json:"user_id_hash"`
	User             *model.UserInfo          `json:"user,omitempty"`
	Runtime          *model.RuntimeInfo       `json:"runtime,omitempty"`
	SDK              *model.SDKInfo           `json:"sdk,omitempty"`
	Release          string                   `json:"release,omitempty"`
	Hostname         string                   `json:"hostname,omitempty"`
	Source           string                   `json:"source,omitempty"`
	Mechanism        *model.Mechanism         `json:"mechanism,omitempty"`
	Tags             map[string]string        `json:"tags,omitempty"`
	Attrs            map[string]string        `json:"attrs"`
	Extra            json.RawMessage          `json:"extra,omitempty"`
	PrevEventID      string                   `json:"prev_event_id,omitempty"`
	NextEventID      string                   `json:"next_event_id,omitempty"`
}

// FacetCount is one value of a facet with its count.
type FacetCount struct {
	Value string `json:"value"`
	Count uint64 `json:"count"`
	Users uint64 `json:"users,omitempty"`
}

// LogFacets are the value counts for the explorer sidebar under the current query.
type LogFacets struct {
	Fields   map[string][]FacetCount `json:"fields"`
	AttrKeys []FacetCount            `json:"attr_keys"`
	TagKeys  []FacetCount            `json:"tag_keys"`
	Values   []FacetCount            `json:"values,omitempty"` // values of one requested attr/tag key
}

// HistogramBucket is one bar of the timeline, split by the requested group.
type HistogramBucket struct {
	TS     time.Time         `json:"ts"`
	Total  uint64            `json:"total"`
	Counts map[string]uint64 `json:"counts"`
}

type Histogram struct {
	BucketSeconds int               `json:"bucket_seconds"`
	GroupBy       string            `json:"group_by"`
	Series        []string          `json:"series"`
	Buckets       []HistogramBucket `json:"buckets"`
}

// LogPattern is one clustered message template.
type LogPattern struct {
	Pattern  string   `json:"pattern"`
	Count    uint64   `json:"count"`
	Sample   string   `json:"sample"`
	Level    string   `json:"level"`
	Services []string `json:"services"`
}

// LogContext is a log line with its neighbours.
type LogContext struct {
	Anchor LogEvent   `json:"anchor"`
	Before []LogEvent `json:"before"`
	After  []LogEvent `json:"after"`
}

// Overview feeds the dashboard stat tiles.
type Overview struct {
	Events        uint64 `json:"events"`
	Errors        uint64 `json:"errors"`
	OpenIssues    uint64 `json:"open_issues"`
	NewIssues     uint64 `json:"new_issues"`
	AffectedUsers uint64 `json:"affected_users"`
	Services      uint64 `json:"services"`
}

// ServiceHealth is one app's traffic and error picture over a range (the Overview "Apps" list).
// Requests come from request lines, errors from issue occurrences, volume from log lines.
type ServiceHealth struct {
	Service string   `json:"service"`
	Envs    []string `json:"envs"`
	// Status is "failing", "degraded", "silent" (nothing recent) or "ok".
	Status    string    `json:"status"`
	Requests  uint64    `json:"requests"`
	Failed    uint64    `json:"failed"`
	FailedPct float64   `json:"failed_pct"`
	RPM       float64   `json:"rpm"`
	P50MS     float64   `json:"p50_ms"`
	P95MS     float64   `json:"p95_ms"`
	Errors    uint64    `json:"errors"`
	Logs      uint64    `json:"logs"`
	LastSeen  time.Time `json:"last_seen"`
}

// Release is one version the project has reported, with its errors in the requested range.
type Release struct {
	Release   string    `json:"release"`
	FirstSeen time.Time `json:"first_seen"`
	Errors    uint64    `json:"errors"`
	Issues    uint64    `json:"issues"`
}

// AttentionItem is an issue the Overview flags, with why: "regressed", "new" or "spike".
type AttentionItem struct {
	Issue         IssueSummary `json:"issue"`
	Reasons       []string     `json:"reasons"`
	Count         uint64       `json:"count"`
	PreviousCount uint64       `json:"previous_count"`
}

// AlertRule is the user-managed rule stored in ClickHouse (table: alert_rules).
// Note: updates are modeled as new inserts; reads return the latest row per (project_id,id).
type AlertRule struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`

	Service    string `json:"service"`
	Env        string `json:"env"`
	HTTPRoute  string `json:"http_route"`
	HTTPMethod string `json:"http_method"`

	ConditionType     string  `json:"condition_type"`       // error_rate | issue_spike | new_issue | regression | log_query
	Query             string  `json:"query,omitempty"`      // log_query: explorer search syntax
	Comparison        string  `json:"comparison,omitempty"` // log_query: gt | gte | lt | lte | eq (default gt)
	Threshold         float64 `json:"threshold"`
	TimeWindowSeconds uint32  `json:"time_window_seconds"`
	CooldownSeconds   uint32  `json:"cooldown_seconds"`
	Enabled           bool    `json:"enabled"`

	// daily_recap rules: the local hour (0-23) to send at, and the IANA time zone that
	// defines "yesterday".
	SendHour uint8  `json:"send_hour"`
	Timezone string `json:"timezone,omitempty"`

	NotificationChannels []string `json:"notification_channels"` // email, webhook, telegram, slack
	NotificationEmails   []string `json:"notification_emails"`
	WebhookURLs          []string `json:"webhook_urls"`
	TelegramChatIDs      []string `json:"telegram_chat_ids"`
	SlackWebhookURLs     []string `json:"slack_webhook_urls"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AlertRuleUpsertRequest struct {
	Rule AlertRule `json:"rule"`
}

// AlertFiring is one recorded alert.
type AlertFiring struct {
	ID            string          `json:"id"`
	RuleID        string          `json:"rule_id"`
	ProjectID     string          `json:"project_id"`
	Service       string          `json:"service"`
	Env           string          `json:"env"`
	ConditionType string          `json:"condition_type"`
	FiredAt       time.Time       `json:"fired_at"`
	WindowStart   time.Time       `json:"window_start"`
	WindowEnd     time.Time       `json:"window_end"`
	Count         uint64          `json:"count"`
	URL           string          `json:"url,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

// IssueActivity is one row of an issue's history (append-only).
type IssueActivity struct {
	ID      string    `json:"id"`
	IssueID string    `json:"issue_id"`
	TS      time.Time `json:"ts"`
	Kind    string    `json:"kind"`
	Actor   string    `json:"actor"`
	From    string    `json:"from,omitempty"`
	To      string    `json:"to,omitempty"`
	Comment string    `json:"comment,omitempty"`
	Release string    `json:"release,omitempty"`
}

// Project is the multi-tenant container used for API key auth + quotas.
type Project struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	MaxEventsPerSec uint32    `json:"max_events_per_second"`
	MaxStorageGB    uint32    `json:"max_storage_gb"`
	RetentionDays   uint16    `json:"retention_days"`
	APIKeyHash      string    `json:"api_key_hash"`
	APIKeyCreatedAt time.Time `json:"api_key_created_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Deleting        bool      `json:"deleting,omitempty"`
}

type ProjectCreateRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MaxEventsPerSec uint32 `json:"max_events_per_second"`
	MaxStorageGB    uint32 `json:"max_storage_gb"`
	RetentionDays   uint16 `json:"retention_days"`
}

type ProjectCreateResponse struct {
	Project Project `json:"project"`
	APIKey  string  `json:"api_key"`
}

type ProjectUpdateRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MaxEventsPerSec uint32 `json:"max_events_per_second"`
	MaxStorageGB    uint32 `json:"max_storage_gb"`
	RetentionDays   uint16 `json:"retention_days"`
}

type ProjectRotateKeyRequest struct {
	ID string `json:"id"`
}

type ProjectRotateKeyResponse struct {
	Project Project `json:"project"`
	APIKey  string  `json:"api_key"`
}

// APIKey is one scoped key of a project. The secret is returned once, at creation.
type APIKey struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	CreatedBy  string     `json:"created_by,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	RevokedBy  string     `json:"revoked_by,omitempty"`
}

type APIKeyCreateRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type APIKeyCreateResponse struct {
	Key    APIKey `json:"key"`
	APIKey string `json:"api_key"`
}

// AggregateGroup is one series of an analytics query: a group key with its row count, the
// metric over the whole range and the metric per bucket (null where the bucket has no rows).
type AggregateGroup struct {
	Key    string     `json:"key"`
	Rows   uint64     `json:"rows"`
	Value  *float64   `json:"value"`
	Series []*float64 `json:"series,omitempty"`
}

// LogAggregate is the analytics view of the explorer selection: a metric (count, avg, p95...)
// over an optional field, split by a group-by field, bucketed over time.
type LogAggregate struct {
	BucketSeconds int              `json:"bucket_seconds"`
	GroupBy       string           `json:"group_by"`
	Metric        string           `json:"metric"`
	Of            string           `json:"of,omitempty"`
	Buckets       []time.Time      `json:"buckets"`
	Total         AggregateGroup   `json:"total"`
	Groups        []AggregateGroup `json:"groups"`
	Other         *AggregateGroup  `json:"other,omitempty"`
	Histogram     *Histogram       `json:"histogram,omitempty"` // count metric only: the stacked timeline shape
}

// RequestPoint is one bucket of request traffic from request_stats_minutely.
type RequestPoint struct {
	TS       time.Time `json:"ts"`
	Requests uint64    `json:"requests"`
	Errors   uint64    `json:"errors"`
	P50MS    float64   `json:"p50_ms"`
	P95MS    float64   `json:"p95_ms"`
	P99MS    float64   `json:"p99_ms"`
}

// RouteStats summarizes one method+route (or, for totals, everything) over the range.
type RouteStats struct {
	Method   string  `json:"method,omitempty"`
	Route    string  `json:"route,omitempty"`
	Requests uint64  `json:"requests"`
	Errors   uint64  `json:"errors"`
	ErrorPct float64 `json:"error_pct"`
	RPS      float64 `json:"rps"`
	P50MS    float64 `json:"p50_ms"`
	P95MS    float64 `json:"p95_ms"`
	P99MS    float64 `json:"p99_ms"`
}

type RequestStats struct {
	BucketSeconds int            `json:"bucket_seconds"`
	From          time.Time      `json:"from"`
	To            time.Time      `json:"to"`
	Totals        RouteStats     `json:"totals"`
	Points        []RequestPoint `json:"points"`
	Routes        []RouteStats   `json:"routes"`
}

// Span is one stored span (traces API).
type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Service      string            `json:"service"`
	Env          string            `json:"env"`
	Release      string            `json:"release,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Status       string            `json:"status"`
	StatusMsg    string            `json:"status_message,omitempty"`
	StartTS      time.Time         `json:"start_ts"`
	DurationUS   uint64            `json:"duration_us"`
	HTTPMethod   string            `json:"http_method,omitempty"`
	HTTPRoute    string            `json:"http_route,omitempty"`
	HTTPStatus   uint16            `json:"http_status,omitempty"`
	Attrs        map[string]string `json:"attrs"`
	Events       []SpanEvent       `json:"events,omitempty"`
}

type SpanEvent struct {
	TS    time.Time         `json:"ts"`
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// TraceSummary is one row of the traces list: the trace's entry span plus trace-wide counts.
type TraceSummary struct {
	TraceID    string    `json:"trace_id"`
	RootSpanID string    `json:"root_span_id"`
	Service    string    `json:"service"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	StartTS    time.Time `json:"start_ts"`
	DurationUS uint64    `json:"duration_us"`
	HTTPMethod string    `json:"http_method,omitempty"`
	HTTPRoute  string    `json:"http_route,omitempty"`
	HTTPStatus uint16    `json:"http_status,omitempty"`
	SpanCount  uint64    `json:"span_count"`
	Errors     uint64    `json:"errors"`
	Services   []string  `json:"services"`
}

// SpanNameStats is the latency/error summary of one span name.
type SpanNameStats struct {
	Name     string  `json:"name"`
	Count    uint64  `json:"count"`
	P50US    float64 `json:"p50_us"`
	P95US    float64 `json:"p95_us"`
	ErrorPct float64 `json:"error_pct"`
}

// LatencyBucket is one power-of-two bucket of entry-span durations: [FromUS, ToUS).
type LatencyBucket struct {
	FromUS uint64 `json:"from_us"`
	ToUS   uint64 `json:"to_us"`
	Count  uint64 `json:"count"`
}

type TraceStats struct {
	ByName  []SpanNameStats `json:"by_name"`
	Latency []LatencyBucket `json:"latency"`
}

// TraceDetail is one trace: its spans (flat; the client builds the tree), the error
// occurrences and the log lines that carry the trace id.
type TraceDetail struct {
	TraceID    string            `json:"trace_id"`
	StartTS    time.Time         `json:"start_ts"`
	DurationUS uint64            `json:"duration_us"`
	Services   []string          `json:"services"`
	Spans      []Span            `json:"spans"`
	Errors     []ErrorOccurrence `json:"errors"`
	Logs       []LogEvent        `json:"logs"`
}

// MetricInfo is one catalog entry for the metric picker.
type MetricInfo struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Unit        string    `json:"unit,omitempty"`
	Monotonic   bool      `json:"monotonic,omitempty"`
	Temporality string    `json:"temporality,omitempty"`
	LastSeen    time.Time `json:"last_seen"`
}

// MetricValue is one bucket of a metric series (null where the bucket has no data).
type MetricValue struct {
	TS    time.Time `json:"ts"`
	Value *float64  `json:"value"`
}

type MetricSerie struct {
	Label  string        `json:"label"`
	Points []MetricValue `json:"points"`
}

// MetricSeries is the answer to GET /metrics/series.
type MetricSeries struct {
	Name          string        `json:"name"`
	Type          string        `json:"type"`
	Unit          string        `json:"unit,omitempty"`
	Agg           string        `json:"agg"`
	GroupBy       string        `json:"group_by,omitempty"`
	BucketSeconds int           `json:"bucket_seconds"`
	Series        []MetricSerie `json:"series"`
	Other         *MetricSerie  `json:"other,omitempty"`
}

// UsageDay is one (day, source) row of project_usage_daily.
type UsageDay struct {
	Day      time.Time `json:"day"`
	Source   string    `json:"source"`
	Rows     uint64    `json:"rows"`
	BytesEst uint64    `json:"bytes_est"`
}

type UsageSource struct {
	Rows     uint64 `json:"rows"`
	BytesEst uint64 `json:"bytes_est"`
	DiskEst  uint64 `json:"disk_est"`
}

type UsageRetention struct {
	Logs         int `json:"logs"`
	Errors       int `json:"errors"`
	Spans        int `json:"spans"`
	Metrics      int `json:"metrics"`
	OverrideDays int `json:"override_days"`
}

// ProjectUsage answers GET /projects/{id}/usage.
type ProjectUsage struct {
	ProjectID      string                 `json:"project_id"`
	QuotaGB        uint32                 `json:"quota_gb"`
	WindowDays     int                    `json:"window_days"`
	StoredBytesEst uint64                 `json:"stored_bytes_est"`
	StoredDiskEst  uint64                 `json:"stored_disk_est"`
	StoredPct      float64                `json:"stored_pct"`
	BySource       map[string]UsageSource `json:"by_source"`
	Daily          []UsageDay             `json:"daily"`
	Retention      UsageRetention         `json:"retention"`
	Compression    map[string]float64     `json:"compression"`
	Deleting       bool                   `json:"deleting"`
}

// AuditEntry is one administrative action.
type AuditEntry struct {
	TS        time.Time `json:"ts"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	ProjectID string    `json:"project_id,omitempty"`
	Target    string    `json:"target,omitempty"`
	Details   string    `json:"details,omitempty"`
}
