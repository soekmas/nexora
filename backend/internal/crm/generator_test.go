package crm

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateCustomerNumber_Format(t *testing.T) {
	fixedTime := time.Date(2026, 10, 11, 15, 30, 0, 0, time.UTC)
	no, err := GenerateCustomerNumber(fixedTime)
	if err != nil {
		t.Fatalf("GenerateCustomerNumber() unexpected error: %v", err)
	}

	if !strings.HasPrefix(no, "CUST-20261011-") {
		t.Errorf("expected prefix CUST-20261011-, got %q", no)
	}

	parts := strings.Split(no, "-")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts separated by hyphen, got %d in %q", len(parts), no)
	}

	if len(parts[2]) != 6 {
		t.Errorf("expected suffix length 6, got %d in %q", len(parts[2]), parts[2])
	}
}

func TestGenerateCustomerNumber_Uniqueness(t *testing.T) {
	const iterations = 1000
	seen := make(map[string]bool, iterations)
	now := time.Now().UTC()

	for i := 0; i < iterations; i++ {
		no, err := GenerateCustomerNumber(now)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if seen[no] {
			t.Fatalf("collision detected on iteration %d: %q", i, no)
		}
		seen[no] = true
	}
}
