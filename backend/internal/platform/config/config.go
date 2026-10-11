// Package config provides strongly-typed environment configuration loading and validation for NEXORA.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all platform configurations using NEXORA_* environment naming.
type Config struct {
	Env             string
	LogLevel        string
	HTTPPort        int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration

	// Operational
	ReadinessTimeout   time.Duration // Maximum time for /readyz dependency checks
	CleanupItemTimeout time.Duration // Per-item timeout for lifecycle cleanup hooks

	// Rate Limiting & Proxy Security
	RateLimitRequests int
	RateLimitWindow   time.Duration
	TrustedProxies    []string

	// Database
	DBHost            string
	DBPort            int
	DBName            string
	DBUser            string
	DBPassword        string
	DBSSLMode         string
	DBMaxConns        int
	DBMinConns        int
	DBMaxConnLifetime time.Duration
	DBConnectTimeout  time.Duration

	// Redis
	RedisHost           string
	RedisPort           int
	RedisPassword       string
	RedisDB             int
	RedisConnectTimeout time.Duration

	// Observability & Tracing
	OTLPEndpoint string
	ServiceName  string
}

// Load loads configuration from environment variables with safe defaults and validates settings.
func Load() (*Config, error) {
	return LoadFrom(getOSEnv)
}

// LoadFrom loads configuration using the provided environment lookup function and validates settings.
func LoadFrom(lookup func(string) string) (*Config, error) {
	var parseErrs []error

	port, err := getEnvInt(lookup, "NEXORA_HTTP_PORT", 8080)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	readTimeout, err := getEnvDuration(lookup, "NEXORA_HTTP_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	writeTimeout, err := getEnvDuration(lookup, "NEXORA_HTTP_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	shutdownTimeout, err := getEnvDuration(lookup, "NEXORA_HTTP_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	readinessTimeout, err := getEnvDuration(lookup, "NEXORA_READINESS_TIMEOUT", 3*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	cleanupItemTimeout, err := getEnvDuration(lookup, "NEXORA_CLEANUP_ITEM_TIMEOUT", 10*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	rateLimitRequests, err := getEnvInt(lookup, "NEXORA_RATE_LIMIT_REQUESTS", 100)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	rateLimitWindow, err := getEnvDuration(lookup, "NEXORA_RATE_LIMIT_WINDOW", 1*time.Minute)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	trustedProxiesRaw := getEnvString(lookup, "NEXORA_TRUSTED_PROXIES", "")
	var trustedProxies []string
	if strings.TrimSpace(trustedProxiesRaw) != "" {
		for _, p := range strings.Split(trustedProxiesRaw, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				trustedProxies = append(trustedProxies, trimmed)
			}
		}
	}

	dbPort, err := getEnvInt(lookup, "NEXORA_DB_PORT", 5432)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	dbMaxConns, err := getEnvInt(lookup, "NEXORA_DB_MAX_CONNS", 25)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	dbMinConns, err := getEnvInt(lookup, "NEXORA_DB_MIN_CONNS", 5)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	dbMaxConnLifetime, err := getEnvDuration(lookup, "NEXORA_DB_MAX_CONN_LIFETIME", 1*time.Hour)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	dbConnectTimeout, err := getEnvDuration(lookup, "NEXORA_DB_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	redisPort, err := getEnvInt(lookup, "NEXORA_REDIS_PORT", 6379)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	redisDB, err := getEnvInt(lookup, "NEXORA_REDIS_DB", 0)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	redisConnectTimeout, err := getEnvDuration(lookup, "NEXORA_REDIS_CONNECT_TIMEOUT", 2*time.Second)
	if err != nil {
		parseErrs = append(parseErrs, err)
	}

	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("configuration parsing errors: %w", errors.Join(parseErrs...))
	}

	cfg := &Config{
		Env:             getEnvString(lookup, "NEXORA_ENV", "development"),
		LogLevel:        getEnvString(lookup, "NEXORA_LOG_LEVEL", "info"),
		HTTPPort:        port,
		ReadTimeout:     readTimeout,
		WriteTimeout:    writeTimeout,
		ShutdownTimeout: shutdownTimeout,

		ReadinessTimeout:   readinessTimeout,
		CleanupItemTimeout: cleanupItemTimeout,

		RateLimitRequests: rateLimitRequests,
		RateLimitWindow:   rateLimitWindow,
		TrustedProxies:    trustedProxies,

		DBHost:            getEnvString(lookup, "NEXORA_DB_HOST", "localhost"),
		DBPort:            dbPort,
		DBName:            getEnvString(lookup, "NEXORA_DB_NAME", "nexora"),
		DBUser:            getEnvString(lookup, "NEXORA_DB_USER", "nexora_admin"),
		DBPassword:        getEnvString(lookup, "NEXORA_DB_PASSWORD", "nexora_secret_dev"),
		DBSSLMode:         getEnvString(lookup, "NEXORA_DB_SSLMODE", "disable"),
		DBMaxConns:        dbMaxConns,
		DBMinConns:        dbMinConns,
		DBMaxConnLifetime: dbMaxConnLifetime,
		DBConnectTimeout:  dbConnectTimeout,

		RedisHost:           getEnvString(lookup, "NEXORA_REDIS_HOST", "localhost"),
		RedisPort:           redisPort,
		RedisPassword:       getEnvString(lookup, "NEXORA_REDIS_PASSWORD", "nexora_redis_dev"),
		RedisDB:             redisDB,
		RedisConnectTimeout: redisConnectTimeout,

		OTLPEndpoint: getEnvString(lookup, "NEXORA_OTEL_EXPORTER_OTLP_ENDPOINT", getEnvString(lookup, "OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		ServiceName:  getEnvString(lookup, "NEXORA_OTEL_SERVICE_NAME", getEnvString(lookup, "OTEL_SERVICE_NAME", "nexora-platform")),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation error: %w", err)
	}

	return cfg, nil
}

// Validate verifies all configuration invariants and safety constraints.
func (c *Config) Validate() error {
	var errs []error

	// Environment validation
	switch strings.ToLower(c.Env) {
	case "development", "test", "testing", "staging", "production":
	default:
		errs = append(errs, fmt.Errorf("invalid NEXORA_ENV %q: must be one of [development, test, testing, staging, production]", c.Env))
	}

	// Log level validation
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("invalid NEXORA_LOG_LEVEL %q: must be one of [debug, info, warn, error]", c.LogLevel))
	}

	// HTTP Port validation
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		errs = append(errs, fmt.Errorf("invalid NEXORA_HTTP_PORT %d: must be between 1 and 65535", c.HTTPPort))
	}

	// Timeout validations
	if c.ReadTimeout <= 0 {
		errs = append(errs, errors.New("NEXORA_HTTP_READ_TIMEOUT must be greater than zero"))
	}
	if c.WriteTimeout <= 0 {
		errs = append(errs, errors.New("NEXORA_HTTP_WRITE_TIMEOUT must be greater than zero"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("NEXORA_HTTP_SHUTDOWN_TIMEOUT must be greater than zero"))
	}
	if c.ReadinessTimeout == 0 {
		c.ReadinessTimeout = 3 * time.Second
	} else if c.ReadinessTimeout < 0 {
		errs = append(errs, errors.New("NEXORA_READINESS_TIMEOUT must be greater than zero"))
	} else if c.ReadinessTimeout > 30*time.Second {
		errs = append(errs, errors.New("NEXORA_READINESS_TIMEOUT must not exceed 30s"))
	}

	if c.CleanupItemTimeout == 0 {
		c.CleanupItemTimeout = 10 * time.Second
	} else if c.CleanupItemTimeout < 0 {
		errs = append(errs, errors.New("NEXORA_CLEANUP_ITEM_TIMEOUT must be greater than zero"))
	} else if c.CleanupItemTimeout > 60*time.Second {
		errs = append(errs, errors.New("NEXORA_CLEANUP_ITEM_TIMEOUT must not exceed 60s"))
	}

	// Rate Limiting validations
	if c.RateLimitRequests < 0 {
		errs = append(errs, errors.New("NEXORA_RATE_LIMIT_REQUESTS must be non-negative"))
	}
	if c.RateLimitWindow < 0 {
		errs = append(errs, errors.New("NEXORA_RATE_LIMIT_WINDOW must be non-negative"))
	}
	if c.RateLimitRequests > 0 && c.RateLimitWindow <= 0 {
		errs = append(errs, errors.New("NEXORA_RATE_LIMIT_WINDOW must be greater than zero"))
	}

	for _, cidr := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			errs = append(errs, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err))
		}
	}

	// Database validations
	if strings.TrimSpace(c.DBHost) == "" {
		errs = append(errs, errors.New("NEXORA_DB_HOST must not be empty"))
	}
	if c.DBPort < 1 || c.DBPort > 65535 {
		errs = append(errs, fmt.Errorf("invalid NEXORA_DB_PORT %d: must be between 1 and 65535", c.DBPort))
	}
	if strings.TrimSpace(c.DBName) == "" {
		errs = append(errs, errors.New("NEXORA_DB_NAME must not be empty"))
	}
	if strings.TrimSpace(c.DBUser) == "" {
		errs = append(errs, errors.New("NEXORA_DB_USER must not be empty"))
	}
	if c.DBMaxConns <= 0 {
		errs = append(errs, errors.New("NEXORA_DB_MAX_CONNS must be greater than zero"))
	}
	if c.DBMinConns < 0 {
		errs = append(errs, errors.New("NEXORA_DB_MIN_CONNS must be non-negative"))
	}
	if c.DBMinConns > c.DBMaxConns {
		errs = append(errs, fmt.Errorf("NEXORA_DB_MIN_CONNS (%d) cannot exceed NEXORA_DB_MAX_CONNS (%d)", c.DBMinConns, c.DBMaxConns))
	}
	if c.DBMaxConnLifetime <= 0 {
		errs = append(errs, errors.New("NEXORA_DB_MAX_CONN_LIFETIME must be greater than zero"))
	}
	if c.DBConnectTimeout <= 0 {
		errs = append(errs, errors.New("NEXORA_DB_CONNECT_TIMEOUT must be greater than zero"))
	}

	// Redis validations
	if strings.TrimSpace(c.RedisHost) == "" {
		errs = append(errs, errors.New("NEXORA_REDIS_HOST must not be empty"))
	}
	if c.RedisPort < 1 || c.RedisPort > 65535 {
		errs = append(errs, fmt.Errorf("invalid NEXORA_REDIS_PORT %d: must be between 1 and 65535", c.RedisPort))
	}
	if c.RedisDB < 0 {
		errs = append(errs, errors.New("NEXORA_REDIS_DB must be non-negative"))
	}
	if c.RedisConnectTimeout <= 0 {
		errs = append(errs, errors.New("NEXORA_REDIS_CONNECT_TIMEOUT must be greater than zero"))
	}

	// Production and Staging hardening rules
	isStrict := strings.EqualFold(c.Env, "production") || strings.EqualFold(c.Env, "staging")
	if isStrict {
		if strings.TrimSpace(c.DBPassword) == "" {
			errs = append(errs, fmt.Errorf("NEXORA_DB_PASSWORD cannot be empty in %s", c.Env))
		} else if c.DBPassword == "nexora_secret_dev" {
			errs = append(errs, fmt.Errorf("NEXORA_DB_PASSWORD cannot use default development credentials in %s", c.Env))
		}

		if strings.EqualFold(c.DBSSLMode, "disable") {
			errs = append(errs, fmt.Errorf("NEXORA_DB_SSLMODE cannot be 'disable' in %s", c.Env))
		}

		if c.RedisPassword == "nexora_redis_dev" {
			errs = append(errs, fmt.Errorf("NEXORA_REDIS_PASSWORD cannot use default development credentials in %s", c.Env))
		}
	}

	return errors.Join(errs...)
}

