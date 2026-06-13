// internal/cache/redis.go
// Redis client factory using go-redis/v9.
package cache

import (
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/barathsuresh/geolink/internal/config"
)

// New creates a Redis client from cfg. Connection is lazy — first use triggers dial.
// The caller is responsible for calling client.Close() on shutdown.
func New(cfg *config.Config) (*redis.Client, error) {
	addr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.RedisPassword,
		DB:           0,
		PoolSize:     20,
		MinIdleConns: 5,
	})

	return client, nil
}
