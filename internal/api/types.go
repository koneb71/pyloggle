package api

import "time"

type LogEvent struct {
	TS        time.Time         `json:"ts"`
	Service   string            `json:"service"`
	Env       string            `json:"env"`
	Level     string            `json:"level"`
	Logger    string            `json:"logger"`
	Message   string            `json:"message"`
	TraceID   string            `json:"trace_id"`
	SpanID    string            `json:"span_id"`
	RequestID string            `json:"request_id"`
	ProjectID string            `json:"project_id"`
	Attrs     map[string]string `json:"attrs"`
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

type IssueSummary struct {
	IssueID          string    `json:"issue_id"`
	Service          string    `json:"service"`
	Env              string    `json:"env"`
	ProjectID        string    `json:"project_id"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	IsRegressed      bool      `json:"is_regressed"`
	StatusUpdatedAt  time.Time `json:"status_updated_at"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	OccurrenceCount  uint64    `json:"occurrence_count"`
	AffectedUsers    uint32    `json:"affected_users"`
	ExceptionType    string    `json:"exception_type"`
	TopFilename      string    `json:"top_filename"`
	TopLineno        uint32    `json:"top_lineno"`
	TopFunction      string    `json:"top_function"`
	StackFingerprint string    `json:"stack_fingerprint"`
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

type ErrorOccurrence struct {
	TS               time.Time         `json:"ts"`
	IssueID          string            `json:"issue_id"`
	Service          string            `json:"service"`
	Env              string            `json:"env"`
	ProjectID        string            `json:"project_id"`
	Title            string            `json:"title"`
	ExceptionType    string            `json:"exception_type"`
	ExceptionMessage string            `json:"exception_message"`
	StackFingerprint string            `json:"stack_fingerprint"`
	StackFrames      []map[string]any  `json:"stack_frames"`
	TraceID          string            `json:"trace_id"`
	RequestID        string            `json:"request_id"`
	HTTPMethod       string            `json:"http_method"`
	HTTPRoute        string            `json:"http_route"`
	HTTPStatus       uint16            `json:"http_status"`
	UserIDHash       string            `json:"user_id_hash"`
	Attrs            map[string]string `json:"attrs"`
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

	ConditionType     string  `json:"condition_type"` // error_rate | issue_spike
	Threshold         float64 `json:"threshold"`
	TimeWindowSeconds uint32  `json:"time_window_seconds"`
	CooldownSeconds   uint32  `json:"cooldown_seconds"`
	Enabled           bool    `json:"enabled"`

	NotificationChannels []string `json:"notification_channels"` // email, webhook
	NotificationEmails   []string `json:"notification_emails"`
	WebhookURLs          []string `json:"webhook_urls"`
	TelegramChatIDs      []string `json:"telegram_chat_ids"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AlertRuleUpsertRequest struct {
	Rule AlertRule `json:"rule"`
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
