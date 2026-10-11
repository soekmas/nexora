package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"example.com/nexora/backend/internal/platform/config"
)

func TestConfigLoad_DefaultDevelopment(t *testing.T) {
	mockEnv := map[string]string{}
	cfg, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
	if err != nil {
		t.Fatalf("expected successful load with defaults, got error: %v", err)
	}

	if cfg.Env != "development" {
		t.Errorf("expected Env=development, got %s", cfg.Env)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("expected HTTPPort=8080, got %d", cfg.HTTPPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel=info, got %s", cfg.LogLevel)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("expected DBHost=localhost, got %s", cfg.DBHost)
	}
	if cfg.DBPort != 5432 {
		t.Errorf("expected DBPort=5432, got %d", cfg.DBPort)
	}
	if cfg.RedisPort != 6379 {
		t.Errorf("expected RedisPort=6379, got %d", cfg.RedisPort)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout=10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.DBConnectTimeout != 5*time.Second {
		t.Errorf("expected DBConnectTimeout=5s, got %v", cfg.DBConnectTimeout)
	}
	if cfg.RedisConnectTimeout != 2*time.Second {
		t.Errorf("expected RedisConnectTimeout=2s, got %v", cfg.RedisConnectTimeout)
	}
	if cfg.RateLimitRequests != 100 {
		t.Errorf("expected RateLimitRequests=100, got %d", cfg.RateLimitRequests)
	}
	if cfg.RateLimitWindow != 1*time.Minute {
		t.Errorf("expected RateLimitWindow=1m, got %v", cfg.RateLimitWindow)
	}
}

func TestConfigLoad_CustomValidValues(t *testing.T) {
	mockEnv := map[string]string{
		"NEXORA_ENV":                   "test",
		"NEXORA_LOG_LEVEL":             "debug",
		"NEXORA_HTTP_PORT":             "9090",
		"NEXORA_HTTP_READ_TIMEOUT":     "5s",
		"NEXORA_HTTP_WRITE_TIMEOUT":    "10s",
		"NEXORA_HTTP_SHUTDOWN_TIMEOUT": "15s",
		"NEXORA_DB_HOST":               "postgres-host",
		"NEXORA_DB_PORT":               "5433",
		"NEXORA_DB_NAME":               "custom_nexora",
		"NEXORA_DB_USER":               "custom_user",
		"NEXORA_DB_PASSWORD":           "custom_secret",
		"NEXORA_DB_SSLMODE":            "require",
		"NEXORA_DB_MAX_CONNS":          "50",
		"NEXORA_DB_MIN_CONNS":          "10",
		"NEXORA_DB_MAX_CONN_LIFETIME":  "30m",
		"NEXORA_REDIS_HOST":            "redis-host",
		"NEXORA_REDIS_PORT":            "6380",
		"NEXORA_REDIS_PASSWORD":        "redis_secret",
		"NEXORA_REDIS_DB":              "2",
		"NEXORA_DB_CONNECT_TIMEOUT":    "8s",
		"NEXORA_REDIS_CONNECT_TIMEOUT": "3s",
		"NEXORA_RATE_LIMIT_REQUESTS":   "500",
		"NEXORA_RATE_LIMIT_WINDOW":     "30s",
	}

	cfg, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
	if err != nil {
		t.Fatalf("expected successful load, got error: %v", err)
	}

	if cfg.Env != "test" || cfg.HTTPPort != 9090 || cfg.LogLevel != "debug" {
		t.Errorf("unexpected loaded values: %+v", cfg)
	}
	if cfg.RateLimitRequests != 500 || cfg.RateLimitWindow != 30*time.Second {
		t.Errorf("unexpected rate limit values: %+v", cfg)
	}
	if cfg.DBHost != "postgres-host" || cfg.DBPort != 5433 || cfg.DBUser != "custom_user" {
		t.Errorf("unexpected database values: %+v", cfg)
	}
	if cfg.RedisHost != "redis-host" || cfg.RedisPort != 6380 || cfg.RedisDB != 2 {
		t.Errorf("unexpected redis values: %+v", cfg)
	}
	if cfg.DBConnectTimeout != 8*time.Second || cfg.RedisConnectTimeout != 3*time.Second {
		t.Errorf("unexpected timeout values: %+v", cfg)
	}
}

func TestConfigLoad_ParsingErrors(t *testing.T) {
	tests := []struct {
		name      string
		envKey    string
		envVal    string
		errSubstr string
	}{
		{
			name:      "invalid integer for HTTP port",
			envKey:    "NEXORA_HTTP_PORT",
			envVal:    "invalid_port",
			errSubstr: "invalid integer for NEXORA_HTTP_PORT",
		},
		{
			name:      "invalid duration for read timeout",
			envKey:    "NEXORA_HTTP_READ_TIMEOUT",
			envVal:    "invalid_duration",
			errSubstr: "invalid duration for NEXORA_HTTP_READ_TIMEOUT",
		},
		{
			name:      "invalid integer for DB port",
			envKey:    "NEXORA_DB_PORT",
			envVal:    "not_a_number",
			errSubstr: "invalid integer for NEXORA_DB_PORT",
		},
		{
			name:      "invalid duration for max conn lifetime",
			envKey:    "NEXORA_DB_MAX_CONN_LIFETIME",
			envVal:    "bad_time",
			errSubstr: "invalid duration for NEXORA_DB_MAX_CONN_LIFETIME",
		},
		{
			name:      "invalid integer for Redis port",
			envKey:    "NEXORA_REDIS_PORT",
			envVal:    "abc",
			errSubstr: "invalid integer for NEXORA_REDIS_PORT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockEnv := map[string]string{
				tc.envKey: tc.envVal,
			}
			_, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
			if err == nil {
				t.Fatalf("expected parsing error for %s=%s, got nil", tc.envKey, tc.envVal)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("expected error containing %q, got %q", tc.errSubstr, err.Error())
			}
		})
	}
}

