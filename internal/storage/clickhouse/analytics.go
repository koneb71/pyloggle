package clickhouse

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
)

// AnalyticsScope is what Analytics → Errors and Usage look at: a project, optionally one app and
// one environment, over a range.
type AnalyticsScope struct {
	ProjectID string
	Service   string
	Env       string
	From      time.Time
	To        time.Time
}

func (s AnalyticsScope) check() (AnalyticsScope, error) {
	if strings.TrimSpace(s.ProjectID) == "" {
		return s, errors.New("missing project_id")
	}
	s.From, s.To = defaultRange(s.From, s.To, 30*24*time.Hour)
	if !s.From.Before(s.To) {
		return s, errors.New("from must be before to")
	}
	return s, nil
}

// filter appends the app and environment conditions.
func (s AnalyticsScope) filter(q string, args []any) (string, []any) {
	if s.Service != "" {
		q += " AND service = ?"
		args = append(args, s.Service)
	}
	if s.Env != "" {
		q += " AND env = ?"
		args = append(args, s.Env)
	}
	return q, args
}

// previous is the same length of time just before the range.
func (s AnalyticsScope) previous() (time.Time, time.Time) {
	return s.From.Add(-s.To.Sub(s.From)), s.From
}

// analyticsBucket is hourly up to two days, daily up to four months, weekly beyond: request counts
// are kept per hour, and a chart of a quarter by the hour is a smear.
func analyticsBucket(from, to time.Time) time.Duration {
	switch span := to.Sub(from); {
	case span <= 48*time.Hour:
		return time.Hour
	case span <= 120*24*time.Hour:
		return 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

// bucketsBetween lists the bucket starts covering [from, to), aligned like toStartOfInterval.
func bucketsBetween(from, to time.Time, step time.Duration) []time.Time {
	var out []time.Time
	for b := from.Truncate(step); b.Before(to) && len(out) < 5000; b = b.Add(step) {
		out = append(out, b)
	}
	return out
}

const (
	fixedCond    = "kind IN ('status', 'resolve_in_release') AND to_value = 'resolved' AND from_value != 'resolved'"
	cameBackCond = "kind = 'regression'"
	reopenCond   = "(kind = 'regression' OR (kind = 'status' AND from_value = 'resolved' AND to_value != 'resolved'))"
	// openedSQL lists each problem with the moment it first appeared.
	openedSQL = "SELECT issue_id, min(first_seen) AS fs FROM issues_agg WHERE project_id = ?"
)

// ErrorAnalytics gathers Analytics → Errors. Problems opened, fixed and coming back come from the
// issue tables, kept for good; releases and people come from error details, kept for
// PYLOGGLE_RETENTION_ERRORS_DAYS.
func (c *Client) ErrorAnalytics(ctx context.Context, s AnalyticsScope) (api.ErrorAnalytics, error) {
	s, err := s.check()
	if err != nil {
		return api.ErrorAnalytics{}, err
	}
	step := analyticsBucket(s.From, s.To)
	out := api.ErrorAnalytics{From: s.From, To: s.To, BucketSeconds: int(step / time.Second),
		Points: []api.ErrorFlowPoint{}, Releases: []api.ReleaseHealth{}, Types: []api.ErrorGroupCount{}, Apps: []api.ErrorGroupCount{}, People: []api.PersonImpact{}}
	if out.Points, out.Totals, err = c.errorFlow(ctx, s, step); err != nil {
		return out, err
	}
	pf, pt := s.previous()
	prev := s
	prev.From, prev.To = pf, pt
	if _, out.Previous, err = c.errorFlow(ctx, prev, prev.To.Sub(prev.From)+time.Hour); err != nil {
		return out, err
	}
	if out.TimeToFix, err = c.timeToFix(ctx, s); err != nil {
		return out, err
	}
	if out.Releases, err = c.releaseHealth(ctx, s); err != nil {
		return out, err
	}
	if out.Types, err = c.errorsByType(ctx, s); err != nil {
		return out, err
	}
	if out.Apps, err = c.errorsByApp(ctx, s); err != nil {
		return out, err
	}
	out.People, err = c.peopleImpact(ctx, s)
	return out, err
}

// errorFlow counts problems opened, fixed and coming back per bucket, and in total.
func (c *Client) errorFlow(ctx context.Context, s AnalyticsScope, step time.Duration) ([]api.ErrorFlowPoint, api.ErrorFlowTotals, error) {
	var totals api.ErrorFlowTotals
	byTS := map[time.Time]*api.ErrorFlowPoint{}
	points := []api.ErrorFlowPoint{}
	for _, b := range bucketsBetween(s.From, s.To, step) {
		points = append(points, api.ErrorFlowPoint{TS: b})
	}
	for i := range points {
		byTS[points[i].TS] = &points[i]
	}
	secs := int(step / time.Second)

	inner, args := s.filter(openedSQL, []any{s.ProjectID})
	q := "SELECT toStartOfInterval(fs, INTERVAL ? SECOND) AS b, count() FROM (" + inner + " GROUP BY issue_id) WHERE fs >= ? AND fs < ? GROUP BY b"
	rows, err := c.conn.Query(ctx, q, append(append([]any{secs}, args...), s.From, s.To)...)
	if err != nil {
		return nil, totals, err
	}
	for rows.Next() {
		var b time.Time
		var n uint64
		if err := rows.Scan(&b, &n); err != nil {
			rows.Close()
			return nil, totals, err
		}
		totals.Opened += n
		if p := byTS[b.UTC()]; p != nil {
			p.Opened += n
		}
	}
	rows.Close()

	q, args = s.filter("SELECT toStartOfInterval(ts, INTERVAL ? SECOND) AS b, countIf("+fixedCond+"), countIf("+cameBackCond+") FROM issues_activity WHERE project_id = ? AND ts >= ? AND ts < ?",
		[]any{secs, s.ProjectID, s.From, s.To})
	rows, err = c.conn.Query(ctx, q+" GROUP BY b", args...)
	if err != nil {
		return nil, totals, err
	}
	defer rows.Close()
	for rows.Next() {
		var b time.Time
		var fixed, back uint64
		if err := rows.Scan(&b, &fixed, &back); err != nil {
			return nil, totals, err
		}
		totals.Fixed += fixed
		totals.CameBack += back
		if p := byTS[b.UTC()]; p != nil {
			p.Fixed += fixed
			p.CameBack += back
		}
	}
	return points, totals, rows.Err()
}

type fixEvent struct {
	IssueID string
	At      time.Time
}

// timeToFix reads the fixes in range, when each problem appeared and when it last came back.
func (c *Client) timeToFix(ctx context.Context, s AnalyticsScope) (api.TimeToFix, error) {
	q, args := s.filter("SELECT issue_id, ts FROM issues_activity WHERE project_id = ? AND ts >= ? AND ts < ? AND "+fixedCond, []any{s.ProjectID, s.From, s.To})
	rows, err := c.conn.Query(ctx, q+" ORDER BY ts DESC LIMIT 5000", args...)
	if err != nil {
		return api.TimeToFix{}, err
	}
	var fixes []fixEvent
	ids := map[string]bool{}
	for rows.Next() {
		var f fixEvent
		if err := rows.Scan(&f.IssueID, &f.At); err != nil {
			rows.Close()
			return api.TimeToFix{}, err
		}
		fixes = append(fixes, f)
		ids[f.IssueID] = true
	}
	rows.Close()
	if len(fixes) == 0 {
		return api.TimeToFix{}, rows.Err()
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	firstSeen := map[string]time.Time{}
	rows, err = c.conn.Query(ctx, "SELECT issue_id, min(first_seen) FROM issues_agg WHERE project_id = ? AND issue_id IN (?) GROUP BY issue_id", s.ProjectID, list)
	if err != nil {
		return api.TimeToFix{}, err
	}
	for rows.Next() {
		var id string
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return api.TimeToFix{}, err
		}
		firstSeen[id] = t
	}
	rows.Close()
	reopened := map[string][]time.Time{}
	rows, err = c.conn.Query(ctx, "SELECT issue_id, ts FROM issues_activity WHERE project_id = ? AND issue_id IN (?) AND ts < ? AND "+reopenCond, s.ProjectID, list, s.To)
	if err != nil {
		return api.TimeToFix{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			return api.TimeToFix{}, err
		}
		reopened[id] = append(reopened[id], t)
	}
	return summarizeFixes(fixes, firstSeen, reopened), rows.Err()
}

// summarizeFixes measures each fix from the moment its problem was last open: when it first
// appeared, or when it last came back (or was reopened) before that fix.
func summarizeFixes(fixes []fixEvent, firstSeen map[string]time.Time, reopened map[string][]time.Time) api.TimeToFix {
	var out api.TimeToFix
	var hours []float64
	for _, f := range fixes {
		start, ok := firstSeen[f.IssueID]
		if !ok {
			continue
		}
		for _, t := range reopened[f.IssueID] {
			if t.Before(f.At) && t.After(start) {
				start = t
			}
		}
		d := f.At.Sub(start)
		if d < 0 {
			d = 0
		}
		hours = append(hours, d.Hours())
		switch {
		case d <= time.Hour:
			out.WithinHour++
		case d <= 24*time.Hour:
			out.WithinDay++
		case d <= 7*24*time.Hour:
			out.WithinWeek++
		default:
			out.Longer++
		}
	}
	out.Fixed = len(hours)
	if len(hours) > 0 {
		sort.Float64s(hours)
		mid := len(hours) / 2
		out.MedianHours = hours[mid]
		if len(hours)%2 == 0 {
			out.MedianHours = (hours[mid-1] + hours[mid]) / 2
		}
		out.MedianHours = math.Round(out.MedianHours*100) / 100
	}
	return out
}

// releaseHealth puts each recent release's errors next to its counted requests.
func (c *Client) releaseHealth(ctx context.Context, s AnalyticsScope) ([]api.ReleaseHealth, error) {
	rows, err := c.conn.Query(ctx, `SELECT release, min(first_seen) AS fs FROM releases WHERE project_id = ? AND release != ''
		GROUP BY release ORDER BY fs DESC, release DESC LIMIT 12`, s.ProjectID)
	if err != nil {
		return nil, err
	}
	out := []api.ReleaseHealth{}
	idx := map[string]int{}
	var names []string
	for rows.Next() {
		var r api.ReleaseHealth
		if err := rows.Scan(&r.Release, &r.FirstSeen); err != nil {
			rows.Close()
			return nil, err
		}
		r.FirstSeen = r.FirstSeen.UTC()
		idx[r.Release] = len(out)
		out = append(out, r)
		names = append(names, r.Release)
	}
	rows.Close()
	if len(out) == 0 {
		return out, rows.Err()
	}

	q, args := s.filter(`SELECT release, count(), uniq(issue_id), uniqIf(user_id_hash, user_id_hash != '')
		FROM error_occurrences WHERE project_id = ? AND ts >= ? AND ts < ? AND release IN (?)`, []any{s.ProjectID, s.From, s.To, names})
	if err := c.eachRow(ctx, q+" GROUP BY release", args, func(scan func(...any) error) error {
		var rel string
		var n, problems, people uint64
		if err := scan(&rel, &n, &problems, &people); err != nil {
			return err
		}
		r := &out[idx[rel]]
		r.Errors, r.Problems, r.People = n, problems, people
		return nil
	}); err != nil {
		return nil, err
	}

	// A problem is new in the release its first occurrence carried, when it first appeared in range.
	firstInRange, fargs := s.filter(openedSQL, []any{s.ProjectID})
	firstInRange += " GROUP BY issue_id HAVING fs >= ? AND fs < ?"
	fargs = append(fargs, s.From, s.To)
	inner, iargs := s.filter("SELECT issue_id, argMin(release, ts) AS r FROM error_occurrences WHERE project_id = ?", []any{s.ProjectID})
	q = "SELECT r, count() FROM (" + inner + " AND issue_id IN (SELECT issue_id FROM (" + firstInRange + ")) GROUP BY issue_id) WHERE r IN (?) GROUP BY r"
	args = append(append(iargs, fargs...), names)
	if err := c.eachRow(ctx, q, args, func(scan func(...any) error) error {
		var rel string
		var n uint64
		if err := scan(&rel, &n); err != nil {
			return err
		}
		out[idx[rel]].NewProblems = n
		return nil
	}); err != nil {
		return nil, err
	}

	q, args = s.filter("SELECT release, sum(requests) FROM request_usage_hourly WHERE project_id = ? AND hour >= ? AND hour < ? AND release IN (?)",
		[]any{s.ProjectID, s.From.Truncate(time.Hour), s.To, names})
	err = c.eachRow(ctx, q+" GROUP BY release", args, func(scan func(...any) error) error {
		var rel string
		var n uint64
		if err := scan(&rel, &n); err != nil {
			return err
		}
		out[idx[rel]].Requests = n
		return nil
	})
	return out, err
}

// errorsByType ranks exception types by errors in range (from the hourly counts, kept for
// PYLOGGLE_RETENTION_STATS_DAYS). Errors logged without an exception have an empty type.
func (c *Client) errorsByType(ctx context.Context, s AnalyticsScope) ([]api.ErrorGroupCount, error) {
	counts, cargs := s.filter("SELECT issue_id, sum(cnt) AS n, uniqIfMergeState(users) AS u FROM issue_counts_hourly WHERE project_id = ? AND bucket >= ? AND bucket < ?",
		[]any{s.ProjectID, s.From.Truncate(time.Hour), s.To})
	types, targs := s.filter("SELECT issue_id, anyLast(exception_type) AS t FROM issues_agg WHERE project_id = ?", []any{s.ProjectID})
	q := "SELECT t, sum(n) AS errors, count() AS problems, uniqIfMerge(u) FROM (" + counts + " GROUP BY issue_id) AS h INNER JOIN (" + types +
		" GROUP BY issue_id) AS a USING (issue_id) GROUP BY t ORDER BY errors DESC, t LIMIT 10"
	return c.errorGroups(ctx, q, append(cargs, targs...))
}

// errorsByApp ranks apps by errors in range.
func (c *Client) errorsByApp(ctx context.Context, s AnalyticsScope) ([]api.ErrorGroupCount, error) {
	q, args := s.filter("SELECT service, sum(cnt) AS errors, uniqExact(issue_id), uniqIfMerge(users) FROM issue_counts_hourly WHERE project_id = ? AND bucket >= ? AND bucket < ?",
		[]any{s.ProjectID, s.From.Truncate(time.Hour), s.To})
	return c.errorGroups(ctx, q+" GROUP BY service ORDER BY errors DESC, service LIMIT 10", args)
}

func (c *Client) errorGroups(ctx context.Context, q string, args []any) ([]api.ErrorGroupCount, error) {
	out := []api.ErrorGroupCount{}
	err := c.eachRow(ctx, q, args, func(scan func(...any) error) error {
		var g api.ErrorGroupCount
		if err := scan(&g.Key, &g.Errors, &g.Problems, &g.People); err != nil {
			return err
		}
		out = append(out, g)
		return nil
	})
	return out, err
}

// peopleImpact ranks the people who hit the most errors. Raw identity only appears when the app
// sends it; otherwise the hash is all there is.
func (c *Client) peopleImpact(ctx context.Context, s AnalyticsScope) ([]api.PersonImpact, error) {
	q, args := s.filter(`SELECT user_id_hash, count() AS n, uniq(issue_id), max(ts),
		anyIf(user_email, user_email != ''), anyIf(user_username, user_username != ''), anyIf(user_id, user_id != '')
		FROM error_occurrences WHERE project_id = ? AND ts >= ? AND ts < ? AND user_id_hash != ''`, []any{s.ProjectID, s.From, s.To})
	out := []api.PersonImpact{}
	err := c.eachRow(ctx, q+" GROUP BY user_id_hash ORDER BY n DESC, user_id_hash LIMIT 10", args, func(scan func(...any) error) error {
		var p api.PersonImpact
		var email, username, id string
		if err := scan(&p.IDHash, &p.Errors, &p.Problems, &p.LastSeen, &email, &username, &id); err != nil {
			return err
		}
		p.LastSeen = p.LastSeen.UTC()
		for _, v := range []string{email, username, id} {
			if v != "" {
				p.Label = v
				break
			}
		}
		out = append(out, p)
		return nil
	})
	return out, err
}

// UsageAnalytics gathers Analytics → Usage from request_usage_hourly. sort orders the endpoints:
// "people" or (default) "requests".
func (c *Client) UsageAnalytics(ctx context.Context, s AnalyticsScope, sortBy string) (api.UsageAnalytics, error) {
	s, err := s.check()
	if err != nil {
		return api.UsageAnalytics{}, err
	}
	step := analyticsBucket(s.From, s.To)
	out := api.UsageAnalytics{From: s.From, To: s.To, BucketSeconds: int(step / time.Second),
		Points: []api.UsagePoint{}, Weeks: []api.UsagePoint{}, Endpoints: []api.EndpointUsage{}, Versions: []api.VersionUsage{}, VersionSeries: []api.AggregateSeries{}}
	base := " FROM request_usage_hourly WHERE project_id = ? AND hour >= ? AND hour < ?"
	scoped := func(from, to time.Time, sel string) (string, []any) {
		return s.filter(sel+base, []any{s.ProjectID, from.Truncate(time.Hour), to})
	}
	const sums = "sum(requests), sum(errors), uniqIfMerge(users)"

	q, args := scoped(s.From, s.To, "SELECT "+sums)
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&out.Totals.Requests, &out.Totals.Errors, &out.Totals.People); err != nil {
		return out, err
	}
	pf, pt := s.previous()
	q, args = scoped(pf, pt, "SELECT "+sums)
	if err := c.conn.QueryRow(ctx, q, args...).Scan(&out.Previous.Requests, &out.Previous.Errors, &out.Previous.People); err != nil {
		return out, err
	}

	points := func(sel string, extra []any, keep func(api.UsagePoint)) error {
		q, args := scoped(s.From, s.To, sel)
		return c.eachRow(ctx, q+" GROUP BY b ORDER BY b", append(extra, args...), func(scan func(...any) error) error {
			var p api.UsagePoint
			if err := scan(&p.TS, &p.Requests, &p.Errors, &p.People); err != nil {
				return err
			}
			p.TS = p.TS.UTC()
			keep(p)
			return nil
		})
	}
	got := map[time.Time]api.UsagePoint{}
	if err := points("SELECT toStartOfInterval(hour, INTERVAL ? SECOND) AS b, "+sums, []any{int(step / time.Second)}, func(p api.UsagePoint) { got[p.TS] = p }); err != nil {
		return out, err
	}
	buckets := bucketsBetween(s.From, s.To, step)
	for _, b := range buckets {
		p, ok := got[b]
		if !ok {
			p = api.UsagePoint{TS: b}
		}
		out.Points = append(out.Points, p)
	}
	if err := points("SELECT toDateTime(toMonday(hour), 'UTC') AS b, "+sums, nil, func(p api.UsagePoint) { out.Weeks = append(out.Weeks, p) }); err != nil {
		return out, err
	}

	order := "requests"
	if sortBy == "people" {
		order = "people"
	}
	q, args = scoped(s.From, s.To, "SELECT http_method, http_route, sum(requests) AS requests, sum(errors), uniqIfMerge(users) AS people")
	if err := c.eachRow(ctx, q+" GROUP BY http_method, http_route ORDER BY "+order+" DESC, http_route, http_method LIMIT 50", args, func(scan func(...any) error) error {
		var e api.EndpointUsage
		if err := scan(&e.Method, &e.Route, &e.Requests, &e.Errors, &e.People); err != nil {
			return err
		}
		out.Endpoints = append(out.Endpoints, e)
		return nil
	}); err != nil {
		return out, err
	}

	q, args = scoped(s.From, s.To, "SELECT release, sum(requests) AS requests, sum(errors), uniqIfMerge(users)")
	var names []string
	if err := c.eachRow(ctx, q+" GROUP BY release ORDER BY requests DESC, release DESC LIMIT 8", args, func(scan func(...any) error) error {
		var v api.VersionUsage
		if err := scan(&v.Release, &v.Requests, &v.Errors, &v.People); err != nil {
			return err
		}
		out.Versions = append(out.Versions, v)
		names = append(names, v.Release)
		return nil
	}); err != nil {
		return out, err
	}
	if len(names) == 0 {
		return out, nil
	}
	firstSeen := map[string]time.Time{}
	if err := c.eachRow(ctx, "SELECT release, min(first_seen) FROM releases WHERE project_id = ? AND release IN (?) GROUP BY release", []any{s.ProjectID, names}, func(scan func(...any) error) error {
		var rel string
		var t time.Time
		if err := scan(&rel, &t); err != nil {
			return err
		}
		firstSeen[rel] = t.UTC()
		return nil
	}); err != nil {
		return out, err
	}
	for i := range out.Versions {
		if t, ok := firstSeen[out.Versions[i].Release]; ok {
			out.Versions[i].FirstSeen = &t
		}
	}

	// Requests per bucket for each version shown, and the rest as "other".
	const other = "\x00other"
	series := map[string][]uint64{}
	pos := map[time.Time]int{}
	for i, b := range buckets {
		pos[b] = i
	}
	q, args = scoped(s.From, s.To, "SELECT toStartOfInterval(hour, INTERVAL ? SECOND) AS b, if(release IN (?), release, ?) AS r, sum(requests)")
	args = append([]any{int(step / time.Second), names, other}, args...)
	if err := c.eachRow(ctx, q+" GROUP BY b, r", args, func(scan func(...any) error) error {
		var b time.Time
		var r string
		var n uint64
		if err := scan(&b, &r, &n); err != nil {
			return err
		}
		i, ok := pos[b.UTC()]
		if !ok {
			return nil
		}
		if series[r] == nil {
			series[r] = make([]uint64, len(buckets))
		}
		series[r][i] += n
		return nil
	}); err != nil {
		return out, err
	}
	for _, name := range append(names, other) {
		if v, ok := series[name]; ok {
			label := name
			if name == other {
				label = "other"
			}
			out.VersionSeries = append(out.VersionSeries, api.AggregateSeries{Name: label, Values: v})
		}
	}
	return out, nil
}

// eachRow runs q and calls fn for every row with its Scan.
func (c *Client) eachRow(ctx context.Context, q string, args []any, fn func(scan func(...any) error) error) error {
	rows, err := c.conn.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}
