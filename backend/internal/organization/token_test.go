package organization

import (
	"strings"
	"testing"
)

func TestDeviceToken_GenerateAndVerify(t *testing.T) {
	plain, hash, err := GenerateDeviceToken()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	if !strings.HasPrefix(plain, DeviceTokenPrefix) {
		t.Fatalf("expected token prefix %s, got %s", DeviceTokenPrefix, plain)
	}

	if len(hash) != 64 { // SHA-256 hex length is 64 characters
		t.Fatalf("expected 64-char sha256 hex string, got %d chars: %s", len(hash), hash)
	}

	// Correct token verification
	if !VerifyDeviceToken(plain, hash) {
		t.Fatalf("expected valid verification for genuine token")
	}

	// Incorrect token verification
	if VerifyDeviceToken("wrong_token", hash) {
		t.Fatalf("expected verification failure for wrong token")
	}

	// Empty / whitespace inputs
	if VerifyDeviceToken("", hash) {
		t.Fatalf("expected verification failure for empty token")
	}
	if VerifyDeviceToken(plain, "") {
		t.Fatalf("expected verification failure for empty hash")
	}
}

func TestDeviceToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	hashes := make(map[string]bool)

	for i := 0; i < 100; i++ {
		plain, hash, err := GenerateDeviceToken()
		if err != nil {
			t.Fatalf("generate token failed: %v", err)
		}
		if tokens[plain] {
			t.Fatalf("duplicate plaintext token generated: %s", plain)
		}
		if hashes[hash] {
			t.Fatalf("duplicate hash generated: %s", hash)
		}
		tokens[plain] = true
		hashes[hash] = true
	}
}