func TestConfigValidate_BoundaryAndInvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *config.Config)
		errSubstr string
	}{
		{
			name: "invalid env name",
			mutate: func(c *config.Config) {
				c.Env = "invalid_environment"
			},
			errSubstr: "invalid NEXORA_ENV",
		},
		{
			name: "invalid log level",
			mutate: func(c *config.Config) {
				c.LogLevel = "verbose"
			},
			errSubstr: "invalid NEXORA_LOG_LEVEL",
		},
		{
			name: "port below range",
			mutate: func(c *config.Config) {
				c.HTTPPort = 0
			},
			errSubstr: "invalid NEXORA_HTTP_PORT",
		},
		{
			name: "port above range",
			mutate: func(c *config.Config) {
				c.HTTPPort = 70000
			},
			errSubstr: "invalid NEXORA_HTTP_PORT",
		},
		{
			name: "negative read timeout",
			mutate: func(c *config.Config) {
				c.ReadTimeout = -1 * time.Second
			},
			errSubstr: "NEXORA_HTTP_READ_TIMEOUT must be greater than zero",
		},
		{
			name: "zero shutdown timeout",
			mutate: func(c *config.Config) {
				c.ShutdownTimeout = 0
			},
			errSubstr: "NEXORA_HTTP_SHUTDOWN_TIMEOUT must be greater than zero",
		},
		{
			name: "empty db host",
			mutate: func(c *config.Config) {
				c.DBHost = "   "
			},
			errSubstr: "NEXORA_DB_HOST must not be empty",
		},
		{
			name: "empty db user",
			mutate: func(c *config.Config) {
				c.DBUser = ""
			},
			errSubstr: "NEXORA_DB_USER must not be empty",
		},
		{
			name: "min conns exceeds max conns",
			mutate: func(c *config.Config) {
				c.DBMaxConns = 10
				c.DBMinConns = 20
			},
			errSubstr: "NEXORA_DB_MIN_CONNS (20) cannot exceed NEXORA_DB_MAX_CONNS (10)",
		},
		{
			name: "empty redis host",
			mutate: func(c *config.Config) {
				c.RedisHost = ""
			},
			errSubstr: "NEXORA_REDIS_HOST must not be empty",
		},
		{
			name: "negative redis db index",
			mutate: func(c *config.Config) {
				c.RedisDB = -1
			},
			errSubstr: "NEXORA_REDIS_DB must be non-negative",
		},
		{
			name: "negative rate limit requests",
			mutate: func(c *config.Config) {
				c.RateLimitRequests = -1
			},
			errSubstr: "NEXORA_RATE_LIMIT_REQUESTS must be non-negative",
		},
		{
			name: "zero rate limit window",
			mutate: func(c *config.Config) {
				c.RateLimitWindow = 0
			},
			errSubstr: "NEXORA_RATE_LIMIT_WINDOW must be greater than zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadFrom(func(string) string { return "" })
			if err != nil {
				t.Fatalf("unexpected error on base load: %v", err)
			}
			tc.mutate(cfg)
			valErr := cfg.Validate()
			if valErr == nil {
				t.Fatalf("expected validation error for test %q, got nil", tc.name)
			}
			if !strings.Contains(valErr.Error(), tc.errSubstr) {
				t.Errorf("expected error containing %q, got %q", tc.errSubstr, valErr.Error())
			}
		})
	}
}

