// Package outbox provides transactional outbox persistence, claiming, and asynchronous event dispatching.
package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status represents the delivery status of an outbox event.
// Must match the CHECK constraint in integration.outbox_event:
// CHECK (status IN ('PENDING','PUBLISHED','FAILED','DEAD_LETTER')).
type Status string

const (
	// StatusPending indicates the event has been written and awaits delivery.
	StatusPending Status = "PENDING"

	// StatusProcessing indicates the event has been claimed by a poller worker and is being dispatched.
	StatusProcessing Status = "PROCESSING"

	// StatusPublished indicates the event was delivered successfully.
	StatusPublished Status = "PUBLISHED"

	// StatusFailed indicates the event experienced a transient delivery error and is eligible for retry.
	StatusFailed Status = "FAILED"

	// StatusDeadLetter indicates the event exceeded maximum retries and will not be polled automatically.
	StatusDeadLetter Status = "DEAD_LETTER"

	// MaxPayloadSize defines the maximum allowed size of an event payload in bytes (1 MiB default).
	MaxPayloadSize = 1024 * 1024
)

var (
	// ErrNoActiveTransaction is returned when an outbox event write is attempted without an active transaction.
	ErrNoActiveTransaction = errors.New("outbox write requires an active transaction context")

	// ErrTenantMismatch is returned when an outbox event tenant ID does not match the transaction tenant context.
	ErrTenantMismatch = errors.New("outbox event tenant_id does not match transaction tenant context")

	// ErrInvalidEvent is returned when an event fails validation.
	ErrInvalidEvent = errors.New("invalid outbox event")

	// ErrPayloadTooLarge is returned when an event payload exceeds MaxPayloadSize.
	ErrPayloadTooLarge = errors.New("event payload exceeds maximum size limit")

	// ErrNoHandlerRegistered is returned when no consumer handler exists for an event type.
	ErrNoHandlerRegistered = errors.New("no handler registered for event type")

	// ErrSensitivePayload is returned when an event payload contains sensitive credentials.
	ErrSensitivePayload = errors.New("event payload contains sensitive credentials or secrets")
)

// Event represents an outbox event corresponding to integration.outbox_event.
type Event struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   uuid.UUID       `json:"aggregate_id"`
	EventType     string          `json:"event_type"`
	EventVersion  int             `json:"event_version"`
	Payload       json.RawMessage `json:"payload"`
	Status        Status          `json:"status"`
	OccurredAt    time.Time       `json:"occurred_at"`
	PublishedAt   *time.Time      `json:"published_at,omitempty"`
	RetryCount    int             `json:"retry_count"`
	LastError     *string         `json:"last_error,omitempty"`
}

