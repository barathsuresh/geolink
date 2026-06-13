// cmd/bulkimport/main.go
// Fast bulk loader for GeoNames data — bypasses Kafka entirely.
//
// Pipeline:
//   TSV parser → 50K-record batches → channel → N workers → PG COPY || TS Import (parallel)
//
// Postgres optimizations applied automatically:
//   • Table set UNLOGGED before load  (disables WAL, ~3x faster writes)
//   • Secondary indexes dropped before load, rebuilt concurrently after
//   • Direct COPY to geonames table (no staging table overhead)
//
// Flags:
//   -truncate   TRUNCATE geonames before import (default: false, uses ON CONFLICT)
//   -skip-ts    Skip Typesense import (Postgres only)
//   -skip-pg    Skip Postgres import (Typesense only)
//   -batch      Batch size (default 50000)
//   -workers    Parallel batch workers (default 4)
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	migrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/typesense/typesense-go/typesense"
	tsapi "github.com/typesense/typesense-go/typesense/api"

	"github.com/barathsuresh/geolink/internal/config"
	"github.com/barathsuresh/geolink/internal/db"
	"github.com/barathsuresh/geolink/internal/models"
)

var geonameColumns = []string{
	"geoname_id", "name", "ascii_name", "alternate_names",
	"latitude", "longitude", "feature_class", "feature_code", "country_code",
	"admin1_code", "admin2_code", "population", "elevation", "timezone", "modified_at",
}

