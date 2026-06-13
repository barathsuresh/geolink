# GEOLINK

> **Real-time location-aware autocomplete API** over 12 million place names.
> Re-ranks results using per-IP search history stored in Redis, so frequent travellers
> to a region see local results rise to the top automatically.

---

## Architecture

### Import flow (one-time)
```
allCountries.txt
      │
      ▼
┌─────────────┐   500-record JSON batches    ┌─────────────┐
│  cmd/       │ ─────────────────────────►  │    Kafka    │
│  importer   │     topic: geo.raw.entries   │  (KRaft)    │
└─────────────┘                              └──────┬──────┘
                                                    │ consume
                                              ┌─────▼──────┐
                                              │ cmd/indexer │
                                              └─────┬───────┘
                                        ┌──────────┴───────────┐
                                        ▼                       ▼
                                  ┌──────────┐           ┌──────────────┐
                                  │PostgreSQL│           │  Typesense   │
                                  │ geonames │           │  (geonames)  │
                                  └──────────┘           └──────────────┘
```

### Live search flow
```
User request
      │
      ▼
┌─────────────────────────────────────────────────────┐
│                  cmd/api  (Gin)                     │
│                                                     │
│  GET /search ──► Typesense search                   │
│                  │                                  │
│                  ▼                                  │
│        IsPersonalizationEnabled?                    │
│            │         │                              │
│           YES        NO ──► return raw results      │
│            │                                        │
│            ▼                                        │
│        GetProfile(Redis)                            │
│            │                                        │
│            ▼                                        │
│        Rerank(results, profile)                     │
│            │                                        │
│            ▼                                        │
│        return SearchResponse                        │
│            │                                        │
│            └──(goroutine)──► Kafka: search.queries  │
└─────────────────────────────────────────────────────┘
                                        │
                                        ▼
                              ┌─────────────────┐
                              │ cmd/personalizer │
                              │                 │
                              │ HINCRBY countries│
                              │ HINCRBY continents│
                              │ HINCRBY features │
                              │ LPUSH recent    │
                              └─────────────────┘
                                        │
                                        ▼
                                    Redis
                              ip:{ip}:countries
                              ip:{ip}:continents
                              ip:{ip}:feature_codes
                              ip:{ip}:recent
```

---

## Tech Stack

| Layer | Technology | Why |
|---|---|---|
| Language | Go 1.22+ | Single static binary, low memory, fast HTTP |
| HTTP | Gin | Fastest Go router, idiomatic middleware |
| Search | Typesense (self-hosted) | Sub-5ms full-text search, faceting, typo-tolerance |
| Messaging | Apache Kafka (KRaft) | Durable event log, fan-out to indexer + personalizer |
| Database | PostgreSQL 16 | Source of truth, COPY upsert for bulk ingest |
| Cache/Profiles | Redis 7 | HINCRBY counters + LPUSH ring buffers in <1ms |
| Containerisation | Docker Compose | One-command local env |

---

## AWS Deployment Stack

| Component | AWS Service |
|---|---|
| API binary | ECS Fargate (auto-scaling, no EC2 mgmt) |
| Importer / Indexer | ECS Fargate one-shot tasks |
| Personalizer | ECS Fargate long-running service |
| Typesense | Self-managed on EC2 (r6g.xlarge) |
| Kafka | Amazon MSK (managed Kafka) |
| PostgreSQL | RDS Aurora Serverless v2 |
| Redis | ElastiCache Serverless |
| Static assets | S3 + CloudFront |
| Secrets | AWS Secrets Manager |
| Observability | CloudWatch + X-Ray |

---

## Quickstart

### Prerequisites
- Docker + Docker Compose
- Go 1.22+
- GeoNames data file (auto-downloaded by importer)

### 1. Start infrastructure
```bash
docker compose up -d
# Starts: Kafka (KRaft), PostgreSQL, Redis, Typesense
```

### 2. Import GeoNames data (one-time, ~3 min for allCountries)
```bash
go run ./cmd/importer
# Downloads allCountries.zip, parses 12M records, publishes to Kafka
```

