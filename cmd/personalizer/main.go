// cmd/personalizer/main.go
// GEOLINK personalizer binary.
// Consumes SearchEvent messages from search.queries and builds
// per-IP profiles in Redis using personalization.UpdateProfile.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/barathsuresh/geolink/internal/cache"
	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/kafka"
	"github.com/barathsuresh/geolink/internal/models"
	"github.com/barathsuresh/geolink/internal/personalization"
)

func main() {
	// ── Health check server — must start first so Cloud Run probe passes ──────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Printf("health server: %v", err)
		}
	}()

	// ── Config ───────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb, err := cache.New(cfg)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()
	log.Println("Redis connected.")

	// ── Kafka consumer ────────────────────────────────────────────────────────
	brokers := cfg.KafkaBrokerList()
	consumer := kafka.NewConsumer(brokers, kafka.TopicSearchQueries, cfg.KafkaGroupIDPersonalizer)
	defer func() {
		if err := consumer.Close(); err != nil {
			log.Printf("consumer close: %v", err)
		}
	}()
	log.Printf("Kafka consumer ready — group=%s topic=%s",
		cfg.KafkaGroupIDPersonalizer, kafka.TopicSearchQueries)

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigs
		log.Printf("Received %s — shutting down…", s)
		cancel()
	}()

	// ── Consumer loop ─────────────────────────────────────────────────────────
	log.Println("Personalizer running — waiting for search events…")
	var total int

	for {
		msg, err := consumer.ReadMessage(ctx)
		if err != nil {
			// Context cancelled → graceful exit.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				break
			}
			log.Printf("WARN ReadMessage: %v — retrying…", err)
			continue
		}

		// ── Deserialise ───────────────────────────────────────────────────────
		var event models.SearchEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("WARN unmarshal event (offset %d): %v — skipping", msg.Offset, err)
			// Commit bad message so we never get stuck on malformed data.
			_ = consumer.CommitMessage(ctx, msg)
			continue
		}

		// Skip events with no useful signal (empty IP or query).
		if event.IP == "" || event.Query == "" {
			_ = consumer.CommitMessage(ctx, msg)
			continue
		}

		// ── Update Redis profile ──────────────────────────────────────────────
		if err := personalization.UpdateProfile(ctx, rdb, cfg, event); err != nil {
			// Redis failure: log but DO NOT commit — message will be retried on restart.
			log.Printf("WARN UpdateProfile ip=%s: %v — not committing offset %d",
				event.IP, err, msg.Offset)
			continue
		}

		// ── Commit offset ─────────────────────────────────────────────────────
		if err := consumer.CommitMessage(ctx, msg); err != nil {
			log.Printf("WARN commit offset %d: %v", msg.Offset, err)
		}

		total++
		// Real-time per-event log — easy to see activity during testing.
		log.Printf("✦ event #%d | ip=%-15s | q=%-20q | country=%-3s | continent=%-13s | feature=%s",
			total, event.IP, event.Query, event.CountryCode, event.Continent, event.FeatureCode)
	}

	log.Printf("Personalizer stopped. Total events processed: %d", total)
}
