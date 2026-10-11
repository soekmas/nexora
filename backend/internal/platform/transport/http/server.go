// Package http provides the HTTP server foundation and probe endpoints.
package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/observability"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Server wraps the standard http.Server with platform routes and probes.
type Server struct {
	httpServer  *http.Server
	router      chi.Router
	db          *pgxpool.Pool
	redis       *redis.Client
	cfg         *config.Config
	logger      *slog.Logger
	rateLimiter platformmw.Limiter
	metrics     *observability.Metrics
	authStore   auth.RefreshTokenStore
}

// NewServer constructs the HTTP Server with chi router, platform middleware, and probe endpoints.
func NewServer(cfg *config.Config, log *slog.Logger, db *pgxpool.Pool, rdb *redis.Client) *Server {
	r := chi.NewRouter()

	metrics := observability.NewMetrics()
	if db != nil {
		metrics.SetDBPoolStatsFunc(func() observability.DBPoolStats {
			stat := db.Stat()
			return observability.DBPoolStats{
				TotalConns:    int(stat.TotalConns()),
				AcquiredConns: int(stat.AcquiredConns()),
				IdleConns:     int(stat.IdleConns()),
				MaxConns:      int(stat.MaxConns()),
			}
		})
	}

	var rl platformmw.Limiter
	if cfg.RateLimitRequests > 0 && cfg.RateLimitWindow > 0 {
		if rdb != nil {
			redisLimiter := platformmw.NewRedisRateLimiter(rdb, cfg.RateLimitRequests, cfg.RateLimitWindow)
			if len(cfg.TrustedProxies) > 0 {
				_ = redisLimiter.SetTrustedProxies(cfg.TrustedProxies)
			}
			rl = redisLimiter
		} else {
			memLimiter := platformmw.NewMemoryRateLimiter(cfg.RateLimitRequests, cfg.RateLimitWindow)
			if len(cfg.TrustedProxies) > 0 {
				_ = memLimiter.SetTrustedProxies(cfg.TrustedProxies)
			}
			rl = memLimiter
		}
	}

	// Platform middleware ordering:
	// 1. RequestID - Generate or validate incoming X-Request-ID and inject into context
	// 2. Metrics - Track in-flight requests, request totals, and latency histogram
	// 3. RealIP - Resolve client IP behind reverse proxy / load balancer
	// 4. Recovery - Catch unhandled panics, log stack internally, return canonical 500
	// 5. StructuredLogger - Log method, path, route, status, latency, request_id with secret header redaction
	// 6. RateLimit - Enforce rate limit per client IP, return 429 canonical envelope
	// 7. Timeout - Bound request execution time, return 504 canonical envelope
	r.Use(platformmw.RequestID)
	r.Use(observability.HTTPMetricsMiddleware(metrics))
	r.Use(chimw.RealIP)
	r.Use(platformmw.Recovery(log))
	r.Use(platformmw.StructuredLogger(log))
	if rl != nil {
		r.Use(platformmw.RateLimit(rl))
	}
	r.Use(platformmw.Timeout(cfg.ReadTimeout))

	// Catch-all handlers for unmatched routes and method conflicts to guarantee 100% JSON error envelope conformance
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		reqID := platformmw.GetRequestID(r.Context())
		apperr.RespondError(w, reqID, apperr.NewNotFoundError("Resource not found"))
	})

	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		reqID := platformmw.GetRequestID(r.Context())
		apperr.RespondError(w, reqID, apperr.NewMethodNotAllowedError(r.Method))
	})

	var authStore auth.RefreshTokenStore
	if db != nil {
		authStore = auth.NewPgxSessionStore(db, log)
	}

	s := &Server{
		router:      r,
		db:          db,
		redis:       rdb,
		cfg:         cfg,
		logger:      log,
		rateLimiter: rl,
		metrics:     metrics,
		authStore:   authStore,
	}

	// Probes & Metrics
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/version", s.handleVersion)
	r.Get("/metrics", metrics.Handler().ServeHTTP)

	// Auth Endpoints (mounted at both root and canonical /api/v1 prefix)
	r.Post("/auth/refresh", s.handleRefresh)
	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/auth/refresh", s.handleRefresh)
	})

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// SetAuthStore sets or replaces the refresh token store (useful for tests or mocking).
func (s *Server) SetAuthStore(store auth.RefreshTokenStore) {
	s.authStore = store
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	handler := NewAuthHandler(s.authStore, s.logger, DefaultAccessTokenTTL, DefaultRefreshTokenTTL)
	handler.HandleRefresh(w, r)
}

// Metrics returns the platform metrics registry instance.
func (s *Server) Metrics() *observability.Metrics {
	return s.metrics
}

// Router returns the chi.Router instance for route mounting.
func (s *Server) Router() chi.Router {
	return s.router
}

// Handler returns the root http.Handler (used for tests and mounting).
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start begins listening on the configured HTTP port.
func (s *Server) Start() error {
	s.logger.Info("Starting HTTP server", slog.String("addr", s.httpServer.Addr), slog.String("env", s.cfg.Env))
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server and stops background rate limiter cleanup.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	if reqID == "" {
		reqID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ReadinessTimeout)
	defer cancel()

	// Check DB connectivity if pool is configured
	if s.db != nil {
		if err := s.db.Ping(ctx); err != nil {
			s.logger.Warn("Readiness check failed on database", slog.String("error", err.Error()))
			apperr.RespondError(w, reqID, &apperr.AppError{
				HTTPStatus: http.StatusServiceUnavailable,
				Detail: apperr.ErrorDetail{
					Code:    "SERVICE_UNAVAILABLE",
					Message: "Database connection failed",
					Details: map[string]any{"dependency": "postgresql"},
				},
			})
			return
		}
	}

	// Check Redis connectivity if client is configured
	if s.redis != nil {
		if err := s.redis.Ping(ctx).Err(); err != nil {
			s.logger.Warn("Readiness check failed on redis", slog.String("error", err.Error()))
			apperr.RespondError(w, reqID, &apperr.AppError{
				HTTPStatus: http.StatusServiceUnavailable,
				Detail: apperr.ErrorDetail{
					Code:    "SERVICE_UNAVAILABLE",
					Message: "Redis connection failed",
					Details: map[string]any{"dependency": "redis"},
				},
			})
			return
		}
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"database": s.db != nil,
		"redis":    s.redis != nil,
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	RespondJSON(w, http.StatusOK, map[string]string{
		"service":     "nexora-api",
		"version":     "0.1.0",
		"environment": s.cfg.Env,
	})
}
