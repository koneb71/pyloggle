package webui

import (
	"net/http"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/auth"
	"github.com/koneb71/pyloggle/internal/logquery"
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

// apiChartAggregate answers Analytics → Charts: GET /logs/aggregate's parameters over a source
// (messages, errors or requests), searched with q in the query language.
func (u *UI) apiChartAggregate(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	source := chartSourceParam(r)
	schema, ok := clickhouse.ChartSchema(source)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_source", "source must be messages, errors or requests")
		return
	}
	parsed, err := logquery.Parse(qs.Get("q"))
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	if _, _, err := parsed.SQLFor(schema); err != nil {
		writeQueryErr(w, err)
		return
	}
	from, to := qRange(r, 24*time.Hour)
	spec, ok := aggregateSpec(w, r, source, from, to)
	if !ok {
		return
	}
	q := clickhouse.ChartQuery{Source: source, ProjectID: auth.ProjectIDFromContext(r.Context()), Service: qs.Get("service"), Env: qs.Get("env"), From: from, To: to, Query: parsed}
	agg, err := u.store.ChartAggregate(r.Context(), q, spec)
	writeAggregate(w, agg, err)
}

// apiChartFields lists what a chart source can be searched, measured and split by.
func (u *UI) apiChartFields(w http.ResponseWriter, r *http.Request) {
	source := chartSourceParam(r)
	schema, ok := clickhouse.ChartSchema(source)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_source", "source must be messages, errors or requests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source":  source,
		"fields":  schema.Kinds(),
		"metrics": clickhouse.ChartMetrics[source],
		"attrs":   source != "requests",
	})
}

func chartSourceParam(r *http.Request) string {
	s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if s == "" {
		return "messages"
	}
	return s
}
