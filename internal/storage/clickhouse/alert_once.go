package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"pyloggle/internal/api"
)

// Alert bookkeeping: what each rule has already sent (alert_notified), and comebacks read from
// the activity log so that editing a problem that came back never looks like a new comeback.

// NotifiedSubjects reports which of the subjects the rule has sent since the given time (the
// zero time means ever).
func (c *Client) NotifiedSubjects(ctx context.Context, projectID, ruleID string, subjects []string, since time.Time) (map[string]bool, error) {
	out := map[string]bool{}
	if len(subjects) == 0 {
		return out, nil
	}
	q, args := notifiedSubjectsQuery(projectID, ruleID, subjects, since)
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}

func notifiedSubjectsQuery(projectID, ruleID string, subjects []string, since time.Time) (string, []any) {
	q := `SELECT DISTINCT subject FROM alert_notified WHERE project_id = ? AND rule_id = ? AND subject IN (?)`
	args := []any{projectID, ruleID, subjects}
	if !since.IsZero() {
		q += " AND notified_at >= ?"
		args = append(args, since.UTC())
	}
	return q, args
}

// MarkNotified records subjects as sent by the rule. open matters only for the "incident"
// subject: 1 while the incident lasts, 0 once it is back to normal.
func (c *Client) MarkNotified(ctx context.Context, projectID, ruleID string, subjects []string, open bool) error {
	if len(subjects) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, `INSERT INTO alert_notified (project_id, rule_id, subject, open, notified_at)`)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, s := range subjects {
		if err := batch.Append(projectID, ruleID, s, boolToU8(open), now); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}

// IncidentOpen reports whether a count rule's last recorded incident is still going on.
func (c *Client) IncidentOpen(ctx context.Context, projectID, ruleID string) (bool, error) {
	var open uint8
	err := c.conn.QueryRow(ctx, `SELECT argMax(open, notified_at) FROM alert_notified WHERE project_id = ? AND rule_id = ? AND subject = 'incident'`,
		projectID, ruleID).Scan(&open)
	return open == 1, err
}

// OpenIncident is a count rule that alerted and has not gone back to normal yet.
type OpenIncident struct {
	RuleID string    `ch:"rule_id"`
	Since  time.Time `ch:"since"`
}

// OpenIncidents lists the project's count rules whose incident is still going on.
func (c *Client) OpenIncidents(ctx context.Context, projectID string) ([]OpenIncident, error) {
	var rows []OpenIncident
	err := c.conn.Select(ctx, &rows, `
		SELECT rule_id, maxIf(notified_at, open = 1) AS since
		FROM alert_notified
		WHERE project_id = ? AND subject = 'incident'
		GROUP BY rule_id
		HAVING argMax(open, notified_at) = 1
		ORDER BY since`, projectID)
	return rows, err
}

// RegressionEvent is one time a problem marked as fixed came back.
type RegressionEvent struct {
	ID      string    `ch:"id"`
	IssueID string    `ch:"issue_id"`
	TS      time.Time `ch:"ts"`
	Issue   api.IssueSummary
}

// RegressionEvents lists the comebacks recorded in the activity log inside the window, newest
// first, with each problem's summary. Each comeback is one row, whatever happens to the
// problem afterwards (assigning or commenting on it adds activity of other kinds).
func (c *Client) RegressionEvents(ctx context.Context, rule api.AlertRule, from, to time.Time, limit int) ([]RegressionEvent, error) {
	if strings.TrimSpace(rule.ProjectID) == "" {
		return nil, errors.New("missing project_id")
	}
	if limit <= 0 {
		limit = 20
	}
	q, args := regressionEventsQuery(rule, from, to, limit)
	var rows []RegressionEvent
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.IssueID] {
			seen[r.IssueID] = true
			ids = append(ids, r.IssueID)
		}
	}
	sums, err := c.QueryIssues(ctx, IssueListQuery{ProjectID: rule.ProjectID, IssueIDs: ids, Limit: len(ids)})
	if err != nil {
		return nil, err
	}
	byID := map[string]api.IssueSummary{}
	for _, s := range sums {
		byID[s.IssueID] = s
	}
	out := rows[:0]
	for _, r := range rows {
		s, ok := byID[r.IssueID]
		if !ok {
			continue // deleted since
		}
		r.TS = r.TS.UTC()
		r.Issue = s
		out = append(out, r)
	}
	return out, nil
}

func regressionEventsQuery(rule api.AlertRule, from, to time.Time, limit int) (string, []any) {
	q := `SELECT id, issue_id, ts FROM issues_activity WHERE project_id = ? AND kind = 'regression' AND ts >= ? AND ts < ?`
	args := []any{rule.ProjectID, from.UTC(), to.UTC()}
	if rule.Service != "" {
		q += " AND service = ?"
		args = append(args, rule.Service)
	}
	if rule.Env != "" {
		q += " AND env = ?"
		args = append(args, rule.Env)
	}
	q += " ORDER BY ts DESC LIMIT ?"
	return q, append(args, limit)
}
