# GEOLINK — Knowledge Transfer Document

> **Purpose:** Complete technical handover for any AI/developer continuing work on this project.
> Generated: 2026-06-02

---

## 1. What Is GEOLINK?

GEOLINK is a **geo-aware autocomplete API** built in Go. It:
- Searches 13.4M+ global place names from the GeoNames dataset
- Personalises results based on each user's IP search history
- Recommends nearby locations using IP geolocation on page load
- Runs as 4 microservices communicating via Kafka

**Tech stack:** Go · PostgreSQL · Typesense · Redis · Apache Kafka · Vanilla JS + Leaflet.js

---

## 2. Project Structure

```
geolink/
├── cmd/
│   ├── api/          → main HTTP API server (port 8080)
│   ├── importer/     → one-shot: reads allCountries.txt → Kafka
│   ├── indexer/      → persistent: Kafka → PostgreSQL + Typesense
│   └── personalizer/ → persistent: Kafka search events → Redis profiles
├── frontend/
│   └── index.html    → single-file vanilla JS + Leaflet.js UI
├── internal/
│   ├── api/
│   │   ├── handler/  → HTTP handlers (search, recommendations, health, etc.)
│   │   ├── middleware/→ IPExtract, RateLimit, AdminAuth
│   │   └── router.go → Gin route wiring
│   ├── config/       → .env loader (envconfig)
│   ├── cache/        → Redis client factory
│   ├── db/           → PostgreSQL client factory + schema
│   ├── geo/          → IP geolocation via ip-api.com
│   ├── importer/     → allCountries.txt parser + Kafka producer
│   ├── indexer/      → Kafka consumer, PG+Typesense upsert
│   ├── kafka/        → producer and consumer wrappers (kafka-go)
│   ├── models/       → GeoName, SearchRequest/Response, IPProfile, events
│   ├── personalization/ → Redis profile read/write/reset
│   └── search/       → Typesense adapter + re-ranker
├── pkg/
│   └── geoip/        → country_code → continent mapping
├── data/
│   └── allCountries.txt → 13.4M GeoNames records (tab-separated, 19 cols)
├── .env              → all configuration
└── docker-compose.yml→ Kafka, PostgreSQL, Redis, Typesense
```

---

## 3. Services & How They Work

### `cmd/importer` — One-shot data pump
- Reads `data/allCountries.txt` (13.4M lines, 19 tab-separated fields)
- Parses each line into a `GeoName` struct
- Batches 5,000 records → JSON → Kafka topic `geo.raw.entries`
- Run once to load data. BatchBytes = 10MB (tuned to avoid Kafka size errors)
- **Do not run again** unless you want to re-index everything

### `cmd/indexer` — Kafka → Databases
- Consumes `geo.raw.entries` topic (group: `indexer-group`)
- For each batch: upserts → PostgreSQL (source of truth), then → Typesense (search index)
- Commits Kafka offset ONLY after both writes succeed (at-least-once)
- Run alongside importer, then stop after import completes

### `cmd/api` — HTTP server (port 8080)
- Gin framework, all routes under `/api/v1`
- **Search flow:**
  1. Parse query params → Typesense autocomplete
  2. If `personalized=true` → fetch Redis IP profile → re-rank results
  3. Return JSON response
  4. Fire `SearchEvent` to Kafka `search.queries` in a goroutine (non-blocking)
  5. **Event quality gates:** query must be ≥3 chars AND top result must be a PPL* feature code
- **CORS:** `Access-Control-Allow-Origin: *` (dev mode, all origins including `file://`)

### `cmd/personalizer` — Profile builder
- Consumes `search.queries` topic (group: `personalizer-group`)
- For each SearchEvent, pipelines to Redis:
  - `HINCRBY ip:{ip}:countries {code} 1`
  - `HINCRBY ip:{ip}:continents {continent} 1`
  - `HINCRBY ip:{ip}:feature_codes {code} 1`
  - `LPUSH ip:{ip}:recent {query}` + `LTRIM` to last 10
  - `EXPIRE` all 4 keys by `IP_PROFILE_TTL_DAYS * 86400`
- Logs every event: `✦ event #N | ip=... | q=... | country=... | feature=...`
- Commits offset after successful Redis write

---

## 4. API Endpoints

Base: `http://localhost:8080/api/v1`

### Public
| Method | Path | Description |
|---|---|---|
| GET | `/search` | Autocomplete search |
| GET | `/recommendations` | Pre-search suggestions (geo-IP + history) |
| GET | `/analytics/profile` | View IP's Redis profile |
| GET | `/health` | Service health check |

