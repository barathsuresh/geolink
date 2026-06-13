# GeoLink

**Real-time location-aware autocomplete over 12 million place names.**

GeoLink learns from each user's search history and re-ranks results so frequent travellers to a region automatically see local places rise to the top — no login, no account, no configuration needed.

---

## Demo

![GeoLink terminal demo](demo/demo.gif)

```bash
# Cold search — no personalisation
curl "http://localhost:8080/api/v1/search?q=Manila&personalized=false"

# Personalised — results re-ranked by IP search history
curl "http://localhost:8080/api/v1/search?q=Man&personalized=true"

# Smart recommendations — history + geo-IP + global fallback
curl "http://localhost:8080/api/v1/recommendations"
```

---

## Architecture

### Import pipeline (`cmd/bulkimport`)

```
allCountries.txt (12M records, ~1.7 GB)
        │
        ▼
  TSV parser  ──►  buffered channel  ──►  N parallel workers
                                                │
                              ┌─────────────────┴──────────────────┐
                              ▼                                     ▼
                    PostgreSQL COPY                      Typesense batch import
                  (UNLOGGED + no indexes                  (50 k-doc batches,
                   during load, rebuilt                   upsert or create)
                   concurrently after)
```

No message broker in the import path — each worker runs Postgres `COPY` and Typesense import **in parallel** within each batch, then waits for both before moving on. Indexes are dropped before the load and rebuilt concurrently afterwards, cutting a 12 M-record import from ~45 min to ~8 min.

---

### Live search & personalisation flow

```
User  ──►  GET /api/v1/search
                  │
                  ▼
           Typesense search  (sub-5 ms full-text)
                  │
          PersonalizationEnabled?
           │              │
          YES             NO ──► return raw results
           │
     GetProfile(Redis)
     dominantCountry ≥ 30%?
           │              │
          YES             NO
           │               └──► Rerank(results, profile)
           │
     country-preferred search
     (merge local + global, de-dup)
           │
     Rerank(merged, profile)
           │
     return SearchResponse
           │
           └── (goroutine, non-blocking) ──► Kafka: search.queries
                                                        │
                                                        ▼
                                              cmd/personalizer
                                            HINCRBY countries
                                            HINCRBY continents
                                            HINCRBY feature_codes
                                            LPUSH   recent
                                                        │
                                                        ▼
                                                     Redis
```

Search events are fire-and-forget — the user already has their results before the Kafka produce begins.

---

### Recommendation engine (`GET /api/v1/recommendations`)

Three sections, fetched concurrently, returned in priority order:

| Priority | Source | Trigger |
|---|---|---|
| 1 | **History-based** | ≥ 5 searches in any single country |
| 2 | **Geo-IP nearby** | Bounding box ± 5° + same country filter |
| 3 | **Global popular** | Fallback when history + geo both empty |

---

## Tech Stack

| Layer | Technology | Reason |
|---|---|---|
| Language | Go 1.25 | Single static binary, low GC pressure, fast HTTP |
| HTTP | Gin | Fastest Go router; idiomatic middleware chain |
| Search | Typesense (self-hosted) | Sub-5 ms full-text, typo-tolerance, no JVM |
| Messaging | Apache Kafka (KRaft) | Durable event log decouples search path from profile writes |
| Database | PostgreSQL 16 | Source of truth; `COPY` + UNLOGGED for bulk ingest |
| Cache / Profiles | Redis 7 | Atomic `HINCRBY` counters + `LPUSH` ring buffers in < 1 ms |
| Containerisation | Docker Compose | One-command local environment |

---

## Quickstart

### Prerequisites

- Docker + Docker Compose
- Go 1.25+

### 1. Start infrastructure

```bash
docker compose up -d
# Starts: Kafka (KRaft), PostgreSQL 16, Redis 7, Typesense 0.25
```

### 2. Copy environment config

```bash
cp .env.example .env
# Edit .env if needed — defaults work for local Docker
```

### 3. Import GeoNames data (one-time)

```bash
# Full 12 M-record dataset — downloads + imports in one step
go run ./cmd/bulkimport -truncate -workers 8

# Flags:
#   -truncate    TRUNCATE table before import (fastest for first run)
#   -workers N   Parallel batch workers (default 4)
#   -batch N     Records per batch (default 50 000)
#   -skip-ts     Skip Typesense, Postgres only
#   -skip-pg     Skip Postgres, Typesense only
```

> First-run tip: use `-truncate` to use fast `COPY` without a staging table. Subsequent runs use `ON CONFLICT DO UPDATE` automatically.

### 4. Start the API

```bash
go run ./cmd/api
# Listening on :8080
```

### 5. Start the personalizer

```bash
go run ./cmd/personalizer
# Consumes search.queries → builds per-IP Redis profiles
```

### 6. Open the frontend

