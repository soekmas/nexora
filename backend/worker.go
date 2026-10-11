package backend

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/lifecycle"
	"example.com/nexora/backend/internal/platform/logger"
	"example.com/nexora/backend/internal/platform/observability"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// WorkerOptions allows injecting pre-configured dependencies or overrides for testing.
type WorkerOptions struct {
	Config     *config.Config
	Logger     *slog.Logger
	DBPool     *pgxpool.Pool
	Dispatcher outbox.Dispatcher
	Metrics    *observability.Metrics
}

// RunWorker initializes and executes the background worker daemon with graceful shutdown.
func RunWorker(ctx context.Context) error {
	return RunWorkerWithOptions(ctx, WorkerOptions{})
}

// RunWorkerWithOptions initializes and executes the worker daemon with injectable dependencies.
func RunWorkerWithOptions(ctx context.Context, opts WorkerOptions) error {
	cfg := opts.Config
	if cfg == nil {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("worker configuration error: %w", err)
		}
	} else {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("worker configuration validation error: %w", err)
		}
	}

	log := opts.Logger
	if log == nil {
		log = logger.New("nexora-worker", cfg.Env, cfg.LogLevel)
	}

	metrics := opts.Metrics
	if metrics == nil {
		metrics = observability.NewMetrics()
	}

	log.Info("Initializing NEXORA Worker service",
		slog.String("env", cfg.Env),
		slog.Duration("shutdown_timeout", cfg.ShutdownTimeout),
	)

	lm := lifecycle.NewManager(log)
	lm.SetItemTimeout(cfg.CleanupItemTimeout)

	var initErr error
	defer func() {
		if initErr != nil {
			log.Warn("Rolling back partially initialized worker resources due to startup failure")
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()
			if cleanErr := lm.Cleanup(cleanupCtx); cleanErr != nil {
				log.Error("Error cleaning up worker partial initialization", slog.String("error", cleanErr.Error()))
			}
		}
	}()

	// Initialize database pool if not provided and not running in synthetic test mode
	var dbPool *pgxpool.Pool
	if opts.DBPool != nil {
		dbPool = opts.DBPool
	} else if cfg.DBHost != "" && cfg.Env != "test" {
		var poolErr error
		dbPool, poolErr = postgres.NewPool(ctx, cfg, log)
		if poolErr != nil {
			initErr = fmt.Errorf("failed to initialize worker database pool: %w", poolErr)
			return initErr
		}
		lm.Register("database_pool", func(_ context.Context) error {
			log.Info("Closing worker database connection pool")
			dbPool.Close()
			return nil
		})
	}

	// Wire outbox poller if database pool is available
	if dbPool != nil {
		dispatcher := opts.Dispatcher
		if dispatcher == nil {
			dispatcher = outbox.NewDispatcher(log)
		}
		pollerCfg := outbox.DefaultPollerConfig()
		poller := outbox.NewPoller(dbPool, dispatcher, pollerCfg, log)
		poller.SetMetrics(metrics)

		lm.Register("outbox_poller", func(c context.Context) error {
			return poller.Stop(c)
		})
		if err := poller.Start(ctx); err != nil {
			initErr = fmt.Errorf("failed to start outbox poller: %w", err)
			return initErr
		}
	}

	// Heartbeat ticker for Phase 0/1 skeleton
	ticker := time.NewTicker(30 * time.Second)
	lm.Register("worker_ticker", func(_ context.Context) error {
		log.Info("Stopping worker heartbeat ticker")
		ticker.Stop()
		return nil
	})

	log.Info("NEXORA Worker daemon is active")

	<-ctx.Done()
	log.Info("Worker shutdown signal received, terminating gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := lm.Cleanup(shutdownCtx); err != nil {
		log.Error("Worker graceful shutdown encountered errors", slog.String("error", err.Error()))
		return fmt.Errorf("worker shutdown error: %w", err)
	}

	log.Info("NEXORA Worker daemon stopped successfully")
	return nil
}
