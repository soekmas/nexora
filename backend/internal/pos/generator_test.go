package pos_test

import (
	"regexp"
	"testing"
	"time"

	"example.com/nexora/backend/internal/pos"
)

func TestPOS_Generator_SessionNumberFormat(t *testing.T) {
	fixedTime := time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)
	num, err := pos.GenerateSessionNumber(fixedTime)
	if err != nil {
		t.Fatalf("GenerateSessionNumber failed: %v", err)
	}

	pattern := `^SES-20261011-[0-9A-Z]{6}$`
	match, err := regexp.MatchString(pattern, num)
	if err != nil {
		t.Fatalf("regexp match failed: %v", err)
	}
	if !match {
		t.Errorf("session number %q does not match pattern %q", num, pattern)
	}
}

func TestPOS_Generator_UniquenessAndEntropy(t *testing.T) {
	const iterations = 5000
	seen := make(map[string]struct{}, iterations)
	now := time.Now().UTC()

	for i := 0; i < iterations; i++ {
		num, err := pos.GenerateSessionNumber(now)
		if err != nil {
			t.Fatalf("GenerateSessionNumber iteration %d failed: %v", i, err)
		}
		if _, exists := seen[num]; exists {
			t.Fatalf("duplicate session number generated at iteration %d: %q", i, num)
		}
		seen[num] = struct{}{}
	}
}
