package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

type traceContextKey struct{}

// TraceContext models the W3C TraceContext specification (traceparent).
type TraceContext struct {
	TraceID string // 16-byte hex (32 characters)
	SpanID  string // 8-byte hex (16 characters)
}

// GenerateTraceID generates a cryptographically random 16-byte hex string (32 characters).
func GenerateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateSpanID generates a cryptographically random 8-byte hex string (16 characters).
func GenerateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ParseTraceParent parses a W3C traceparent header value: "00-<trace_id>-<span_id>-<flags>".
func ParseTraceParent(header string) (TraceContext, bool) {
	trimmed := strings.TrimSpace(header)
	parts := strings.Split(trimmed, "-")
	if len(parts) != 4 {
		return TraceContext{}, false
	}
	version, traceID, spanID := parts[0], parts[1], parts[2]
	if version != "00" || len(traceID) != 32 || len(spanID) != 16 {
		return TraceContext{}, false
	}
	return TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
	}, true
}

// FormatTraceParent serializes a TraceContext to a W3C traceparent header value.
func (tc TraceContext) FormatTraceParent() string {
	if tc.TraceID == "" || tc.SpanID == "" {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-01", tc.TraceID, tc.SpanID)
}

// WithTraceContext injects TraceContext into context.Context.
func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// FromTraceContext extracts TraceContext from context.Context, generating a new one if missing.
func FromTraceContext(ctx context.Context) TraceContext {
	if tc, ok := ctx.Value(traceContextKey{}).(TraceContext); ok && tc.TraceID != "" {
		return tc
	}
	return TraceContext{
		TraceID: GenerateTraceID(),
		SpanID:  GenerateSpanID(),
	}
}
