package clickhouse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"pyloggle/internal/api"
)

type alertRuleRow struct {
	ID          string `ch:"id"`
	ProjectID   string `ch:"project_id"`
	Name        string `ch:"name"`
	Description string `ch:"description"`

	Service    string `ch:"service"`
	Env        string `ch:"env"`
	HTTPRoute  string `ch:"http_route"`
	HTTPMethod string `ch:"http_method"`

	ConditionType     string  `ch:"condition_type"`
	Threshold         float64 `ch:"threshold"`
	TimeWindowSeconds uint32  `ch:"time_window_seconds"`
	CooldownSeconds   uint32  `ch:"cooldown_seconds"`

	Enabled uint8 `ch:"enabled"`

	NotificationChannels []string `ch:"notification_channels"`
	NotificationEmails   []string `ch:"notification_emails"`
	WebhookURLs          []string `ch:"webhook_urls"`
	TelegramChatIDs      []string `ch:"telegram_chat_ids"`

	CreatedAt time.Time `ch:"created_at"`
	UpdatedAt time.Time `ch:"updated_at"`
}

func (r alertRuleRow) toAPI() api.AlertRule {
	return api.AlertRule{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		Name:        r.Name,
		Description: r.Description,
		Service:     r.Service,
		Env:         r.Env,
		HTTPRoute:   r.HTTPRoute,
		HTTPMethod:  r.HTTPMethod,
		ConditionType:     r.ConditionType,
		Threshold:         r.Threshold,
		TimeWindowSeconds: r.TimeWindowSeconds,
		CooldownSeconds:   r.CooldownSeconds,
		Enabled:           r.Enabled == 1,
		NotificationChannels: r.NotificationChannels,
		NotificationEmails:   r.NotificationEmails,
		WebhookURLs:          r.WebhookURLs,
		TelegramChatIDs:      r.TelegramChatIDs,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
	}
}

