package database

import (
	"errors"
)

// Errors.
var (
	ErrNotFound         = errors.New("database entry not found")
	ErrPermissionDenied = errors.New("access to database record denied")
	ErrReadOnly         = errors.New("database is read only")
	ErrShuttingDown     = errors.New("database system is shutting down")
	ErrNotImplemented   = errors.New("not implemented by this storage")
)
