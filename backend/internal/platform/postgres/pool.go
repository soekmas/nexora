package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/config"
)

// NewPool initializes and verifies a PostgreSQL connection pool using validated configuration.
func NewPool(ctx context.Context, cfg *config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}
	if log == nil {
		log = slog.Default()
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseDSN())
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DBMaxConns)
	poolCfg.MinConns = int32(cfg.DBMinConns)
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolCfg.HealthCheckPeriod = 1 * time.Minute

	log.Debug("Configuring PostgreSQL connection pool",
		slog.String("dsn", cfg.RedactedDatabaseDSN()),
		slog.Int("max_conns", cfg.DBMaxConns),
		slog.Int("min_conns", cfg.DBMinConns),
		slog.Duration("max_conn_lifetime", cfg.DBMaxConnLifetime),
		slog.Duration("connect_timeout", cfg.DBConnectTimeout),
	)

	connectCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", MapError(err))
	}

	// Verify connectivity
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", MapError(err))
	}

	log.Info("PostgreSQL connection pool initialized and verified",
		slog.String("host", cfg.DBHost),
		slog.Int("port", cfg.DBPort),
		slog.String("db_name", cfg.DBName),
	)

	return pool, nil
}
