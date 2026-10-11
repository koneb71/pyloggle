// Package api holds the JSON types returned by the HTTP API and the UI API.
package api

import (
	"encoding/json"
	"time"

	"pyloggle/internal/model"
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
	IssueID         string    `json:"issue_id"`
	Service         string    `json:"service"`
	Env             string    `json:"env"`
	ProjectID       string    `json:"project_id"`
	Title           string    `json:"title"`
	Culprit         string    `json:"culprit"`
	LastMessage     string    `json:"last_message"`
	Level           string    `json:"level"`
	Status          string    `json:"status"`
	IsRegressed     bool      `json:"is_regressed"`
	Assignee        string    `json:"assignee,omitempty"`
	StatusUpdatedAt time.Time `json:"status_updated_at"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	OccurrenceCount uint64    `json:"occurrence_count"`
	AffectedUsers   uint64    `json:"affected_users"`
	ExceptionType   string    `json:"exception_type"`
	ExceptionModule string    `json:"exception_module,omitempty"`
	LastRelease     string    `json:"last_release,omitempty"`
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
	ProjectID     string
	Service       string
	Env           string
	IssueID       string
	Status        string
	Regressed     bool
	RegressedFrom string
	UpdatedBy     string
	Comment       string
	Assignee      string
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

// Project is the multi-tenant container used for API key auth + quotas.
type Project struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	MaxEventsPerSec uint32    `json:"max_events_per_second"`
	MaxStorageGB    uint32    `json:"max_storage_gb"`
	APIKeyHash      string    `json:"api_key_hash"`
	APIKeyCreatedAt time.Time `json:"api_key_created_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ProjectCreateRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MaxEventsPerSec uint32 `json:"max_events_per_second"`
	MaxStorageGB    uint32 `json:"max_storage_gb"`
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
}

type ProjectRotateKeyRequest struct {
	ID string `json:"id"`
}

type ProjectRotateKeyResponse struct {
	Project Project `json:"project"`
	APIKey  string  `json:"api_key"`
}
