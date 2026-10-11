package webui

import (
	"context"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/issue"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

// Store is everything the UI API reads and writes. *clickhouse.Client implements it; tests use a fake.
type Store interface {
	// issues
	QueryIssues(ctx context.Context, q clickhouse.IssueListQuery) ([]api.IssueSummary, error)
	CountIssuesByStatus(ctx context.Context, q clickhouse.IssueListQuery) (map[string]uint64, error)
	GetIssueSummary(ctx context.Context, projectID, issueID string) (api.IssueSummary, bool, error)
	IssueSparklines(ctx context.Context, projectID string, issueIDs []string, hours int) (map[string][]uint64, error)
	IssueTrend(ctx context.Context, projectID, issueID string, from, to time.Time, bucketSeconds int) ([]api.TimeSeriesPoint, error)
	IssueBreakdown(ctx context.Context, projectID, issueID, field string, from, to time.Time, limit int) ([]api.FacetCount, error)
	GetOccurrence(ctx context.Context, projectID, eventID string) (api.ErrorOccurrence, bool, error)
	LatestOccurrence(ctx context.Context, projectID, issueID string) (api.ErrorOccurrence, bool, error)
	QueryOccurrences(ctx context.Context, projectID, issueID string, from, to, before time.Time, limit int) ([]api.ErrorOccurrence, error)
	SetIssueState(ctx context.Context, st api.IssueStateChange) error
	GetIssueState(ctx context.Context, projectID, issueID string) (issue.State, bool, error)
	AddIssueComment(ctx context.Context, projectID, issueID, actor, body string) error
	ListIssueActivity(ctx context.Context, projectID, issueID string, limit int) ([]api.IssueActivity, error)
	ListAssignees(ctx context.Context, projectID string) ([]string, error)

	// logs
	QueryLogs(ctx context.Context, q clickhouse.LogQuery) ([]api.LogEvent, error)
	LogHistogram(ctx context.Context, q clickhouse.LogQuery, groupBy string, bucket time.Duration) (api.Histogram, error)
	LogFacets(ctx context.Context, q clickhouse.LogQuery, fields []string, attrKeys int, attrKey string) (api.LogFacets, error)
	LogContext(ctx context.Context, projectID, eventID string, before, after int) (api.LogContext, bool, error)
	LogPatterns(ctx context.Context, q clickhouse.LogQuery, limit int) ([]api.LogPattern, error)
	CountLogs(ctx context.Context, q clickhouse.LogQuery) (uint64, error)

	// metrics
	Overview(ctx context.Context, projectID, service, env string, from, to time.Time) (api.Overview, error)
	ErrorTrend(ctx context.Context, projectID, service, env, httpRoute, httpMethod string, from, to time.Time, bucketSeconds int) ([]api.TimeSeriesPoint, error)
	LogVolumeByLevel(ctx context.Context, projectID, service, env string, from, to time.Time, bucketSeconds int) (api.Histogram, error)
	TopIssues(ctx context.Context, projectID, service, env string, from, to time.Time, limit int) ([]api.IssueSummary, error)

	// saved views
	ListSavedSearches(ctx context.Context, projectID, userID string, includePublic bool, onlyPinned bool, limit int) ([]api.SavedSearch, error)
	UpsertSavedSearch(ctx context.Context, s api.SavedSearch) (api.SavedSearch, error)
	SoftDeleteSavedSearch(ctx context.Context, projectID, userID, id string) error

	// projects
	ListProjects(ctx context.Context, limit int) ([]clickhouse.ProjectView, error)
	GetProjectByID(ctx context.Context, projectID string) (clickhouse.ProjectView, bool, error)
	CreateProject(ctx context.Context, p clickhouse.ProjectUpsert) (clickhouse.ProjectView, string, error)
	UpsertProject(ctx context.Context, p clickhouse.ProjectUpsert) (clickhouse.ProjectView, error)
	RotateProjectAPIKey(ctx context.Context, projectID string) (clickhouse.ProjectView, string, error)

	// alerts
	ListAlertRules(ctx context.Context, projectID string) ([]api.AlertRule, error)
	UpsertAlertRule(ctx context.Context, rule api.AlertRule) (api.AlertRule, error)
	ListAlertFirings(ctx context.Context, projectID string, limit int) ([]api.AlertFiring, error)
}

var _ Store = (*clickhouse.Client)(nil)

// KeyResolver maps API keys to projects (tenant.Resolver in production).
type KeyResolver interface {
	ResolveByAPIKey(ctx context.Context, apiKey string) (tenant.Project, bool, error)
}
