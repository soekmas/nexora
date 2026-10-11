// Package crm provides customer numbering generators.
package crm

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateCustomerNumber generates a collision-resistant customer identifier
// in the format CUST-YYYYMMDD-XXXXXX using cryptographically secure random entropy.
// Strictly prohibits MAX(customer_no)+1 or serial locks.
func GenerateCustomerNumber(t time.Time) (string, error) {
	if t.IsZero() {
		t = time.Now().UTC()
	} else {
		t = t.UTC()
	}

	datePart := t.Format("20060102")
	const suffixLen = 6
	suffix := make([]byte, suffixLen)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := 0; i < suffixLen; i++ {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto rand failed: %w", err)
		}
		suffix[i] = charset[num.Int64()]
	}

	return fmt.Sprintf("CUST-%s-%s", datePart, string(suffix)), nil
}
