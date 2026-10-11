package webui

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/storage/clickhouse"
)

func (u *UI) apiOverview(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	o, err := u.store.Overview(r.Context(), auth.ProjectIDFromContext(r.Context()), qs.Get("service"), qs.Get("env"), from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "overview": o})
}

func (u *UI) apiErrorTrend(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	bucket := qInt(r, "bucket_seconds", 0, 7*86400)
	if bucket == 0 {
		bucket = int(clickhouse.NiceBucket(from, to, qInt(r, "buckets", 80, 500)) / time.Second)
	}
	pts, err := u.store.ErrorTrend(r.Context(), auth.ProjectIDFromContext(r.Context()), qs.Get("service"), qs.Get("env"), qs.Get("http_route"), qs.Get("http_method"), from, to, bucket)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if pts == nil {
		pts = []api.TimeSeriesPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bucket_seconds": bucket, "points": pts})
}

func (u *UI) apiLogVolume(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	h, err := u.store.LogVolumeByLevel(r.Context(), auth.ProjectIDFromContext(r.Context()), qs.Get("service"), qs.Get("env"), from, to, qInt(r, "bucket_seconds", 0, 7*86400))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (u *UI) apiTopIssues(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	items, err := u.store.TopIssues(r.Context(), auth.ProjectIDFromContext(r.Context()), qs.Get("service"), qs.Get("env"), from, to, qInt(r, "limit", 10, 100))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.IssueSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "items": items})
}

type requestStatsResponse struct {
	api.RequestStats
	Source    string `json:"source"`
	Available bool   `json:"available"`
}

// apiRequestStats serves the dashboard request tiles from request_stats_minutely (source=logs,
// the default) or from the OpenTelemetry HTTP server duration histogram (source=otel).
func (u *UI) apiRequestStats(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	bucket := qInt(r, "bucket_seconds", 0, 7*86400)
	if bucket == 0 {
		bucket = int(clickhouse.NiceBucket(from, to, qInt(r, "buckets", 80, 500)) / time.Second)
	}
	projectID := auth.ProjectIDFromContext(r.Context())
	source := strings.ToLower(strings.TrimSpace(qs.Get("source")))
	if source == "" {
		source = "logs"
	}
	var st api.RequestStats
	var err error
	available := true
	switch source {
	case "logs":
		st, err = u.store.RequestStats(r.Context(), projectID, qs.Get("service"), qs.Get("env"), from, to, bucket, qInt(r, "limit", 10, 100))
	case "otel":
		st, available, err = u.store.RequestStatsFromOTel(r.Context(), projectID, qs.Get("service"), qs.Get("env"), from, to, bucket, qInt(r, "limit", 10, 100))
	default:
		writeErr(w, http.StatusBadRequest, "bad_source", "source must be logs or otel")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if st.Points == nil {
		st.Points = []api.RequestPoint{}
	}
	if st.Routes == nil {
		st.Routes = []api.RouteStats{}
	}
	if st.BucketSeconds == 0 {
		st.BucketSeconds, st.From, st.To = bucket, from, to
	}
	writeJSON(w, http.StatusOK, requestStatsResponse{RequestStats: st, Source: source, Available: available})
}

// apiMetricNames lists the OTLP metrics the project has reported (the dashboard picker).
func (u *UI) apiMetricNames(w http.ResponseWriter, r *http.Request) {
	items, err := u.store.MetricNames(r.Context(), auth.ProjectIDFromContext(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.MetricInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "aggs": clickhouse.MetricAggs})
}

// apiMetricSeries computes one metric over time: ?name&agg&group_by&from&to&buckets&service&env&limit.
func (u *UI) apiMetricSeries(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	name := strings.TrimSpace(qs.Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "missing name")
		return
	}
	from, to := qRange(r, 24*time.Hour)
	q := clickhouse.MetricQuery{
		ProjectID: auth.ProjectIDFromContext(r.Context()), Name: name, Agg: qs.Get("agg"), GroupBy: qs.Get("group_by"),
		Service: qs.Get("service"), Env: qs.Get("env"), From: from, To: to, Limit: qInt(r, "limit", 10, 50),
	}
	if secs := qInt(r, "bucket_seconds", 0, 7*86400); secs > 0 {
		q.Bucket = time.Duration(secs) * time.Second
	} else {
		q.Bucket = clickhouse.NiceBucket(from, to, qInt(r, "buckets", 60, 500))
	}
	series, found, err := u.store.MetricSeries(r.Context(), q)
	if err != nil {
		if errors.Is(err, clickhouse.ErrBadAgg) {
			writeErr(w, http.StatusBadRequest, "bad_agg", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "unknown_metric", "the project has not reported "+name)
		return
	}
	if series.Series == nil {
		series.Series = []api.MetricSerie{}
	}
	writeJSON(w, http.StatusOK, series)
}