### Protected (Header: `X-Admin-Key: <value>`)
| Method | Path | Description |
|---|---|---|
| PUT | `/toggle/global` | Enable/disable personalisation globally |
| PUT | `/toggle/ip` | Enable/disable personalisation for one IP |
| DELETE | `/analytics/profile/reset` | Clear IP profile |

### Search Query Params
```
?q=Mumbai           required: search query (1-100 chars)
&personalized=true  optional: apply re-ranking (default false)
&ip=1.2.3.4         optional: explicit IP override
&limit=10           optional: results (max 50, default 10)
&page=1             optional: pagination
&country_code=IN    optional: filter by country
&feature_code=PPLC  optional: filter by feature type
```

### Error Format (all endpoints)
```json
{ "error": "message here", "code": "ERROR_CODE" }
```

---

## 5. Personalisation — How It Works

### Profile building (automatic)
Every search with `len(query) >= 3` AND top result is a `PPL*` feature code fires a `SearchEvent` to Kafka. The personalizer writes counters to Redis per IP.

### Re-ranking formula
```
finalScore = 1.0
  + 0.40 × (country_count / max_country_count)    // country match
  + 0.20 × (continent_count / max_continent_count) // continent match
  + 0.15 × (feature_count / max_feature_count)     // feature type match
  + 0.25 × (1.0 if query in recent_searches else 0) // recently searched
```

Results with `score > 1.0` get `"boosted": true` in the response.

### Profile Redis keys
```
ip:{ip}:countries    HASH  country_code → count
ip:{ip}:continents   HASH  continent → count
ip:{ip}:feature_codes HASH feature_code → count
ip:{ip}:recent       LIST  recent queries (newest first, capped at 10)
```
All keys expire after `IP_PROFILE_TTL_DAYS` (default 30 days).

### Personalisation toggle
- Global: `SET personalization:global_enabled "true"/"false"`
- Per-IP: `SET personalization:ip:{ip}:enabled "true"/"false"`

---

## 6. Recommendations Endpoint

`GET /api/v1/recommendations?ip=<ip>&limit=8`

Returns two sections simultaneously (fetched with `sync.WaitGroup` in parallel):

**Section 1 — "Based on your searches" (✦)**
- Reads Redis profile
- Takes top 3 most-searched countries
- Queries Typesense: `country_code:=XX && feature_class:=P` sorted by population
- Only shown if profile exists

**Section 2 — "Near you" (📍)**
- Calls `ip-api.com/json/{ip}` (3s timeout, graceful fail)
- Gets lat/lng → Typesense filter: `±5° bounding box, feature_class:=P`
- Falls back to country-level if bounding box returns nothing
- Skipped for loopback/private IPs (`::1`, `127.x.x.x`, RFC-1918 ranges)

**Fallback — "Popular places" (🌍)**
- Only shown if both above return nothing
- `feature_class:=P && population:>500000` sorted by population

### Response shape
```json
{
  "ip": "153.33.229.99",
  "detected_location": { "country": "India", "country_code": "IN", "city": "Chennai", "lat": 13.08, "lng": 80.27 },
  "sections": [
    { "source": "history", "label": "Based on your searches", "icon": "✦", "items": [...] },
    { "source": "nearby",  "label": "Near Chennai",           "icon": "📍", "items": [...] }
  ],
  "time_taken_ms": 45
}
```

---

## 7. Dataset — GeoNames `allCountries.txt`

**Stats:** 13,434,733 records · 19 tab-separated fields

| Field # | Name | Example |
|---|---|---|
| 0 | geoname_id | 1275339 |
| 1 | name | Mumbai |
| 2 | ascii_name | Mumbai |
| 3 | alternate_names | Bombay,मुंबई,... |
| 4 | latitude | 19.07283 |
| 5 | longitude | 72.88261 |
| 6 | feature_class | P |
| 7 | feature_code | PPLA |
| 8 | country_code | IN |
| 10 | admin1_code | 16 |
| 14 | population | 12442373 |
| 17 | timezone | Asia/Kolkata |

**Feature Classes:**
- `P` = Populated places (cities, towns) ← most relevant for autocomplete
- `A` = Administrative divisions (countries, states, districts)
- `S` = Spots/buildings (airports, railway stations, hotels)
- `T` = Terrain (mountains, islands, capes)
- `H` = Hydrographic (rivers, lakes, oceans)
- `R` = Roads/railroads
- `L` = Land areas (parks, reserves)