func main() {
	truncate   := flag.Bool("truncate", false, "TRUNCATE geonames table before import (fastest for first run)")
	skipTS     := flag.Bool("skip-ts", false, "skip Typesense import")
	skipPG     := flag.Bool("skip-pg", false, "skip Postgres import")
	batchSz    := flag.Int("batch", 50_000, "records per batch")
	numWorkers := flag.Int("workers", 4, "parallel batch workers")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// ── Postgres ──────────────────────────────────────────────────────────────
	var pool *pgxpool.Pool
	if !*skipPG {
		pool, err = db.New(cfg)
		if err != nil {
			slog.Error("postgres connect", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		slog.Info("postgres connected")
	}

	// ── Typesense ─────────────────────────────────────────────────────────────
	var tsClient *typesense.Client
	if !*skipTS {
		tsClient = typesense.NewClient(
			typesense.WithServer(fmt.Sprintf("http://%s:%s", cfg.TypesenseHost, cfg.TypesensePort)),
			typesense.WithAPIKey(cfg.TypesenseAPIKey),
			typesense.WithConnectionTimeout(5*time.Minute),
		)
		slog.Info("typesense connected", "host", cfg.TypesenseHost, "port", cfg.TypesensePort)

		if *truncate {
			slog.Info("dropping typesense collection")
			_, _ = tsClient.Collection(cfg.TypesenseCollection).Delete(ctx)
			for i := 0; i < 30; i++ {
				time.Sleep(2 * time.Second)
				healthy, err := tsClient.Health(ctx, 3*time.Second)
				if err == nil && healthy {
					break
				}
				slog.Info("waiting for typesense to settle...", "attempt", i+1)
			}
		}
		if err := initCollection(tsClient, cfg.TypesenseCollection); err != nil {
			slog.Error("typesense init collection", "err", err)
			os.Exit(1)
		}
	}

	// ── Run migrations ────────────────────────────────────────────────────────
	if !*skipPG {
		migDSN := fmt.Sprintf("pgx5://%s:%s@%s:%s/%s?sslmode=%s",
			cfg.PostgresUser, cfg.PostgresPassword,
			cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB, cfg.PostgresSSLMode,
		)
		m, err := migrate.New("file://migrations", migDSN)
		if err != nil {
			slog.Error("migrate init", "err", err)
			os.Exit(1)
		}
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			slog.Error("migrate up", "err", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	}

	// ── Pre-load Postgres optimizations ──────────────────────────────────────
	if !*skipPG {
		if *truncate {
			slog.Info("truncating geonames table")
			if _, err := pool.Exec(ctx, "TRUNCATE TABLE geonames"); err != nil {
				slog.Error("truncate failed", "err", err)
				os.Exit(1)
			}
		}
		slog.Info("disabling indexes and WAL for bulk load")
		if err := disableIndexes(ctx, pool); err != nil {
			slog.Error("disable indexes failed", "err", err)
			os.Exit(1)
		}
	}

	// ── Ensure data file exists (download + unzip if missing) ─────────────────
	slog.Info("checking data file", "path", cfg.GeonamesFilePath)
	if err := ensureDataFile(cfg.GeonamesFilePath, cfg.GeonamesDownloadURL); err != nil {
		slog.Error("ensure data file", "err", err)
		os.Exit(1)
	}

	// ── Stream TSV → channel → worker pool ────────────────────────────────────
	t0 := time.Now()
	var total atomic.Int64

	f, err := os.Open(cfg.GeonamesFilePath)
	if err != nil {
		slog.Error("open file", "path", cfg.GeonamesFilePath, "err", err)
		os.Exit(1)
	}
	defer f.Close()

	const scanBuf = 4 << 20 // 4 MB scanner buffer
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scanBuf), scanBuf)

	// Use "create" action for fresh imports — skips per-doc lookup, much faster.
	tsAction := "upsert"
	if *truncate {
		tsAction = "create"
	}

	// Buffered channel: allows the file reader to stay ahead of workers.
	batchCh := make(chan []models.GeoName, *numWorkers*2)

	// Producer: reads file, sends batches.
	var scanErr error
	go func() {
		defer close(batchCh)
		buf := make([]models.GeoName, 0, *batchSz)
		for scanner.Scan() {
			g, err := parseLine(scanner.Text())
			if err != nil || g == nil {
				continue
			}
			buf = append(buf, *g)
			if len(buf) >= *batchSz {
				send := make([]models.GeoName, len(buf))
				copy(send, buf)
				batchCh <- send
				buf = buf[:0]
			}
		}
		if len(buf) > 0 {
			batchCh <- buf
		}
		scanErr = scanner.Err()
	}()

	slog.Info("starting import",
		"file", cfg.GeonamesFilePath,
		"batch_size", *batchSz,
		"workers", *numWorkers,
		"ts_action", tsAction,
	)

	// Worker pool: each worker consumes batches from channel,
	// running PG COPY and TS import in parallel within each batch.
	var wg sync.WaitGroup
	for i := 0; i < *numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range batchCh {
				var innerWG sync.WaitGroup
				var pgErr, tsErr error

				if !*skipPG {
					innerWG.Add(1)
					go func(batch []models.GeoName) {
						defer innerWG.Done()
						pgErr = pgCopy(ctx, pool, batch, *truncate)
					}(b)
				}

				if !*skipTS {
					innerWG.Add(1)
					go func(batch []models.GeoName) {
						defer innerWG.Done()
						tsErr = tsImport(ctx, tsClient, cfg.TypesenseCollection, batch, tsAction)
					}(b)
				}

				innerWG.Wait()

				if pgErr != nil {
					slog.Error("postgres batch failed", "err", pgErr)
					os.Exit(1)
				}
				if tsErr != nil {
					slog.Error("typesense batch failed", "err", tsErr)
					// Non-fatal: TS failures logged per-doc inside tsImport.
				}

				n := total.Add(int64(len(b)))
				if n%500_000 == 0 {
					elapsed := time.Since(t0)
					slog.Info("progress",
						"records",      n,
						"elapsed",      elapsed.Round(time.Second).String(),
						"rate_per_sec", int(float64(n)/elapsed.Seconds()),
					)
				}
			}
		}()
	}

	wg.Wait()

	if scanErr != nil {
		slog.Error("scanner", "err", scanErr)
		os.Exit(1)
	}

	// ── Post-load Postgres: rebuild indexes concurrently + set LOGGED ──────────
	if !*skipPG {
		slog.Info("rebuilding indexes concurrently (this takes ~2-5 min for 12M records)")
		if err := rebuildIndexes(ctx, pool); err != nil {
			slog.Error("rebuild indexes failed", "err", err)
			os.Exit(1)
		}
		slog.Info("indexes rebuilt")
	}

	elapsed := time.Since(t0)
	slog.Info("import complete",
		"total_records", total.Load(),
		"elapsed",       elapsed.Round(time.Second).String(),
		"avg_rate",      fmt.Sprintf("%.0f rec/s", float64(total.Load())/elapsed.Seconds()),
	)
}

// ── Postgres ──────────────────────────────────────────────────────────────────

func disableIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		ALTER TABLE geonames SET UNLOGGED;
		DROP INDEX IF EXISTS idx_geonames_country;
		DROP INDEX IF EXISTS idx_geonames_feature_code;
		DROP INDEX IF EXISTS idx_geonames_population;
	`)
	return err
}

func rebuildIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	indexes := []struct{ name, stmt string }{
		{"idx_geonames_country", `CREATE INDEX idx_geonames_country ON geonames(country_code)`},
		{"idx_geonames_feature_code", `CREATE INDEX idx_geonames_feature_code ON geonames(feature_code)`},
		{"idx_geonames_population", `CREATE INDEX idx_geonames_population ON geonames(population DESC)`},
	}

	errs := make([]error, len(indexes))
	var wg sync.WaitGroup
	for i, idx := range indexes {
		i, idx := i, idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := pool.Exec(ctx, idx.stmt); err != nil {
				errs[i] = fmt.Errorf("%s: %w", idx.name, err)
			}
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	_, err := pool.Exec(ctx, `ALTER TABLE geonames SET LOGGED`)
	return err
}

func pgCopy(ctx context.Context, pool *pgxpool.Pool, batch []models.GeoName, truncated bool) error {
	rows := make([][]any, len(batch))
	for i, g := range batch {
		rows[i] = []any{
			g.GeonameID, g.Name, g.ASCIIName, g.AlternateNames,
			g.Latitude, g.Longitude, g.FeatureClass, g.FeatureCode, g.CountryCode,
			g.Admin1Code, g.Admin2Code, g.Population, g.Elevation, g.Timezone, g.ModifiedAt,
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if truncated {
		if _, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"geonames"},
			geonameColumns,
			pgx.CopyFromRows(rows),
		); err != nil {
			return fmt.Errorf("copy: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `
			CREATE TEMP TABLE _bulk_staging (
				geoname_id BIGINT, name TEXT, ascii_name TEXT, alternate_names TEXT,
				latitude DOUBLE PRECISION, longitude DOUBLE PRECISION,
				feature_class VARCHAR(1), feature_code VARCHAR(10), country_code CHAR(2),
				admin1_code VARCHAR(20), admin2_code VARCHAR(80),
				population BIGINT, elevation INT, timezone VARCHAR(40), modified_at TEXT
			) ON COMMIT DROP`,
		); err != nil {
			return fmt.Errorf("create staging: %w", err)
		}
		if _, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"_bulk_staging"},
			geonameColumns,
			pgx.CopyFromRows(rows),
		); err != nil {
			return fmt.Errorf("copy staging: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO geonames SELECT
				geoname_id, name, ascii_name, alternate_names,
				latitude, longitude, feature_class, feature_code, country_code,
				admin1_code, admin2_code, population, elevation, timezone, modified_at::DATE
			FROM _bulk_staging
			ON CONFLICT (geoname_id) DO UPDATE SET
				name=EXCLUDED.name, ascii_name=EXCLUDED.ascii_name,
				alternate_names=EXCLUDED.alternate_names,
				latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude,
				feature_class=EXCLUDED.feature_class, feature_code=EXCLUDED.feature_code,
				country_code=EXCLUDED.country_code, admin1_code=EXCLUDED.admin1_code,
				admin2_code=EXCLUDED.admin2_code, population=EXCLUDED.population,
				elevation=EXCLUDED.elevation, timezone=EXCLUDED.timezone,
				modified_at=EXCLUDED.modified_at`,
		); err != nil {
			return fmt.Errorf("merge: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// ── Typesense ─────────────────────────────────────────────────────────────────

const maxAltNamesBytes = 4096

func tsImport(ctx context.Context, client *typesense.Client, collection string, batch []models.GeoName, action string) error {
	docs := make([]interface{}, 0, len(batch))
	for _, g := range batch {
		doc := map[string]any{
			"id":           fmt.Sprintf("%d", g.GeonameID),
			"geoname_id":   g.GeonameID,
			"name":         g.Name,
			"ascii_name":   g.ASCIIName,
			"country_code": g.CountryCode,
			"feature_code": g.FeatureCode,
			"feature_class": g.FeatureClass,
			"population":   g.Population,
			"latitude":     g.Latitude,
			"longitude":    g.Longitude,
			"timezone":     g.Timezone,
		}
		// Omit alternate_names if empty (optional field — sending "" can cause
		// validation failures in some Typesense versions). Truncate if huge:
		// some GeoNames entries exceed 100KB, hitting Typesense doc size limits.
		if alt := g.AlternateNames; alt != "" {
			if len(alt) > maxAltNamesBytes {
				alt = alt[:maxAltNamesBytes]
				if i := strings.LastIndexByte(alt, ','); i > 0 {
					alt = alt[:i]
				}
			}
			doc["alternate_names"] = alt
		}
		docs = append(docs, doc)
	}

	results, err := client.Collection(collection).Documents().Import(ctx, docs, &tsapi.ImportDocumentsParams{
		Action: &action,
	})
	if err != nil {
		return fmt.Errorf("typesense import: %w", err)
	}

	failed := 0
	for i, r := range results {
		if !r.Success {
			failed++
			if failed <= 5 {
				id := ""
				if m, ok := docs[i].(map[string]any); ok {
					id, _ = m["id"].(string)
				}
				slog.Warn("typesense doc error", "id", id, "error", r.Error)
			}
		}
	}
	if failed > 0 {
		slog.Warn("typesense partial failure", "failed", failed, "total", len(batch))
	}
	return nil
}
