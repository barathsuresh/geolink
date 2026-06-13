-- 001_create_geonames.sql
-- Initial schema for the GeoNames dataset.
-- Run via golang-migrate or manually:
--   psql -U geolink -d geolink -f migrations/001_create_geonames.sql

CREATE TABLE IF NOT EXISTS geonames (
    geoname_id        BIGINT PRIMARY KEY,
    name              TEXT NOT NULL,
    ascii_name        TEXT,
    alternate_names   TEXT,        -- pipe-separated list
    latitude          DOUBLE PRECISION,
    longitude         DOUBLE PRECISION,
    feature_class     CHAR(1),     -- A, H, L, P, R, S, T, U, V
    feature_code      VARCHAR(10),
    country_code      CHAR(2),
    admin1_code       VARCHAR(20),
    admin2_code       VARCHAR(80),
    population        BIGINT DEFAULT 0,
    elevation         INT,
    timezone          VARCHAR(40),
    modified_at       DATE,
    created_at        TIMESTAMP DEFAULT NOW()
);

-- Query-pattern indexes
CREATE INDEX IF NOT EXISTS idx_geonames_country       ON geonames(country_code);
CREATE INDEX IF NOT EXISTS idx_geonames_feature_code  ON geonames(feature_code);
CREATE INDEX IF NOT EXISTS idx_geonames_population    ON geonames(population DESC);
