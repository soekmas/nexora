package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Constants for audit metadata sanitization.
const (
	// MaxMetadataSize is the maximum permitted size for an audit metadata JSON payload (256 KiB).
	MaxMetadataSize = 256 * 1024

	// RedactedPlaceholder is the string substituted for scrubbed sensitive credential values.
	RedactedPlaceholder = "[REDACTED]"
)

// blockedKeyPatterns contains normalized substring or suffix patterns that identify sensitive credential keys.
var blockedKeyPatterns = []string{
	"password",
	"passwd",
	"secret",
	"clientsecret",
	"token",
	"accesstoken",
	"refreshtoken",
	"sessiontoken",
	"authheader",
	"authorization",
	"apikey",
	"privatekey",
	"mfacode",
	"totp",
	"cvv",
	"cvc",
	"pan",
	"cardnumber",
	"creditcard",
}

// normalizeKey strips non-alphanumeric runes and converts to lowercase.
func normalizeKey(k string) string {
	var sb strings.Builder
	for _, r := range k {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		}
	}
	return sb.String()
}

// isSensitiveKey checks whether a normalized property key matches any blocked sensitive credential pattern.
func isSensitiveKey(k string) bool {
	norm := normalizeKey(k)
	if norm == "" {
		return false
	}
	for _, pattern := range blockedKeyPatterns {
		if strings.Contains(norm, pattern) {
			return true
		}
	}
	return false
}

// ScrubMetadata recursively inspects a JSON payload and replaces sensitive values with "[REDACTED]".
// If raw is empty or whitespace, it returns a canonical empty JSON object "{}" without error.
// If raw exceeds MaxMetadataSize, it returns ErrMetadataTooLarge.
// If raw is not valid JSON, it returns ErrInvalidMetadata.
//
// NOTE: Structural JSON filtering removes credentials under recognized property names, but cannot
// automatically detect every secret embedded in arbitrary free-form text or unrecognized keys.
// Upstream callers remain responsible for ensuring safe audit metadata is provided.
func ScrubMetadata(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}

	if len(trimmed) > MaxMetadataSize {
		return nil, fmt.Errorf("%w: size %d exceeds %d bytes", ErrMetadataTooLarge, len(trimmed), MaxMetadataSize)
	}

	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMetadata, err.Error())
	}

	scrubbed := scrubValue(parsed)

	out, err := json.Marshal(scrubbed)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal failure", ErrInvalidMetadata)
	}

	if len(out) > MaxMetadataSize {
		return nil, fmt.Errorf("%w: scrubbed size %d exceeds %d bytes", ErrMetadataTooLarge, len(out), MaxMetadataSize)
	}

	return out, nil
}

func scrubValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		scrubbedMap := make(map[string]any, len(v))
		for k, child := range v {
			if isSensitiveKey(k) {
				scrubbedMap[k] = RedactedPlaceholder
			} else {
				scrubbedMap[k] = scrubValue(child)
			}
		}
		return scrubbedMap
	case []any:
		scrubbedSlice := make([]any, len(v))
		for i, child := range v {
			scrubbedSlice[i] = scrubValue(child)
		}
		return scrubbedSlice
	default:
		return v
	}
}

// DetectSensitiveKeys checks whether any unredacted sensitive keys exist in the JSON payload.
// Used for validation in strict mode or unit testing.
func DetectSensitiveKeys(raw []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMetadata, err.Error())
	}

	var detected []string
	walkForSensitive(parsed, &detected)
	return detected, nil
}

func walkForSensitive(val any, detected *[]string) {
	switch v := val.(type) {
	case map[string]any:
		for k, child := range v {
			if isSensitiveKey(k) {
				// If not already redacted, note it
				strVal, isStr := child.(string)
				if !isStr || strVal != RedactedPlaceholder {
					*detected = append(*detected, k)
				}
			}
			walkForSensitive(child, detected)
		}
	case []any:
		for _, child := range v {
			walkForSensitive(child, detected)
		}
	}
}
