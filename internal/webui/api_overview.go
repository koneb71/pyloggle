package webui

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/storage/clickhouse"
)

// apiServiceHealth serves the Overview "Apps" list: ?from&to&env&limit.
func (u *UI) apiServiceHealth(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	items, err := u.store.ServiceHealth(r.Context(), auth.ProjectIDFromContext(r.Context()), r.URL.Query().Get("env"), from, to, qInt(r, "limit", 50, 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.ServiceHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": items})
}

// apiReleases lists recent versions with their errors in the range: ?from&to&limit.
func (u *UI) apiReleases(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	items, err := u.store.ListReleases(r.Context(), auth.ProjectIDFromContext(r.Context()), from, to, qInt(r, "limit", 10, 100))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.Release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": items})
}

// What makes an issue "spike": at least this many times in the range, and at least this many
// times its count in the range before.
const (
	spikeMinCount = 10
	spikeMinRatio = 2.0
)

// apiAttention answers "what needs a look?" for the Overview: issues that came back after being
// resolved, issues first seen in the range, and issues happening much more than in the range
// before. Came-back issues lead (someone believed them fixed); the rest follow by volume.
// ?from&to&service&env&limit. counts holds the totals per reason beyond the limit.
func (u *UI) apiAttention(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	projectID := auth.ProjectIDFromContext(r.Context())
	limit := qInt(r, "limit", 6, 50)
	pf, pt := previousRange(from, to)
	scope := api.AlertRule{ProjectID: projectID, Service: strings.TrimSpace(qs.Get("service")), Env: strings.TrimSpace(qs.Get("env"))}
	rows, err := u.store.IssueSpikeCounts(r.Context(), scope, pf, pt, from, to, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	items, counts, err := u.rankAttention(r, projectID, rows, from)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if len(items) > limit {
		items = items[:limit]
	}
	summaries := make([]api.IssueSummary, len(items))
	for i := range items {
		summaries[i] = items[i].Issue
	}
	u.attachSparklines(r, projectID, summaries)
	for i := range items {
		items[i].Issue = summaries[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": items, "counts": counts})
}

func (u *UI) rankAttention(r *http.Request, projectID string, rows []clickhouse.IssueSpikeRow, from time.Time) ([]api.AttentionItem, map[string]int, error) {
	counts := map[string]int{"regressed": 0, "new": 0, "spike": 0}
	items := []api.AttentionItem{}
	if len(rows) == 0 {
		return items, counts, nil
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.IssueID
	}
	summaries, err := u.store.QueryIssues(r.Context(), clickhouse.IssueListQuery{ProjectID: projectID, IssueIDs: ids, Limit: len(ids)})
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]api.IssueSummary, len(summaries))
	for _, s := range summaries {
		byID[s.IssueID] = s
	}
	for _, row := range rows {
		s, ok := byID[row.IssueID]
		if !ok || s.Status != "open" {
			continue // ignored and resolved issues are handled on purpose
		}
		var reasons []string
		if s.IsRegressed {
			reasons = append(reasons, "regressed")
		}
		if !s.FirstSeen.Before(from) {
			reasons = append(reasons, "new")
		} else if row.CurCount >= spikeMinCount && float64(row.CurCount) >= spikeMinRatio*float64(row.PrevCount) {
			reasons = append(reasons, "spike")
		}
		if len(reasons) == 0 {
			continue
		}
		for _, reason := range reasons {
			counts[reason]++
		}
		items = append(items, api.AttentionItem{Issue: s, Reasons: reasons, Count: row.CurCount, PreviousCount: row.PrevCount})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Reasons[0] == "regressed", items[j].Reasons[0] == "regressed"
		if a != b {
			return a
		}
		return items[i].Count > items[j].Count
	})
	return items, counts, nil
}
