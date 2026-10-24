// Package redisx holds the optional Redis-backed infrastructure of the
// template.
//
// Redis is never a source of truth here: the database owns every durable fact.
// Redis only carries cross-instance state — the rate-limit buckets today —
// which is exactly the state an in-process map cannot share between replicas.
// Nothing in this package is required to run the application; it is opt-in
// through `ratelimit.backend: redis`.
package redisx

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/example/go-web-template/internal/platform/config"
)

// Open dials Redis and verifies connectivity.
//
// A deployment that configured Redis but cannot reach it fails at startup on
// purpose: silently falling back to in-process state would keep every replica
// limiting only its own traffic while the operator believes the cluster shares
// the buckets (which matters most during exactly the incident that took Redis
// down).
func Open(cfg *config.Redis, logger *zap.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.DialTimeout,
		WriteTimeout: cfg.DialTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), cfg.PingTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis %q: %w", cfg.Addr, err)
	}
	if logger != nil {
		logger.Info("redis connected", zap.String("addr", cfg.Addr), zap.Int("db", cfg.DB))
	}
	return client, nil
}
