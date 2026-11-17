package storage

import "errors"

// Errors for storages.
var (
	ErrNotFound        = errors.New("storage entry not found")
	ErrRecordMalformed = errors.New("record is malformed")
	// ErrBatchAborted is reported by an AbortableBatcher for a batch that was
	// aborted and thus not stored.
	ErrBatchAborted = errors.New("batch aborted")
)
