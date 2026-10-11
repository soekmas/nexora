package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Default Argon2id parameters approved in DEC-B1-001.
const (
	DefaultArgon2Time        uint32 = 3
	DefaultArgon2Memory      uint32 = 64 * 1024 // 64 MiB in KiB
	DefaultArgon2Parallelism uint8  = 4
	DefaultArgon2KeyLen      uint32 = 32
	DefaultArgon2SaltLen     uint32 = 16
)

// PasswordHasher provides password hashing and verification primitives.
type PasswordHasher interface {
	HashPassword(password string) (string, error)
	VerifyPassword(password, encodedHash string) (bool, error)
}

// Argon2idHasher implements PasswordHasher using the Argon2id algorithm.
type Argon2idHasher struct {
	time        uint32
	memory      uint32
	parallelism uint8
	keyLen      uint32
	saltLen     uint32
}

// NewArgon2idHasher creates a new Argon2idHasher with the approved parameters.
func NewArgon2idHasher() *Argon2idHasher {
	return &Argon2idHasher{
		time:        DefaultArgon2Time,
		memory:      DefaultArgon2Memory,
		parallelism: DefaultArgon2Parallelism,
		keyLen:      DefaultArgon2KeyLen,
		saltLen:     DefaultArgon2SaltLen,
	}
}

// HashPassword generates a salt, hashes the plaintext password, and returns a PHC-formatted string.
// Format: $argon2id$v=19$m=65536,t=3,p=4$<b64salt>$<b64key>
func (h *Argon2idHasher) HashPassword(password string) (string, error) {
	if len(password) == 0 {
		return "", ErrInvalidPassword
	}

	salt := make([]byte, h.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate cryptographic salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.parallelism, h.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.time, h.parallelism, b64Salt, b64Key)

	return encoded, nil
}

// VerifyPassword performs constant-time verification of a plaintext password against an encoded hash.
func (h *Argon2idHasher) VerifyPassword(password, encodedHash string) (bool, error) {
	if len(password) == 0 || len(encodedHash) == 0 {
		return false, ErrInvalidCredentials
	}

	parts := strings.Split(encodedHash, "$")
	// Expected parts: ["", "argon2id", "v=19", "m=65536,t=3,p=4", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[0] != "" {
		return false, ErrMalformedPasswordHash
	}

	if parts[1] != "argon2id" {
		return false, fmt.Errorf("%w: expected argon2id, got %s", ErrUnsupportedHashVersion, parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("%w: version %d", ErrUnsupportedHashVersion, version)
	}

	var memory, timeVal uint32
	var threads uint8
	paramPairs := strings.Split(parts[3], ",")
	if len(paramPairs) != 3 {
		return false, ErrMalformedPasswordHash
	}

	for _, pair := range paramPairs {
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			return false, ErrMalformedPasswordHash
		}
		switch kv[0] {
		case "m":
			m, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil || m == 0 {
				return false, ErrMalformedPasswordHash
			}
			memory = uint32(m)
		case "t":
			t, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil || t == 0 {
				return false, ErrMalformedPasswordHash
			}
			timeVal = uint32(t)
		case "p":
			p, err := strconv.ParseUint(kv[1], 10, 8)
			if err != nil || p == 0 {
				return false, ErrMalformedPasswordHash
			}
			threads = uint8(p)
		default:
			return false, ErrMalformedPasswordHash
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, ErrMalformedPasswordHash
	}

	expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedKey) == 0 {
		return false, ErrMalformedPasswordHash
	}

	computedKey := argon2.IDKey([]byte(password), salt, timeVal, memory, threads, uint32(len(expectedKey)))

	if subtle.ConstantTimeCompare(computedKey, expectedKey) == 1 {
		return true, nil
	}

	return false, nil
}
