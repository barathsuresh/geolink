// internal/config/config.go
// Centralised configuration loader.
// Reads from a .env file (if present) then from environment variables.
// All services call config.Load() at startup.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for GEOLINK services.
type Config struct {
	// Application
	AppEnv         string // APP_ENV
	AppPort        string // APP_PORT
	CORSOrigins    string // CORS_ALLOWED_ORIGINS — comma-separated (default: * in dev)

	// PostgreSQL
	PostgresHost     string // POSTGRES_HOST
	PostgresPort     string // POSTGRES_PORT
	PostgresUser     string // POSTGRES_USER
	PostgresPassword string // POSTGRES_PASSWORD
	PostgresDB       string // POSTGRES_DB
	PostgresSSLMode  string // POSTGRES_SSL_MODE (default: require)

	// Redis
	RedisHost     string // REDIS_HOST
	RedisPort     string // REDIS_PORT
	RedisPassword string // REDIS_PASSWORD

	// Kafka (comma-separated broker list, e.g. "localhost:9092")
	KafkaBrokers             string // KAFKA_BROKERS
	KafkaGroupIDIndexer      string // KAFKA_GROUP_ID_INDEXER
	KafkaGroupIDPersonalizer string // KAFKA_GROUP_ID_PERSONALIZER

	// Typesense
	TypesenseHost       string // TYPESENSE_HOST
	TypesensePort       string // TYPESENSE_PORT
	TypesenseAPIKey     string // TYPESENSE_API_KEY
	TypesenseCollection string // TYPESENSE_COLLECTION

	// GeoNames data pipeline
	GeonamesFilePath    string // GEONAMES_FILE_PATH
	GeonamesDownloadURL string // GEONAMES_DOWNLOAD_URL
	GeonamesBatchSize   int    // GEONAMES_BATCH_SIZE (default 500)

	// Personalization
	PersonalizationGlobal bool // PERSONALIZATION_GLOBAL (default true)
	VelocityLimit         int  // VELOCITY_LIMIT (default 100)
	VelocityWindowSeconds int  // VELOCITY_WINDOW_SECONDS (default 60)
	IPProfileTTLDays      int  // IP_PROFILE_TTL_DAYS (default 30)
	RecentSearchesLimit   int  // RECENT_SEARCHES_LIMIT (default 10)
	SearchCacheTTLSeconds int  // SEARCH_CACHE_TTL_SECONDS (default 60)

	// Admin
	AdminAPIKey string // ADMIN_API_KEY
}

// KafkaBrokerList returns KafkaBrokers as a parsed []string slice.
func (c *Config) KafkaBrokerList() []string {
	return strings.Split(c.KafkaBrokers, ",")
}

// Load reads environment variables (and an optional .env file) into a Config.
// Returns an error if any required variable is missing.
func Load() (*Config, error) {
	// Load .env file if it exists; silently ignore file-not-found.
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Non-fatal: the file may simply not exist in production.
		_ = err
	}

	cfg := &Config{
		// Application
		AppEnv:      getEnv("APP_ENV", "development"),
		AppPort:     getEnv("APP_PORT", "8080"),
		CORSOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),

		// PostgreSQL
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "geolink"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "geolink"),
		PostgresDB:       getEnv("POSTGRES_DB", "geolink"),
		PostgresSSLMode:  getEnv("POSTGRES_SSL_MODE", "require"),

		// Redis
		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),

		// Kafka
		KafkaBrokers:             getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaGroupIDIndexer:      getEnv("KAFKA_GROUP_ID_INDEXER", "indexer-group"),
		KafkaGroupIDPersonalizer: getEnv("KAFKA_GROUP_ID_PERSONALIZER", "personalizer-group"),

		// Typesense
		TypesenseHost:       getEnv("TYPESENSE_HOST", "localhost"),
		TypesensePort:       getEnv("TYPESENSE_PORT", "8108"),
		TypesenseAPIKey:     getEnv("TYPESENSE_API_KEY", "geolink-local-key"),
		TypesenseCollection: getEnv("TYPESENSE_COLLECTION", "geonames"),

		// GeoNames
		GeonamesFilePath:    getEnv("GEONAMES_FILE_PATH", "./data/allCountries.txt"),
		GeonamesDownloadURL: getEnv("GEONAMES_DOWNLOAD_URL", "https://download.geonames.org/export/dump/allCountries.zip"),
		GeonamesBatchSize:   getEnvInt("GEONAMES_BATCH_SIZE", 500),

		// Personalization
		PersonalizationGlobal: getEnvBool("PERSONALIZATION_GLOBAL", true),
		VelocityLimit:         getEnvInt("VELOCITY_LIMIT", 100),
		VelocityWindowSeconds: getEnvInt("VELOCITY_WINDOW_SECONDS", 60),
		IPProfileTTLDays:      getEnvInt("IP_PROFILE_TTL_DAYS", 30),
		RecentSearchesLimit:   getEnvInt("RECENT_SEARCHES_LIMIT", 10),
		SearchCacheTTLSeconds: getEnvInt("SEARCH_CACHE_TTL_SECONDS", 60),

		// Admin
		AdminAPIKey: getEnv("ADMIN_API_KEY", ""),
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// validate checks that required fields are non-empty.
func (c *Config) validate() error {
	required := map[string]string{
		"POSTGRES_USER":     c.PostgresUser,
		"POSTGRES_PASSWORD": c.PostgresPassword,
		"POSTGRES_DB":       c.PostgresDB,
		"TYPESENSE_API_KEY": c.TypesenseAPIKey,
		"KAFKA_BROKERS":     c.KafkaBrokers,
		"ADMIN_API_KEY":     c.AdminAPIKey,
	}
	for key, val := range required {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("required env var %q is not set", key)
		}
	}
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}
