package clickhouse

import (
	"context"
	"fmt"
	"time"
)

// Per-project retention. A project's retention_days (shorter than the server's) cannot be a
// TTL: with ttl_only_drop_parts a part goes only when every row in it expired, and parts mix
// projects. A lightweight DELETE hides rows but frees nothing until the part merges, which old
// partitions never do. So once a day the trimmer issues one partition-scoped mutation per
// (project, table, day): it rewrites exactly that day's parts and the space comes back.

// trimTargets maps event tables to the server retention that bounds the days to visit.
func trimTargets(opts MigrateOptions) []retentionTarget {
	var out []retentionTarget
	for _, t := range retentionTargets(opts) {
		switch t.Table {
		case "logs_raw", "error_occurrences", "spans", "metrics_points":
			out = append(out, t)
		}
	}
	return out
}

// trimDays lists the UTC days whose rows a project must lose: older than the project's
// retention, still inside the server's (older partitions are already gone), and after the
// last trimmed day. Oldest first so a partial run resumes cleanly.
func trimDays(today time.Time, serverDays, projectDays int, lastTrimmed time.Time) []time.Time {
	if projectDays <= 0 || serverDays <= 0 || projectDays >= serverDays {
		return nil
	}
	today = today.UTC().Truncate(24 * time.Hour)
	oldest := today.AddDate(0, 0, -serverDays+1)
	newest := today.AddDate(0, 0, -projectDays)
	var out []time.Time
	for d := oldest; !d.After(newest); d = d.AddDate(0, 0, 1) {
		if !lastTrimmed.IsZero() && !d.After(lastTrimmed.UTC().Truncate(24*time.Hour)) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// TrimProjects applies every project's retention override. At most one mutation per table is
// in flight at a time; a table that is still mutating is revisited on the next run.
func (c *Client) TrimProjects(ctx context.Context, opts MigrateOptions, logf func(string, ...any)) error {
	opts = opts.withDefaults()
	if logf == nil {
		logf = func(string, ...any) {}
	}
	projects, err := c.ListProjects(ctx, 500)
	if err != nil {
		return err
	}
	today := time.Now().UTC()
	for _, t := range trimTargets(opts) {
		exists, err := c.tableExists(ctx, t.Table)
		if err != nil || !exists {
			continue
		}
		serverDays := t.Days(opts)
		busy := false
		for _, p := range projects {
			if p.RetentionDays == 0 || int(p.RetentionDays) >= serverDays || busy {
				continue
			}
			key := fmt.Sprintf("trim.%s.%s", p.ID, t.Table)
			var last time.Time
			if v, _ := c.GetSetting(ctx, key); v != "" {
				last, _ = time.Parse("2006-01-02", v)
			}
			for _, day := range trimDays(today, serverDays, int(p.RetentionDays), last) {
				if n, err := c.PendingMutations(ctx, ""); err == nil && n > 0 {
					if tn, _ := c.pendingMutationsOn(ctx, t.Table); tn > 0 {
						busy = true
						break
					}
				}
				id := day.Format("20060102")
				var parts uint64
				if err := c.conn.QueryRow(ctx, "SELECT count() FROM system.parts WHERE database = currentDatabase() AND table = ? AND partition_id = ? AND active", t.Table, id).Scan(&parts); err != nil {
					return err
				}
				if parts > 0 {
					stmt := fmt.Sprintf("ALTER TABLE %s DELETE IN PARTITION ID '%s' WHERE project_id = ?", t.Table, id)
					if err := c.conn.Exec(ctx, stmt, p.ID); err != nil {
						return fmt.Errorf("%s: %w", stmt, err)
					}
					logf("retention: trimming %s from %s for project %s (keeps %d days)", day.Format("2006-01-02"), t.Table, p.ID, p.RetentionDays)
				}
				if err := c.SetSetting(ctx, key, day.Format("2006-01-02")); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Client) pendingMutationsOn(ctx context.Context, table string) (int, error) {
	var n uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM system.mutations WHERE database = currentDatabase() AND table = ? AND is_done = 0", table).Scan(&n); err != nil {
		return 0, err
	}
	return int(n), nil
}