```bash
open frontend/index.html
# No dev server needed — static HTML, embedded in the binary for production
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `APP_ENV` | `development` | `development` or `production` |
| `APP_PORT` | `8080` | HTTP listen port |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated allowed origins |
| `POSTGRES_HOST` | `localhost` | PostgreSQL host |
| `POSTGRES_PORT` | `5432` | PostgreSQL port |
| `POSTGRES_USER` | `geolink` | PostgreSQL user |
| `POSTGRES_PASSWORD` | `geolink` | PostgreSQL password |
| `POSTGRES_DB` | `geolink` | PostgreSQL database |
| `POSTGRES_SSL_MODE` | `disable` | `disable` / `require` |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | _(empty)_ | Redis password |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |
| `KAFKA_GROUP_ID_INDEXER` | `indexer-group` | Consumer group for indexer |
| `KAFKA_GROUP_ID_PERSONALIZER` | `personalizer-group` | Consumer group for personalizer |
| `TYPESENSE_HOST` | `localhost` | Typesense host |
| `TYPESENSE_PORT` | `8108` | Typesense port |
| `TYPESENSE_API_KEY` | `geolink-local-key` | Typesense API key |
| `TYPESENSE_COLLECTION` | `geonames` | Typesense collection name |
| `GEONAMES_FILE_PATH` | `./data/allCountries.txt` | Path to extracted TSV |
| `GEONAMES_DOWNLOAD_URL` | _(GeoNames URL)_ | Auto-download source |
| `GEONAMES_BATCH_SIZE` | `5000` | Records per Kafka message |
| `PERSONALIZATION_GLOBAL` | `true` | Master personalisation switch |
| `VELOCITY_LIMIT` | `100` | Max requests per window |
| `VELOCITY_WINDOW_SECONDS` | `60` | Rate-limit sliding window (s) |
| `IP_PROFILE_TTL_DAYS` | `30` | Redis profile expiry |
| `RECENT_SEARCHES_LIMIT` | `10` | Max recent queries per profile |
| `SEARCH_CACHE_TTL_SECONDS` | `60` | Non-personalised result cache TTL |
| `ADMIN_API_KEY` | _(required)_ | Key for protected admin endpoints |

---

## API Reference

### `GET /api/v1/search`

Autocomplete with optional personalisation and country-preferred ranking.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `q` | string | **required** | Search query (1–100 chars) |
| `personalized` | bool | `true` | Enable re-ranking by IP profile |
| `ip` | string | client IP | Override IP for profile lookup |
| `limit` | int | `10` | Results per page (max 50) |
| `page` | int | `1` | Page number |
| `country_code` | string | — | Filter by ISO country code |
| `feature_code` | string | — | Filter by GeoNames feature code |

```bash
curl "http://localhost:8080/api/v1/search?q=Manila&personalized=false"
curl "http://localhost:8080/api/v1/search?q=Man&personalized=true"
curl "http://localhost:8080/api/v1/search?q=Cebu&country_code=PH&feature_code=PPLC"
```

**Response:**
```json
{
  "query": "Manila",
  "results": [
    {
      "geoname_id": 1701668,
      "name": "Manila",
      "country_code": "PH",
      "feature_code": "PPLC",
      "feature_label": "Capital City",
      "population": 1600000,
      "latitude": 14.6042,
      "longitude": 120.9822,
      "score": 1.95,
      "boosted": true
    }
  ],
  "total": 101,
  "page": 1,
  "limit": 10,
  "personalized": true,
  "time_taken_ms": 8
}
```

---

### `GET /api/v1/recommendations`

Returns personalised place recommendations in up to three concurrent sections: history-based, geo-IP nearby, and global popular fallback.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `ip` | string | client IP | Override IP |
| `limit` | int | `8` | Max items per section (max 25) |

```bash
curl "http://localhost:8080/api/v1/recommendations"
```

**Response:**
```json
{
  "ip": "1.2.3.4",
  "detected_location": {
    "country": "Philippines",
    "country_code": "PH",
    "city": "Manila",
    "lat": 14.6042,
    "lng": 120.9822
  },
  "sections": [
    {
      "source": "history",
      "label": "Based on your searches",
      "icon": "✦",
      "items": [{ "name": "Cebu City", "country_code": "PH", "reason": "history" }]
    },
    {
      "source": "nearby",
      "label": "Near Manila",
      "icon": "📍",
      "items": [{ "name": "Quezon City", "country_code": "PH", "reason": "nearby" }]
    }
  ],
  "time_taken_ms": 12
}
```

---

### `GET /api/v1/health`

Infrastructure liveness check.

```bash
curl http://localhost:8080/api/v1/health
# {"status":"ok","postgres":"ok","redis":"ok","typesense":"ok"}
```

---

### `GET /api/v1/analytics/profile`

View the raw personalisation profile for an IP.

```bash
curl "http://localhost:8080/api/v1/analytics/profile?ip=1.2.3.4"
```

---

### `POST /api/v1/events/search`

Explicitly record a search selection event (called by the frontend when a user clicks a result). Used to build profile signal from intentional selections, not just keystrokes.

```bash
curl -X POST http://localhost:8080/api/v1/events/search \
  -H "Content-Type: application/json" \
  -d '{"query": "Manila", "country_code": "PH", "feature_code": "PPLC"}'
