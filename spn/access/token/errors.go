package token

import "errors"

// Errors.
var (
	ErrEmpty          = errors.New("token storage is empty")
	ErrNoZone         = errors.New("no zone specified")
	ErrTokenInvalid   = errors.New("token is invalid")
	ErrTokenMalformed = errors.New("token malformed")
	ErrTokenUsed      = errors.New("token already used")
	ErrZoneMismatch   = errors.New("zone mismatch")
	ErrZoneTaken      = errors.New("zone taken")
	ErrZoneUnknown    = errors.New("zone unknown")
)