// DatabaseDSN formats a PostgreSQL connection string according to the config.
func (c *Config) DatabaseDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// RedactedDatabaseDSN formats a PostgreSQL connection string with password masked.
func (c *Config) RedactedDatabaseDSN() string {
	return fmt.Sprintf("postgres://%s:******@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// RedisAddr returns the Redis host:port address.
func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}

// String implements fmt.Stringer, returning a string representation with secrets redacted.
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config(Env=%s, LogLevel=%s, HTTPPort=%d, ReadTimeout=%v, WriteTimeout=%v, ShutdownTimeout=%v, "+
			"ReadinessTimeout=%v, CleanupItemTimeout=%v, "+
			"RateLimitRequests=%d, RateLimitWindow=%v, "+
			"DBHost=%s, DBPort=%d, DBName=%s, DBUser=%s, DBPassword=[REDACTED], DBSSLMode=%s, "+
			"DBMaxConns=%d, DBMinConns=%d, DBMaxConnLifetime=%v, DBConnectTimeout=%v, "+
			"RedisHost=%s, RedisPort=%d, RedisPassword=[REDACTED], RedisDB=%d, RedisConnectTimeout=%v)",
		c.Env, c.LogLevel, c.HTTPPort, c.ReadTimeout, c.WriteTimeout, c.ShutdownTimeout,
		c.ReadinessTimeout, c.CleanupItemTimeout,
		c.RateLimitRequests, c.RateLimitWindow,
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBSSLMode,
		c.DBMaxConns, c.DBMinConns, c.DBMaxConnLifetime, c.DBConnectTimeout,
		c.RedisHost, c.RedisPort, c.RedisDB, c.RedisConnectTimeout,
	)
}

