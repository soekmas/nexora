// Package pos provides session numbering generator.
package pos

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

const sessionCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateSessionNumber generates a collision-resistant session identifier
// in the format SES-YYYYMMDD-XXXXXX using cryptographically secure random entropy.
// Strictly conforms to the architecture rule prohibiting MAX(number)+1 or serial locks.
func GenerateSessionNumber(t time.Time) (string, error) {
	if t.IsZero() {
		t = time.Now().UTC()
	} else {
		t = t.UTC()
	}

	datePart := t.Format("20060102")
	const suffixLen = 6
	suffix := make([]byte, suffixLen)
	charsetLen := big.NewInt(int64(len(sessionCharset)))

	for i := 0; i < suffixLen; i++ {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto rand failed: %w", err)
		}
		suffix[i] = sessionCharset[num.Int64()]
	}

	return fmt.Sprintf("SES-%s-%s", datePart, string(suffix)), nil
}
