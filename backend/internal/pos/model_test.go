package pos_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/pos"
)

func TestPOS_Model_IsNonTerminalStatus(t *testing.T) {
	tests := []struct {
		status   string
		expected bool
	}{
		{pos.SessionStatusOpening, true},
		{pos.SessionStatusOpen, true},
		{pos.SessionStatusActive, true},
		{pos.SessionStatusClosing, true},
		{pos.SessionStatusVarianceReview, true},
		{pos.SessionStatusClosed, false},
		{"UNKNOWN", false},
		{"", false},
	}

	for _, tc := range tests {
		got := pos.IsNonTerminalStatus(tc.status)
		if got != tc.expected {
			t.Errorf("IsNonTerminalStatus(%q) = %v; want %v", tc.status, got, tc.expected)
		}
	}
}

func TestPOS_Model_MaskBlindCountFields(t *testing.T) {
	expectedCash := 150000.0
	actualCash := 140000.0
	variance := -10000.0
	sess := &pos.Session{
		ID:           uuid.New(),
		TerminalID:   uuid.New(),
		Status:       pos.SessionStatusOpen,
		OpeningCash:  100000.0,
		ExpectedCash: &expectedCash,
		ClosingCash:  &actualCash,
		ActualCash:   &actualCash,
		Variance:     &variance,
		OpenedAt:     time.Now().UTC(),
	}

	masked := sess.MaskBlindCountFields()
	if masked == nil {
		t.Fatal("expected non-nil masked session")
	}

	if masked.ExpectedCash != nil {
		t.Errorf("expected ExpectedCash to be nil, got %v", *masked.ExpectedCash)
	}
	if masked.ClosingCash != nil {
		t.Errorf("expected ClosingCash to be nil, got %v", *masked.ClosingCash)
	}
	if masked.ActualCash != nil {
		t.Errorf("expected ActualCash to be nil, got %v", *masked.ActualCash)
	}
	if masked.Variance != nil {
		t.Errorf("expected Variance to be nil, got %v", *masked.Variance)
	}

	// Unmasked fields must remain intact
	if masked.ID != sess.ID {
		t.Errorf("expected ID %s, got %s", sess.ID, masked.ID)
	}
	if masked.OpeningCash != sess.OpeningCash {
		t.Errorf("expected OpeningCash %f, got %f", sess.OpeningCash, masked.OpeningCash)
	}

	// Nil safety
	var nilSess *pos.Session
	if nilSess.MaskBlindCountFields() != nil {
		t.Error("expected nil on nil session")
	}
}