### 3. Index into PostgreSQL + Typesense (run alongside importer)
```bash
go run ./cmd/indexer
# Consumes geo.raw.entries → upserts Postgres + Typesense
```

### 4. Start the API
```bash
go run ./cmd/api
# Listens on :8080
```

### 5. Start the personalizer
```bash
go run ./cmd/personalizer
# Consumes search.queries → builds Redis IP profiles
```

### 6. Open the frontend
```bash
open frontend/index.html
# Or just double-click the file — no server needed
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `APP_ENV` | `development` | `development` or `production` |
| `APP_PORT` | `8080` | HTTP listen port |
| `POSTGRES_HOST` | `localhost` | PostgreSQL hostname |
| `POSTGRES_PORT` | `5432` | PostgreSQL port |
| `POSTGRES_USER` | `geolink` | PostgreSQL user |
| `POSTGRES_PASSWORD` | `geolink` | PostgreSQL password |
| `POSTGRES_DB` | `geolink` | PostgreSQL database name |
| `REDIS_HOST` | `localhost` | Redis hostname |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_PASSWORD` | `` | Redis password (empty = none) |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |
| `KAFKA_GROUP_ID_INDEXER` | `indexer-group` | Consumer group for indexer |
| `KAFKA_GROUP_ID_PERSONALIZER` | `personalizer-group` | Consumer group for personalizer |
| `TYPESENSE_HOST` | `localhost` | Typesense hostname |
| `TYPESENSE_PORT` | `8108` | Typesense HTTP port |
| `TYPESENSE_API_KEY` | `geolink-local-key` | Typesense API key |
| `TYPESENSE_COLLECTION` | `geonames` | Typesense collection name |
| `GEONAMES_FILE_PATH` | `./data/allCountries.txt` | Path to extracted GeoNames file |
| `GEONAMES_DOWNLOAD_URL` | *(GeoNames URL)* | Auto-download URL for zip |
| `GEONAMES_BATCH_SIZE` | `500` | Records per Kafka message |
| `VELOCITY_LIMIT` | `100` | Max search requests per window |
| `VELOCITY_WINDOW_SECONDS` | `60` | Rate-limit sliding window (s) |
| `IP_PROFILE_TTL_DAYS` | `30` | Redis profile expiry (days) |
| `RECENT_SEARCHES_LIMIT` | `10` | Max recent queries per profile |
| `ADMIN_API_KEY` | `` | Key for protected admin endpoints |

---

## API Reference

### `GET /api/v1/search`
Autocomplete search with optional personalisation.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `q` | string | **required** | Search query (1–100 chars) |
| `personalized` | bool | `true` | Enable re-ranking |
| `ip` | string | client IP | Override IP for profile lookup |
| `limit` | int | `10` | Results per page (max 50) |
| `page` | int | `1` | Page number |
| `country_code` | string | — | Filter by ISO country code |
| `feature_code` | string | — | Filter by GeoNames feature code |

```bash
# Basic search
curl "http://localhost:8080/api/v1/search?q=Manila&personalized=false"

# Personalised search
curl "http://localhost:8080/api/v1/search?q=Man&personalized=true"

# With filters
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
  "personalized": true,
  "time_taken_ms": 8
}
```

---

### `GET /api/v1/health`
Infrastructure liveness probe.
```bash
curl http://localhost:8080/api/v1/health
# {"status":"ok","postgres":"ok","redis":"ok","typesense":"ok"}
```

---

### `GET /api/v1/analytics/profile`
View the personalisation profile for an IP.
```bash
curl "http://localhost:8080/api/v1/analytics/profile?ip=::1"
```

---

### `PUT /api/v1/toggle/global` *(Admin)*
Enable or disable personalisation globally.
```bash
curl -X PUT http://localhost:8080/api/v1/toggle/global \
  -H "X-Admin-Key: your-secret-key-here" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

---

### `PUT /api/v1/toggle/ip` *(Admin)*
Enable or disable personalisation for a specific IP.
```bash
curl -X PUT http://localhost:8080/api/v1/toggle/ip \
  -H "X-Admin-Key: your-secret-key-here" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false, "ip": "1.2.3.4"}'
