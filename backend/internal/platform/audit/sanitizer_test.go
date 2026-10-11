package audit_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"example.com/nexora/backend/internal/platform/audit"
)

func TestSanitizer_ScrubSensitiveKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]any
	}{
		{
			name: "plain password scrubbed",
			input: `{
				"username": "cashier1",
				"password": "supersecretpassword",
				"store_id": "123"
			}`,
			expected: map[string]any{
				"username": "cashier1",
				"password": audit.RedactedPlaceholder,
				"store_id": "123",
			},
		},
		{
			name: "token and secret scrubbed",
			input: `{
				"api_key": "key_12345",
				"client_secret": "sec_99999",
				"refresh_token": "rt_abcdef",
				"access_token": "at_ghijkl",
				"status": "active"
			}`,
			expected: map[string]any{
				"api_key":       audit.RedactedPlaceholder,
				"client_secret": audit.RedactedPlaceholder,
				"refresh_token": audit.RedactedPlaceholder,
				"access_token":  audit.RedactedPlaceholder,
				"status":        "active",
			},
		},
		{
			name: "payment credentials scrubbed",
			input: `{
				"pan": "4111111111111111",
				"card_number": "5500000000000004",
				"cvv": "123",
				"cvc": "999",
				"amount": 50000
			}`,
			expected: map[string]any{
				"pan":         audit.RedactedPlaceholder,
				"card_number": audit.RedactedPlaceholder,
				"cvv":         audit.RedactedPlaceholder,
				"cvc":         audit.RedactedPlaceholder,
				"amount":      json.Number("50000"),
			},
		},
		{
			name: "nested objects and arrays",
			input: `{
				"user": {
					"id": "u-1",
					"password_hash": "hash_val",
					"mfa": {
						"totp_secret": "JBSWY3DPEHPK3PXP"
					}
				},
				"items": [
					{"sku": "SKU-1", "auth_header": "Bearer secret"}
				]
			}`,
			expected: map[string]any{
				"user": map[string]any{
					"id":            "u-1",
					"password_hash": audit.RedactedPlaceholder,
					"mfa": map[string]any{
						"totp_secret": audit.RedactedPlaceholder,
					},
				},
				"items": []any{
					map[string]any{
						"sku":         "SKU-1",
						"auth_header": audit.RedactedPlaceholder,
					},
				},
			},
		},
		{
			name: "unicode escaped keys",
			input: `{
				"\u0070\u0061\u0073\u0073\u0077\u006f\u0072\u0064": "mySecret",
				"safe_field": "safe_value"
			}`,
			expected: map[string]any{
				"password":   audit.RedactedPlaceholder,
				"safe_field": "safe_value",
			},
		},
		{
			name: "safe values containing sensitive words are not scrubbed",
			input: `{
				"notes": "User forgot password at counter, reset was initiated",
				"description": "Card payment token was verified by terminal"
			}`,
			expected: map[string]any{
				"notes":       "User forgot password at counter, reset was initiated",
				"description": "Card payment token was verified by terminal",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scrubbed, err := audit.ScrubMetadata([]byte(tc.input))
			if err != nil {
				t.Fatalf("unexpected error scrubbing metadata: %v", err)
			}

			var result map[string]any
			dec := json.NewDecoder(bytes.NewReader(scrubbed))
			dec.UseNumber()
			if err := dec.Decode(&result); err != nil {
				t.Fatalf("failed to decode scrubbed output: %v", err)
			}

			expectedJSON, _ := json.Marshal(tc.expected)
			resultJSON, _ := json.Marshal(result)

			if string(expectedJSON) != string(resultJSON) {
				t.Errorf("\nexpected: %s\ngot:      %s", string(expectedJSON), string(resultJSON))
			}
		})
	}
}

func TestSanitizer_EmptyAndInvalidJSON(t *testing.T) {
	// Empty or whitespace returns "{}"
	res, err := audit.ScrubMetadata(nil)
	if err != nil || string(res) != "{}" {
		t.Errorf("expected {}, got %s, err: %v", string(res), err)
	}

	res, err = audit.ScrubMetadata([]byte("   \n\t  "))
	if err != nil || string(res) != "{}" {
		t.Errorf("expected {}, got %s, err: %v", string(res), err)
	}

	// Malformed JSON returns ErrInvalidMetadata
	_, err = audit.ScrubMetadata([]byte("{not_json"))
	if !errors.Is(err, audit.ErrInvalidMetadata) {
		t.Errorf("expected ErrInvalidMetadata, got %v", err)
	}
}

func TestSanitizer_PayloadSizeLimit(t *testing.T) {
	// Payload slightly over MaxMetadataSize (256 KiB)
	largeStr := strings.Repeat("A", audit.MaxMetadataSize+10)
	payload := `{"data":"` + largeStr + `"}`

	_, err := audit.ScrubMetadata([]byte(payload))
	if !errors.Is(err, audit.ErrMetadataTooLarge) {
		t.Errorf("expected ErrMetadataTooLarge, got %v", err)
	}
}

func TestSanitizer_DetectSensitiveKeys(t *testing.T) {
	raw := []byte(`{
		"safe": "hello",
		"password": "secret_value",
		"nested": {
			"api_key": "key_abc"
		}
	}`)

	keys, err := audit.DetectSensitiveKeys(raw)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 detected sensitive keys, got %d (%v)", len(keys), keys)
	}
}
