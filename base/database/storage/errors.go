package storage

import "errors"

// Errors for storages.
var (
	ErrNotFound        = errors.New("storage entry not found")
	ErrRecordMalformed = errors.New("record is malformed")
)