```

---

### `DELETE /api/v1/analytics/profile/reset` *(Admin)*
Wipe the profile for an IP.
```bash
curl -X DELETE "http://localhost:8080/api/v1/analytics/profile/reset?ip=::1" \
  -H "X-Admin-Key: your-secret-key-here"
```

---

## Benchmark Results

> Run with: `k6 run load-tests/search.js` (50 VUs, 30s)

| Test | p50 | p95 | p99 | RPS |
|---|---|---|---|---|
| Cold search | 1.4ms | 4.3ms | ~8ms | 958 |
| Personalised | 3.1ms | 6.2ms | ~10ms | 845 |
| Peak concurrent (200 VUs) | 10ms | 258ms | ~400ms | 1,661 |

> **Note:** The concurrent ramp test (0→200 VUs, no think time) shows the single-binary local stack
> holds cleanly at ≤100 VUs (p95 < 20ms). Errors appear above ~150 VUs as Typesense's local
> connection pool saturates. In production, horizontal Fargate scaling + multi-node Typesense
> would push this well above 5,000 RPS.

---

## How Personalisation Works

Every search fires a `SearchEvent` to Kafka (non-blocking goroutine):

```json
{ "ip": "1.2.3.4", "query": "Manila", "top_result_country": "PH",
  "top_result_feature_code": "PPLC", "continent": "Asia", "timestamp": 1234567890 }
```

The **personalizer** consumes this and increments four Redis structures:

```
HINCRBY ip:1.2.3.4:countries      PH    1
HINCRBY ip:1.2.3.4:continents     Asia  1
HINCRBY ip:1.2.3.4:feature_codes  PPLC  1
LPUSH   ip:1.2.3.4:recent         Manila
LTRIM   ip:1.2.3.4:recent         0 9
```

On the next search, the **re-ranker** applies this formula:

```
finalScore = typesenseScore
           + 0.40 × (countries[result.country]   / max_country_count)
           + 0.20 × (continents[result.continent] / max_continent_count)
           + 0.15 × (features[result.feature]    / max_feature_count)
           + 0.25 × (1.0 if result.name in recent else 0.0)
```

All weights are clamped to `[0, 1]`. Results are then re-sorted descending by `finalScore`.
Results with `finalScore > typesenseScore` have `boosted: true` in the response.

---

## Interview Talking Points

- **Why Typesense over Elasticsearch?** Typesense is a single Go binary with sub-5ms search, no JVM, no cluster config needed for <50M docs. ES would be 5× more ops overhead for this scale.

- **Why Kafka between importer and indexer?** Decouples ingest rate from write capacity. The indexer can be paused, redeployed, or scaled without losing data. At 12M records, a direct HTTP pipeline would lose progress on crash.

- **Why Redis HINCRBY instead of a profile JSON blob?** Atomic increment — 1000 concurrent requests can increment the same counter without locks or read-modify-write cycles. The HASH also lets us inspect and reset individual dimensions without re-serialising the whole profile.

- **Why fire-and-forget search events?** The event is not on the critical path — the user has already received their results. A synchronous Kafka produce would add 5-20ms to every search response. We accept eventual consistency in the profile.

- **At-least-once delivery in the personalizer:** Offsets are committed only after `UpdateProfile` succeeds. If Redis is down, the consumer does not commit, so the event will be replayed on restart. Bad messages (unmarshal errors) are skip-committed immediately so a malformed message never blocks the queue.

- **Rate limiting design:** Redis INCR + EXPIRE. The first request in a window sets the TTL; subsequent requests in the same window just increment. This gives a fixed-window counter with O(1) Redis ops. No Lua script needed.

- **Scaling path:** Typesense → multi-node cluster. Kafka → increase partitions + add indexer replicas. Redis → ElastiCache cluster mode (hash slots by IP prefix). API → horizontal ECS Fargate scaling behind ALB.

- **Why not store profiles in PostgreSQL?** Profile reads happen on every search request. A Redis HGETALL on 4 keys takes <1ms. A Postgres SELECT with JSON aggregation would be 5-20ms and compete with search queries on the same connection pool.