func (c *Client) ListAlertRules(ctx context.Context, projectID string) ([]api.AlertRule, error) {
	if projectID == "" {
		return nil, errors.New("missing project_id")
	}
	q := `
		SELECT
			id,
			project_id,
			argMax(name, a.updated_at) as name,
			argMax(description, a.updated_at) as description,
			argMax(service, a.updated_at) as service,
			argMax(env, a.updated_at) as env,
			argMax(http_route, a.updated_at) as http_route,
			argMax(http_method, a.updated_at) as http_method,
			argMax(condition_type, a.updated_at) as condition_type,
			argMax(threshold, a.updated_at) as threshold,
			argMax(time_window_seconds, a.updated_at) as time_window_seconds,
			argMax(cooldown_seconds, a.updated_at) as cooldown_seconds,
			argMax(enabled, a.updated_at) as enabled,
			argMax(notification_channels, a.updated_at) as notification_channels,
			argMax(notification_emails, a.updated_at) as notification_emails,
			argMax(webhook_urls, a.updated_at) as webhook_urls,
			argMax(telegram_chat_ids, a.updated_at) as telegram_chat_ids,
			min(created_at) as created_at,
			max(a.updated_at) as updated_at
		FROM alert_rules AS a
		WHERE project_id = ?
		GROUP BY project_id, id
		ORDER BY updated_at DESC
	`

	var rows []alertRuleRow
	if err := c.conn.Select(ctx, &rows, q, projectID); err != nil {
		return nil, err
	}
	out := make([]api.AlertRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

func (c *Client) ListEnabledAlertRules(ctx context.Context) ([]api.AlertRule, error) {
	q := `
		SELECT
			id,
			project_id,
			argMax(name, a.updated_at) as name,
			argMax(description, a.updated_at) as description,
			argMax(service, a.updated_at) as service,
			argMax(env, a.updated_at) as env,
			argMax(http_route, a.updated_at) as http_route,
			argMax(http_method, a.updated_at) as http_method,
			argMax(condition_type, a.updated_at) as condition_type,
			argMax(threshold, a.updated_at) as threshold,
			argMax(time_window_seconds, a.updated_at) as time_window_seconds,
			argMax(cooldown_seconds, a.updated_at) as cooldown_seconds,
			argMax(enabled, a.updated_at) as enabled,
			argMax(notification_channels, a.updated_at) as notification_channels,
			argMax(notification_emails, a.updated_at) as notification_emails,
			argMax(webhook_urls, a.updated_at) as webhook_urls,
			argMax(telegram_chat_ids, a.updated_at) as telegram_chat_ids,
			min(created_at) as created_at,
			max(a.updated_at) as updated_at
		FROM alert_rules AS a
		GROUP BY project_id, id
		HAVING enabled = 1
		ORDER BY updated_at DESC
	`

	var rows []alertRuleRow
	if err := c.conn.Select(ctx, &rows, q); err != nil {
		return nil, err
	}
	out := make([]api.AlertRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

func (c *Client) UpsertAlertRule(ctx context.Context, rule api.AlertRule) (api.AlertRule, error) {
	now := time.Now().UTC()
	if rule.ID == "" {
		rule.ID = newID()
	}
	if rule.ProjectID == "" {
		return api.AlertRule{}, errors.New("missing project_id")
	}
	if rule.TimeWindowSeconds == 0 {
		rule.TimeWindowSeconds = 60
	}
	if rule.CooldownSeconds == 0 {
		rule.CooldownSeconds = rule.TimeWindowSeconds
	}

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO alert_rules (
			id, project_id, name, description,
			service, env, http_route, http_method,
			condition_type, threshold, time_window_seconds, cooldown_seconds,
			enabled, notification_channels, notification_emails, webhook_urls, telegram_chat_ids,
			created_at, updated_at
		)
	`)
	if err != nil {
		return api.AlertRule{}, err
	}

	createdAt := rule.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := now

	if err := batch.Append(
		rule.ID,
		rule.ProjectID,
		rule.Name,
		rule.Description,
		rule.Service,
		rule.Env,
		rule.HTTPRoute,
		rule.HTTPMethod,
		rule.ConditionType,
		rule.Threshold,
		rule.TimeWindowSeconds,
		rule.CooldownSeconds,
		boolToU8(rule.Enabled),
		rule.NotificationChannels,
		rule.NotificationEmails,
		rule.WebhookURLs,
		rule.TelegramChatIDs,
		createdAt,
		updatedAt,
	); err != nil {
		_ = batch.Abort()
		return api.AlertRule{}, err
	}
	if err := batch.Send(); err != nil {
		return api.AlertRule{}, err
	}

	rule.CreatedAt = createdAt
	rule.UpdatedAt = updatedAt
	return rule, nil
}

func boolToU8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

type issueSpikeRow struct {
	IssueID    string `ch:"issue_id"`
	Title      string `ch:"title"`
	CurCount   uint64 `ch:"cur_count"`
	PrevCount  uint64 `ch:"prev_count"`
}

func (c *Client) CountErrorOccurrences(ctx context.Context, rule api.AlertRule, from, to time.Time) (uint64, error) {
	q := `
		SELECT count()
		FROM error_occurrences
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
		` + optionalEq("service", rule.Service) + optionalEq("env", rule.Env) + optionalEq("http_route", rule.HTTPRoute) + optionalEq("http_method", rule.HTTPMethod)

	var n uint64
	args := []any{rule.ProjectID, from.UTC(), to.UTC()}
	if rule.Service != "" {
		q += " AND service = ?\n"
		args = append(args, rule.Service)
	}
	if rule.Env != "" {
		q += " AND env = ?\n"
		args = append(args, rule.Env)
	}
	if rule.HTTPRoute != "" {
		q += " AND http_route = ?\n"
		args = append(args, rule.HTTPRoute)
	}
	if rule.HTTPMethod != "" {
		q += " AND http_method = ?\n"
		args = append(args, rule.HTTPMethod)
	}

	if err := c.conn.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *Client) IssueSpikeCounts(ctx context.Context, rule api.AlertRule, prevFrom, prevTo, curFrom, curTo time.Time, limit int) ([]issueSpikeRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	q := `
		SELECT
			issue_id,
			any(title) as title,
			countIf(ts >= ? AND ts < ?) as cur_count,
			countIf(ts >= ? AND ts < ?) as prev_count
		FROM error_occurrences
		WHERE project_id = ?
		  AND ts >= ?
		  AND ts < ?
		GROUP BY issue_id
		HAVING cur_count > 0
		ORDER BY cur_count DESC
		LIMIT ?
	`
	var rows []issueSpikeRow
	args := []any{
		curFrom.UTC(), curTo.UTC(),
		prevFrom.UTC(), prevTo.UTC(),
		rule.ProjectID,
		prevFrom.UTC(), curTo.UTC(),
	}
	if rule.Service != "" {
		q += " AND service = ?\n"
		args = append(args, rule.Service)
	}
	if rule.Env != "" {
		q += " AND env = ?\n"
		args = append(args, rule.Env)
	}
	if rule.HTTPRoute != "" {
		q += " AND http_route = ?\n"
		args = append(args, rule.HTTPRoute)
	}
	if rule.HTTPMethod != "" {
		q += " AND http_method = ?\n"
		args = append(args, rule.HTTPMethod)
	}
	args = append(args, limit)

	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

type alertFiringRow struct {
	FiredAt time.Time `ch:"fired_at"`
}

func (c *Client) GetLastAlertFiredAt(ctx context.Context, ruleID string) (time.Time, bool, error) {
	q := `
		SELECT max(fired_at) as fired_at
		FROM alert_firings
		WHERE rule_id = ?
	`
	var rows []alertFiringRow
	if err := c.conn.Select(ctx, &rows, q, ruleID); err != nil {
		return time.Time{}, false, err
	}
	if len(rows) == 0 || rows[0].FiredAt.IsZero() {
		return time.Time{}, false, nil
	}
	return rows[0].FiredAt, true, nil
}

type AlertFiring struct {
	ID            string
	RuleID        string
	ProjectID     string
	Service       string
	Env           string
	ConditionType string
	FiredAt       time.Time
	WindowStart   time.Time
	WindowEnd     time.Time
	DedupeKey     string
	Payload       any
}

func (c *Client) InsertAlertFiring(ctx context.Context, f AlertFiring) error {
	now := time.Now().UTC()
	if f.ID == "" {
		f.ID = newID()
	}
	if f.FiredAt.IsZero() {
		f.FiredAt = now
	}
	if f.DedupeKey == "" {
		f.DedupeKey = f.RuleID + "|" + f.WindowStart.UTC().Format(time.RFC3339Nano) + "|" + f.WindowEnd.UTC().Format(time.RFC3339Nano)
	}
	b, _ := json.Marshal(f.Payload)

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO alert_firings (
			id, rule_id, project_id, service, env, condition_type,
			fired_at, window_start, window_end,
			dedupe_key, payload
		)
	`)
	if err != nil {
		return err
	}
	if err := batch.Append(
		f.ID,
		f.RuleID,
		f.ProjectID,
		f.Service,
		f.Env,
		f.ConditionType,
		f.FiredAt.UTC(),
		f.WindowStart.UTC(),
		f.WindowEnd.UTC(),
		f.DedupeKey,
		string(b),
	); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}


