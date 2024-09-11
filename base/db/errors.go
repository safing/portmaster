package db

import (
	"errors"
)

// Errors.
var (
	ErrNotFound         = errors.New("database entry not found")
	ErrPermissionDenied = errors.New("access to database record denied")
	ErrReadOnly         = errors.New("database is read only")
	ErrInvalidRecord    = errors.New("invalid database record")
	ErrShuttingDown     = errors.New("database system is shutting down")
	ErrStopped          = errors.New("database is stopped")
	ErrTimeout          = errors.New("timed out")
	ErrCanceled         = errors.New("canceled")
	ErrNotImplemented   = errors.New("not implemented by this storage")
)
