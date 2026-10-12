package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // recap rules name IANA time zones; the Alpine image has no zoneinfo

	"github.com/koneb71/pyloggle/internal/alerting"
	"github.com/koneb71/pyloggle/internal/config"
	"github.com/koneb71/pyloggle/internal/httpserver"
	"github.com/koneb71/pyloggle/internal/storage/clickhouse"
	"github.com/koneb71/pyloggle/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-version", "--version", "version":
			fmt.Println("pyloggle ingestd " + version.String())
			return
		case "-h", "-help", "--help", "help":
			fmt.Println("pyloggle ingestd " + version.String() + "\n\nConfigured through environment variables; see " + config.DocsHome + "reference/configuration")
			return
		}
	}
	cfg := config.FromEnv()
	log.Printf("pyloggle ingestd %s", version.String())
	if err := cfg.Validate(); err != nil {
		log.Fatalf("configuration: %v", err)
	}
	if cfg.UIAuthMode == "disabled" {
		log.Printf("WARNING: sign-in is off (PYLOGGLE_UI_AUTH=disabled): anyone who can reach port %s can read and change everything. Use it only on a trusted network.", cfg.Port)
	}

	ch := connectWithRetry(cfg, 2*time.Minute)
	defer func() {
		_ = ch.Close()
	}()

	migrateOpts := clickhouse.MigrateOptions{
		Rebuild:              cfg.MigrateRebuild,
		RetentionLogsDays:    cfg.RetentionLogsDays,
		RetentionErrorsDays:  cfg.RetentionErrorsDays,
		RetentionStatsDays:   cfg.RetentionStatsDays,
		RetentionSpansDays:   cfg.RetentionSpansDays,
		RetentionMetricsDays: cfg.RetentionMetricsDays,
	}
	if cfg.AutoMigrate {
		mctx, mcancel := context.WithTimeout(context.Background(), 30*time.Minute)
		applied, err := ch.Migrate(mctx, migrateOpts)
		mcancel()
		if err != nil {
			log.Fatalf("schema migration failed (applied %v): %v", applied, err)
		}
		if len(applied) == 0 {
			log.Printf("schema up to date")
		}
	}
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := ch.ApplyRetention(rctx, migrateOpts); err != nil {
		log.Printf("retention: %v (continuing with the current TTLs)", err)
	}
	rcancel()

	srv := httpserver.New(cfg, ch)
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Optional alerting runner (polls ClickHouse for enabled rules).
	if cfg.AlertingEnabled {
		r := alerting.NewRunner(cfg, ch)
		go r.Run(ctx)
	}
	// Per-project retention overrides: one pass shortly after start, then daily at 03:10 UTC.
	go runTrimmer(ctx, ch, migrateOpts)

	go func() {
		log.Printf("pyloggle ingestd listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	_ = httpSrv.Shutdown(sctx)
}

// connectWithRetry keeps trying while ClickHouse boots (common under docker compose).
func connectWithRetry(cfg config.Config, maxWait time.Duration) *clickhouse.Client {
	deadline := time.Now().Add(maxWait)
	backoff := time.Second
	for {
		ch, err := clickhouse.New(cfg.ClickHouseDSN, cfg.MaxQueueSize, cfg.BatchSize, cfg.BatchTimeout)
		if err == nil {
			return ch
		}
		if time.Now().After(deadline) {
			log.Fatalf("clickhouse connect: %v", err)
		}
		log.Printf("clickhouse not ready (%v); retrying in %s", err, backoff)
		time.Sleep(backoff)
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

// runTrimmer applies per-project retention overrides on a daily schedule.
func runTrimmer(ctx context.Context, ch *clickhouse.Client, opts clickhouse.MigrateOptions) {
	run := func() {
		tctx, cancel := context.WithTimeout(ctx, time.Hour)
		defer cancel()
		if err := ch.TrimProjects(tctx, opts, log.Printf); err != nil {
			log.Printf("retention trim: %v", err)
		}
	}
	select {
	case <-time.After(2 * time.Minute):
		run()
	case <-ctx.Done():
		return
	}
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 10, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		select {
		case <-time.After(time.Until(next)):
			run()
		case <-ctx.Done():
			return
		}
	}
}
