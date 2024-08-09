package terminal

import (
	"context"
	"errors"
	"fmt"

	"github.com/safing/structures/varint"
)

// Error is a terminal error.
type Error struct {
	// id holds the internal error ID.
	id uint8
	// external signifies if the error was received from the outside.
	external bool
	// err holds the wrapped error or the default error message.
	err error
}

// ID returns the internal ID of the error.
func (e *Error) ID() uint8 {
	return e.id
}

// Error returns the human readable format of the error.
func (e *Error) Error() string {
	if e.external {
		return "[ext] " + e.err.Error()
	}
	return e.err.Error()
}

// IsExternal returns whether the error occurred externally.
func (e *Error) IsExternal() bool {
	if e == nil {
		return false
	}

	return e.external
}

// Is returns whether the given error is of the same type.
func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}

	t, ok := target.(*Error) //nolint:errorlint // Error implementation, not usage.
	if !ok {
		return false
	}
	return e.id == t.id
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error {
	if e == nil || e.err == nil {
		return nil
	}
	return e.err
}

// With adds context and details where the error occurred. The provided
// message is appended to the error.
// A new error with the same ID is returned and must be compared with
// errors.Is().
func (e *Error) With(format string, a ...interface{}) *Error {
	// Return nil if error is nil.
	if e == nil {
		return nil
	}

	return &Error{
		id:  e.id,
		err: fmt.Errorf(e.Error()+": "+format, a...),
	}
}

// Wrap adds context higher up in the call chain. The provided message is
// prepended to the error.
// A new error with the same ID is returned and must be compared with
// errors.Is().
func (e *Error) Wrap(format string, a ...interface{}) *Error {
	// Return nil if error is nil.
	if e == nil {
		return nil
	}

	return &Error{
		id:  e.id,
		err: fmt.Errorf(format+": "+e.Error(), a...),
	}
}

// AsExternal creates and returns an external version of the error.
func (e *Error) AsExternal() *Error {
	// Return nil if error is nil.
	if e == nil {
		return nil
	}

	return &Error{
		id:       e.id,
		err:      e.err,
		external: true,
	}
}

// Pack returns the serialized internal error ID. The additional message is
// lost and is replaced with the default message upon parsing.
func (e *Error) Pack() []byte {
	// Return nil slice if error is nil.
	if e == nil {
		return nil
	}

	return varint.Pack8(e.id)
}

// ParseExternalError parses an external error.
func ParseExternalError(id []byte) (*Error, error) {
	// Return nil for an empty error.
	if len(id) == 0 {
		return ErrStopping.AsExternal(), nil
	}

	parsedID, _, err := varint.Unpack8(id)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack error ID: %w", err)
	}

	return NewExternalError(parsedID), nil
}

// NewExternalError creates an external error based on the given ID.
func NewExternalError(id uint8) *Error {
	err, ok := errorRegistry[id]
	if ok {
		return err.AsExternal()
	}

	return ErrUnknownError.AsExternal()
}

var errorRegistry = make(map[uint8]*Error)

func registerError(id uint8, err error) *Error {
	// Check for duplicate.
	_, ok := errorRegistry[id]
	if ok {
		panic(fmt.Sprintf("error with id %d already registered", id))
	}

	newErr := &Error{
		id:  id,
		err: err,
	}

	errorRegistry[id] = newErr
	return newErr
}

// func (e *Error) IsSpecial() bool {
// 	if e == nil {
// 		return false
// 	}
// 	return e.id > 0 && e.id < 8
// }

// IsOK returns if the error represents a "OK" or success status.
func (e *Error) IsOK() bool {
	return !e.IsError()
}

// IsError returns if the error represents an erronous condition.
func (e *Error) IsError() bool {
	if e == nil || e.err == nil {
		return false
	}
	if e.id == 0 || e.id >= 8 {
		return true
	}
	return false
}

// Terminal Errors.
var (
	// ErrUnknownError is the default error.
	ErrUnknownError = registerError(0, errors.New("unknown error"))

	// Error IDs 1-7 are reserved for special "OK" values.

	ErrStopping    = registerError(2, errors.New("stopping"))
	ErrExplicitAck = registerError(3, errors.New("explicit ack"))
	ErrNoActivity  = registerError(4, errors.New("no activity"))

	// Errors IDs 8 and up are for regular errors.

	ErrInternalError          = registerError(8, errors.New("internal error"))
	ErrMalformedData          = registerError(9, errors.New("malformed data"))
	ErrUnexpectedMsgType      = registerError(10, errors.New("unexpected message type"))
	ErrUnknownOperationType   = registerError(11, errors.New("unknown operation type"))
	ErrUnknownOperationID     = registerError(12, errors.New("unknown operation id"))
	ErrPermissionDenied       = registerError(13, errors.New("permission denied"))
	ErrIntegrity              = registerError(14, errors.New("integrity violated"))
	ErrInvalidOptions         = registerError(15, errors.New("invalid options"))
	ErrHubNotReady            = registerError(16, errors.New("hub not ready"))
	ErrRateLimited            = registerError(24, errors.New("rate limited"))
	ErrIncorrectUsage         = registerError(22, errors.New("incorrect usage"))
	ErrTimeout                = registerError(62, errors.New("timed out"))
	ErrUnsupportedVersion     = registerError(93, errors.New("unsupported version"))
	ErrHubUnavailable         = registerError(101, errors.New("hub unavailable"))
	ErrAbandonedTerminal      = registerError(102, errors.New("terminal is being abandoned"))
	ErrShipSunk               = registerError(108, errors.New("ship sunk"))
	ErrDestinationUnavailable = registerError(113, errors.New("destination unavailable"))
	ErrTryAgainLater          = registerError(114, errors.New("try again later"))
	ErrConnectionError        = registerError(121, errors.New("connection error"))
	ErrQueueOverflow          = registerError(122, errors.New("queue overflowed"))
	ErrCanceled               = registerError(125, context.Canceled)
)
