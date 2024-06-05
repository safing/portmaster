package osdetail

import "errors"

var (
	// ErrNotSupported is returned when an operation is not supported on the current platform.
	ErrNotSupported = errors.New("not supported")
	// ErrNotFound is returned when the desired data is not found.
	ErrNotFound = errors.New("not found")
	// ErrEmptyOutput is a special error that is returned when an operation has no error, but also returns to data.
	ErrEmptyOutput = errors.New("command succeeded with empty output")
)