**Key feature codes:**
- `PPLC` = Capital city
- `PPLA` = State/province capital
- `PPLA2` = District capital
- `PPL` = City/town
- `PPLX` = Neighbourhood

---

## 8. Kafka Topics & Consumer Groups

| Topic | Producer | Consumer | Group ID |
|---|---|---|---|
| `geo.raw.entries` | importer | indexer | `indexer-group` |
| `search.queries` | api | personalizer | `personalizer-group` |

**Configuration (tuned):**
- Broker `message.max.bytes` = 10MB (set via `kafka-configs`)
- Producer `BatchBytes` = 10MB (set in `internal/kafka/producer.go`)
- Producer write timeout = 15s

---

## 9. Configuration (`.env`)

```env
# Application
APP_ENV=development
API_PORT=8080
ADMIN_KEY=your-secret-key-here

# PostgreSQL
POSTGRES_DSN=postgresql://geolink:geolink@localhost:5432/geolink

# Redis
REDIS_ADDR=localhost:6379

# Typesense
TYPESENSE_HOST=localhost
TYPESENSE_PORT=8108
TYPESENSE_API_KEY=geolink-local-key
TYPESENSE_COLLECTION=geonames

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP_ID_INDEXER=indexer-group
KAFKA_GROUP_ID_PERSONALIZER=personalizer-group

# GeoNames data
GEONAMES_FILE=./data/allCountries.txt
GEONAMES_ZIP=./data/allCountries.zip
GEONAMES_BATCH_SIZE=5000

# Personalizer
IP_PROFILE_TTL_DAYS=30
RECENT_SEARCHES_LIMIT=10

# Rate limiting
RATE_LIMIT_REQUESTS=60
RATE_LIMIT_WINDOW_SECONDS=60
```

---

## 10. Running the Stack

### Prerequisites
```bash
docker-compose up -d   # starts Kafka, PostgreSQL, Redis, Typesense
```

### First-time data import (one-time, ~45 mins for 13M records)
```bash
# Terminal 1
go run ./cmd/indexer

# Terminal 2
go run ./cmd/importer
# Wait for: "Import complete. Total: 13434733 records"
# Then Ctrl+C both terminals
```

### Daily operation
```bash
# Terminal 1
go run ./cmd/api

# Terminal 2
go run ./cmd/personalizer

# (indexer only needed if re-importing data)
```

### Re-import from scratch
```bash
# 1. Truncate PostgreSQL
docker exec postgres psql -U geolink -d geolink -c "TRUNCATE TABLE geonames;"

# 2. Delete Typesense collection
curl -X DELETE http://localhost:8108/collections/geonames -H "X-TYPESENSE-API-KEY: geolink-local-key"

# 3. Reset Kafka offsets
docker exec kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group indexer-group --topic geo.raw.entries \
  --reset-offsets --to-latest --execute

# 4. Re-run importer + indexer
```

---

## 11. Current Issues & Known Limitations

### Issue 1: "Tempe" returns irrelevant results for Indian users
**Problem:** When an Indian user searches "Tempe", Typesense returns `Tempe, Arizona (US)` and `Tempe, Bali (ID)` because there is no place called "Tempe" in India. Personalisation boosts Indian results but can't create results that don't exist.

**Root cause:** The re-ranker only re-orders existing results — it cannot inject country-filtered results when the query has no match in the user's preferred country.

**Proposed fix (not yet implemented):**
- Try a country-filtered Typesense query first (e.g., `q=Tempe && country_code:=IN`)
- If results ≥ 1, use them as primary
- Merge remaining slots with global results (de-duped)
- This is a "country-preferred search" mode rather than post-hoc re-ranking

### Issue 2: Intermediate keystrokes still fire events (pre-fix)
**Status:** Fixed. The 3-char minimum AND PPL* feature gate was added to `search.go` line 104. Events before this fix have polluted the profile with: PK (from single "T"), ID (from "Tempe"), NO (from "Tem").

### Issue 3: Profile noise from mistyped searches
**Example:** User typed `q="Pooonamale"` → matched `Poonamallee, IN`. This is fine. But `q="Pooon"` → `BCH` (beach, US) polluted the profile. The PPL* gate now blocks this.

### Issue 4: Recent searches are keystrokes not completed searches
`recent_searches` currently stores every fired event query (including intermediates). Improvement: only store queries where the user paused for >1s or pressed Enter.

