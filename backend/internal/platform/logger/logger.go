// Package logger initializes structured logging via log/slog with automatic sensitive data redaction.
package logger

import (
	"log/slog"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// RedactedPlaceholder is substituted for sensitive credentials and tokens in log outputs.
const RedactedPlaceholder = "[REDACTED]"

var (
	bearerTokenRegex = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9_\-\.]+`)
	basicAuthRegex   = regexp.MustCompile(`(?i)(Basic\s+)[A-Za-z0-9+/=]+`)
	dsnPasswordRegex = regexp.MustCompile(`([a-zA-Z0-9+.-]+://[^:]+:)([^@]+)(@)`)
)

// sensitiveKeyPatterns contains normalized keywords that indicate sensitive credential attributes.
var sensitiveKeyPatterns = []string{
	"password",
	"passwd",
	"secret",
	"clientsecret",
	"token",
	"accesstoken",
	"refreshtoken",
	"sessiontoken",
	"authheader",
	"authorization",
	"cookie",
	"setcookie",
	"apikey",
	"privatekey",
	"cvv",
	"cvc",
	"pan",
	"cardnumber",
	"creditcard",
}

func normalizeKey(k string) string {
	var sb strings.Builder
	for _, r := range k {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}

func isSensitiveKey(k string) bool {
	norm := normalizeKey(k)
	if norm == "" {
		return false
	}
	for _, pattern := range sensitiveKeyPatterns {
		if strings.Contains(norm, pattern) {
			return true
		}
	}
	return false
}

// sanitizeStringValue inspects string attributes for Bearer tokens or connection strings containing passwords.
func sanitizeStringValue(s string) string {
	// 1. Redact Bearer and Basic tokens in headers or messages
	if strings.Contains(s, "Bearer ") || strings.Contains(s, "bearer ") {
		s = bearerTokenRegex.ReplaceAllString(s, "${1}"+RedactedPlaceholder)
	}
	if strings.Contains(s, "Basic ") || strings.Contains(s, "basic ") {
		s = basicAuthRegex.ReplaceAllString(s, "${1}"+RedactedPlaceholder)
	}

	// 2. Sanitize database and redis connection strings (postgres://user:pass@..., redis://...)
	if strings.Contains(s, "://") && strings.Contains(s, "@") {
		s = dsnPasswordRegex.ReplaceAllString(s, "${1}"+RedactedPlaceholder+"${3}")
	}

	return s
}

// RedactAttr is an slog ReplaceAttr function that strips credentials and redacts sensitive parameters.
func RedactAttr(_ []string, a slog.Attr) slog.Attr {
	// If the attribute key is sensitive, completely mask its value
	if isSensitiveKey(a.Key) {
		a.Value = slog.StringValue(RedactedPlaceholder)
		return a
	}

	// For string values, sanitize embedded tokens or connection string passwords
	if a.Value.Kind() == slog.KindString {
		str := a.Value.String()
		sanitized := sanitizeStringValue(str)
		if sanitized != str {
			a.Value = slog.StringValue(sanitized)
		}
	}

	return a
}

// New creates a new structured logger with service identification, environment tag, and automatic credential redaction.
func New(service string, env string, levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: RedactAttr,
	}

	var handler slog.Handler
	if strings.ToLower(env) == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	if service == "" {
		service = "nexora"
	}
	if env == "" {
		env = "development"
	}

	log := slog.New(handler).With(
		slog.String("service", service),
		slog.String("env", env),
	)

	return log
}

// Init initializes the global slog default logger according to environment and level.
func Init(env string, levelStr string) *slog.Logger {
	log := New("nexora", env, levelStr)
	slog.SetDefault(log)
	return log
}
