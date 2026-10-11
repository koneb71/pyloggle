package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/issue"
)

// epoch is the "unset" sentinel for DateTime64 columns (ClickHouse cannot store year 1).
var epoch = time.Unix(0, 0).UTC()

// GetIssueState returns the latest lifecycle state of an issue (every column, argMax by
// updated_at), plus the issue's occurrence total when a count snooze is active.
func (c *Client) GetIssueState(ctx context.Context, projectID, issueID string) (issue.State, bool, error) {
	st, _, found, err := c.loadIssueState(ctx, projectID, issueID)
	if err != nil || !found {
		return issue.State{}, false, err
	}
	if st.SnoozeCount > 0 {
		_ = c.conn.QueryRow(ctx, `SELECT sum(occurrence_count) FROM issues_agg WHERE project_id = ? AND issue_id = ?`, projectID, issueID).Scan(&st.OccurrenceCount)
	}
	return st, true, nil
}

// loadIssueState reads the current row and when it was written.
func (c *Client) loadIssueState(ctx context.Context, projectID, issueID string) (issue.State, time.Time, bool, error) {
	rows, err := c.conn.Query(ctx, `
		SELECT argMax(status, updated_at), argMax(is_regressed, updated_at), argMax(regressed_from, updated_at),
		       argMax(assignee, updated_at), argMax(snooze_until, updated_at), argMax(snooze_count, updated_at),
		       argMax(resolved_in_release, updated_at), max(updated_at)
		FROM issues_state WHERE project_id = ? AND issue_id = ? GROUP BY issue_id`, projectID, issueID)
	if err != nil {
		return issue.State{}, time.Time{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return issue.State{}, time.Time{}, false, rows.Err()
	}
	var st issue.State
	var regressed uint8
	var until, updatedAt time.Time
	if err := rows.Scan(&st.Status, &regressed, &st.RegressedFrom, &st.Assignee, &until, &st.SnoozeCount, &st.ResolvedInRelease, &updatedAt); err != nil {
		return issue.State{}, time.Time{}, false, err
	}
	st.Regressed = regressed == 1
	if until.Unix() > 0 {
		st.SnoozeUntil = until.UTC()
	}
	return st, updatedAt.UTC(), true, nil
}

// GetIssueStatus is the pre-0.3 shape (status + regressed only).
func (c *Client) GetIssueStatus(ctx context.Context, projectID, issueID string) (string, bool, bool, error) {
	st, ok, err := c.GetIssueState(ctx, projectID, issueID)
	return st.Status, st.Regressed, ok, err
}

// SetIssueState applies a change on top of the current state and appends the new full row plus
// an activity entry. issues_state is read with argMax per column, so a partial row (say, a
// regression written by ingest) would otherwise wipe the assignee and snooze fields.
func (c *Client) SetIssueState(ctx context.Context, ch api.IssueStateChange) error {
	if strings.TrimSpace(ch.ProjectID) == "" || strings.TrimSpace(ch.IssueID) == "" {
		return errors.New("missing project_id/issue_id")
	}
	if ch.Service == "" || ch.Env == "" {
		// Look the scope up so callers can act with just the issue id.
		if s, ok, err := c.GetIssueSummary(ctx, ch.ProjectID, ch.IssueID); err == nil && ok {
			ch.Service, ch.Env = s.Service, s.Env
		}
	}
	cur, curAt, found, err := c.loadIssueState(ctx, ch.ProjectID, ch.IssueID)
	if err != nil {
		return err
	}
	next := cur
	if !found {
		next.Status = issue.StatusOpen
	}
	if ch.Status != "" {
		if ch.Status != next.Status {
			// A real transition drops the previous status' qualifiers.
			next.SnoozeUntil, next.SnoozeCount, next.ResolvedInRelease = time.Time{}, 0, ""
		}
		next.Status = ch.Status
		next.Regressed = ch.Regressed
		next.RegressedFrom = ch.RegressedFrom
	}
	if ch.Assignee != nil {
		next.Assignee = strings.TrimSpace(*ch.Assignee)
	}
	if ch.SnoozeUntil != nil {
		next.SnoozeUntil = ch.SnoozeUntil.UTC()
	}
	if ch.SnoozeCount != nil {
		next.SnoozeCount = *ch.SnoozeCount
	}
	if ch.ResolvedInRelease != nil {
		next.ResolvedInRelease = strings.TrimSpace(*ch.ResolvedInRelease)
	}
	// Every reader picks the row with the greatest updated_at, so a change written in the same
	// millisecond as the previous one (resolve in the UI, regression from ingest right after)
	// must still sort after it.
	now := time.Now().UTC()
	if !now.After(curAt) {
		now = curAt.Add(time.Millisecond)
	}
	until := epoch
	if !next.SnoozeUntil.IsZero() {
		until = next.SnoozeUntil.UTC()
	}
	// Timestamps are bound as formatted strings: the driver renders a bare time.Time parameter
	// to the second and would drop the milliseconds the ordering depends on.
	if err := c.conn.Exec(ctx, `
		INSERT INTO issues_state (issue_id, service, env, project_id, status, is_regressed, regressed_from, updated_at, updated_by, comment, assignee,
			snooze_until, snooze_count, resolved_in_release)
		VALUES (?, ?, ?, ?, ?, ?, ?, `+ts3Expr+`, ?, ?, ?, `+ts3Expr+`, ?, ?)`,
		ch.IssueID, ch.Service, ch.Env, ch.ProjectID, next.Status, boolToU8(next.Regressed), next.RegressedFrom, ts3(now), ch.UpdatedBy, ch.Comment, next.Assignee,
		ts3(until), next.SnoozeCount, next.ResolvedInRelease); err != nil {
		return err
	}
	act := api.IssueActivity{IssueID: ch.IssueID, TS: now, Actor: ch.UpdatedBy, Comment: ch.Comment, Release: ch.Release, Kind: ch.Kind}
	switch {
	case act.Kind != "":
	case ch.Status != "" && ch.Regressed && ch.UpdatedBy == "system":
		act.Kind = "regression"
	case ch.ResolvedInRelease != nil && *ch.ResolvedInRelease != "":
		act.Kind = "resolve_in_release"
	case ch.SnoozeUntil != nil || ch.SnoozeCount != nil:
		act.Kind = "snooze"
	case ch.Status != "":
		act.Kind = "status"
	case ch.Assignee != nil:
		act.Kind = "assign"
	default:
		act.Kind = "comment"
	}
	switch act.Kind {
	case "assign":
		act.From, act.To = cur.Assignee, next.Assignee
	case "snooze":
		act.From, act.To = cur.Status, snoozeLabel(next)
	case "resolve_in_release":
		act.From, act.To = cur.Status, next.Status
		if act.Release == "" {
			act.Release = next.ResolvedInRelease
		}
	case "comment":
	default:
		act.From, act.To = cur.Status, next.Status
	}
	return c.addActivity(ctx, ch.ProjectID, ch.Service, ch.Env, act)
}

func snoozeLabel(st issue.State) string {
	switch {
	case !st.SnoozeUntil.IsZero() && st.SnoozeCount > 0:
		return fmt.Sprintf("until %s or %d occurrences", st.SnoozeUntil.UTC().Format(time.RFC3339), st.SnoozeCount)
	case !st.SnoozeUntil.IsZero():
		return "until " + st.SnoozeUntil.UTC().Format(time.RFC3339)
	case st.SnoozeCount > 0:
		return fmt.Sprintf("until %d occurrences", st.SnoozeCount)
	}
	return "ignored"
}

func (c *Client) addActivity(ctx context.Context, projectID, service, env string, a api.IssueActivity) error {
	if a.ID == "" {
		a.ID = newID()
	}
	if a.TS.IsZero() {
		a.TS = time.Now().UTC()
	}
	return c.conn.Exec(ctx, `
		INSERT INTO issues_activity (ts, project_id, service, env, issue_id, id, kind, actor, from_value, to_value, comment, release)
		VALUES (`+ts3Expr+`, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts3(a.TS), projectID, service, env, a.IssueID, a.ID, a.Kind, a.Actor, a.From, a.To, a.Comment, a.Release)
}

// AddIssueComment appends a comment to the activity log without touching the state.
func (c *Client) AddIssueComment(ctx context.Context, projectID, issueID, actor, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment")
	}
	var service, env string
	if s, ok, err := c.GetIssueSummary(ctx, projectID, issueID); err == nil && ok {
		service, env = s.Service, s.Env
	}
	return c.addActivity(ctx, projectID, service, env, api.IssueActivity{IssueID: issueID, Kind: "comment", Actor: actor, Comment: strings.TrimSpace(body)})
}

// ListIssueActivity returns an issue's history, newest first.
func (c *Client) ListIssueActivity(ctx context.Context, projectID, issueID string, limit int) ([]api.IssueActivity, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := c.conn.Query(ctx, `SELECT id, ts, kind, actor, from_value, to_value, comment, release
		FROM issues_activity WHERE project_id = ? AND issue_id = ? ORDER BY ts DESC LIMIT ?`, projectID, issueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []api.IssueActivity{}
	for rows.Next() {
		a := api.IssueActivity{IssueID: issueID}
		if err := rows.Scan(&a.ID, &a.TS, &a.Kind, &a.Actor, &a.From, &a.To, &a.Comment, &a.Release); err != nil {
			return nil, err
		}
		a.TS = a.TS.UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAssignees suggests people: actors and assignees seen in the last 90 days.
func (c *Client) ListAssignees(ctx context.Context, projectID string) ([]string, error) {
	rows, err := c.conn.Query(ctx, `SELECT DISTINCT v FROM (
			SELECT actor AS v FROM issues_activity WHERE project_id = ? AND ts > now() - INTERVAL 90 DAY
			UNION ALL
			SELECT to_value AS v FROM issues_activity WHERE project_id = ? AND kind = 'assign' AND ts > now() - INTERVAL 90 DAY
		) WHERE v != '' AND v != 'system' AND v != 'ui' AND v != 'api' ORDER BY v LIMIT 200`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReleaseFirstSeen returns when a release was first reported for a project.
func (c *Client) ReleaseFirstSeen(ctx context.Context, projectID, release string) (time.Time, bool, error) {
	var t time.Time
	var n uint64
	if err := c.conn.QueryRow(ctx, `SELECT min(first_seen), count() FROM releases WHERE project_id = ? AND release = ?`, projectID, release).Scan(&t, &n); err != nil {
		return time.Time{}, false, err
	}
	if n == 0 || t.Unix() <= 0 {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
