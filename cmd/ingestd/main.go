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

	ch, err := clickhouse.New(cfg.ClickHouseDSN, cfg.MaxQueueSize, cfg.BatchSize, cfg.BatchTimeout)
	if err != nil {
		log.Fatalf("clickhouse connect: %v", err)
	}
	defer func() {
		_ = ch.Close()
	}()

	srv := httpserver.New(cfg, ch)
	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
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
