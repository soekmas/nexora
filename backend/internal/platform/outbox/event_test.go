package outbox_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/outbox"
)

func TestEvent_ValidationAndEnvelope(t *testing.T) {
	tenantID := uuid.New()
	aggregateID := uuid.New()

	t.Run("valid event constructs and validates successfully", func(t *testing.T) {
		payload := map[string]any{"order_id": "ord-123", "amount": 50000}
		evt, err := outbox.NewEvent(tenantID, "sales_order", aggregateID, "SalesOrderCompleted", 1, payload)
		if err != nil {
			t.Fatalf("unexpected error creating event: %v", err)
		}
		if evt.ID == uuid.Nil {
			t.Fatalf("expected non-nil event ID")
		}
		if evt.Status != outbox.StatusPending {
			t.Fatalf("expected initial status PENDING, got %s", evt.Status)
		}
		if evt.RetryCount != 0 {
			t.Fatalf("expected initial retry count 0, got %d", evt.RetryCount)
		}

		env := evt.ToEnvelope()
		if env.EventID != evt.ID.String() {
			t.Fatalf("expected envelope event_id %s, got %s", evt.ID, env.EventID)
		}
		if env.EventType != "SalesOrderCompleted" {
			t.Fatalf("expected event_type SalesOrderCompleted, got %s", env.EventType)
		}
		if env.TenantID != tenantID.String() {
			t.Fatalf("expected tenant_id %s, got %s", tenantID, env.TenantID)
		}
	})

	t.Run("missing tenant id fails validation", func(t *testing.T) {
		_, err := outbox.NewEvent(uuid.Nil, "sales_order", aggregateID, "SalesOrderCompleted", 1, map[string]any{"a": 1})
		if !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
	})

	t.Run("missing aggregate type fails validation", func(t *testing.T) {
		_, err := outbox.NewEvent(tenantID, "", aggregateID, "SalesOrderCompleted", 1, map[string]any{"a": 1})
		if !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
	})

	t.Run("missing aggregate id fails validation", func(t *testing.T) {
		_, err := outbox.NewEvent(tenantID, "sales_order", uuid.Nil, "SalesOrderCompleted", 1, map[string]any{"a": 1})
		if !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
	})

	t.Run("missing event type fails validation", func(t *testing.T) {
		_, err := outbox.NewEvent(tenantID, "sales_order", aggregateID, "", 1, map[string]any{"a": 1})
		if !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent, got %v", err)
		}
	})

	t.Run("invalid version fails validation", func(t *testing.T) {
		evt := &outbox.Event{
			ID:            uuid.New(),
			TenantID:      tenantID,
			AggregateType: "sales_order",
			AggregateID:   aggregateID,
			EventType:     "TestEvent",
			EventVersion:  0,
			Payload:       json.RawMessage(`{"ok":true}`),
			Status:        outbox.StatusPending,
			OccurredAt:    time.Now(),
		}
		if err := outbox.Validate(evt); !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent for version 0, got %v", err)
		}
	})

	t.Run("empty or invalid JSON payload fails validation", func(t *testing.T) {
		evt := &outbox.Event{
			ID:            uuid.New(),
			TenantID:      tenantID,
			AggregateType: "sales_order",
			AggregateID:   aggregateID,
			EventType:     "TestEvent",
			EventVersion:  1,
			Payload:       json.RawMessage(`{invalid_json}`),
			Status:        outbox.StatusPending,
			OccurredAt:    time.Now(),
		}
		if err := outbox.Validate(evt); !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent for malformed JSON, got %v", err)
		}
	})

	t.Run("sensitive credentials in payload rejected", func(t *testing.T) {
		badPayloads := []map[string]any{
			{"user": "alice", "password": "supersecretpassword"},
			{"client_id": "c1", "client_secret": "secret123"},
			{"client_id": "c2", "clientSecret": "secret123"},
			{"client_id": "c3", "client-secret": "secret123"},
			{"token": "tok", "access_token": "bearer xyz"},
			{"token": "tok2", "accessToken": "bearer xyz"},
			{"token": "tok3", "refresh_token": "bearer abc"},
			{"token": "tok4", "refreshToken": "bearer abc"},
			{"card": "41111111", "cvv": "123"},
			{"service": "svc", "api_key": "key123"},
			{"service": "svc2", "apiKey": "key123"},
			{"crypto": "rsa", "private_key": "privkey"},
			{"crypto": "rsa2", "privateKey": "privkey"},
			{"header": "Bearer", "authorization": "Bearer xyz"},
		}

		for _, p := range badPayloads {
			_, err := outbox.NewEvent(tenantID, "user", aggregateID, "UserRegistered", 1, p)
			if !errors.Is(err, outbox.ErrSensitivePayload) {
				t.Fatalf("expected ErrSensitivePayload for payload %+v, got %v", p, err)
			}
		}
	})

	t.Run("nested sensitive keys and arrays rejected", func(t *testing.T) {
		nestedPayloads := []string{
			`{"details": {"auth": {"password": "secret"}}}`,
			`{"items": [{"service": "gateway", "api_key": "sec"}]}`,
			`[{"payment": {"card": {"cvv": "123"}}}]`,
			`{"account": {"keys": [{"private_key": "-----BEGIN..."}]}}`,
		}

		for _, raw := range nestedPayloads {
			_, err := outbox.NewEvent(tenantID, "user", aggregateID, "UserUpdated", 1, json.RawMessage(raw))
			if !errors.Is(err, outbox.ErrSensitivePayload) {
				t.Fatalf("expected ErrSensitivePayload for nested payload %s, got %v", raw, err)
			}
		}
	})

	t.Run("unicode escaped sensitive keys rejected", func(t *testing.T) {
		escapedPayloads := []string{
			`{"\u0070\u0061\u0073\u0073\u0077\u006f\u0072\u0064": "secret"}`, // \u0070... = "password"
			`{"\u0061\u0070\u0069\u005f\u006b\u0065\u0079": "key123"}`,       // \u0061... = "api_key"
			`{"\u0063\u0076\u0076": "456"}`,                                  // \u0063... = "cvv"
		}

		for _, raw := range escapedPayloads {
			_, err := outbox.NewEvent(tenantID, "user", aggregateID, "UserUpdated", 1, json.RawMessage(raw))
			if !errors.Is(err, outbox.ErrSensitivePayload) {
				t.Fatalf("expected ErrSensitivePayload for unicode-escaped payload %s, got %v", raw, err)
			}
		}
	})

	t.Run("harmless text values containing sensitive words are accepted", func(t *testing.T) {
		harmlessPayloads := []map[string]any{
			{"note": "User changed their password yesterday at 2pm"},
			{"description": "Requires manager authorization for discount"},
			{"comment": "API key was rotated successfully per security policy"},
			{"title": "CVV security guidelines document"},
		}

		for _, p := range harmlessPayloads {
			evt, err := outbox.NewEvent(tenantID, "audit", aggregateID, "AuditNoteAdded", 1, p)
			if err != nil {
				t.Fatalf("harmless payload unexpectedly rejected: %v for payload %+v", err, p)
			}
			if evt == nil {
				t.Fatal("expected non-nil event")
			}
		}
	})

	t.Run("payload size limit enforcement", func(t *testing.T) {
		// Valid payload below limit
		smallPayload := map[string]any{"data": strings.Repeat("x", 1000)}
		_, err := outbox.NewEvent(tenantID, "item", aggregateID, "ItemUpdated", 1, smallPayload)
		if err != nil {
			t.Fatalf("small payload failed: %v", err)
		}

		// Payload exactly at 1 MiB limit
		// Construct valid JSON: {"pad":"..."}
		const prefix = `{"pad":"`
		const suffix = `"}`
		padLen := outbox.MaxPayloadSize - len(prefix) - len(suffix)
		exactPayloadBytes := []byte(prefix + strings.Repeat("a", padLen) + suffix)
		if len(exactPayloadBytes) != outbox.MaxPayloadSize {
			t.Fatalf("expected exact payload length %d, got %d", outbox.MaxPayloadSize, len(exactPayloadBytes))
		}
		evtExact, err := outbox.NewEvent(tenantID, "item", aggregateID, "ItemUpdated", 1, json.RawMessage(exactPayloadBytes))
		if err != nil {
			t.Fatalf("exact 1 MiB payload unexpectedly rejected: %v", err)
		}
		if evtExact == nil {
			t.Fatal("expected non-nil event for exact payload")
		}

		// Payload exceeding 1 MiB limit by 1 byte
		oversizedExact := []byte(prefix + strings.Repeat("a", padLen+1) + suffix)
		_, err = outbox.NewEvent(tenantID, "item", aggregateID, "ItemUpdated", 1, json.RawMessage(oversizedExact))
		if !errors.Is(err, outbox.ErrPayloadTooLarge) {
			t.Fatalf("expected ErrPayloadTooLarge for %d bytes, got %v", len(oversizedExact), err)
		}
	})

	t.Run("aggregate type exceeding 100 characters fails validation", func(t *testing.T) {
		longAgg := strings.Repeat("a", 101)
		_, err := outbox.NewEvent(tenantID, longAgg, aggregateID, "TestEvent", 1, map[string]any{"ok": true})
		if !errors.Is(err, outbox.ErrInvalidEvent) {
			t.Fatalf("expected ErrInvalidEvent for long aggregate_type, got %v", err)
		}
	})
}
