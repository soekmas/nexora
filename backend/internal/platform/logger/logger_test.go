package logger_test

import (
	"bytes"
	"log/slog"
	"testing"

	"example.com/nexora/backend/internal/platform/logger"
)

func TestLogger_Redaction(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: logger.RedactAttr,
	})
	log := slog.New(handler)

	log.Info("test sensitive logging",
		slog.String("password", "super_secret_123"),
		slog.String("client_secret", "client_sec_456"),
		slog.String("access_token", "at_789"),
		slog.String("refresh_token", "rt_101"),
		slog.String("session_token", "st_102"),
		slog.String("auth_header", "Bearer raw_token"),
		slog.String("cookie", "sess=cookie_val"),
		slog.String("api_key", "key_999"),
		slog.String("cvv", "999"),
		slog.String("credit_card", "4111222233334444"),
		slog.String("safe_key", "safe_value"),
		slog.String("raw_message", "User provided header Bearer secret_bearer_token_xyz"),
		slog.String("db_dsn", "postgres://admin:secret_db_pass@127.0.0.1:5432/nexora_db?sslmode=disable"),
	)

	out := buf.String()

	sensitiveStrings := []string{
		"super_secret_123",
		"client_sec_456",
		"at_789",
		"rt_101",
		"st_102",
		"raw_token",
		"cookie_val",
		"key_999",
		"4111222233334444",
		"secret_bearer_token_xyz",
		"secret_db_pass",
	}

	for _, s := range sensitiveStrings {
		if bytes.Contains([]byte(out), []byte(s)) {
			t.Errorf("CRITICAL LEAK: found sensitive string %q in log output: %s", s, out)
		}
	}

	if !bytes.Contains([]byte(out), []byte("safe_value")) {
		t.Errorf("expected safe_value to be preserved in log: %s", out)
	}

	if !bytes.Contains([]byte(out), []byte("Bearer [REDACTED]")) {
		t.Errorf("expected Bearer [REDACTED] in message: %s", out)
	}

	if !bytes.Contains([]byte(out), []byte("postgres://admin:[REDACTED]@127.0.0.1:5432")) {
		t.Errorf("expected redacted postgres DSN: %s", out)
	}
}

func TestLogger_NewAndInit(t *testing.T) {
	log := logger.New("test-service", "test-env", "debug")
	if log == nil {
		t.Fatal("expected non-nil logger")
	}

	initLog := logger.Init("test-env", "info")
	if initLog == nil {
		t.Fatal("expected non-nil logger from Init")
	}
}