// LogValue implements slog.LogValuer to ensure sensitive fields are masked when logging the entire Config.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("log_level", c.LogLevel),
		slog.Int("http_port", c.HTTPPort),
		slog.Duration("read_timeout", c.ReadTimeout),
		slog.Duration("write_timeout", c.WriteTimeout),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Duration("readiness_timeout", c.ReadinessTimeout),
		slog.Duration("cleanup_item_timeout", c.CleanupItemTimeout),
		slog.Int("rate_limit_requests", c.RateLimitRequests),
		slog.Duration("rate_limit_window", c.RateLimitWindow),
		slog.String("db_host", c.DBHost),
		slog.Int("db_port", c.DBPort),
		slog.String("db_name", c.DBName),
		slog.String("db_user", c.DBUser),
		slog.String("db_password", "[REDACTED]"),
		slog.String("db_sslmode", c.DBSSLMode),
		slog.Int("db_max_conns", c.DBMaxConns),
		slog.Int("db_min_conns", c.DBMinConns),
		slog.Duration("db_max_conn_lifetime", c.DBMaxConnLifetime),
		slog.Duration("db_connect_timeout", c.DBConnectTimeout),
		slog.String("redis_host", c.RedisHost),
		slog.Int("redis_port", c.RedisPort),
		slog.String("redis_password", "[REDACTED]"),
		slog.Int("redis_db", c.RedisDB),
		slog.Duration("redis_connect_timeout", c.RedisConnectTimeout),
	)
}

func getEnvString(lookup func(string) string, key, defaultVal string) string {
	if val := lookup(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(lookup func(string) string, key string, defaultVal int) (int, error) {
	val := lookup(key)
	if val == "" {
		return defaultVal, nil
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid integer for %s: %w", key, err)
	}
	return parsed, nil
}

func getEnvDuration(lookup func(string) string, key string, defaultVal time.Duration) (time.Duration, error) {
	val := lookup(key)
	if val == "" {
		return defaultVal, nil
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid duration for %s: %w", key, err)
	}
	return parsed, nil
}

func getOSEnv(key string) string {
	return os.Getenv(key)
}
