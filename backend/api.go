// Package backend provides root lifecycles for the NEXORA modular monolith.
package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"example.com/nexora/backend/internal/crm"
	"example.com/nexora/backend/internal/identity"
	"example.com/nexora/backend/internal/master"
	"example.com/nexora/backend/internal/organization"
	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/idempotency"
	"example.com/nexora/backend/internal/platform/lifecycle"
	"example.com/nexora/backend/internal/platform/logger"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
	transport "example.com/nexora/backend/internal/platform/transport/http"
	"example.com/nexora/backend/internal/pos"
	"example.com/nexora/backend/internal/pricing"
	"example.com/nexora/backend/internal/promotion"
)

// APIOptions allows injecting pre-configured dependencies or overrides for testing.
type APIOptions struct {
	Config *config.Config
	Logger *slog.Logger
	DBPool *pgxpool.Pool
	Redis  *redis.Client
}

// RunAPI initializes and executes the HTTP API server with graceful shutdown.
func RunAPI(ctx context.Context) error {
	return RunAPIWithOptions(ctx, APIOptions{})
}

// RunAPIWithOptions initializes and executes the HTTP API server with injectable dependencies.
func RunAPIWithOptions(ctx context.Context, opts APIOptions) error {
	cfg := opts.Config
	if cfg == nil {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("configuration error: %w", err)
		}
	} else {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("configuration validation error: %w", err)
		}
	}

	log := opts.Logger
	if log == nil {
		log = logger.New("nexora-api", cfg.Env, cfg.LogLevel)
	}

	log.Info("Initializing NEXORA API service",
		slog.String("env", cfg.Env),
		slog.Int("port", cfg.HTTPPort),
		slog.Duration("read_timeout", cfg.ReadTimeout),
		slog.Duration("write_timeout", cfg.WriteTimeout),
		slog.Duration("shutdown_timeout", cfg.ShutdownTimeout),
	)

	lm := lifecycle.NewManager(log)
	lm.SetItemTimeout(cfg.CleanupItemTimeout)

	// Ensure partial initialization is cleaned up in LIFO order if startup fails
	var initErr error
	defer func() {
		if initErr != nil {
			log.Warn("Rolling back partially initialized resources due to startup failure")
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()
			if cleanErr := lm.Cleanup(cleanupCtx); cleanErr != nil {
				log.Error("Error cleaning up partial initialization", slog.String("error", cleanErr.Error()))
			}
		}
	}()

	// 1. PostgreSQL connection pool (optional in Phase 0/1 skeleton if unreachable)
	dbPool := opts.DBPool
	if dbPool == nil {
		var poolErr error
		dbPool, poolErr = postgres.NewPool(ctx, cfg, log)
		if poolErr != nil {
			log.Warn("PostgreSQL pool unreachable during boot (server will still start)", slog.String("error", poolErr.Error()))
			dbPool = nil
		} else {
			lm.Register("postgres_pool", func(_ context.Context) error {
				log.Info("Closing PostgreSQL connection pool")
				dbPool.Close()
				return nil
			})
		}
	} else {
		lm.Register("postgres_pool", func(_ context.Context) error {
			log.Info("Closing provided PostgreSQL connection pool")
			dbPool.Close()
			return nil
		})
	}

	// 2. Redis client (optional in Phase 0/1 skeleton if unreachable)
	rdb := opts.Redis
	if rdb == nil {
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr(),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		redisCtx, redisCancel := context.WithTimeout(ctx, cfg.RedisConnectTimeout)
		if err := rdb.Ping(redisCtx).Err(); err != nil {
			log.Warn("Redis unreachable during boot (server will still start)", slog.String("error", err.Error()))
		} else {
			log.Info("Redis client connected")
		}
		redisCancel()
		lm.Register("redis_client", func(_ context.Context) error {
			log.Info("Closing Redis client")
			return rdb.Close()
		})
	} else {
		lm.Register("redis_client", func(_ context.Context) error {
			log.Info("Closing provided Redis client")
			return rdb.Close()
		})
	}

	// 3. HTTP Server construction
	srv := transport.NewServer(cfg, log, dbPool, rdb)
	if dbPool != nil {
		tm := postgres.NewTxManager(dbPool, log)
		idStore := identity.NewPgxStore(dbPool)
		idAudit := audit.NewPgxStore(dbPool, tm, log)
		idOutbox := outbox.NewPgxWriter(log)
		idHasher := identity.NewArgon2idHasher()
		idService := identity.NewService(idStore, idHasher, tm, idAudit, idOutbox, log)
		idHandler := identity.NewHandler(idService, log)
		srv.Router().Mount("/api/v1/identity", idHandler.Routes())

		orgStore := organization.NewPgxStore(dbPool)
		orgAudit := audit.NewPgxStore(dbPool, tm, log)
		orgOutbox := outbox.NewPgxWriter(log)
		orgService := organization.NewService(orgStore, tm, orgAudit, orgOutbox, log)
		orgHandler := organization.NewHandler(orgService, log)
		srv.Router().Mount("/api/v1/organization", orgHandler.Routes())

		masterStore := master.NewPgxStore(dbPool)
		masterAudit := audit.NewPgxStore(dbPool, tm, log)
		masterOutbox := outbox.NewPgxWriter(log)
		masterService := master.NewService(masterStore, tm, masterAudit, masterOutbox, log)
		masterHandler := master.NewHandler(masterService, log)
		srv.Router().Mount("/api/v1/master", masterHandler.Routes())
		srv.Router().Mount("/api/v1/products", masterHandler.ProductAliasRoutes())

		crmStore := crm.NewPgxStore(dbPool)
		crmAudit := audit.NewPgxStore(dbPool, tm, log)
		crmOutbox := outbox.NewPgxWriter(log)
		crmService := crm.NewService(crmStore, tm, crmAudit, crmOutbox, log)
		crmHandler := crm.NewHandler(crmService, log)
		srv.Router().Mount("/api/v1/crm", crmHandler.Routes())
		srv.Router().Mount("/api/v1/customers", crmHandler.CustomerAliasRoutes())

		posStore := pos.NewPgxStore(dbPool)
		posAudit := audit.NewPgxStore(dbPool, tm, log)
		posOutbox := outbox.NewPgxWriter(log)
		posIdem := idempotency.NewPgxStore(dbPool, log)
		posService := pos.NewService(posStore, tm, posAudit, posOutbox, log)
		posHandler := pos.NewHandler(posService, posIdem, log)
		srv.Router().Mount("/api/v1/pos", posHandler.Routes())
		srv.Router().Mount("/pos", posHandler.CompatibilityRoutes())

		pricingStore := pricing.NewPgxStore(dbPool)
		pricingAudit := audit.NewPgxStore(dbPool, tm, log)
		pricingOutbox := outbox.NewPgxWriter(log)
		pricingService := pricing.NewService(pricingStore, tm, pricingAudit, pricingOutbox, log)
		pricingHandler := pricing.NewHandler(pricingService, log)
		srv.Router().Mount("/api/v1/pricing", pricingHandler.Routes())
		srv.Router().Mount("/pricing", pricingHandler.CompatibilityRoutes())

		promoStore := promotion.NewPgxStore(dbPool)
		promoAudit := audit.NewPgxStore(dbPool, tm, log)
		promoOutbox := outbox.NewPgxWriter(log)
		promoIdem := idempotency.NewPgxStore(dbPool, log)
		promoService := promotion.NewService(promoStore, tm, promoAudit, promoOutbox, log)
		promoHandler := promotion.NewHandler(promoService, promoIdem, log)
		srv.Router().Mount("/api/v1/promotions", promoHandler.Routes())
		srv.Router().Mount("/promotions", promoHandler.CompatibilityRoutes())
	}
	lm.Register("http_server", func(cleanupCtx context.Context) error {
		log.Info("Shutting down HTTP server")
		return srv.Shutdown(cleanupCtx)
	})

	// 4. Runtime execution
	serverErrors := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		initErr = err
		log.Error("HTTP server failed unexpectedly", slog.String("error", err.Error()))
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Info("Shutdown signal received, gracefully terminating...")
	}

	// 5. Orderly shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := lm.Cleanup(shutdownCtx); err != nil {
		log.Error("Graceful shutdown encountered errors", slog.String("error", err.Error()))
		return fmt.Errorf("shutdown error: %w", err)
	}

	log.Info("NEXORA API service stopped successfully")
	return nil
}
