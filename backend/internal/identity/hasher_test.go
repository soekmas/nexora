package identity_test

import (
	"errors"
	"strings"
	"testing"

	"example.com/nexora/backend/internal/identity"
)

func TestArgon2idHasher_HashAndVerify(t *testing.T) {
	hasher := identity.NewArgon2idHasher()

	password := "CorrectHorseBatteryStaple!2026"
	hash1, err := hasher.HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected hash error: %v", err)
	}

	if !strings.HasPrefix(hash1, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("unexpected hash prefix: %s", hash1)
	}

	// Verify matching password
	match, err := hasher.VerifyPassword(password, hash1)
	if err != nil {
		t.Fatalf("unexpected verify error: %v", err)
	}
	if !match {
		t.Fatal("expected password to match hash")
	}

	// Verify incorrect password
	match, err = hasher.VerifyPassword("WrongPassword123", hash1)
	if err != nil {
		t.Fatalf("unexpected verify error on wrong password: %v", err)
	}
	if match {
		t.Fatal("expected wrong password to not match")
	}

	// Verify distinct salts for identical passwords
	hash2, err := hasher.HashPassword(password)
	if err != nil {
		t.Fatalf("unexpected second hash error: %v", err)
	}
	if hash1 == hash2 {
		t.Fatal("expected distinct cryptographic salts for identical plaintext passwords")
	}

	match2, err := hasher.VerifyPassword(password, hash2)
	if err != nil || !match2 {
		t.Fatalf("expected hash2 to match original password, match=%t err=%v", match2, err)
	}
}

func TestArgon2idHasher_MalformedAndInvalidHashes(t *testing.T) {
	hasher := identity.NewArgon2idHasher()

	testCases := []struct {
		name        string
		password    string
		hash        string
		expectError error
	}{
		{
			name:        "empty password",
			password:    "",
			hash:        "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
			expectError: identity.ErrInvalidCredentials,
		},
		{
			name:        "empty hash",
			password:    "secret",
			hash:        "",
			expectError: identity.ErrInvalidCredentials,
		},
		{
			name:        "not enough segments",
			password:    "secret",
			hash:        "argon2id$v=19$m=65536",
			expectError: identity.ErrMalformedPasswordHash,
		},
		{
			name:        "unsupported algorithm",
			password:    "secret",
			hash:        "$bcrypt$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
			expectError: identity.ErrUnsupportedHashVersion,
		},
		{
			name:        "unsupported version",
			password:    "secret",
			hash:        "$argon2id$v=99$m=65536,t=3,p=4$c2FsdA$aGFzaA",
			expectError: identity.ErrUnsupportedHashVersion,
		},
		{
			name:        "malformed parameters",
			password:    "secret",
			hash:        "$argon2id$v=19$invalid_params$c2FsdA$aGFzaA",
			expectError: identity.ErrMalformedPasswordHash,
		},
		{
			name:        "corrupted base64 salt",
			password:    "secret",
			hash:        "$argon2id$v=19$m=65536,t=3,p=4$!!invalid_b64!!$aGFzaA",
			expectError: identity.ErrMalformedPasswordHash,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			match, err := hasher.VerifyPassword(tc.password, tc.hash)
			if match {
				t.Fatalf("expected match to be false on invalid input")
			}
			if tc.expectError != nil && !errors.Is(err, tc.expectError) {
				t.Fatalf("expected error %v, got %v", tc.expectError, err)
			}
		})
	}
}

func BenchmarkArgon2idHasher_VerifyPassword(b *testing.B) {
	hasher := identity.NewArgon2idHasher()
	password := "BenchmarkPassword2026!"
	hash, err := hasher.HashPassword(password)
	if err != nil {
		b.Fatalf("failed to hash password: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		match, err := hasher.VerifyPassword(password, hash)
		if err != nil || !match {
			b.Fatalf("unexpected benchmark verification failure: %v", err)
		}
	}
}
