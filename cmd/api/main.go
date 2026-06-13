// cmd/api/main.go
// GEOLINK API binary — location-aware autocomplete service.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/typesense/typesense-go/typesense"

	geoapi "github.com/barathsuresh/geolink/internal/api"
	"github.com/barathsuresh/geolink/internal/cache"
	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/db"
	"github.com/barathsuresh/geolink/internal/kafka"
)

func main() {
	// ── Structured JSON logging ───────────────────────────────────────────────
	// slog.SetDefault also redirects the standard log package to this handler,
	// so log.Printf calls in dependencies emit JSON too.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	log.SetFlags(0) // timestamps handled by slog

	// ── Config ───────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// ── PostgreSQL ────────────────────────────────────────────────────────────
	pool, err := db.New(cfg)
	if err != nil {
		slog.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("postgres connected")

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb, err := cache.New(cfg)
	if err != nil {
		slog.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()
	slog.Info("redis connected")

	// ── Typesense ─────────────────────────────────────────────────────────────
	tsClient := typesense.NewClient(
		typesense.WithServer(fmt.Sprintf("http://%s:%s", cfg.TypesenseHost, cfg.TypesensePort)),
		typesense.WithAPIKey(cfg.TypesenseAPIKey),
	)
	slog.Info("typesense connected", "host", cfg.TypesenseHost, "port", cfg.TypesensePort)

	// ── Kafka producer (search events) ───────────────────────────────────────
	producer := kafka.NewProducer(cfg.KafkaBrokerList(), kafka.TopicSearchQueries)
	defer func() {
		if err := producer.Close(); err != nil {
			slog.Warn("kafka producer close error", "err", err)
		}
	}()
	slog.Info("kafka producer ready")

	// ── Router ────────────────────────────────────────────────────────────────
	router := geoapi.NewRouter(geoapi.RouterDeps{
		Cfg:      cfg,
		Pool:     pool,
		RDB:      rdb,
		TSClient: tsClient,
		Producer: producer,
	})

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("GEOLINK API listening", "port", cfg.AppPort, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server listen error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	slog.Info("GEOLINK API stopped")
}
