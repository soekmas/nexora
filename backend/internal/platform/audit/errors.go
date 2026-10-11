package audit

import "errors"

// Standard audit platform errors.
var (
	// ErrNoActiveTransaction is returned when an audit operation requiring an active transaction
	// is invoked without one present in the context.
	ErrNoActiveTransaction = errors.New("audit: active transaction required in context")

	// ErrInvalidAction is returned when an action code is not recognized in the approved audit action catalog.
	ErrInvalidAction = errors.New("audit: action code not recognized in approved audit catalog")

	// ErrReasonRequired is returned when an audit action mandates a reason code/text but none was provided.
	ErrReasonRequired = errors.New("audit: reason is required for this audit action")

	// ErrMissingTenantContext is returned when tenant identity is missing from context or audit log.
	ErrMissingTenantContext = errors.New("audit: missing tenant context")

	// ErrTenantMismatch is returned when the audit log tenant ID does not match the active ambient context.
	ErrTenantMismatch = errors.New("audit: tenant ID mismatch with security context")

	// ErrInvalidStatus is returned when an audit status is not SUCCESS, FAILED, or DENIED.
	ErrInvalidStatus = errors.New("audit: status must be SUCCESS, FAILED, or DENIED")

	// ErrInvalidSeverity is returned when a security event severity is not valid.
	ErrInvalidSeverity = errors.New("audit: invalid severity level")

	// ErrInvalidMetadata is returned when metadata is not valid JSON.
	ErrInvalidMetadata = errors.New("audit: metadata must be valid JSON")

	// ErrMetadataTooLarge is returned when metadata exceeds MaxMetadataSize.
	ErrMetadataTooLarge = errors.New("audit: metadata size exceeds maximum permitted limit")

	// ErrSensitiveDataDetected is returned when unredacted sensitive credential properties are detected.
	ErrSensitiveDataDetected = errors.New("audit: sensitive credential property detected in audit metadata")

	// ErrInvalidPagination is returned when query pagination parameters are invalid.
	ErrInvalidPagination = errors.New("audit: invalid pagination parameters")

	// ErrRecordNotFound is returned when a requested audit record does not exist.
	ErrRecordNotFound = errors.New("audit: record not found")

	// ErrFieldLengthExceeded is returned when a string field exceeds maximum database column length.
	ErrFieldLengthExceeded = errors.New("audit: field length exceeds maximum allowed")
)
