// Command wtwlt-server is the Raspberry Pi backend: it ingests weather-station
// MQTT messages into SQLite and serves the website/API from the same binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tlugger/wtwlt/server/internal/config"
	"github.com/tlugger/wtwlt/server/internal/ingest"
	"github.com/tlugger/wtwlt/server/internal/store"
	"github.com/tlugger/wtwlt/server/internal/web"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("wtwlt-server %s\n", version)
		return
	}

	cfg := config.Load()
	log.Printf("wtwlt-server %s starting (db=%s, mqtt=%s:%s, http=%s)",
		version, cfg.DBPath, cfg.MQTTHost, cfg.MQTTPort, cfg.HTTPAddr)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ing := ingest.New(cfg, st)
	if err := ing.Start(); err != nil {
		// Non-fatal: the client keeps retrying in the background.
		log.Printf("ingest: initial connect pending: %v", err)
	}
	defer ing.Stop()

	// Rollups + retention: recompute hourly/daily aggregates from raw and prune
	// raw older than the retention window. Idempotent; runs on a timer.
	runMaintenance := func() {
		now := time.Now().UTC()
		if err := st.RollupHourly(now.AddDate(0, 0, -9)); err != nil {
			log.Printf("maintenance: rollup hourly: %v", err)
		}
		dailySince := now.AddDate(0, 0, -3650) // ~all raw when retention is disabled
		if cfg.RetentionDays > 0 {
			dailySince = now.AddDate(0, 0, -(cfg.RetentionDays + 2))
		}
		if err := st.RollupDaily(dailySince); err != nil {
			log.Printf("maintenance: rollup daily: %v", err)
		}
		if cfg.RetentionDays > 0 {
			if n, err := st.PruneRaw(now.AddDate(0, 0, -cfg.RetentionDays)); err != nil {
				log.Printf("maintenance: prune raw: %v", err)
			} else if n > 0 {
				log.Printf("maintenance: pruned %d raw readings older than %dd", n, cfg.RetentionDays)
			}
		}
	}
	runMaintenance() // populate rollups on startup
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			runMaintenance()
		}
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           web.New(st).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("http: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	// Wait for a termination signal, then shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Printf("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
