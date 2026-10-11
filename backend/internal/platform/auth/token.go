// Package auth provides authentication, session management, and scope authorization for NEXORA.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	// TokenPrefix is the mandatory prefix for all opaque NEXORA bearer access tokens.
	TokenPrefix = "nex_at_"

	// RefreshTokenPrefix is the mandatory prefix for all opaque NEXORA refresh tokens.
	RefreshTokenPrefix = "nex_rt_"

	// TokenEntropyBytes defines the number of secure random bytes used to generate tokens.
	TokenEntropyBytes = 32
)

// GenerateToken generates a cryptographically secure opaque bearer token with the nex_at_ prefix
// and returns both the raw token (sent only once to the client) and its SHA-256 hex digest for storage.
func GenerateToken() (string, string, error) {
	bytes := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate secure random token: %w", err)
	}

	rawToken := TokenPrefix + hex.EncodeToString(bytes)
	tokenHash := HashToken(rawToken)
	return rawToken, tokenHash, nil
}

// GenerateRefreshToken generates a cryptographically secure opaque refresh token with the nex_rt_ prefix
// and returns both the raw token (sent only once to the client) and its SHA-256 hex digest for storage.
func GenerateRefreshToken() (string, string, error) {
	bytes := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate secure random refresh token: %w", err)
	}

	rawToken := RefreshTokenPrefix + hex.EncodeToString(bytes)
	tokenHash := HashToken(rawToken)
	return rawToken, tokenHash, nil
}

// HashToken computes the deterministic SHA-256 digest of an opaque bearer token.
// The raw token is NEVER stored in the database.
func HashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// ValidateTokenFormat verifies whether a given token conforms to the required nex_at_ prefix
// and minimum length criteria without evaluating its database validity.
func ValidateTokenFormat(token string) bool {
	if !strings.HasPrefix(token, TokenPrefix) {
		return false
	}

	rest := strings.TrimPrefix(token, TokenPrefix)
	// Must contain at least 32 hex/alphanumeric characters and no spaces or control characters
	if len(rest) < 32 || len(rest) > 128 {
		return false
	}

	for _, c := range rest {
		if !isValidTokenRune(c) {
			return false
		}
	}

	return true
}

// ValidateRefreshTokenFormat verifies whether a given token conforms to the required nex_rt_ prefix
// and minimum length criteria without evaluating its database validity.
func ValidateRefreshTokenFormat(token string) bool {
	if !strings.HasPrefix(token, RefreshTokenPrefix) {
		return false
	}

	rest := strings.TrimPrefix(token, RefreshTokenPrefix)
	if len(rest) < 32 || len(rest) > 128 {
		return false
	}

	for _, c := range rest {
		if !isValidTokenRune(c) {
			return false
		}
	}

	return true
}

func isValidTokenRune(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '_' || c == '-'
}

// ParseBearerToken extracts the raw token from an Authorization header value.
// It returns an error if the header is empty, does not use Bearer scheme, or has an invalid format.
func ParseBearerToken(authHeader string) (string, error) {
	if authHeader == "" {
		return "", errors.New("authorization header is required")
	}

	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("authorization header must use Bearer scheme")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("bearer token is empty")
	}

	if !ValidateTokenFormat(token) {
		return "", errors.New("invalid bearer token format")
	}

	return token, nil
}
