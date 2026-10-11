package httpserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"pyloggle/internal/metrics"
)

func TestMetricsHandler(t *testing.T) {
	metrics.IngestRequests.Inc("ingest", "202")
	rec := httptest.NewRecorder()
	MetricsHandler("").ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("open metrics: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{"# TYPE pyloggle_ingest_events_total counter", `pyloggle_ingest_requests_total{door="ingest",status="202"}`, "# TYPE pyloggle_ch_flush_seconds histogram", "pyloggle_ch_queue_capacity"} {
		if !strings.Contains(body, want) {
			t.Fatalf("exposition missing %q:\n%s", want, body[:min(len(body), 2000)])
		}
	}
	rec = httptest.NewRecorder()
	MetricsHandler("s3cret").ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("token required: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	MetricsHandler("s3cret").ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("with token: %d", rec.Code)
	}
}
