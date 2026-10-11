package backend_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"example.com/nexora/backend"
	"example.com/nexora/backend/internal/platform/config"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate free port: %v", err)
	}
	defer func() {
		_ = l.Close()
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func TestRunAPI_GracefulShutdown(t *testing.T) {
	port := getFreePort(t)

	cfg := &config.Config{
		Env:                 "test",
		LogLevel:            "debug",
		HTTPPort:            port,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		RateLimitRequests:   100,
		RateLimitWindow:     1 * time.Minute,
		DBHost:              "127.0.0.1",
		DBPort:              5432,
		DBName:              "nexora",
		DBUser:              "test_user",
		DBPassword:          "test_pass",
		DBSSLMode:           "disable",
		DBMaxConns:          5,
		DBMinConns:          1,
		DBMaxConnLifetime:   10 * time.Minute,
		RedisHost:           "127.0.0.1",
		RedisPort:           6379,
		RedisDB:             0,
		DBConnectTimeout:    2 * time.Second,
		RedisConnectTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- backend.RunAPIWithOptions(ctx, backend.APIOptions{
			Config: cfg,
			Logger: newTestLogger(),
		})
	}()

	// Wait briefly for server to bind
	time.Sleep(100 * time.Millisecond)

	// Trigger graceful shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean shutdown, got error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunAPI did not terminate within expected duration after cancellation")
	}
}

func TestRunAPI_ConfigError(t *testing.T) {
	invalidCfg := &config.Config{
		Env:                 "production",
		HTTPPort:            8080,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		DBHost:              "localhost",
		DBPort:              5432,
		DBName:              "nexora",
		DBUser:              "user",
		DBPassword:          "nexora_secret_dev", // Forbidden default password in production!
		DBSSLMode:           "disable",           // Forbidden disable in production!
		DBMaxConns:          10,
		DBMinConns:          2,
		DBMaxConnLifetime:   1 * time.Hour,
		DBConnectTimeout:    2 * time.Second,
		RedisHost:           "localhost",
		RedisPort:           6379,
		RedisConnectTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := backend.RunAPIWithOptions(ctx, backend.APIOptions{
		Config: invalidCfg,
		Logger: newTestLogger(),
	})

	if err == nil {
		t.Fatal("expected config validation error in production, got nil")
	}
	if !strings.Contains(err.Error(), "cannot use default development credentials") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunAPI_PortConflictStartupFailure(t *testing.T) {
	// Occupy wildcard port to guarantee address already in use on srv.Start()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skipf("cannot allocate port: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{
		Env:                 "test",
		LogLevel:            "debug",
		HTTPPort:            port,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		ShutdownTimeout:     2 * time.Second,
		DBHost:              "localhost",
		DBPort:              5432,
		DBName:              "nexora",
		DBUser:              "test_user",
		DBPassword:          "test_pass",
		DBSSLMode:           "disable",
		DBMaxConns:          5,
		DBMinConns:          1,
		DBMaxConnLifetime:   10 * time.Minute,
		DBConnectTimeout:    2 * time.Second,
		RedisHost:           "localhost",
		RedisPort:           6379,
		RedisConnectTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	startErr := backend.RunAPIWithOptions(ctx, backend.APIOptions{
		Config: cfg,
		Logger: newTestLogger(),
	})

	if startErr == nil {
		t.Fatal("expected startup failure on port conflict, got nil")
	}
}
