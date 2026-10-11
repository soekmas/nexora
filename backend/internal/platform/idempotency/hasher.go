// Package idempotency provides database-backed idempotency management for NEXORA operations.
package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/gowebpki/jcs"
)

const (
	// HeaderIdempotencyKey is the canonical HTTP header name defined in OpenAPI.
	HeaderIdempotencyKey = "Idempotency-Key"

	// MaxKeyLength defines the maximum permitted idempotency key length matching the OpenAPI specification.
	MaxKeyLength = 128

	// MinKeyLength defines the minimum permitted idempotency key length.
	MinKeyLength = 1
)

var (
	// ErrMissingKey is returned when a required Idempotency-Key header is absent.
	ErrMissingKey = errors.New("idempotency key is required")

	// ErrKeyTooShort is returned when an idempotency key is empty.
	ErrKeyTooShort = errors.New("idempotency key cannot be empty")

	// ErrKeyTooLong is returned when an idempotency key exceeds 128 characters.
	ErrKeyTooLong = fmt.Errorf("idempotency key exceeds maximum length of %d characters", MaxKeyLength)

	// ErrInvalidKeyFormat is returned when an idempotency key contains whitespace or control characters.
	ErrInvalidKeyFormat = errors.New("idempotency key contains invalid characters")

	// ErrMalformedJSON is returned when a JSON request payload is syntactically invalid.
	ErrMalformedJSON = errors.New("request payload is not valid JSON")
)

// ValidateKey validates that an idempotency key conforms to length and character constraints.
func ValidateKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if len(trimmed) < MinKeyLength {
		return ErrKeyTooShort
	}
	if len(key) > MaxKeyLength {
		return ErrKeyTooLong
	}

	for _, r := range key {
		// Only allow printable non-whitespace ASCII characters (33..126)
		if r <= 32 || r > 126 {
			return ErrInvalidKeyFormat
		}
	}

	return nil
}

// CanonicalizeJSON transforms a JSON byte sequence into its canonical representation
// according to RFC 8785 (JSON Canonicalization Scheme - JCS).
// Empty or whitespace-only payloads return an empty byte slice without error.
// Syntactically invalid JSON returns ErrMalformedJSON.
func CanonicalizeJSON(payload []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return []byte{}, nil
	}

	canonical, err := jcs.Transform(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedJSON, err)
	}

	return canonical, nil
}

// HashJSONPayload computes a deterministic SHA-256 hex digest of a JSON payload
// after canonicalizing it according to RFC 8785.
func HashJSONPayload(payload []byte) (string, error) {
	canonical, err := CanonicalizeJSON(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// HashRawPayload computes a SHA-256 hex digest of raw bytes with leading/trailing whitespace trimmed.
func HashRawPayload(payload []byte) string {
	trimmed := bytes.TrimSpace(payload)
	sum := sha256.Sum256(trimmed)
	return hex.EncodeToString(sum[:])
}

// HashRequestPayload computes the canonical request fingerprint.
// For JSON requests (application/json, +json suffix, or unspecified content type),
// it enforces RFC 8785 canonicalization and returns an error if the JSON is malformed.
// For explicit non-JSON content types, it computes the SHA-256 hash of the trimmed raw bytes.
func HashRequestPayload(contentType string, payload []byte) (string, error) {
	mediaType := ""
	if contentType != "" {
		parsed, _, err := mime.ParseMediaType(contentType)
		if err == nil {
			mediaType = strings.ToLower(parsed)
		}
	}

	// Default to JSON unless explicitly specified as a non-JSON media type
	isJSON := mediaType == "" ||
		mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json")

	if isJSON {
		return HashJSONPayload(payload)
	}

	return HashRawPayload(payload), nil
}

// HashPayload computes a deterministic SHA-256 hex digest of the payload.
// If the payload is valid JSON (or empty), it canonicalizes under RFC 8785.
// For non-JSON payloads, it falls back to raw trimmed hashing.
func HashPayload(payload []byte) string {
	hash, err := HashJSONPayload(payload)
	if err == nil {
		return hash
	}
	return HashRawPayload(payload)
}

// NormalizeOperation formats the HTTP method and route into a standardized operation identifier.
func NormalizeOperation(method, route string) string {
	return fmt.Sprintf("%s %s", strings.ToUpper(strings.TrimSpace(method)), strings.TrimSpace(route))
}
