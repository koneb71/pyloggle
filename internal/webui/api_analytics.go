package webui

import (
	"net/http"
	"time"

	"github.com/koneb71/pyloggle/internal/auth"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
)

// analyticsScope reads Analytics' shared filters: the range (last 30 days by default), the app
// and the environment.
func analyticsScope(r *http.Request) clickhouse.AnalyticsScope {
	from, to := qRange(r, 30*24*time.Hour)
	qs := r.URL.Query()
	return clickhouse.AnalyticsScope{ProjectID: auth.ProjectIDFromContext(r.Context()), Service: qs.Get("service"), Env: qs.Get("env"), From: from, To: to}
}

// apiErrorAnalytics answers Analytics → Errors.
func (u *UI) apiErrorAnalytics(w http.ResponseWriter, r *http.Request) {
	out, err := u.store.ErrorAnalytics(r.Context(), analyticsScope(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	out.DetailsKeptDays = u.cfg.RetentionErrorsDays
	writeJSON(w, http.StatusOK, out)
}

// apiUsageAnalytics answers Analytics → Usage; ?sort=people ranks endpoints by people.
func (u *UI) apiUsageAnalytics(w http.ResponseWriter, r *http.Request) {
	out, err := u.store.UsageAnalytics(r.Context(), analyticsScope(r), r.URL.Query().Get("sort"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