---

## 12. Frontend (frontend/index.html)

Single file, no build step, no npm. CDN dependencies:
- **Leaflet.js 1.9.4** — interactive map
- **api.ipify.org** — resolve real public IP before loading recommendations

### Key JS functions
| Function | Purpose |
|---|---|
| `resolveIP()` | Hits health endpoint + fetches real public IP from ipify |
| `loadRecommendations()` | Calls `/recommendations?ip=...`, caches result |
| `renderRecommendations(data)` | Renders sections array (history + nearby) |
| `doSearch(q)` | Debounced (200ms) Typesense autocomplete call |
| `renderResults(data)` | Renders search results with boost badges |
| `selectResult(i)` | Map flyTo + marker + overlay card |
| `countryFlag(code)` | `0x1F1E6 - 65 + charCode` → correct regional indicator emoji |

### Country flag formula (correct)
```js
function countryFlag(code) {
  if (!code || code.length !== 2) return '🏳️';
  return code.toUpperCase().replace(/./g,
    c => String.fromCodePoint(0x1F1E6 - 65 + c.charCodeAt(0)));
}
// NOTE: 0x1F1E6 is correct (Regional Indicator A). 0x1F1E0 is WRONG (off by 6).
```

---

## 13. Typesense Schema

Collection name: `geonames`

| Field | Type | Indexed | Facet |
|---|---|---|---|
| geoname_id | int64 | yes | no |
| name | string | yes (search) | no |
| ascii_name | string | yes (search) | no |
| alternate_names | string | yes (search) | no |
| latitude | float | yes | no |
| longitude | float | yes | no |
| feature_class | string | yes | yes |
| feature_code | string | yes | yes |
| country_code | string | yes | yes |
| admin1_code | string | yes | no |
| population | int64 | yes (sort) | no |
| timezone | string | yes | no |

Default sort: `population:desc`

---

## 14. PostgreSQL Schema

```sql
CREATE TABLE geonames (
    geoname_id      BIGINT PRIMARY KEY,
    name            TEXT NOT NULL,
    ascii_name      TEXT,
    alternate_names TEXT,
    latitude        DOUBLE PRECISION,
    longitude       DOUBLE PRECISION,
    feature_class   CHAR(1),
    feature_code    VARCHAR(10),
    country_code    CHAR(2),
    admin1_code     VARCHAR(20),
    population      BIGINT DEFAULT 0,
    timezone        VARCHAR(40),
    modified_at     DATE
);
CREATE INDEX geonames_country ON geonames(country_code);
CREATE INDEX geonames_feature ON geonames(feature_code);
CREATE INDEX geonames_pop     ON geonames(population DESC);
```

---

## 15. Rate Limiting

Applied only to `GET /search`. Implementation in `internal/api/middleware/ratelimit.go`:
- Key: `velocity:{ip}` in Redis
- `INCR` on each request; first request sets `EXPIRE` = window seconds
- Returns `429 Too Many Requests` with `Retry-After` header when exceeded
- Configurable: `RATE_LIMIT_REQUESTS=60`, `RATE_LIMIT_WINDOW_SECONDS=60`

---

## 16. Work In Progress / Next Suggestions

1. **Country-preferred search mode** — try `country_code:=IN && q=X` first, merge with global
2. **Recent searches deduplication** — only record final query, not intermediates
3. **Profile quality score** — weight events by query length (longer = more intentional)
4. **Typesense geo-point field** — migrate `latitude`/`longitude` from `float` to `geopoint` type to enable native `_geoRadius` distance sorting (currently uses bounding box)
5. **Production CORS** — lock down `Access-Control-Allow-Origin` to specific domain
6. **Auth for personalisation toggle** — current admin key is plaintext in `.env`
7. **Session-based profiles** — combine IP + cookie for cross-network persistence

---

## 17. Postman Quick-Start

Base URL: `http://localhost:8080/api/v1`

```
GET  /health
GET  /search?q=Mumbai&personalized=true&ip=153.33.229.99&limit=10
GET  /recommendations?ip=153.33.229.99&limit=8
GET  /analytics/profile?ip=153.33.229.99
PUT  /toggle/global          Body: {"enabled": true}   Header: X-Admin-Key: your-secret-key-here
PUT  /toggle/ip              Body: {"enabled": false, "ip": "153.33.229.99"}
DEL  /analytics/profile/reset?ip=153.33.229.99
```