// EventEnvelope represents the canonical serialized event envelope adhering to docs/architecture/12-event-catalog.md.
type EventEnvelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	EventVersion  int             `json:"event_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	TenantID      string          `json:"tenant_id"`
	OccurredAt    string          `json:"occurred_at"`
	Status        string          `json:"status"`
	RetryCount    int             `json:"retry_count"`
	Payload       json.RawMessage `json:"payload"`
}

// ToEnvelope converts an internal Event to its canonical serialized envelope.
func (e *Event) ToEnvelope() *EventEnvelope {
	return &EventEnvelope{
		EventID:       e.ID.String(),
		EventType:     e.EventType,
		EventVersion:  e.EventVersion,
		AggregateType: e.AggregateType,
		AggregateID:   e.AggregateID.String(),
		TenantID:      e.TenantID.String(),
		OccurredAt:    e.OccurredAt.UTC().Format(time.RFC3339Nano),
		Status:        string(e.Status),
		RetryCount:    e.RetryCount,
		Payload:       e.Payload,
	}
}

// NewEvent constructs and validates an event ready for transactional outbox insertion.
func NewEvent(
	tenantID uuid.UUID,
	aggregateType string,
	aggregateID uuid.UUID,
	eventType string,
	version int,
	payload any,
) (*Event, error) {
	if version <= 0 {
		version = 1
	}

	var payloadBytes []byte
	switch p := payload.(type) {
	case json.RawMessage:
		payloadBytes = p
	case []byte:
		payloadBytes = p
	default:
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to marshal payload: %v", ErrInvalidEvent, err)
		}
	}

	evt := &Event{
		ID:            uuid.New(),
		TenantID:      tenantID,
		AggregateType: strings.TrimSpace(aggregateType),
		AggregateID:   aggregateID,
		EventType:     strings.TrimSpace(eventType),
		EventVersion:  version,
		Payload:       payloadBytes,
		Status:        StatusPending,
		OccurredAt:    time.Now().UTC(),
		RetryCount:    0,
	}

	if err := Validate(evt); err != nil {
		return nil, err
	}

	return evt, nil
}

// Validate checks that an event satisfies the required schema, boundary, and security constraints.
func Validate(e *Event) error {
	if e == nil {
		return fmt.Errorf("%w: event is nil", ErrInvalidEvent)
	}
	if e.ID == uuid.Nil {
		return fmt.Errorf("%w: event id must not be nil", ErrInvalidEvent)
	}
	if e.TenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant_id must not be nil", ErrInvalidEvent)
	}
	if e.AggregateType == "" || len(e.AggregateType) > 100 {
		return fmt.Errorf("%w: aggregate_type must be non-empty and <= 100 characters", ErrInvalidEvent)
	}
	if e.AggregateID == uuid.Nil {
		return fmt.Errorf("%w: aggregate_id must not be nil", ErrInvalidEvent)
	}
	if e.EventType == "" || len(e.EventType) > 150 {
		return fmt.Errorf("%w: event_type must be non-empty and <= 150 characters", ErrInvalidEvent)
	}
	if e.EventVersion < 1 {
		return fmt.Errorf("%w: event_version must be >= 1", ErrInvalidEvent)
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return fmt.Errorf("%w: payload must be valid non-empty JSON", ErrInvalidEvent)
	}
	if len(e.Payload) > MaxPayloadSize {
		return fmt.Errorf("%w: payload size %d bytes exceeds maximum limit of %d bytes", ErrPayloadTooLarge, len(e.Payload), MaxPayloadSize)
	}

	switch e.Status {
	case StatusPending, StatusProcessing, StatusPublished, StatusFailed, StatusDeadLetter:
	default:
		return fmt.Errorf("%w: unrecognized status %q", ErrInvalidEvent, e.Status)
	}

	// Security: Guard against committing raw credentials or sensitive PII via recursive structural inspection.
	// Note: Structural key validation protects against credential fields in JSON keys (including nested objects,
	// arrays, and Unicode escapes), but cannot inspect unstructured text values. Event constructors and domain
	// aggregates remain authoritative for sanitizing event data prior to serialization.
	var parsed any
	if err := json.Unmarshal(e.Payload, &parsed); err != nil {
		return fmt.Errorf("%w: payload must be valid JSON: %v", ErrInvalidEvent, err)
	}

	if err := inspectSensitiveKeys(parsed); err != nil {
		return err
	}

	return nil
}

var blockedNormalizedKeys = map[string]struct{}{
	"password":      {},
	"clientsecret":  {},
	"accesstoken":   {},
	"refreshtoken":  {},
	"apikey":        {},
	"privatekey":    {},
	"authorization": {},
	"cvv":           {},
	"creditcard":    {},
}

func normalizeKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func inspectSensitiveKeys(node any) error {
	switch val := node.(type) {
	case map[string]any:
		for k, v := range val {
			norm := normalizeKey(k)
			if _, blocked := blockedNormalizedKeys[norm]; blocked {
				return fmt.Errorf("%w: sensitive key %q found in event payload", ErrSensitivePayload, k)
			}
			if err := inspectSensitiveKeys(v); err != nil {
				return err
			}
		}
	case []any:
		for _, elem := range val {
			if err := inspectSensitiveKeys(elem); err != nil {
				return err
			}
		}
	}
	return nil
}
