package auth

// Status represents the lifecycle state of an Auth entry.
type Status string

const (
	// StatusActive indicates the auth is valid and ready for execution.
	StatusActive Status = "active"
	// StatusError indicates the auth is temporarily unavailable due to errors.
	StatusError Status = "error"
	// StatusDisabled marks the auth as intentionally disabled.
	StatusDisabled Status = "disabled"
)
