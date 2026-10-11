package idempotency_test

import (
	"errors"
	"strings"
	"testing"

	"example.com/nexora/backend/internal/platform/idempotency"
)

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		{
			name:    "valid alphanumeric key",
			key:     "pos_sale_12345",
			wantErr: nil,
		},
		{
			name:    "valid uuid key",
			key:     "e7b22a0c-6a10-44ec-b8f2-892797e88383",
			wantErr: nil,
		},
		{
			name:    "valid key with symbols",
			key:     "order:20261010-001@terminal-1",
			wantErr: nil,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: idempotency.ErrKeyTooShort,
		},
		{
			name:    "whitespace only key",
			key:     "   ",
			wantErr: idempotency.ErrKeyTooShort,
		},
		{
			name:    "key with spaces",
			key:     "my key 123",
			wantErr: idempotency.ErrInvalidKeyFormat,
		},
		{
			name:    "key with newline",
			key:     "key\n123",
			wantErr: idempotency.ErrInvalidKeyFormat,
		},
		{
			name:    "key exceeding 128 characters",
			key:     strings.Repeat("a", 129),
			wantErr: idempotency.ErrKeyTooLong,
		},
		{
			name:    "key exactly 128 characters",
			key:     strings.Repeat("a", 128),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := idempotency.ValidateKey(tt.key)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestHashPayload(t *testing.T) {
	t.Run("empty and whitespace payloads yield identical hash", func(t *testing.T) {
		h1 := idempotency.HashPayload(nil)
		h2 := idempotency.HashPayload([]byte(""))
		h3 := idempotency.HashPayload([]byte("   \n\t  "))

		if h1 != h2 || h2 != h3 {
			t.Fatalf("expected identical empty hash, got %q, %q, %q", h1, h2, h3)
		}
	})

	t.Run("insignificant whitespace differences yield identical hash", func(t *testing.T) {
		jsonCompact := []byte(`{"order_id":"123","total":50000}`)
		jsonPretty := []byte("{\n  \"order_id\": \"123\",\n  \"total\": 50000\n}")

		h1, err1 := idempotency.HashJSONPayload(jsonCompact)
		h2, err2 := idempotency.HashJSONPayload(jsonPretty)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v, %v", err1, err2)
		}
		if h1 != h2 {
			t.Fatalf("expected identical hash for compacted vs pretty JSON, got %q vs %q", h1, h2)
		}
	})

	t.Run("reordered top-level object keys yield identical hash", func(t *testing.T) {
		p1 := []byte(`{"total":50000,"order_id":"123","active":true}`)
		p2 := []byte(`{"order_id":"123","active":true,"total":50000}`)

		h1, err1 := idempotency.HashJSONPayload(p1)
		h2, err2 := idempotency.HashJSONPayload(p2)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v, %v", err1, err2)
		}
		if h1 != h2 {
			t.Fatalf("expected identical hash for reordered top-level keys, got %q vs %q", h1, h2)
		}
	})

	t.Run("reordered nested object keys yield identical hash", func(t *testing.T) {
		p1 := []byte(`{"order":{"customer":{"name":"Alice","id":"c-1"},"id":"ord-1"}}`)
		p2 := []byte(`{"order":{"id":"ord-1","customer":{"id":"c-1","name":"Alice"}}}`)

		h1, err1 := idempotency.HashJSONPayload(p1)
		h2, err2 := idempotency.HashJSONPayload(p2)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v, %v", err1, err2)
		}
		if h1 != h2 {
			t.Fatalf("expected identical hash for reordered nested keys, got %q vs %q", h1, h2)
		}
	})

	t.Run("arrays with different element order yield distinct hash", func(t *testing.T) {
		p1 := []byte(`{"items":["apple","banana"]}`)
		p2 := []byte(`{"items":["banana","apple"]}`)

		h1, err1 := idempotency.HashJSONPayload(p1)
		h2, err2 := idempotency.HashJSONPayload(p2)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v, %v", err1, err2)
		}
		if h1 == h2 {
			t.Fatalf("expected different hash for arrays with different order, got identical %q", h1)
		}
	})

	t.Run("numeric forms yield identical canonical hash", func(t *testing.T) {
		p1 := []byte(`{"amount":100}`)
		p2 := []byte(`{"amount":100.0}`)
		p3 := []byte(`{"amount":1e2}`)

		h1, err1 := idempotency.HashJSONPayload(p1)
		h2, err2 := idempotency.HashJSONPayload(p2)
		h3, err3 := idempotency.HashJSONPayload(p3)

		if err1 != nil || err2 != nil || err3 != nil {
			t.Fatalf("unexpected errors: %v, %v, %v", err1, err2, err3)
		}
		if h1 != h2 || h2 != h3 {
			t.Fatalf("expected identical hash for numeric equivalences 100, 100.0, 1e2, got %q, %q, %q", h1, h2, h3)
		}
	})

	t.Run("escaped strings and unicode yield identical hash", func(t *testing.T) {
		p1 := []byte(`{"msg":"caf\u00e9"}`)
		p2 := []byte(`{"msg":"café"}`)

		h1, err1 := idempotency.HashJSONPayload(p1)
		h2, err2 := idempotency.HashJSONPayload(p2)

		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error: %v, %v", err1, err2)
		}
		if h1 != h2 {
			t.Fatalf("expected identical hash for unicode vs escaped string, got %q vs %q", h1, h2)
		}
	})

	t.Run("invalid json returns ErrMalformedJSON", func(t *testing.T) {
		invalidPayloads := [][]byte{
			[]byte(`{"order_id": 123,`),
			[]byte(`{unquoted_key: "val"}`),
			[]byte(`{"num": 12.34.56}`),
			[]byte(`{"trailing": true,}`),
		}

		for _, p := range invalidPayloads {
			_, err := idempotency.HashJSONPayload(p)
			if !errors.Is(err, idempotency.ErrMalformedJSON) {
				t.Fatalf("expected ErrMalformedJSON for %q, got %v", string(p), err)
			}
		}
	})

	t.Run("hash request payload handles content types correctly", func(t *testing.T) {
		// Valid JSON with application/json
		h1, err1 := idempotency.HashRequestPayload("application/json; charset=utf-8", []byte(`{"b":2,"a":1}`))
		if err1 != nil {
			t.Fatalf("unexpected error: %v", err1)
		}
		h2, err2 := idempotency.HashRequestPayload("application/json", []byte(`{"a":1,"b":2}`))
		if err2 != nil {
			t.Fatalf("unexpected error: %v", err2)
		}
		if h1 != h2 {
			t.Fatalf("expected identical hash for JSON with different media types, got %q vs %q", h1, h2)
		}

		// Invalid JSON with application/json fails
		_, err3 := idempotency.HashRequestPayload("application/json", []byte(`{invalid}`))
		if !errors.Is(err3, idempotency.ErrMalformedJSON) {
			t.Fatalf("expected ErrMalformedJSON for invalid JSON, got %v", err3)
		}

		// Raw text with text/plain succeeds
		rawText := []byte("this is raw unformatted text")
		hRaw, errRaw := idempotency.HashRequestPayload("text/plain", rawText)
		if errRaw != nil {
			t.Fatalf("unexpected error for text/plain: %v", errRaw)
		}
		if hRaw == "" {
			t.Fatalf("expected non-empty raw hash")
		}
	})
}

func TestNormalizeOperation(t *testing.T) {
	op := idempotency.NormalizeOperation("post", " /api/v1/pos/orders ")
	if op != "POST /api/v1/pos/orders" {
		t.Fatalf("expected 'POST /api/v1/pos/orders', got %q", op)
	}
}
