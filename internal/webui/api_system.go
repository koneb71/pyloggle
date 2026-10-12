package webui

import (
	"net/http"
	"runtime"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
	"github.com/koneb71/pyloggle/internal/metrics"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
	"github.com/koneb71/pyloggle/internal/sysinfo"
	"github.com/koneb71/pyloggle/internal/version"
)

type systemTable struct {
	clickhouse.TableInfo
	RetentionDays int `json:"retention_days,omitempty"`
}

// apiSystem answers GET /system (admin): process, ClickHouse, ingest, retention and alerting
// health. The numbers are the same ones /metrics exposes, shaped for the page.
func (u *UI) apiSystem(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	now := time.Now().UTC()
	started := u.startedAt
	var snap sysinfo.Snapshot
	if u.sys != nil {
		snap = u.sys.Snapshot()
		if started.IsZero() {
			started = snap.StartedAt
		}
	}
	if started.IsZero() {
		started = now
	}
	retention := map[string]int{"logs_raw": u.cfg.RetentionLogsDays, "error_occurrences": u.cfg.RetentionErrorsDays, "spans": u.cfg.RetentionSpansDays, "metrics_points": u.cfg.RetentionMetricsDays, "issue_counts_hourly": u.cfg.RetentionStatsDays, "request_stats_minutely": u.cfg.RetentionStatsDays}
	tables := []systemTable{}
	for _, t := range snap.Info.Tables {
		tables = append(tables, systemTable{TableInfo: t, RetentionDays: retention[t.Name]})
	}
	rate, _ := u.store.GlobalLogVolume(r.Context(), now.Add(-time.Hour), now, 60)
	if rate == nil {
		rate = []api.TimeSeriesPoint{}
	}
	rules, _ := u.store.ListEnabledAlertRules(r.Context())
	stats := u.store.Stats()
	if stats.Queues == nil {
		stats.Queues = []clickhouse.BatcherStats{}
	}
	var lastRun *time.Time
	if ts := metrics.AlertLastRun.Get(); ts > 0 {
		t := time.Unix(int64(ts), 0).UTC()
		lastRun = &t
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    version.String(),
		"go_version": runtime.Version(),
		"started_at": started,
		"uptime_s":   int(now.Sub(started).Seconds()),
		"clickhouse": map[string]any{
			"up": snap.Up, "version": snap.Info.Version, "sampled_at": snap.At, "error": snap.Error,
			"disk":              map[string]uint64{"free": snap.Info.DiskFree, "total": snap.Info.DiskTotal},
			"mutations_pending": snap.Info.MutationsPending,
			"tables":            tables,
		},
		"ingest": map[string]any{
			"rate":     rate,
			"events":   metrics.IngestEvents.ByLabel("outcome"),
			"rejected": metrics.IngestRejected.ByLabel("door"),
			"requests": metrics.IngestRequests.ByLabel("door"),
			"queues":   stats.Queues,
			"streams":  map[string]float64{"active": metrics.StreamsActive.Get(), "overflow": metrics.StreamOverflow.Get()},
			"spans":    metrics.SpansAccepted.ByLabel("project"),
			"points":   metrics.PointsAccepted.ByLabel("project"),
		},
		"retention": map[string]int{"logs": u.cfg.RetentionLogsDays, "errors": u.cfg.RetentionErrorsDays, "spans": u.cfg.RetentionSpansDays, "metrics": u.cfg.RetentionMetricsDays, "stats": u.cfg.RetentionStatsDays},
		"alerting": map[string]any{
			"enabled": u.cfg.AlertingEnabled, "interval_s": int(u.cfg.AlertingPollInterval.Seconds()), "rules_enabled": len(rules), "last_run": lastRun,
			"evaluations": metrics.AlertEvaluations.ByLabel("condition"), "firings": metrics.AlertFirings.ByLabel("condition"), "notify_failures": metrics.AlertNotifyFailure.ByLabel("notifier"),
		},
	})
}
