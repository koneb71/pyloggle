package webui

import (
	"net/http"
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

// apiRequestStats serves the dashboard request tiles from request_stats_minutely.
func (u *UI) apiRequestStats(w http.ResponseWriter, r *http.Request) {
	from, to := qRange(r, 24*time.Hour)
	qs := r.URL.Query()
	bucket := qInt(r, "bucket_seconds", 0, 7*86400)
	if bucket == 0 {
		bucket = int(clickhouse.NiceBucket(from, to, qInt(r, "buckets", 80, 500)) / time.Second)
	}
	st, err := u.store.RequestStats(r.Context(), auth.ProjectIDFromContext(r.Context()), qs.Get("service"), qs.Get("env"), from, to, bucket, qInt(r, "limit", 10, 100))
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
	writeJSON(w, http.StatusOK, st)
}
