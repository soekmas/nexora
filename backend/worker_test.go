package backend_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"example.com/nexora/backend"
	"example.com/nexora/backend/internal/platform/config"
	"example.com/nexora/backend/internal/platform/postgres"
)

func TestRunWorker_GracefulShutdown(t *testing.T) {
	cfg := &config.Config{
		Env:                 "test",
		LogLevel:            "debug",
		ShutdownTimeout:     2 * time.Second,
		HTTPPort:            8080,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		RateLimitRequests:   100,
		RateLimitWindow:     1 * time.Minute,
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

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- backend.RunWorkerWithOptions(ctx, backend.WorkerOptions{
			Config: cfg,
			Logger: newTestLogger(),
		})
	}()

	// Wait briefly for worker to start
	time.Sleep(50 * time.Millisecond)

	// Trigger graceful shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean worker shutdown, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWorker did not terminate within expected duration after cancellation")
	}
}

func TestRunWorker_ConfigError(t *testing.T) {
	invalidCfg := &config.Config{
		Env:             "invalid_environment_name",
		ShutdownTimeout: 2 * time.Second,
	}

	ctx := context.Background()
	err := backend.RunWorkerWithOptions(ctx, backend.WorkerOptions{
		Config: invalidCfg,
		Logger: newTestLogger(),
	})

	if err == nil {
		t.Fatal("expected config validation error in worker startup, got nil")
	}
	if !strings.Contains(err.Error(), "invalid NEXORA_ENV") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunWorker_WithOutboxPollerShutdown(t *testing.T) {
	testCfg := &config.Config{
		Env:                 "test",
		LogLevel:            "debug",
		ShutdownTimeout:     2 * time.Second,
		HTTPPort:            8080,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		RateLimitRequests:   100,
		RateLimitWindow:     1 * time.Minute,
		DBHost:              "127.0.0.1",
		DBPort:              15432,
		DBName:              "nexora_test",
		DBUser:              "nexora_test",
		DBPassword:          "test_secret_pass",
		DBSSLMode:           "disable",
		DBMaxConns:          5,
		DBMinConns:          1,
		DBMaxConnLifetime:   15 * time.Minute,
		DBConnectTimeout:    2 * time.Second,
		RedisHost:           "localhost",
		RedisPort:           6379,
		RedisConnectTimeout: 2 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, testCfg, newTestLogger())
	if err != nil {
		t.Skipf("Live test DB not reachable (%v), skipping poller worker test", err)
		return
	}
	defer pool.Close()

	workerCtx, workerCancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- backend.RunWorkerWithOptions(workerCtx, backend.WorkerOptions{
			Config: testCfg,
			Logger: newTestLogger(),
			DBPool: pool,
		})
	}()

	// Wait briefly for worker and poller to start
	time.Sleep(50 * time.Millisecond)

	// Trigger graceful shutdown
	workerCancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean worker and poller shutdown, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not shut down within timeout")
	}
}
