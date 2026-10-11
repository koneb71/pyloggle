package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pyloggle/internal/alerting"
	"pyloggle/internal/config"
	"pyloggle/internal/httpserver"
	"pyloggle/internal/storage/clickhouse"
)

func main() {
	cfg := config.FromEnv()

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
