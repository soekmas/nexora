package organization

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// DeviceTokenPrefix provides human and log discernibility for device tokens.
	DeviceTokenPrefix = "dev_tok_"
	// DeviceTokenEntropyBytes specifies 32 bytes (256 bits) of cryptographic randomness.
	DeviceTokenEntropyBytes = 32
)

// GenerateDeviceToken generates a cryptographically secure random device token
// and its SHA-256 hash. Per DEC-B1-007, only the hash is persisted in the database,
// while the plaintext is returned exactly once to the client upon registration.
func GenerateDeviceToken() (plaintextToken string, tokenHash string, err error) {
	bytes := make([]byte, DeviceTokenEntropyBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate device token entropy: %w", err)
	}

	plaintextToken = DeviceTokenPrefix + hex.EncodeToString(bytes)
	tokenHash = HashDeviceToken(plaintextToken)
	return plaintextToken, tokenHash, nil
}

// HashDeviceToken computes the SHA-256 hex digest of a device token.
func HashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// VerifyDeviceToken performs a constant-time comparison between a provided plaintext token
// and a stored SHA-256 token hash.
func VerifyDeviceToken(providedToken, storedHash string) bool {
	if strings.TrimSpace(providedToken) == "" || strings.TrimSpace(storedHash) == "" {
		return false
	}

	computedHash := HashDeviceToken(providedToken)
	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) == 1
}