# {"status":"recorded"}
```

---

### `DELETE /api/v1/profile/reset`

Clear your own search history profile (no auth required).

```bash
curl -X DELETE "http://localhost:8080/api/v1/profile/reset"
```

---

### `PUT /api/v1/toggle/global` _(Admin)_

Enable or disable personalisation globally.

```bash
curl -X PUT http://localhost:8080/api/v1/toggle/global \
  -H "X-Admin-Key: your-secret-key-here" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

---

### `PUT /api/v1/toggle/ip` _(Admin)_

Enable or disable personalisation for a specific IP.

```bash
curl -X PUT http://localhost:8080/api/v1/toggle/ip \
  -H "X-Admin-Key: your-secret-key-here" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false, "ip": "1.2.3.4"}'
```

---

### `DELETE /api/v1/analytics/profile/reset` _(Admin)_

Wipe the profile for any IP.

```bash
curl -X DELETE "http://localhost:8080/api/v1/analytics/profile/reset?ip=1.2.3.4" \
  -H "X-Admin-Key: your-secret-key-here"
```

---

## How Personalisation Works

Every search fires a `SearchEvent` to Kafka in a non-blocking goroutine:

```json
{
  "ip": "1.2.3.4",
  "query": "Manila",
  "country_code": "PH",
  "feature_code": "PPLC",
  "continent": "Asia",
  "timestamp": 1234567890
}
```

The **personalizer** consumes this and updates four Redis structures atomically:

```
HINCRBY ip:1.2.3.4:countries      PH    1
HINCRBY ip:1.2.3.4:continents     Asia  1
HINCRBY ip:1.2.3.4:feature_codes  PPLC  1
LPUSH   ip:1.2.3.4:recent         Manila
LTRIM   ip:1.2.3.4:recent         0 9
```

On the next search, the **re-ranker** applies a weighted boost formula:

```
finalScore = typesenseScore
           + 0.40 × (countries[result.country]    / max_country_count)
           + 0.20 × (continents[result.continent] / max_continent_count)
           + 0.15 × (features[result.feature]     / max_feature_count)
           + 0.25 × (1.0 if result.name in recent else 0.0)
```

All weights are clamped to `[0, 1]`. Results with `finalScore > typesenseScore` have `"boosted": true` in the response.

If one country makes up ≥ 30% of the user's total searches, a country-preferred pass runs first — merging local results at the top before re-ranking — so queries like `"Tempe"` surface the user's local Tempe before the one in Arizona.

---

## Benchmark Results

> Run with `k6` — 50 VUs, 30 s sustained load, local Docker stack.

| Scenario | p50 | p95 | p99 | RPS |
|---|---|---|---|---|
| Cold search (no cache) | 1.4 ms | 4.3 ms | ~8 ms | 958 |
| Personalised search | 3.1 ms | 6.2 ms | ~10 ms | 845 |
| Peak concurrent (200 VUs) | 10 ms | 258 ms | ~400 ms | 1 661 |

p95 stays under 20 ms up to ~100 VUs. Latency climbs above ~150 VUs as Typesense's local connection pool saturates — a multi-node Typesense cluster and horizontal API replicas would push past 5 000 RPS.

---

## Design Decisions

**Why Typesense over Elasticsearch?**
Single Go binary, sub-5 ms search, no JVM, no cluster config needed for < 50 M docs. Elasticsearch would be 5× the ops overhead at this scale.

**Why bypass Kafka in the import pipeline?**
The import is a one-shot offline job. Routing 12 M records through Kafka → consumer → DB would add broker overhead and a second process to babysit. Direct parallel workers with `COPY` are faster and simpler for a batch job.

**Why Kafka for search events?**
Search events are on the hot path. Kafka decouples the personalizer's write throughput from the API's response time. The personalizer can be paused, redeployed, or scaled independently without losing events.

**Why Redis `HINCRBY` instead of a profile JSON blob?**
Atomic increment — 1 000 concurrent searches can update the same counter without locks or read-modify-write cycles. `HGETALL` on 4 keys takes < 1 ms vs. a Postgres SELECT that would compete with live search queries on the same connection pool.

**Why fire-and-forget search events?**
The event is not on the critical path — the user already received their results. A synchronous Kafka produce would add 5–20 ms to every response. The personalizer commits offsets only after `UpdateProfile` succeeds, so at-least-once delivery is preserved without blocking the search path.

**Rate limiting design**
Redis `INCR` + `EXPIRE`. The first request in a window sets the TTL; subsequent requests increment it. Fixed-window counter in O(1) Redis ops — no Lua script, no sidecar.

---

## Production Deployment (AWS)

| Component | AWS Service |
|---|---|
| API | ECS Fargate (auto-scaling, no EC2 management) |
| Bulk importer | ECS Fargate one-shot task |
| Personalizer | ECS Fargate long-running service |
| Typesense | EC2 r6g.xlarge (self-managed, memory-optimised) |
| Kafka | Amazon MSK |
| PostgreSQL | RDS Aurora Serverless v2 |
| Redis | ElastiCache Serverless |
| Secrets | AWS Secrets Manager |
| Observability | CloudWatch + X-Ray |