func TestConfigValidate_ProductionHardening(t *testing.T) {
	t.Run("rejects dev db password in production", func(t *testing.T) {
		mockEnv := map[string]string{
			"NEXORA_ENV":            "production",
			"NEXORA_DB_PASSWORD":    "nexora_secret_dev",
			"NEXORA_DB_SSLMODE":     "require",
			"NEXORA_REDIS_PASSWORD": "strong_redis_password_123",
		}
		_, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
		if err == nil {
			t.Fatal("expected production validation error for default db password, got nil")
		}
		if !strings.Contains(err.Error(), "cannot use default development credentials") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("rejects empty db password in production", func(t *testing.T) {
		mockEnv := map[string]string{
			"NEXORA_ENV":            "production",
			"NEXORA_DB_PASSWORD":    "   ",
			"NEXORA_DB_SSLMODE":     "require",
			"NEXORA_REDIS_PASSWORD": "strong_redis_password_123",
		}
		_, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
		if err == nil {
			t.Fatal("expected error for empty db password in production, got nil")
		}
		if !strings.Contains(err.Error(), "NEXORA_DB_PASSWORD cannot be empty") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("rejects sslmode disable in production", func(t *testing.T) {
		mockEnv := map[string]string{
			"NEXORA_ENV":            "production",
			"NEXORA_DB_PASSWORD":    "super_secret_prod_pw_999",
			"NEXORA_DB_SSLMODE":     "disable",
			"NEXORA_REDIS_PASSWORD": "strong_redis_password_123",
		}
		_, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
		if err == nil {
			t.Fatal("expected error for sslmode=disable in production, got nil")
		}
		if !strings.Contains(err.Error(), "NEXORA_DB_SSLMODE cannot be 'disable'") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("rejects default redis password in production", func(t *testing.T) {
		mockEnv := map[string]string{
			"NEXORA_ENV":            "production",
			"NEXORA_DB_PASSWORD":    "super_secret_prod_pw_999",
			"NEXORA_DB_SSLMODE":     "require",
			"NEXORA_REDIS_PASSWORD": "nexora_redis_dev",
		}
		_, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
		if err == nil {
			t.Fatal("expected error for default redis password in production, got nil")
		}
		if !strings.Contains(err.Error(), "NEXORA_REDIS_PASSWORD cannot use default development credentials") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("accepts properly configured production settings", func(t *testing.T) {
		mockEnv := map[string]string{
			"NEXORA_ENV":            "production",
			"NEXORA_DB_PASSWORD":    "super_secret_prod_pw_999",
			"NEXORA_DB_SSLMODE":     "require",
			"NEXORA_REDIS_PASSWORD": "strong_redis_password_123",
		}
		cfg, err := config.LoadFrom(func(k string) string { return mockEnv[k] })
		if err != nil {
			t.Fatalf("expected valid production config to pass, got: %v", err)
		}
		if cfg.Env != "production" {
			t.Errorf("expected Env=production, got %s", cfg.Env)
		}
	})
}

func TestConfig_SecretRedaction(t *testing.T) {
	cfg := &config.Config{
		Env:           "test",
		DBUser:        "test_user",
		DBPassword:    "super_secret_db_password",
		DBHost:        "localhost",
		DBPort:        5432,
		DBName:        "test_db",
		DBSSLMode:     "require",
		RedisPassword: "super_secret_redis_password",
	}

	// 1. RedactedDatabaseDSN must NOT contain the raw password
	redactedDSN := cfg.RedactedDatabaseDSN()
	if strings.Contains(redactedDSN, "super_secret_db_password") {
		t.Errorf("RedactedDatabaseDSN leaked raw password: %s", redactedDSN)
	}
	if !strings.Contains(redactedDSN, "******") {
		t.Errorf("RedactedDatabaseDSN did not mask password with asterisks: %s", redactedDSN)
	}

	// 2. String() must NOT contain the raw passwords
	strRepr := cfg.String()
	if strings.Contains(strRepr, "super_secret_db_password") {
		t.Errorf("String() leaked raw db password: %s", strRepr)
	}
	if strings.Contains(strRepr, "super_secret_redis_password") {
		t.Errorf("String() leaked raw redis password: %s", strRepr)
	}
	if !strings.Contains(strRepr, "DBPassword=[REDACTED]") {
		t.Errorf("String() did not include redaction marker: %s", strRepr)
	}

	// 3. LogValue() slog representation must mask passwords
	val := cfg.LogValue()
	if val.Kind() != slog.KindGroup {
		t.Fatalf("expected slog GroupValue, got %v", val.Kind())
	}
	for _, attr := range val.Group() {
		if attr.Key == "db_password" && attr.Value.String() != "[REDACTED]" {
			t.Errorf("LogValue() db_password is not redacted: %v", attr.Value)
		}
		if attr.Key == "redis_password" && attr.Value.String() != "[REDACTED]" {
			t.Errorf("LogValue() redis_password is not redacted: %v", attr.Value)
		}
	}
}
