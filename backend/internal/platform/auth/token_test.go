package auth_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"example.com/nexora/backend/internal/platform/auth"
)

func TestGenerateToken(t *testing.T) {
	rawToken, tokenHash, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if !strings.HasPrefix(rawToken, auth.TokenPrefix) {
		t.Errorf("expected raw token to have prefix %s, got %s", auth.TokenPrefix, rawToken)
	}

	// Verify length: "nex_at_" (7) + 64 hex chars = 71
	if len(rawToken) != 7+64 {
		t.Errorf("expected token length 71, got %d (%s)", len(rawToken), rawToken)
	}

	// Verify SHA-256 digest
	expectedSum := sha256.Sum256([]byte(rawToken))
	expectedHash := hex.EncodeToString(expectedSum[:])
	if tokenHash != expectedHash {
		t.Errorf("expected token hash %s, got %s", expectedHash, tokenHash)
	}

	// Ensure raw token != token hash
	if rawToken == tokenHash {
		t.Errorf("raw token must not equal token hash")
	}

	// Generate another token to ensure randomness
	raw2, hash2, _ := auth.GenerateToken()
	if rawToken == raw2 || tokenHash == hash2 {
		t.Errorf("generated tokens must be unique and non-deterministic")
	}
}

func TestValidateTokenFormat(t *testing.T) {
	validToken, _, _ := auth.GenerateToken()

	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{
			name:  "valid generated token",
			token: validToken,
			valid: true,
		},
		{
			name:  "missing prefix",
			token: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			valid: false,
		},
		{
			name:  "wrong prefix",
			token: "jwt_at_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			valid: false,
		},
		{
			name:  "too short rest",
			token: "nex_at_short",
			valid: false,
		},
		{
			name:  "contains spaces",
			token: "nex_at_0123456789abcdef 0123456789abcdef0123456789abcdef",
			valid: false,
		},
		{
			name:  "contains illegal characters",
			token: "nex_at_0123456789abcdef<script>0123456789abcdef</script>",
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ValidateTokenFormat(tc.token)
			if got != tc.valid {
				t.Errorf("ValidateTokenFormat(%q) = %v; want %v", tc.token, got, tc.valid)
			}
		})
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	rawToken, tokenHash, err := auth.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}

	if !strings.HasPrefix(rawToken, auth.RefreshTokenPrefix) {
		t.Errorf("expected raw token prefix %s, got %s", auth.RefreshTokenPrefix, rawToken)
	}

	// Verify length: "nex_rt_" (7) + 64 hex = 71
	if len(rawToken) != 7+64 {
		t.Errorf("expected token length 71, got %d", len(rawToken))
	}

	expectedSum := sha256.Sum256([]byte(rawToken))
	expectedHash := hex.EncodeToString(expectedSum[:])
	if tokenHash != expectedHash {
		t.Errorf("expected token hash %s, got %s", expectedHash, tokenHash)
	}

	// Ensure access token cannot validate as refresh token
	if auth.ValidateTokenFormat(rawToken) {
		t.Errorf("refresh token must not validate as access token")
	}
	if !auth.ValidateRefreshTokenFormat(rawToken) {
		t.Errorf("refresh token must validate as refresh token")
	}
}

func TestValidateRefreshTokenFormat(t *testing.T) {
	validToken, _, _ := auth.GenerateRefreshToken()
	validAccessToken, _, _ := auth.GenerateToken()

	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{
			name:  "valid refresh token",
			token: validToken,
			valid: true,
		},
		{
			name:  "access token rejected as refresh token",
			token: validAccessToken,
			valid: false,
		},
		{
			name:  "missing prefix",
			token: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			valid: false,
		},
		{
			name:  "too short",
			token: "nex_rt_short",
			valid: false,
		},
		{
			name:  "contains spaces",
			token: "nex_rt_0123456789abcdef 0123456789abcdef0123456789abcdef",
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ValidateRefreshTokenFormat(tc.token)
			if got != tc.valid {
				t.Errorf("ValidateRefreshTokenFormat(%q) = %v; want %v", tc.token, got, tc.valid)
			}
		})
	}
}

func TestParseBearerToken(t *testing.T) {
	validToken, _, _ := auth.GenerateToken()

	tests := []struct {
		name        string
		header      string
		wantToken   string
		expectError bool
	}{
		{
			name:        "valid Bearer token",
			header:      "Bearer " + validToken,
			wantToken:   validToken,
			expectError: false,
		},
		{
			name:        "case-insensitive bearer",
			header:      "bearer " + validToken,
			wantToken:   validToken,
			expectError: false,
		},
		{
			name:        "empty header",
			header:      "",
			expectError: true,
		},
		{
			name:        "missing Bearer scheme",
			header:      validToken,
			expectError: true,
		},
		{
			name:        "Basic auth scheme",
			header:      "Basic YWRtaW46c2VjcmV0",
			expectError: true,
		},
		{
			name:        "empty token",
			header:      "Bearer ",
			expectError: true,
		},
		{
			name:        "invalid token format",
			header:      "Bearer invalid_format_token",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := auth.ParseBearerToken(tc.header)
			if tc.expectError && err == nil {
				t.Errorf("expected error for header %q, got nil", tc.header)
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error for header %q: %v", tc.header, err)
			}
			if !tc.expectError && got != tc.wantToken {
				t.Errorf("got token %q, want %q", got, tc.wantToken)
			}
		})
	}
}
